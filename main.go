package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"

	"hookdock/internal/api"
	"hookdock/internal/notify"
	"hookdock/internal/scheduler"
	"hookdock/internal/store"
)

//go:embed all:web/dist
var webFS embed.FS

func main() {
	// -dump-openapi：把 OpenAPI 文档打到 stdout 后退出（构建脚本生成前端内嵌文档用）
	if len(os.Args) > 1 && os.Args[1] == "-dump-openapi" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(openapiDoc); err != nil {
			log.Fatalf("dump openapi: %v", err)
		}
		return
	}

	dataDir := env("HOOKDOCK_DATA", "./data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Fatalf("创建数据目录失败: %v", err)
	}
	st, err := store.Open(path.Join(dataDir, "hookdock.db"))
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer st.Close()

	hub := scheduler.NewHub()
	notifier := notify.NewNotifier(st)
	sched := scheduler.New(st, hub, envInt("HOOKDOCK_WORKERS", 4), notifier)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sched.Start(ctx)

	seed(st)

	srv := api.New(st, hub, notifier)
	mux := http.NewServeMux()
	srv.Routes(mux)
	mux.HandleFunc("/openapi.json", openapiHandler)
	mux.Handle("/", spaHandler())

	addr := env("HOOKDOCK_ADDR", ":8080")
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           logMiddleware(srv.WithCORS(mux)),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second, // 慢连接防线（SSE 需要 WriteTimeout 保持 0）
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Printf("hookdock 监听 %s，数据目录 %s", addr, dataDir)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP 服务退出: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("收到退出信号，正在关闭…")
	cancel()
	shutCtx, c := context.WithTimeout(context.Background(), 10*time.Second)
	defer c()
	_ = httpSrv.Shutdown(shutCtx)
	sched.Stop(10 * time.Second)
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		var n int
		for _, ch := range v {
			if ch < '0' || ch > '9' {
				return def
			}
			n = n*10 + int(ch-'0')
		}
		if n > 0 {
			return n
		}
	}
	return def
}

// seed 首次启动建一个示例端点，方便立刻验证
func seed(st *store.Store) {
	eps, err := st.ListEndpoints()
	if err != nil || len(eps) > 0 {
		return
	}
	e := &store.Endpoint{
		Slug:        "demo",
		Title:       "示例端点",
		Description: "收到任何请求都会留档；带 action=deploy 的请求会自动生成一个每 5 分钟执行的任务。",
		Auth:        "none",
		MaxBody:     1048576,
		RetainDays:  30,
		RateLimit:   0,
		Enabled:     true,
	}
	if err := st.UpsertEndpoint(e); err != nil {
		log.Printf("seed endpoint: %v", err)
		return
	}
	rule := &store.Rule{
		EndpointID: e.ID,
		Name:       "action=deploy 触发部署检查",
		Source:     "body",
		Path:       "action",
		Op:         "eq",
		Value:      "deploy",
		JobName:    "部署检查",
		Schedule:   "@every 5m",
		// 不要把 {{ payload.* }} 拼进 sh -c：Expand 是裸字符串替换，替换值里的
		// `;id;` 会被 shell 当命令执行。payload 通过环境变量 HOOKDOCK_PAYLOAD 传。
		ExecArgv:    `["/bin/sh","-c","echo deploy check; date"]`,
		TimeoutSec:  30,
		MaxAttempts: 3,
		Mode:        "recurring",
		Enabled:     true,
	}
	if err := st.CreateRule(rule); err != nil {
		log.Printf("seed rule: %v", err)
	}
}

// ---------- 静态资源（SPA） ----------

func spaHandler() http.Handler {
	sub, err := fs.Sub(webFS, "web/dist")
	if err != nil {
		log.Fatalf("嵌入静态资源失败: %v", err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if f, err := sub.Open(p); err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		// API / hook 路径不参与 SPA 回退：未注册的接口应明确 404，而不是返回 index.html
		if strings.HasPrefix(r.URL.Path, "/api") || strings.HasPrefix(r.URL.Path, "/hook") {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"接口不存在"}`))
			return
		}
		// SPA 回退
		b, err := fs.ReadFile(sub, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(b)
	})
}

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/stream") {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		next.ServeHTTP(w, r)
		if strings.HasPrefix(r.URL.Path, "/api") || strings.HasPrefix(r.URL.Path, "/hook") {
			log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
		}
	})
}
