package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"hookdock/internal/scheduler"
	"hookdock/internal/store"
)

type Server struct {
	st         *store.Store
	hub        *scheduler.Hub
	adminToken string
	limiter    *rateLimiter
	cors       corsConfig
	rlMu       sync.Mutex
}

func New(st *store.Store, hub *scheduler.Hub) *Server {
	return &Server{
		st:         st,
		hub:        hub,
		adminToken: os.Getenv("HOOKDOCK_ADMIN_TOKEN"),
		limiter:    newRateLimiter(),
		cors:       loadCORS(),
	}
}

// ---------- 工具 ----------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(v)
}

func (s *Server) adminOK(r *http.Request) bool {
	if s.adminToken == "" {
		return true
	}
	got := r.Header.Get("X-Admin-Token")
	if got == "" {
		got = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.adminToken)) == 1
}

func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.adminOK(r) {
			writeErr(w, http.StatusUnauthorized, "需要管理令牌")
			return
		}
		next(w, r)
	}
}

// ---------- 限流 ----------

type bucket struct {
	tokens float64
	last   time.Time
}

type rateLimiter struct {
	mu sync.Mutex
	m  map[string]*bucket
}

func newRateLimiter() *rateLimiter { return &rateLimiter{m: map[string]*bucket{}} }

// Allow 每端点每分钟 limit 次
func (rl *rateLimiter) Allow(key string, limit int) bool {
	if limit <= 0 {
		return true
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	b, ok := rl.m[key]
	t := time.Now()
	if !ok {
		rl.m[key] = &bucket{tokens: float64(limit) - 1, last: t}
		return true
	}
	elapsed := t.Sub(b.last).Seconds()
	b.tokens += elapsed * float64(limit) / 60.0
	if b.tokens > float64(limit) {
		b.tokens = float64(limit)
	}
	b.last = t
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// ---------- CORS ----------

// corsConfig 决定各路径族允许哪些来源。
//   - 留空 = 严格同源（不返回 Access-Control-Allow-Origin，浏览器会拦跨域）
//   - "*"  = 允许任意来源
//   - 否则 = 逗号分隔的白名单，命中则原样回显该 Origin
type corsConfig struct {
	hookOrigins []string // /hook/*：webhook 常被第三方页面/服务调用
	apiOrigins  []string // /api/*：管理接口，默认同源
}

func loadCORS() corsConfig {
	return corsConfig{
		hookOrigins: splitOrigins(os.Getenv("HOOKDOCK_CORS_HOOK_ORIGINS")),
		apiOrigins:  splitOrigins(os.Getenv("HOOKDOCK_CORS_API_ORIGINS")),
	}
}

func splitOrigins(v string) []string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// allowOrigin 返回该请求应当拿到的 ACAO 值；空串表示不允跨域（不发该头）。
func allowOrigin(origin string, allowed []string) string {
	if origin == "" || len(allowed) == 0 {
		return ""
	}
	for _, a := range allowed {
		if a == "*" {
			return "*"
		}
		if strings.EqualFold(a, origin) {
			return origin
		}
	}
	return ""
}

// WithCORS 包一层跨域处理：预检（OPTIONS）直接 204 返回，不再落到业务 handler，
// 否则浏览器每次跨域 POST 前都会拿预检当真请求收进库里。
func (s *Server) WithCORS(next http.Handler) http.Handler {
	if s.cors.hookOrigins == nil && s.cors.apiOrigins == nil {
		// 未配置：完全同源，但仍然要把 OPTIONS 挡掉，避免污染数据
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		var allowed string
		switch {
		case strings.HasPrefix(r.URL.Path, "/hook/"):
			allowed = allowOrigin(origin, s.cors.hookOrigins)
		case strings.HasPrefix(r.URL.Path, "/api/"):
			allowed = allowOrigin(origin, s.cors.apiOrigins)
		case r.URL.Path == "/openapi.json":
			allowed = "*" // 公开文档，供外部工具读取
		}
		if allowed != "" {
			w.Header().Set("Access-Control-Allow-Origin", allowed)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Admin-Token, X-Hook-Token, X-Hub-Signature-256, Authorization")
			w.Header().Set("Access-Control-Max-Age", "600")
			if allowed != "*" {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
		}
		if r.Method == http.MethodOptions {
			// 预检到此为止：不管来源是否被允许，都不进业务 handler
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---------- 入参校验 ----------

// 字段上限：防止超长字段塞爆数据库与 UI，也防止极端值拖垮调度器
const (
	maxNameLen      = 200   // 任务名 / 规则名
	maxPayloadLen   = 65536 // payload JSON
	maxExecArgvLen  = 8192  // 命令 JSON 数组
	maxRuleValueLen = 200   // 规则比较值（含 regex 源码）
	minTimeoutSec   = 1
	maxTimeoutSec   = 3600
	minAttempts     = 1
	maxAttemptsCap  = 10
	maxBodyHard     = 8 << 20 // 单端点 body 上限的硬顶：8MB
)

// validateJobFields 校验并归一化任务字段，返回可直接写库的 (name, timeout, attempts)。
func validateJobFields(name, execArgv, payload string, timeout, attempts int64) (string, int64, int64, error) {
	if nr := len([]rune(name)); nr > maxNameLen {
		return "", 0, 0, fmt.Errorf("name 过长：%d 字符，上限 %d", nr, maxNameLen)
	}
	if len(execArgv) > maxExecArgvLen {
		return "", 0, 0, fmt.Errorf("exec_argv 过长：%d 字节，上限 %d", len(execArgv), maxExecArgvLen)
	}
	if len(payload) > maxPayloadLen {
		return "", 0, 0, fmt.Errorf("payload 过长：%d 字节，上限 %d", len(payload), maxPayloadLen)
	}
	// 未指定（0）时给默认值；显式给出则必须在区间内，负数/超大值一律拒绝
	if timeout == 0 {
		timeout = 60
	}
	if timeout < minTimeoutSec || timeout > maxTimeoutSec {
		return "", 0, 0, fmt.Errorf("timeout_sec 需在 %d~%d 之间，收到 %d", minTimeoutSec, maxTimeoutSec, timeout)
	}
	if attempts == 0 {
		attempts = 3
	}
	if attempts < minAttempts || attempts > maxAttemptsCap {
		return "", 0, 0, fmt.Errorf("max_attempts 需在 %d~%d 之间，收到 %d", minAttempts, maxAttemptsCap, attempts)
	}
	return name, timeout, attempts, nil
}

// ---------- webhook 接收 ----------

// slug 只允许小写字母、数字、连字符、下划线，长度 1~64
var validSlug = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

func clientIP(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		return strings.TrimSpace(strings.Split(v, ",")[0])
	}
	if v := r.Header.Get("X-Real-IP"); v != "" {
		return v
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// HandleHook 处理 /hook/{slug}
func (s *Server) HandleHook(w http.ResponseWriter, r *http.Request) {
	// 先规范化路径再取 slug：未规范化的客户端 / 反代可能用 .. 绕过前缀判断
	cleaned := path.Clean(r.URL.Path)
	if cleaned != r.URL.Path {
		// 路径含 . / .. 等，规范化后必须仍在 /hook/ 下，否则拒绝
		if !strings.HasPrefix(cleaned, "/hook/") {
			writeErr(w, 404, "端点不存在或已停用")
			return
		}
	}
	slug := strings.TrimPrefix(cleaned, "/hook/")
	slug = strings.TrimSuffix(slug, "/")
	if slug == "" || strings.Contains(slug, "/") {
		writeErr(w, 404, "端点不存在或已停用")
		return
	}
	if !validSlug.MatchString(slug) {
		writeErr(w, 404, "端点不存在或已停用")
		return
	}
	ep, err := s.st.GetEndpointBySlug(slug)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if ep == nil || !ep.Enabled {
		writeErr(w, 404, "端点不存在或已停用")
		return
	}

	// 限流
	if !s.limiter.Allow("rl:"+ep.Slug, int(ep.RateLimit)) {
		w.Header().Set("Retry-After", "1")
		writeErr(w, http.StatusTooManyRequests, "超过该端点速率限制")
		return
	}

	maxBody := ep.MaxBody
	if maxBody <= 0 {
		maxBody = 1048576
	}
	if maxBody > maxBodyHard {
		maxBody = maxBodyHard
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil {
		writeErr(w, 400, "读取 body 失败")
		return
	}
	truncated := int64(len(raw)) > maxBody
	if truncated {
		raw = raw[:maxBody]
	}
	_ = r.Body.Close()

	// 鉴权
	authOK, authKind, authMsg := true, "none", ""
	switch ep.Auth {
	case "token":
		authKind = "token"
		got := r.Header.Get("X-Hook-Token")
		if got == "" {
			got = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		}
		authOK = subtle.ConstantTimeCompare([]byte(got), []byte(ep.Secret)) == 1
	case "hmac":
		authKind = "hmac"
		sig := r.Header.Get("X-Hub-Signature-256")
		if sig == "" {
			sig = r.Header.Get("X-Signature-256")
		}
		sig = strings.TrimPrefix(sig, "sha256=")
		mac := hmac.New(sha256.New, []byte(ep.Secret))
		mac.Write(raw)
		want := hex.EncodeToString(mac.Sum(nil))
		authOK = subtle.ConstantTimeCompare([]byte(strings.ToLower(sig)), []byte(want)) == 1
	}
	if !authOK {
		authMsg = "签名/令牌校验失败"
	}

	// 鉴权失败：不落库，只记一条审计日志。否则攻击者可以用无效签名刷满 requests 表。
	if !authOK {
		log.Printf("webhook 鉴权失败 endpoint=%s auth=%s ip=%s ua=%q body_bytes=%d",
			ep.Slug, authKind, clientIP(r), r.UserAgent(), len(raw))
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"ok": false, "error": authMsg, "request_id": store.NewID("rq"),
		})
		return
	}

	// payload 解析
	payload := parsePayload(raw, r.Header.Get("Content-Type"))

	hdrJSON, _ := json.Marshal(redactHeaders(r.Header))
	req := &store.Request{
		EndpointID:  ep.ID,
		Method:      r.Method,
		Path:        r.URL.Path,
		Query:       r.URL.RawQuery,
		Headers:     string(hdrJSON),
		Body:        string(raw),
		BodyBytes:   int64(len(raw)),
		Truncated:   truncated,
		ContentType: r.Header.Get("Content-Type"),
		RemoteIP:    clientIP(r),
		UserAgent:   r.UserAgent(),
		AuthOK:      authOK,
		AuthKind:    authKind,
		Payload:     payload,
	}
	if err := s.st.InsertRequest(req); err != nil {
		log.Printf("insert request: %v", err)
		writeErr(w, 500, "落库失败")
		return
	}

	// 规则匹配 → 建任务
	matched := []string{}
	rules, _ := s.st.ListRules(ep.ID)
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if !matchRule(&rule, req) {
			continue
		}
		schedule := rule.Schedule
		if schedule == "" {
			schedule = "@every 5m"
		}
		sch, perr := scheduler.Parse(schedule)
		if perr != nil {
			log.Printf("rule %d 非法 schedule: %v", rule.ID, perr)
			continue
		}
		name := rule.JobName
		if name == "" {
			name = rule.Name
		}
		if rule.Mode == "per_event" {
			// per_event：每次命中都建一条独立任务，立即执行一次（schedule 仅作重试节拍兜底）
			job := &store.Job{
				Name:        name + " · " + time.Now().Format("01-02 15:04:05"),
				EndpointID:  &ep.ID,
				RuleID:      &rule.ID,
				Schedule:    schedule,
				ExecArgv:    rule.ExecArgv,
				Payload:     req.Payload,
				TimeoutSec:  rule.TimeoutSec,
				MaxAttempts: rule.MaxAttempts,
				Enabled:     true,
				OneShot:     true, // 执行完一次自动停用
				NextRunAt:   time.Now().Unix(),
			}
			if err := s.st.CreateJob(job); err != nil {
				log.Printf("create per-event job from rule %d: %v", rule.ID, err)
				continue
			}
			matched = append(matched, job.ID)
			s.hub.Publish("job.created", map[string]any{
				"job_id": job.ID, "job_name": job.Name, "rule": rule.Name,
				"request_id": req.ID, "mode": "per_event",
			})
			continue
		}
		// recurring（默认）：同一规则只维护一条常驻任务，命中只刷新它的 payload 与下次执行时间，
		// 避免每来一个请求就多一条重复任务。
		existing := s.st.JobByRule(rule.ID)
		if existing != nil {
			existing.Payload = req.Payload
			existing.Schedule = schedule
			existing.ExecArgv = rule.ExecArgv
			existing.TimeoutSec = rule.TimeoutSec
			existing.MaxAttempts = rule.MaxAttempts
			existing.NextRunAt = sch.Next(time.Now()).Unix()
			if err := s.st.UpdateJob(existing); err != nil {
				log.Printf("refresh job %s from rule %d: %v", existing.ID, rule.ID, err)
				continue
			}
			matched = append(matched, existing.ID)
			s.hub.Publish("job.refreshed", map[string]any{
				"job_id": existing.ID, "job_name": existing.Name, "rule": rule.Name, "request_id": req.ID,
			})
			continue
		}
		job := &store.Job{
			Name:        name,
			EndpointID:  &ep.ID,
			RuleID:      &rule.ID,
			Schedule:    schedule,
			ExecArgv:    rule.ExecArgv,
			Payload:     req.Payload,
			TimeoutSec:  rule.TimeoutSec,
			MaxAttempts: rule.MaxAttempts,
			Enabled:     true,
			NextRunAt:   sch.Next(time.Now()).Unix(),
		}
		if err := s.st.CreateJob(job); err != nil {
			log.Printf("create job from rule %d: %v", rule.ID, err)
			continue
		}
		matched = append(matched, job.ID)
		s.hub.Publish("job.created", map[string]any{
			"job_id": job.ID, "job_name": job.Name, "rule": rule.Name, "request_id": req.ID,
			"mode": "recurring",
		})
	}

	s.hub.Publish("request.received", map[string]any{
		"id": req.ID, "endpoint": ep.Slug, "method": req.Method,
		"body_bytes": req.BodyBytes, "remote_ip": req.RemoteIP,
		"received_at": req.ReceivedAt, "matched_jobs": matched,
		"preview": preview(req.Body, 200),
	})

	writeJSON(w, 200, map[string]any{
		"ok":           true,
		"request_id":   req.ID,
		"endpoint":     ep.Slug,
		"matched_jobs": matched,
		"received_at":  req.ReceivedAt,
	})
}

func redactHeaders(h http.Header) map[string]string {
	out := map[string]string{}
	for k, v := range h {
		lk := strings.ToLower(k)
		if lk == "authorization" || lk == "cookie" || strings.Contains(lk, "token") || strings.Contains(lk, "secret") {
			out[k] = "***redacted***"
			continue
		}
		out[k] = strings.Join(v, ", ")
	}
	return out
}

func preview(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func parsePayload(raw []byte, ct string) string {
	if len(raw) == 0 {
		return "{}"
	}
	var v any
	if json.Unmarshal(raw, &v) == nil {
		b, _ := json.Marshal(v)
		return string(b)
	}
	if strings.Contains(ct, "application/x-www-form-urlencoded") {
		if vals, err := url.ParseQuery(string(raw)); err == nil {
			m := map[string]any{}
			for k, vv := range vals {
				if len(vv) == 1 {
					m[k] = vv[0]
				} else {
					m[k] = vv
				}
			}
			b, _ := json.Marshal(m)
			return string(b)
		}
	}
	b, _ := json.Marshal(map[string]string{"text": string(raw)})
	return string(b)
}

// ---------- 规则匹配 ----------

// regex 编译缓存：规则 id -> 已编译正则。规则变更时由 invalidateRegex 失效。
var (
	regexCacheMu sync.RWMutex
	regexCache   = map[int64]*regexp.Regexp{}
)

func cachedRegex(rule *store.Rule) *regexp.Regexp {
	regexCacheMu.RLock()
	re, ok := regexCache[rule.ID]
	regexCacheMu.RUnlock()
	if ok {
		return re
	}
	compiled, err := regexp.Compile(rule.Value)
	if err != nil {
		return nil // 保存时已校验过；运行中编译失败视为不匹配
	}
	regexCacheMu.Lock()
	regexCache[rule.ID] = compiled
	regexCacheMu.Unlock()
	return compiled
}

func invalidateRegex(ruleID int64) {
	regexCacheMu.Lock()
	delete(regexCache, ruleID)
	regexCacheMu.Unlock()
}

func matchRule(rule *store.Rule, req *store.Request) bool {
	var actual string
	var exists bool
	switch rule.Source {
	case "body":
		actual, exists = jsonPath(req.Payload, rule.Path)
	case "query":
		vals, _ := url.ParseQuery(req.Query)
		v, ok := vals[rule.Path]
		if ok && len(v) > 0 {
			actual, exists = v[0], true
		}
	case "header":
		var h map[string]string
		_ = json.Unmarshal([]byte(req.Headers), &h)
		for k, v := range h {
			if strings.EqualFold(k, rule.Path) {
				actual, exists = v, true
				break
			}
		}
	}
	switch rule.Op {
	case "exists":
		return exists
	case "eq":
		return exists && actual == rule.Value
	case "contains":
		return exists && strings.Contains(actual, rule.Value)
	case "prefix":
		return exists && strings.HasPrefix(actual, rule.Value)
	case "regex":
		re := cachedRegex(rule)
		return re != nil && exists && re.MatchString(actual)
	}
	return false
}

// jsonPath 极简实现：支持 a.b.c 与 a.0.b
func jsonPath(payloadJSON, path string) (string, bool) {
	if path == "" {
		return "", false
	}
	var root any
	if json.Unmarshal([]byte(payloadJSON), &root) != nil {
		return "", false
	}
	cur := root
	for _, seg := range strings.Split(path, ".") {
		switch node := cur.(type) {
		case map[string]any:
			v, ok := node[seg]
			if !ok {
				return "", false
			}
			cur = v
		case []any:
			idx, err := strconv.Atoi(seg)
			if err != nil || idx < 0 || idx >= len(node) {
				return "", false
			}
			cur = node[idx]
		default:
			return "", false
		}
	}
	switch v := cur.(type) {
	case string:
		return v, true
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10), true
		}
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(v), true
	case nil:
		return "", true
	default:
		b, _ := json.Marshal(v)
		return string(b), true
	}
}

// ---------- SSE ----------

func (s *Server) HandleSSE(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, "streaming 不支持")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	fmt.Fprintf(w, "event: hello\ndata: {\"ok\":true}\n\n")
	fl.Flush()

	ch := s.hub.Sub()
	defer s.hub.Unsub(ch)
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprintf(w, ": ping\n\n")
			fl.Flush()
		case msg, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg)
			fl.Flush()
		}
	}
}

// ---------- 管理 API ----------

func (s *Server) Routes(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"ok": true, "ts": time.Now().Unix()})
	})
	mux.HandleFunc("/hook/", s.HandleHook)
	mux.HandleFunc("/api/stream", s.HandleSSE)

	mux.HandleFunc("/api/overview", s.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		st, err := s.st.Stats()
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		eps, _ := s.st.ListEndpoints()
		reqs, _ := s.st.ListRequests(0, 12, 0)
		runs, _ := s.st.ListRuns("", "", 8)
		writeJSON(w, 200, map[string]any{
			"stats": st, "endpoints": eps, "recent_requests": reqs, "recent_runs": runs,
			"admin_required": s.adminToken != "",
		})
	}))

	mux.HandleFunc("/api/endpoints", s.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			eps, err := s.st.ListEndpoints()
			if err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			writeJSON(w, 200, eps)
		case http.MethodPost:
			var e store.Endpoint
			if err := readJSON(r, &e); err != nil {
				writeErr(w, 400, "JSON 解析失败")
				return
			}
			if e.Slug == "" {
				writeErr(w, 400, "slug 必填")
				return
			}
			if !validSlug.MatchString(e.Slug) {
				writeErr(w, 400, "slug 只允许小写字母、数字、连字符、下划线，且以字母或数字开头，长度 1~64")
				return
			}
			if e.MaxBody <= 0 {
				e.MaxBody = 1048576
			}
			if e.MaxBody > maxBodyHard {
				writeErr(w, 400, fmt.Sprintf("max_body 过大：%d，上限 %d", e.MaxBody, maxBodyHard))
				return
			}
			if e.RetainDays < 0 || e.RetainDays > 3650 {
				writeErr(w, 400, "retain_days 需在 0~3650 之间")
				return
			}
			if e.RateLimit < 0 || e.RateLimit > 6000 {
				writeErr(w, 400, "rate_limit 需在 0~6000 之间（0=不限）")
				return
			}
			if e.Secret != "" && len(e.Secret) > 512 {
				writeErr(w, 400, "secret 过长：上限 512 字节")
				return
			}
			if err := s.st.UpsertEndpoint(&e); err != nil {
				writeErr(w, 400, err.Error())
				return
			}
			writeJSON(w, 200, e)
		default:
			writeErr(w, 405, "方法不允许")
		}
	}))

	mux.HandleFunc("/api/endpoints/", s.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/api/endpoints/")
		parts := strings.Split(strings.Trim(rest, "/"), "/")
		if len(parts) == 0 || parts[0] == "" {
			writeErr(w, 400, "缺少端点标识")
			return
		}
		key := parts[0]
		var ep *store.Endpoint
		if id, err := strconv.ParseInt(key, 10, 64); err == nil {
			ep, _ = s.st.GetEndpoint(id)
		} else {
			ep, _ = s.st.GetEndpointBySlug(key)
		}
		if ep == nil {
			writeErr(w, 404, "端点不存在")
			return
		}
		if len(parts) == 1 {
			switch r.Method {
			case http.MethodGet:
				writeJSON(w, 200, ep)
			case http.MethodPut, http.MethodPatch:
				var in store.Endpoint
				if err := readJSON(r, &in); err != nil {
					writeErr(w, 400, "JSON 解析失败")
					return
				}
				in.ID = ep.ID
				if in.Slug == "" {
					in.Slug = ep.Slug
				}
				if in.Secret == "" {
					in.Secret = ep.Secret
				}
				if err := s.st.UpsertEndpoint(&in); err != nil {
					writeErr(w, 400, err.Error())
					return
				}
				writeJSON(w, 200, in)
			case http.MethodDelete:
				if err := s.st.DeleteEndpoint(ep.ID); err != nil {
					writeErr(w, 500, err.Error())
					return
				}
				writeJSON(w, 200, map[string]bool{"ok": true})
			default:
				writeErr(w, 405, "方法不允许")
			}
			return
		}
		switch parts[1] {
		case "requests":
			limit := 50
			if v := r.URL.Query().Get("limit"); v != "" {
				if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
					limit = n
				}
			}
			var before int64
			if v := r.URL.Query().Get("before"); v != "" {
				before, _ = strconv.ParseInt(v, 10, 64)
			}
			reqs, err := s.st.ListRequests(ep.ID, limit, before)
			if err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			writeJSON(w, 200, reqs)
		case "rules":
			if r.Method == http.MethodGet {
				rs, err := s.st.ListRules(ep.ID)
				if err != nil {
					writeErr(w, 500, err.Error())
					return
				}
				writeJSON(w, 200, rs)
				return
			}
			if r.Method == http.MethodPost {
				var rule store.Rule
				// 预置默认值：Go 零值无法区分「缺省」与「显式 false」
				rule.Enabled = true
				rule.Mode = "recurring"
				if err := readJSON(r, &rule); err != nil {
					writeErr(w, 400, "JSON 解析失败")
					return
				}
				rule.EndpointID = ep.ID
				if rule.Source == "" {
					rule.Source = "body"
				}
				if rule.Op == "" {
					rule.Op = "eq"
				}
				if rule.ExecArgv == "" {
					writeErr(w, 400, "exec_argv 必填（JSON 数组）")
					return
				}
				var argv []string
				if json.Unmarshal([]byte(rule.ExecArgv), &argv) != nil || len(argv) == 0 {
					writeErr(w, 400, "exec_argv 必须是 JSON 字符串数组")
					return
				}
				if _, err := scheduler.Parse(rule.Schedule); err != nil {
					writeErr(w, 400, "schedule 非法: "+err.Error())
					return
				}
				if n := len([]rune(rule.Name)); n > maxNameLen {
					writeErr(w, 400, fmt.Sprintf("规则名过长：%d 字符，上限 %d", n, maxNameLen))
					return
				}
				if n := len(rule.Value); n > maxRuleValueLen {
					writeErr(w, 400, fmt.Sprintf("比较值过长：%d 字节，上限 %d", n, maxRuleValueLen))
					return
				}
				if n := len(rule.ExecArgv); n > maxExecArgvLen {
					writeErr(w, 400, fmt.Sprintf("exec_argv 过长：%d 字节，上限 %d", n, maxExecArgvLen))
					return
				}
				if rule.TimeoutSec == 0 {
					rule.TimeoutSec = 60
				}
				if rule.TimeoutSec < minTimeoutSec || rule.TimeoutSec > maxTimeoutSec {
					writeErr(w, 400, fmt.Sprintf("timeout_sec 需在 %d~%d 之间，收到 %d", minTimeoutSec, maxTimeoutSec, rule.TimeoutSec))
					return
				}
				if rule.MaxAttempts == 0 {
					rule.MaxAttempts = 3
				}
				if rule.MaxAttempts < minAttempts || rule.MaxAttempts > maxAttemptsCap {
					writeErr(w, 400, fmt.Sprintf("max_attempts 需在 %d~%d 之间，收到 %d", minAttempts, maxAttemptsCap, rule.MaxAttempts))
					return
				}
				// regex 在保存时预编译：非法正则当场拒绝，也避免每次匹配重复编译
				if rule.Op == "regex" {
					if _, err := regexp.Compile(rule.Value); err != nil {
						writeErr(w, 400, "regex 非法: "+err.Error())
						return
					}
				}
				if err := s.st.CreateRule(&rule); err != nil {
					writeErr(w, 400, err.Error())
					return
				}
				writeJSON(w, 200, rule)
				return
			}
			writeErr(w, 405, "方法不允许")
		default:
			writeErr(w, 404, "未知子资源")
		}
	}))

	mux.HandleFunc("/api/rules/", s.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		idStr := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/rules/"), "/")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeErr(w, 400, "非法规则 id")
			return
		}
		if r.Method != http.MethodDelete {
			writeErr(w, 405, "方法不允许")
			return
		}
		if err := s.st.DeleteRule(id); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		invalidateRegex(id)
		writeJSON(w, 200, map[string]bool{"ok": true})
	}))

	mux.HandleFunc("/api/requests/", s.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/requests/"), "/")
		parts := strings.Split(rest, "/")
		req, err := s.st.GetRequest(parts[0])
		if err != nil || req == nil {
			writeErr(w, 404, "请求不存在")
			return
		}
		if len(parts) == 1 {
			writeJSON(w, 200, req)
			return
		}
		if parts[1] == "make-job" && r.Method == http.MethodPost {
			var in struct {
				Name        string `json:"name"`
				Schedule    string `json:"schedule"`
				ExecArgv    string `json:"exec_argv"`
				TimeoutSec  int64  `json:"timeout_sec"`
				MaxAttempts int64  `json:"max_attempts"`
			}
			if err := readJSON(r, &in); err != nil {
				writeErr(w, 400, "JSON 解析失败")
				return
			}
			if in.Schedule == "" {
				in.Schedule = "@every 5m"
			}
			sch, perr := scheduler.Parse(in.Schedule)
			if perr != nil {
				writeErr(w, 400, "schedule 非法: "+perr.Error())
				return
			}
			var argv []string
			if json.Unmarshal([]byte(in.ExecArgv), &argv) != nil || len(argv) == 0 {
				writeErr(w, 400, "exec_argv 必须是 JSON 字符串数组")
				return
			}
			name, timeout, attempts, verr := validateJobFields(in.Name, in.ExecArgv, req.Payload, in.TimeoutSec, in.MaxAttempts)
			if verr != nil {
				writeErr(w, 400, verr.Error())
				return
			}
			job := &store.Job{
				Name: name, EndpointID: &req.EndpointID, Schedule: in.Schedule,
				ExecArgv: in.ExecArgv, Payload: req.Payload, TimeoutSec: timeout,
				MaxAttempts: attempts, Enabled: true, NextRunAt: sch.Next(time.Now()).Unix(),
			}
			if job.Name == "" {
				job.Name = "来自 " + req.ID
			}
			if err := s.st.CreateJob(job); err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			s.hub.Publish("job.created", map[string]any{"job_id": job.ID, "job_name": job.Name})
			writeJSON(w, 200, job)
			return
		}
		writeErr(w, 404, "未知子资源")
	}))

	mux.HandleFunc("/api/jobs", s.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			js, err := s.st.ListJobs()
			if err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			writeJSON(w, 200, js)
		case http.MethodPost:
			var in struct {
				Name        string `json:"name"`
				Schedule    string `json:"schedule"`
				ExecArgv    string `json:"exec_argv"`
				Payload     string `json:"payload"`
				TimeoutSec  int64  `json:"timeout_sec"`
				MaxAttempts int64  `json:"max_attempts"`
				Enabled     *bool  `json:"enabled"`
			}
			if err := readJSON(r, &in); err != nil {
				writeErr(w, 400, "JSON 解析失败")
				return
			}
			sch, perr := scheduler.Parse(in.Schedule)
			if perr != nil {
				writeErr(w, 400, "schedule 非法: "+perr.Error())
				return
			}
			var argv []string
			if json.Unmarshal([]byte(in.ExecArgv), &argv) != nil || len(argv) == 0 {
				writeErr(w, 400, "exec_argv 必须是 JSON 字符串数组")
				return
			}
			enabled := true
			if in.Enabled != nil {
				enabled = *in.Enabled
			}
			if in.Payload == "" {
				in.Payload = "{}"
			}
			name, timeout, attempts, verr := validateJobFields(in.Name, in.ExecArgv, in.Payload, in.TimeoutSec, in.MaxAttempts)
			if verr != nil {
				writeErr(w, 400, verr.Error())
				return
			}
			job := &store.Job{
				Name: name, Schedule: in.Schedule, ExecArgv: in.ExecArgv, Payload: in.Payload,
				TimeoutSec: timeout, MaxAttempts: attempts, Enabled: enabled,
				NextRunAt: sch.Next(time.Now()).Unix(),
			}
			if job.Name == "" {
				job.Name = "任务 " + job.ID
			}
			if err := s.st.CreateJob(job); err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			writeJSON(w, 200, job)
		default:
			writeErr(w, 405, "方法不允许")
		}
	}))

	mux.HandleFunc("/api/jobs/", s.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/jobs/"), "/")
		parts := strings.Split(rest, "/")
		job, err := s.st.GetJob(parts[0])
		if err != nil || job == nil {
			writeErr(w, 404, "任务不存在")
			return
		}
		if len(parts) == 1 {
			switch r.Method {
			case http.MethodGet:
				writeJSON(w, 200, job)
			case http.MethodPut, http.MethodPatch:
				var in store.Job
				if err := readJSON(r, &in); err != nil {
					writeErr(w, 400, "JSON 解析失败")
					return
				}
				if in.Schedule != "" {
					if _, perr := scheduler.Parse(in.Schedule); perr != nil {
						writeErr(w, 400, "schedule 非法: "+perr.Error())
						return
					}
				}
				// 先算出「合并后的候选值」，再统一走校验，避免 PUT 绕过 /api/jobs 的入参限制
				name := job.Name
				if in.Name != "" {
					name = in.Name
				}
				execArgv := job.ExecArgv
				if in.ExecArgv != "" {
					var argv []string
					if json.Unmarshal([]byte(in.ExecArgv), &argv) != nil || len(argv) == 0 {
						writeErr(w, 400, "exec_argv 必须是 JSON 字符串数组")
						return
					}
					execArgv = in.ExecArgv
				}
				payload := job.Payload
				if in.Payload != "" {
					payload = in.Payload
				}
				timeout := job.TimeoutSec
				if in.TimeoutSec != 0 {
					timeout = in.TimeoutSec
				}
				attempts := job.MaxAttempts
				if in.MaxAttempts != 0 {
					attempts = in.MaxAttempts
				}
				// 用户改过的 Task「1h」这类空名字保持原样；只有真的填了才校验
				if in.Name != "" {
					vName, vTimeout, vAttempts, verr := validateJobFields(name, execArgv, payload, timeout, attempts)
					if verr != nil {
						writeErr(w, 400, verr.Error())
						return
					}
					name, timeout, attempts = vName, vTimeout, vAttempts
				} else {
					if n := len(payload); n > maxPayloadLen {
						writeErr(w, 400, fmt.Sprintf("payload 过大：%d 字节，上限 %d", n, maxPayloadLen))
						return
					}
					if n := len(execArgv); n > maxExecArgvLen {
						writeErr(w, 400, fmt.Sprintf("exec_argv 过长：%d 字节，上限 %d", n, maxExecArgvLen))
						return
					}
					if timeout < minTimeoutSec || timeout > maxTimeoutSec {
						writeErr(w, 400, fmt.Sprintf("timeout_sec 需在 %d~%d 之间，收到 %d", minTimeoutSec, maxTimeoutSec, timeout))
						return
					}
					if attempts < minAttempts || attempts > maxAttemptsCap {
						writeErr(w, 400, fmt.Sprintf("max_attempts 需在 %d~%d 之间，收到 %d", minAttempts, maxAttemptsCap, attempts))
						return
					}
				}
				if in.Schedule != "" {
					job.Schedule = in.Schedule
					if sch, perr := scheduler.Parse(in.Schedule); perr == nil {
						job.NextRunAt = sch.Next(time.Now()).Unix()
					}
				}
				job.Name, job.ExecArgv, job.Payload = name, execArgv, payload
				job.TimeoutSec, job.MaxAttempts = timeout, attempts
				// store.Job.Enabled 是 bool，无法区分「没传」与「传 false」，
				// 所以启停统一走 /api/jobs/{id}/toggle，这里不动 Enabled。
				if job.Enabled && job.NextRunAt <= time.Now().Unix() {
					if sch, perr := scheduler.Parse(job.Schedule); perr == nil {
						job.NextRunAt = sch.Next(time.Now()).Unix()
					}
				}
				if err := s.st.UpdateJob(job); err != nil {
					writeErr(w, 500, err.Error())
					return
				}
				writeJSON(w, 200, job)
			case http.MethodDelete:
				if err := s.st.DeleteJob(job.ID); err != nil {
					writeErr(w, 500, err.Error())
					return
				}
				writeJSON(w, 200, map[string]bool{"ok": true})
			default:
				writeErr(w, 405, "方法不允许")
			}
			return
		}
		switch parts[1] {
		case "toggle":
			enabled := !job.Enabled
			next := int64(0)
			if enabled {
				if sch, perr := scheduler.Parse(job.Schedule); perr == nil {
					next = sch.Next(time.Now()).Unix()
				}
			}
			if err := s.st.SetJobEnabled(job.ID, enabled, next); err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			writeJSON(w, 200, map[string]any{"ok": true, "enabled": enabled})
		case "run":
			runID := store.NewID("run")
			if err := s.st.EnqueueRun(runID, job.ID, job.MaxAttempts); err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			s.hub.Publish("run.queued", map[string]any{"run_id": runID, "job_id": job.ID, "job_name": job.Name})
			writeJSON(w, 200, map[string]any{"ok": true, "run_id": runID})
		case "runs":
			rs, err := s.st.ListRuns(job.ID, "", 50)
			if err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			writeJSON(w, 200, rs)
		default:
			writeErr(w, 404, "未知子资源")
		}
	}))

	mux.HandleFunc("/api/runs", s.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		limit := 50
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
				limit = n
			}
		}
		rs, err := s.st.ListRuns(r.URL.Query().Get("job"), r.URL.Query().Get("status"), limit)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, rs)
	}))

	mux.HandleFunc("/api/runs/", s.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/runs/"), "/")
		parts := strings.Split(rest, "/")
		run, err := s.st.GetRun(parts[0])
		if err != nil || run == nil {
			writeErr(w, 404, "运行记录不存在")
			return
		}
		if len(parts) > 1 && parts[1] == "retry" && r.Method == http.MethodPost {
			if err := s.st.RequeueRun(run.ID); err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			s.hub.Publish("run.queued", map[string]any{"run_id": run.ID, "job_id": run.JobID})
			writeJSON(w, 200, map[string]any{"ok": true, "run_id": run.ID})
			return
		}
		writeJSON(w, 200, run)
	}))
}
