package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"hookdock/internal/notify"
	"hookdock/internal/scheduler"
	"hookdock/internal/store"
)

// ctxPrincipal 请求上下文里存放 *principal 的 key
type ctxKey string

const ctxPrincipal ctxKey = "principal"

type Server struct {
	st         *store.Store
	hub        *scheduler.Hub
	notifier   *notify.Notifier
	adminToken string
	adminName  string
	limiter    *rateLimiter
	cors       corsConfig
	trustProxy bool // 仅在明确配置 HOOKDOCK_TRUST_PROXY=1（部署在反代后）时信任 X-Forwarded-For
	rlMu       sync.Mutex
}

func New(st *store.Store, hub *scheduler.Hub, notifier *notify.Notifier) *Server {
	tp := false
	switch strings.ToLower(strings.TrimSpace(os.Getenv("HOOKDOCK_TRUST_PROXY"))) {
	case "1", "true", "yes":
		tp = true
	}
	return &Server{
		st:         st,
		hub:        hub,
		notifier:   notifier,
		adminToken: os.Getenv("HOOKDOCK_ADMIN_TOKEN"),
		trustProxy: tp,
		adminName:  envAdminName(),
		limiter:    newRateLimiter(),
		cors:       loadCORS(),
	}
}

// envAdminName 环境管理员在界面上的显示名，缺省 env-admin。
func envAdminName() string {
	if n := strings.TrimSpace(os.Getenv("HOOKDOCK_ADMIN_NAME")); n != "" {
		return n
	}
	return "env-admin"
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

// ---------- 权限模型 ----------

// 细粒度权限。一个令牌拿到哪些权限决定它能做什么，
// 这样才能出现「只能发 webhook」和「能在请求里带任务创建」这两种不同令牌。
const (
	PermHookSend      = "hook.send"      // 往端点 POST webhook
	PermHookTrigger   = "hook.trigger"   // 在 webhook 请求里附带任务指令，即时建任务
	PermRequestRead   = "request.read"   // 查看请求记录
	PermRequestWrite  = "request.write"  // 请求转任务、删除请求
	PermJobRead       = "job.read"       // 查看任务与运行
	PermJobWrite      = "job.write"      // 新建/编辑/启停/删除任务、重投运行
	PermEndpointWrite = "endpoint.write" // 新建/改名/删除端点与规则
	PermTokenManage   = "token.manage"   // 管理令牌本身
	PermChannelManage = "channel.manage" // 管理推送渠道（含读取含密钥的配置）
)

// allPerms 顺序即 UI 展示顺序
var allPerms = []string{
	PermHookSend, PermHookTrigger, PermRequestRead, PermRequestWrite,
	PermJobRead, PermJobWrite, PermEndpointWrite, PermTokenManage, PermChannelManage,
}

// rolePerms 角色预设。令牌没有显式 perms 时按角色展开，方便「选个角色就好」。
var rolePerms = map[string][]string{
	// 只发不收：只能触发 webhook，看不到任何界面数据
	"sender": {PermHookSend},
	// 发 + 带任务指令：请求里可以带 __job 直接建任务，仍看不到管理界面
	"trigger": {PermHookSend, PermHookTrigger},
	// 值班操作员：能看能改任务，不能动端点配置与令牌
	"operator": {PermHookSend, PermHookTrigger, PermRequestRead, PermRequestWrite, PermJobRead, PermJobWrite},
	// 全权
	"admin": allPerms,
}

var validRoles = []string{"sender", "trigger", "operator", "admin"}

// permLabels 权限的中文说明，供 UI 直接展示。
var permLabels = map[string]string{
	PermHookSend:      "发送 webhook（POST /hook/<端点>）",
	PermHookTrigger:   "在 webhook 请求里直接创建任务",
	PermRequestRead:   "查看收到的请求",
	PermRequestWrite:  "把请求转成任务",
	PermJobRead:       "查看任务与运行记录",
	PermJobWrite:      "新建/编辑/启停/删除任务，重投运行",
	PermEndpointWrite: "管理端点与规则",
	PermTokenManage:   "管理令牌",
	PermChannelManage: "管理推送渠道（含测试发送）",
}

func validPerm(p string) bool {
	_, ok := permLabels[p]
	return ok
}

// rolesSummary 角色预设的展示数据，供前端渲染下拉与权限说明。
func rolesSummary() []map[string]any {
	out := make([]map[string]any, 0, len(validRoles))
	for _, r := range validRoles {
		out = append(out, map[string]any{"role": r, "perms": rolePerms[r]})
	}
	return out
}

func validRole(r string) bool {
	for _, v := range validRoles {
		if v == r {
			return true
		}
	}
	return false
}

// principal 一次请求的身份。EnvToken 走 HOOKDOCK_ADMIN_TOKEN 时为 env-admin。
type principal struct {
	Kind     string   `json:"kind"` // token | env | open（服务端未配令牌时的同源开发态）
	TokenID  int64    `json:"token_id,omitempty"`
	Name     string   `json:"name"`
	Role     string   `json:"role"`
	Perms    []string `json:"perms"`
	Scopes   []string `json:"scopes"`
	ReadOnly bool     `json:"read_only,omitempty"` // 由 perms 推导，仅前端展示便利
}

func (p *principal) can(perm string) bool {
	for _, v := range p.Perms {
		if v == perm {
			return true
		}
	}
	return false
}

// scopeOK 判断该身份能否访问某端点；Scopes 为空表示不限制。
func (p *principal) scopeOK(slug string) bool {
	if len(p.Scopes) == 0 {
		return true
	}
	for _, v := range p.Scopes {
		if v == slug {
			return true
		}
	}
	return false
}

// expandPerms 令牌显式 perms 优先；否则按角色预设展开。
func expandPerms(t *store.Token) []string {
	if len(t.Perms) > 0 {
		out := make([]string, 0, len(t.Perms))
		for _, p := range t.Perms {
			if p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	if ps, ok := rolePerms[t.Role]; ok {
		return append([]string(nil), ps...)
	}
	return []string{PermHookSend}
}

func hashToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// newTokenPlain 生成明文令牌与用于列表辨认的前缀。明文只在创建时返回一次。
func newTokenPlain() (plain, prefix string) {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	plain = "hk_" + hex.EncodeToString(b)
	prefix = plain[:11]
	return plain, prefix
}

// requestToken 取出请求携带的 API 令牌明文。
// 注意：/hook/ 路径不接受 Authorization 头 —— 那里可能装的是端点自己的 X-Hook-Token，
// 混用会导致「端点密钥」被当成 API 令牌去查，从而把正常请求判成 401。
func requestToken(r *http.Request) string {
	if v := r.Header.Get("X-Api-Token"); v != "" {
		return v
	}
	if v := r.Header.Get("X-Admin-Token"); v != "" {
		return v
	}
	if strings.HasPrefix(r.URL.Path, "/hook/") {
		return ""
	}
	// EventSource（SSE）无法自定义请求头，只能靠查询参数传令牌。
	// 仅限 /api/stream：其它路径一律只认请求头，避免令牌落进 URL 与访问日志。
	if r.URL.Path == "/api/stream" {
		if v := r.URL.Query().Get("token"); v != "" {
			return v
		}
	}
	if v := r.Header.Get("Authorization"); strings.HasPrefix(v, "Bearer ") {
		return strings.TrimPrefix(v, "Bearer ")
	}
	return ""
}

// identify 解析请求身份。
// 优先级：库内令牌 → 环境变量 ADMIN_TOKEN（全权，兼容旧部署）→ 未配置任何令牌时的开放态。
func (s *Server) identify(r *http.Request) (*principal, string) {
	got := requestToken(r)
	if got != "" {
		if hash := hashToken(got); hash != "" {
			if t, err := s.st.TokenByHash(hash); err == nil && t != nil {
				s.st.TouchToken(t.ID)
				return &principal{
					Kind: "token", TokenID: t.ID, Name: t.Name, Role: t.Role,
					Perms: expandPerms(t), Scopes: t.Scopes,
				}, ""
			}
		}
	}
	if s.adminToken != "" {
		if got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(s.adminToken)) == 1 {
			return &principal{Kind: "env", Name: s.adminName, Role: "admin", Perms: allPerms}, ""
		}
		if got == "" {
			return nil, "需要令牌"
		}
		// 带了令牌但既不匹配库内令牌也不匹配环境令牌
		return nil, "令牌无效或已停用"
	}
	// 服务端既没有库内令牌也没配 ADMIN_TOKEN：开放态（仅限本机/内网使用）
	if n, _ := s.st.CountTokens(); n == 0 {
		return &principal{Kind: "open", Name: s.adminName, Role: "admin", Perms: allPerms}, ""
	}
	if got == "" {
		return nil, "需要令牌"
	}
	return nil, "令牌无效或已停用"
}

// requirePerm 要求某项权限。401 = 没认出身份，403 = 认出了但没这个权限。
func (s *Server) requirePerm(perm string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			p, msg := s.identify(r)
			if p == nil {
				writeErr(w, http.StatusUnauthorized, msg)
				return
			}
			if !p.can(perm) {
				writeErr(w, http.StatusForbidden, "该令牌没有 "+perm+" 权限")
				return
			}
			next(w, r.WithContext(context.WithValue(r.Context(), ctxPrincipal, p)))
		}
	}
}

// principalOf 取出当前请求身份（requirePerm 已注入）。
func principalOf(r *http.Request) *principal {
	if p, ok := r.Context().Value(ctxPrincipal).(*principal); ok {
		return p
	}
	return nil
}

// requireAdmin 兼容旧调用点：等价于要求任意一项写入权限中的最高档，这里统一用 token.manage 语义过宽，
// 因此直接要求「已认证」即可，具体权限在 handler 内按需判断。
func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, msg := s.identify(r)
		if p == nil {
			writeErr(w, http.StatusUnauthorized, msg)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxPrincipal, p)))
	}
}

// requireAny 要求身份具备给定权限中的任意一项（用于同一路径 GET/POST 权限不同的场景，
// 细粒度判断交给 handler 内部的 needPerm）。
func (s *Server) requireAny(perms ...string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			p, msg := s.identify(r)
			if p == nil {
				writeErr(w, http.StatusUnauthorized, msg)
				return
			}
			ok := false
			for _, perm := range perms {
				if p.can(perm) {
					ok = true
					break
				}
			}
			if !ok {
				writeErr(w, http.StatusForbidden, "该令牌没有访问此接口的权限（需要 "+strings.Join(perms, " 或 ")+"）")
				return
			}
			next(w, r.WithContext(context.WithValue(r.Context(), ctxPrincipal, p)))
		}
	}
}

// needPerm 在 handler 内部按请求方法细分权限。不满足时已写好 403，调用方直接 return。
func needPerm(w http.ResponseWriter, r *http.Request, perm string) bool {
	p := principalOf(r)
	if p != nil && p.can(perm) {
		return true
	}
	writeErr(w, http.StatusForbidden, "该令牌没有 "+perm+" 权限")
	return false
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
	// 防泄漏：键数量超阈值时清理 10 分钟未活跃的桶（端点被删后残留）
	if len(rl.m) > 4096 {
		for k2, b2 := range rl.m {
			if t.Sub(b2.last) > 10*time.Minute {
				delete(rl.m, k2)
			}
		}
	}
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
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Api-Token, X-Admin-Token, X-Hook-Token, X-Hook-Job, X-Hub-Signature-256, Authorization")
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
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// clientIPT 遵循可选的代理信任策略：默认不信 XFF（可伪造），只在 HOOKDOCK_TRUST_PROXY=1 时采用
func (s *Server) clientIPT(r *http.Request) string {
	if s.trustProxy {
		if v := r.Header.Get("X-Forwarded-For"); v != "" {
			return strings.TrimSpace(strings.Split(v, ",")[0])
		}
		if v := r.Header.Get("X-Real-IP"); v != "" {
			return strings.TrimSpace(v)
		}
	}
	return clientIP(r)
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

	// 令牌层：服务端一旦建过启用中的令牌，webhook 也必须带令牌，
	// 且该令牌必须有 hook.send 权限、且作用域覆盖此端点。
	// 端点自身的 auth（token/hmac）仍然保留，作为来源校验的第二道闸。
	var caller *principal
	if s.hasTokens() {
		p, msg := s.identify(r)
		if p == nil {
			log.Printf("webhook 缺令牌 endpoint=%s ip=%s ua=%q", ep.Slug, s.clientIPT(r), r.UserAgent())
			writeErr(w, http.StatusUnauthorized, msg+"（该端点要求访问令牌，请在 X-Api-Token 头携带）")
			return
		}
		if !p.can(PermHookSend) {
			log.Printf("webhook 令牌无权限 endpoint=%s token=%s perm=%s", ep.Slug, p.Name, PermHookSend)
			writeErr(w, http.StatusForbidden, "该令牌没有 hook.send 权限")
			return
		}
		if !p.scopeOK(ep.Slug) {
			log.Printf("webhook 令牌作用域不含端点 endpoint=%s token=%s", ep.Slug, p.Name)
			writeErr(w, http.StatusForbidden, "该令牌无权访问端点 "+ep.Slug)
			return
		}
		caller = p
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
			ep.Slug, authKind, s.clientIPT(r), r.UserAgent(), len(raw))
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
		RemoteIP:    s.clientIPT(r),
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

	// 请求自带任务指令（需 hook.trigger 权限）：让调用方在发 webhook 的同时直接下单，
	// 不必提前在界面里配规则。指令可放在 X-Hook-Job 头，或 JSON body 的 __job__ 字段。
	inline := []string{}
	if dir, derr := extractJobDirective(r, payload); derr != nil {
		writeErr(w, 400, "任务指令非法: "+derr.Error())
		return
	} else if dir != nil {
		if caller == nil || !caller.can(PermHookTrigger) {
			log.Printf("webhook 带任务指令但无权限 endpoint=%s caller=%v", ep.Slug, caller != nil)
			writeErr(w, http.StatusForbidden,
				"该令牌没有 hook.trigger 权限，不能在请求里直接创建任务")
			return
		}
		job, jerr := s.buildInlineJob(ep, req, dir)
		if jerr != nil {
			writeErr(w, 400, "任务指令非法: "+jerr.Error())
			return
		}
		if err := s.st.CreateJob(job); err != nil {
			log.Printf("inline job: %v", err)
			writeErr(w, 500, "创建任务失败")
			return
		}
		inline = append(inline, job.ID)
		s.hub.Publish("job.created", map[string]any{
			"job_id": job.ID, "job_name": job.Name, "request_id": req.ID, "mode": "inline",
			"by": callerName(caller),
		})
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

	all := append(append([]string{}, matched...), inline...)
	writeJSON(w, 200, map[string]any{
		"ok":           true,
		"request_id":   req.ID,
		"endpoint":     ep.Slug,
		"matched_jobs": all,
		"rule_jobs":    matched,
		"inline_jobs":  inline,
		"received_at":  req.ReceivedAt,
	})
}

func callerName(p *principal) string {
	if p == nil {
		return "anonymous"
	}
	return p.Name
}

// jobDirective 调用方在 webhook 请求里附带的建任务指令。
type jobDirective struct {
	Name        string   `json:"name"`
	Schedule    string   `json:"schedule"`
	ExecArgv    []string `json:"exec_argv"`
	TimeoutSec  int64    `json:"timeout_sec"`
	MaxAttempts int64    `json:"max_attempts"`
	Payload     string   `json:"payload"`
	OneShot     bool     `json:"oneshot"`
}

// extractJobDirective 从 X-Hook-Job 头或 body 的 __job__ 字段取任务指令。
// 两者都没给时返回 (nil, nil)，表示这条请求不要求建任务。
func extractJobDirective(r *http.Request, payload string) (*jobDirective, error) {
	rawHdr := strings.TrimSpace(r.Header.Get("X-Hook-Job"))
	if rawHdr != "" {
		var d jobDirective
		if err := json.Unmarshal([]byte(rawHdr), &d); err != nil {
			return nil, fmt.Errorf("X-Hook-Job 不是合法 JSON: %w", err)
		}
		return &d, nil
	}
	var body map[string]json.RawMessage
	if json.Unmarshal([]byte(payload), &body) != nil {
		return nil, nil
	}
	rawJob, ok := body["__job__"]
	if !ok {
		return nil, nil
	}
	var d jobDirective
	if err := json.Unmarshal(rawJob, &d); err != nil {
		return nil, fmt.Errorf("body.__job__ 不是合法任务对象: %w", err)
	}
	return &d, nil
}

// buildInlineJob 校验并组装一条来自请求的任务。
func (s *Server) buildInlineJob(ep *store.Endpoint, req *store.Request, d *jobDirective) (*store.Job, error) {
	if d == nil {
		return nil, fmt.Errorf("任务指令为空")
	}
	if len(d.ExecArgv) == 0 {
		return nil, fmt.Errorf("exec_argv 必填且不能为空数组")
	}
	argvJSON, _ := json.Marshal(d.ExecArgv)
	schedule := strings.TrimSpace(d.Schedule)
	if schedule == "" {
		schedule = "@every 5m"
	}
	sch, err := scheduler.Parse(schedule)
	if err != nil {
		return nil, fmt.Errorf("schedule 非法: %w", err)
	}
	name := d.Name
	if name == "" {
		name = "内联任务 · " + time.Now().Format("01-02 15:04:05")
	}
	payloadStr := d.Payload
	if payloadStr == "" {
		payloadStr = req.Payload
	}
	n, timeout, attempts, err := validateJobFields(name, string(argvJSON), payloadStr, d.TimeoutSec, d.MaxAttempts)
	if err != nil {
		return nil, err
	}
	job := &store.Job{
		Name:        n,
		EndpointID:  &ep.ID,
		Schedule:    schedule,
		ExecArgv:    string(argvJSON),
		Payload:     payloadStr,
		TimeoutSec:  timeout,
		MaxAttempts: attempts,
		Enabled:     true,
		OneShot:     d.OneShot,
		NextRunAt:   sch.Next(time.Now()).Unix(),
	}
	return job, nil
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
	// SSE 需要读权限；EventSource 不能带自定义头，所以前端用 ?token= 传（下面 HandleSSE 里读）
	mux.HandleFunc("/api/stream", s.requireAny(PermRequestRead, PermJobRead)(s.HandleSSE))

	mux.HandleFunc("/api/overview", s.requireAny(PermRequestRead, PermJobRead, PermEndpointWrite)(func(w http.ResponseWriter, r *http.Request) {
		st, err := s.st.Stats()
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		p := principalOf(r)
		eps, _ := s.st.ListEndpoints()
		eps = s.scopedEndpoints(p, eps)
		reqs, _ := s.st.ListRequests(0, 12, 0)
		if len(p.Scopes) > 0 {
			// scoped 令牌：最近请求只从其授权端点里取，避免越权预览
			var merged []store.Request
			for _, e := range eps {
				part, _ := s.st.ListRequests(e.ID, 12, 0)
				merged = append(merged, part...)
			}
			sort.Slice(merged, func(i, j int) bool { return merged[i].ReceivedAt > merged[j].ReceivedAt })
			if len(merged) > 12 {
				merged = merged[:12]
			}
			reqs = merged
		}
		runs, _ := s.st.ListRuns("", "", 8)
		writeJSON(w, 200, map[string]any{
			"stats": st, "endpoints": eps, "recent_requests": reqs, "recent_runs": runs,
			"admin_required": s.adminToken != "" || s.hasTokens(),
			"me":             p,
		})
	}))

	mux.HandleFunc("/api/endpoints", s.requireAny(PermRequestRead, PermJobRead, PermEndpointWrite)(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			eps, err := s.st.ListEndpoints()
			if err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			writeJSON(w, 200, s.scopedEndpoints(principalOf(r), eps))
		case http.MethodPost:
			if !needPerm(w, r, PermEndpointWrite) {
				return
			}
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

	mux.HandleFunc("/api/endpoints/", s.requireAny(PermRequestRead, PermJobRead, PermEndpointWrite)(func(w http.ResponseWriter, r *http.Request) {
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
		if !principalOf(r).scopeOK(ep.Slug) {
			writeErr(w, 404, "端点不存在")
			return
		}
		if len(parts) == 1 {
			switch r.Method {
			case http.MethodGet:
				if !needPerm(w, r, PermRequestRead) && !needPerm(w, r, PermJobRead) {
					return
				}
				writeJSON(w, 200, ep)
			case http.MethodPut, http.MethodPatch:
				if !needPerm(w, r, PermEndpointWrite) {
					return
				}
				body, rerr := io.ReadAll(io.LimitReader(r.Body, 1<<20))
				if rerr != nil || len(body) == 0 {
					writeErr(w, 400, "JSON 解析失败")
					return
				}
				var raw map[string]any
				if err := json.Unmarshal(body, &raw); err != nil {
					writeErr(w, 400, "JSON 解析失败")
					return
				}
				// 合并候选值：只有请求里显式出现的字段才覆盖现值，防止零值覆盖整行
				up := *ep
				if v, ok := raw["slug"]; ok {
					sv, _ := v.(string)
					if sv != up.Slug {
						if !validSlug.MatchString(sv) {
							writeErr(w, 400, "slug 只允许小写字母、数字、连字符、下划线，且以字母或数字开头，长度 1~64")
							return
						}
						if other, _ := s.st.GetEndpointBySlug(sv); other != nil && other.ID != up.ID {
							writeErr(w, 400, fmt.Sprintf("slug %q 已被其他端点占用", sv))
							return
						}
						up.Slug = sv
					}
				}
				if v, ok := raw["title"]; ok {
					sv, _ := v.(string)
					if len(sv) > 200 {
						writeErr(w, 400, "title 过长：上限 200 字符")
						return
					}
					up.Title = sv
				}
				if v, ok := raw["description"]; ok {
					sv, _ := v.(string)
					if len(sv) > 2000 {
						writeErr(w, 400, "description 过长：上限 2000 字符")
						return
					}
					up.Description = sv
				}
				if v, ok := raw["auth"]; ok {
					sv, _ := v.(string)
					switch sv {
					case "none", "token", "hmac":
						up.Auth = sv
					default:
						writeErr(w, 400, "auth 只允许 none / token / hmac")
						return
					}
				}
				if v, ok := raw["secret"]; ok {
					sv, _ := v.(string)
					if len(sv) > 512 {
						writeErr(w, 400, "secret 过长：上限 512 字节")
						return
					}
					up.Secret = sv // 显式传空串=清空
				}
				if up.Auth == "token" || up.Auth == "hmac" {
					if up.Secret == "" {
						writeErr(w, 400, "auth 为 token/hmac 时 secret 不能为空")
						return
					}
				} else {
					up.Secret = ""
				}
				if v, ok := raw["max_body"]; ok {
					nv, _ := v.(float64)
					if nv < 1 || nv > float64(maxBodyHard) {
						writeErr(w, 400, fmt.Sprintf("max_body 需在 1~%d 之间", maxBodyHard))
						return
					}
					up.MaxBody = int64(nv)
				}
				if v, ok := raw["retain_days"]; ok {
					nv, _ := v.(float64)
					if nv < 0 || nv > 3650 {
						writeErr(w, 400, "retain_days 需在 0~3650 之间")
						return
					}
					up.RetainDays = int64(nv)
				}
				if v, ok := raw["rate_limit"]; ok {
					nv, _ := v.(float64)
					if nv < 0 || nv > 6000 {
						writeErr(w, 400, "rate_limit 需在 0~6000 之间（0=不限）")
						return
					}
					up.RateLimit = int64(nv)
				}
				if v, ok := raw["enabled"]; ok {
					bv, _ := v.(bool)
					up.Enabled = bv
				}
				if err := s.st.UpsertEndpoint(&up); err != nil {
					writeErr(w, 400, err.Error())
					return
				}
				writeJSON(w, 200, up)
			case http.MethodDelete:
				if !needPerm(w, r, PermEndpointWrite) {
					return
				}
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
			if !needPerm(w, r, PermRequestRead) {
				return
			}
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
				if !needPerm(w, r, PermJobRead) && !needPerm(w, r, PermRequestRead) {
					return
				}
				rs, err := s.st.ListRules(ep.ID)
				if err != nil {
					writeErr(w, 500, err.Error())
					return
				}
				writeJSON(w, 200, rs)
				return
			}
			if r.Method == http.MethodPost {
				if !needPerm(w, r, PermEndpointWrite) {
					return
				}
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

	mux.HandleFunc("/api/rules/", s.requirePerm(PermEndpointWrite)(func(w http.ResponseWriter, r *http.Request) {
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

	mux.HandleFunc("/api/requests/", s.requireAny(PermRequestRead, PermRequestWrite)(func(w http.ResponseWriter, r *http.Request) {
		rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/requests/"), "/")
		parts := strings.Split(rest, "/")
		req, err := s.st.GetRequest(parts[0])
		if err != nil || req == nil {
			writeErr(w, 404, "请求不存在")
			return
		}
		if len(parts) == 1 {
			if !needPerm(w, r, PermRequestRead) {
				return
			}
			writeJSON(w, 200, req)
			return
		}
		if parts[1] == "make-job" && r.Method == http.MethodPost {
			// 「请求转任务」需要两条权限：能看请求 + 能建任务。
			// 只能 hook.send 的令牌到这里会被 403 挡掉，这正是「只能发请求」与「能创建任务」的分界。
			if !needPerm(w, r, PermRequestWrite) {
				return
			}
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

	mux.HandleFunc("/api/jobs", s.requireAny(PermJobRead, PermJobWrite)(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if !needPerm(w, r, PermJobRead) {
				return
			}
			js, err := s.st.ListJobs()
			if err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			writeJSON(w, 200, js)
		case http.MethodPost:
			if !needPerm(w, r, PermJobWrite) {
				return
			}
			var in struct {
				Name        string                `json:"name"`
				Schedule    string                `json:"schedule"`
				ExecArgv    string                `json:"exec_argv"`
				Payload     string                `json:"payload"`
				TimeoutSec  int64                 `json:"timeout_sec"`
				MaxAttempts int64                 `json:"max_attempts"`
				Enabled     *bool                 `json:"enabled"`
				Notify      []store.NotifyBinding `json:"notify"`
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
			notifyBindings, nerr := s.normalizeNotify(in.Notify)
			if nerr != nil {
				writeErr(w, 400, nerr.Error())
				return
			}
			job := &store.Job{
				Name: name, Schedule: in.Schedule, ExecArgv: in.ExecArgv, Payload: in.Payload,
				Notify: notifyBindings, TimeoutSec: timeout, MaxAttempts: attempts, Enabled: enabled,
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

	mux.HandleFunc("/api/jobs/", s.requireAny(PermJobRead, PermJobWrite)(func(w http.ResponseWriter, r *http.Request) {
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
				if !needPerm(w, r, PermJobRead) {
					return
				}
				writeJSON(w, 200, job)
			case http.MethodPut, http.MethodPatch:
				if !needPerm(w, r, PermJobWrite) {
					return
				}
				var in struct {
					Name        string                 `json:"name"`
					Schedule    string                 `json:"schedule"`
					ExecArgv    string                 `json:"exec_argv"`
					Payload     string                 `json:"payload"`
					TimeoutSec  int64                  `json:"timeout_sec"`
					MaxAttempts int64                  `json:"max_attempts"`
					Notify      *[]store.NotifyBinding `json:"notify"`
				}
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
				if in.Notify != nil {
					bindings, nerr := s.normalizeNotify(*in.Notify)
					if nerr != nil {
						writeErr(w, 400, nerr.Error())
						return
					}
					job.Notify = bindings
				}
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
				if !needPerm(w, r, PermJobWrite) {
					return
				}
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
			if !needPerm(w, r, PermJobWrite) {
				return
			}
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
			if !needPerm(w, r, PermJobWrite) {
				return
			}
			runID := store.NewID("run")
			if err := s.st.EnqueueRun(runID, job.ID, job.MaxAttempts); err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			s.hub.Publish("run.queued", map[string]any{"run_id": runID, "job_id": job.ID, "job_name": job.Name})
			writeJSON(w, 200, map[string]any{"ok": true, "run_id": runID})
		case "runs":
			if !needPerm(w, r, PermJobRead) {
				return
			}
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

	mux.HandleFunc("/api/runs", s.requirePerm(PermJobRead)(func(w http.ResponseWriter, r *http.Request) {
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

	mux.HandleFunc("/api/runs/", s.requireAny(PermJobRead, PermJobWrite)(func(w http.ResponseWriter, r *http.Request) {
		rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/runs/"), "/")
		parts := strings.Split(rest, "/")
		run, err := s.st.GetRun(parts[0])
		if err != nil || run == nil {
			writeErr(w, 404, "运行记录不存在")
			return
		}
		if len(parts) > 1 && parts[1] == "retry" && r.Method == http.MethodPost {
			if !needPerm(w, r, PermJobWrite) {
				return
			}
			if err := s.st.RequeueRun(run.ID); err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			s.hub.Publish("run.queued", map[string]any{"run_id": run.ID, "job_id": run.JobID})
			writeJSON(w, 200, map[string]any{"ok": true, "run_id": run.ID})
			return
		}
		if !needPerm(w, r, PermJobRead) {
			return
		}
		writeJSON(w, 200, run)
	}))

	// ---------- 令牌管理 ----------
	mux.HandleFunc("/api/whoami", func(w http.ResponseWriter, r *http.Request) {
		p, msg := s.identify(r)
		if p == nil {
			writeJSON(w, 401, map[string]any{"ok": false, "error": msg, "auth_required": s.hasTokens() || s.adminToken != ""})
			return
		}
		writeJSON(w, 200, map[string]any{
			"ok": true, "me": p, "all_perms": allPerms, "perm_labels": permLabels, "roles": rolesSummary(),
			"auth_required": s.hasTokens() || s.adminToken != "",
		})
	})

	mux.HandleFunc("/api/tokens", s.requirePerm(PermTokenManage)(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			ts, err := s.st.ListTokens()
			if err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			writeJSON(w, 200, map[string]any{"tokens": ts, "all_perms": allPerms, "perm_labels": permLabels, "roles": rolesSummary()})
		case http.MethodPost:
			var in struct {
				Name    string   `json:"name"`
				Role    string   `json:"role"`
				Perms   []string `json:"perms"`
				Scopes  []string `json:"scopes"`
				Note    string   `json:"note"`
				Enabled *bool    `json:"enabled"`
			}
			if err := readJSON(r, &in); err != nil {
				writeErr(w, 400, "JSON 解析失败")
				return
			}
			if strings.TrimSpace(in.Name) == "" {
				writeErr(w, 400, "name 必填（用来说明这个令牌给谁用）")
				return
			}
			if n := len([]rune(in.Name)); n > maxNameLen {
				writeErr(w, 400, fmt.Sprintf("name 过长：%d 字符，上限 %d", n, maxNameLen))
				return
			}
			if in.Role == "" {
				in.Role = "operator"
			}
			if !validRole(in.Role) {
				writeErr(w, 400, "role 非法，可选："+strings.Join(validRoles, " / "))
				return
			}
			for _, perm := range in.Perms {
				if !validPerm(perm) {
					writeErr(w, 400, "未知权限："+perm)
					return
				}
			}
			for _, sc := range in.Scopes {
				if !validSlug.MatchString(sc) {
					writeErr(w, 400, "scopes 里的端点标识非法："+sc)
					return
				}
			}
			enabled := true
			if in.Enabled != nil {
				enabled = *in.Enabled
			}
			plain, prefix := newTokenPlain()
			t := &store.Token{
				Name: in.Name, Prefix: prefix, Role: in.Role, Perms: in.Perms,
				Scopes: in.Scopes, Note: in.Note, Enabled: enabled,
			}
			if err := s.st.CreateToken(t, hashToken(plain)); err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			// 明文只在此刻返回一次，之后库里只有 hash
			writeJSON(w, 200, map[string]any{
				"token": t, "plain": plain,
				"warning": "请立刻复制保存：明文不会再次显示，库里只存 sha256",
			})
		default:
			writeErr(w, 405, "方法不允许")
		}
	}))

	mux.HandleFunc("/api/tokens/", s.requirePerm(PermTokenManage)(func(w http.ResponseWriter, r *http.Request) {
		idStr := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/tokens/"), "/")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeErr(w, 400, "非法令牌 id")
			return
		}
		t, err := s.st.GetToken(id)
		if err != nil || t == nil {
			writeErr(w, 404, "令牌不存在")
			return
		}
		switch r.Method {
		case http.MethodPut, http.MethodPatch:
			var in struct {
				Name    string   `json:"name"`
				Role    string   `json:"role"`
				Perms   []string `json:"perms"`
				Scopes  []string `json:"scopes"`
				Note    string   `json:"note"`
				Enabled *bool    `json:"enabled"`
			}
			if err := readJSON(r, &in); err != nil {
				writeErr(w, 400, "JSON 解析失败")
				return
			}
			if in.Name != "" {
				if n := len([]rune(in.Name)); n > maxNameLen {
					writeErr(w, 400, fmt.Sprintf("name 过长：%d 字符，上限 %d", n, maxNameLen))
					return
				}
				t.Name = in.Name
			}
			if in.Role != "" {
				if !validRole(in.Role) {
					writeErr(w, 400, "role 非法，可选："+strings.Join(validRoles, " / "))
					return
				}
				t.Role = in.Role
			}
			if in.Perms != nil {
				for _, perm := range in.Perms {
					if !validPerm(perm) {
						writeErr(w, 400, "未知权限："+perm)
						return
					}
				}
				t.Perms = in.Perms
			}
			if in.Scopes != nil {
				for _, sc := range in.Scopes {
					if !validSlug.MatchString(sc) {
						writeErr(w, 400, "scopes 里的端点标识非法："+sc)
						return
					}
				}
				t.Scopes = in.Scopes
			}
			if in.Note != "" {
				t.Note = in.Note
			}
			if in.Enabled != nil {
				t.Enabled = *in.Enabled
			}
			if err := s.st.UpdateToken(t); err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			writeJSON(w, 200, t)
		case http.MethodDelete:
			if err := s.st.DeleteToken(t.ID); err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			writeJSON(w, 200, map[string]bool{"ok": true})
		default:
			writeErr(w, 405, "方法不允许")
		}
	}))

	// ---------- 推送渠道 ----------
	mux.HandleFunc("/api/channels/types", s.requireAny(PermChannelManage, PermJobRead, PermJobWrite)(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"types": notify.Catalog()})
	}))

	mux.HandleFunc("/api/channels", s.requireAny(PermChannelManage, PermJobRead, PermJobWrite)(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			cs, err := s.st.ListChannels()
			if err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			if !principalOf(r).can(PermChannelManage) {
				// 只读身份只看到渠道名，不下发可能含密钥的配置
				for i := range cs {
					cs[i].Config = map[string]string{}
					cs[i].LastError = ""
				}
			}
			writeJSON(w, 200, cs)
		case http.MethodPost:
			if !needPerm(w, r, PermChannelManage) {
				return
			}
			var in struct {
				Name    string            `json:"name"`
				Type    string            `json:"type"`
				Config  map[string]string `json:"config"`
				Enabled *bool             `json:"enabled"`
			}
			if err := readJSON(r, &in); err != nil {
				writeErr(w, 400, "JSON 解析失败")
				return
			}
			enabled := true
			if in.Enabled != nil {
				enabled = *in.Enabled
			}
			ch, msg := buildChannel(0, in.Name, in.Type, in.Config, enabled)
			if msg != "" {
				writeErr(w, 400, msg)
				return
			}
			if err := s.st.CreateChannel(ch); err != nil {
				writeErr(w, 400, err.Error())
				return
			}
			writeJSON(w, 200, ch)
		default:
			writeErr(w, 405, "方法不允许")
		}
	}))

	mux.HandleFunc("/api/channels/", s.requireAny(PermChannelManage, PermJobRead, PermJobWrite)(func(w http.ResponseWriter, r *http.Request) {
		rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/channels/"), "/")
		parts := strings.Split(rest, "/")
		id, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			writeErr(w, 400, "非法渠道 id")
			return
		}
		ch, err := s.st.GetChannel(id)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		if ch == nil {
			writeErr(w, 404, "渠道不存在")
			return
		}
		if len(parts) > 1 && parts[1] == "test" {
			if r.Method != http.MethodPost {
				writeErr(w, 405, "方法不允许")
				return
			}
			if !needPerm(w, r, PermChannelManage) {
				return
			}
			if s.notifier == nil {
				writeErr(w, 503, "推送服务未初始化")
				return
			}
			if err := s.notifier.Test(ch); err != nil {
				writeJSON(w, 502, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			writeJSON(w, 200, map[string]any{"ok": true})
			return
		}
		if len(parts) > 1 {
			writeErr(w, 404, "未知子资源")
			return
		}
		switch r.Method {
		case http.MethodGet:
			if !principalOf(r).can(PermChannelManage) {
				ch.Config = map[string]string{}
				ch.LastError = ""
			}
			writeJSON(w, 200, ch)
		case http.MethodPut, http.MethodPatch:
			if !needPerm(w, r, PermChannelManage) {
				return
			}
			var in struct {
				Name    string             `json:"name"`
				Type    string             `json:"type"`
				Config  *map[string]string `json:"config"`
				Enabled *bool              `json:"enabled"`
			}
			if err := readJSON(r, &in); err != nil {
				writeErr(w, 400, "JSON 解析失败")
				return
			}
			name, typ, cfg, enabled := ch.Name, ch.Type, ch.Config, ch.Enabled
			if in.Name != "" {
				name = in.Name
			}
			if in.Type != "" {
				typ = in.Type
			}
			if in.Config != nil {
				cfg = *in.Config
			}
			if in.Enabled != nil {
				enabled = *in.Enabled
			}
			up, msg := buildChannel(ch.ID, name, typ, cfg, enabled)
			if msg != "" {
				writeErr(w, 400, msg)
				return
			}
			if err := s.st.UpdateChannel(up); err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			up.LastSentAt, up.LastError, up.CreatedAt = ch.LastSentAt, ch.LastError, ch.CreatedAt
			writeJSON(w, 200, up)
		case http.MethodDelete:
			if !needPerm(w, r, PermChannelManage) {
				return
			}
			if err := s.st.DeleteChannel(ch.ID); err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			writeJSON(w, 200, map[string]bool{"ok": true})
		default:
			writeErr(w, 405, "方法不允许")
		}
	}))
}

// buildChannel 校验渠道入参并归一化。返回非空 msg 表示校验失败。
func buildChannel(id int64, name, typ string, cfg map[string]string, enabled bool) (*store.Channel, string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, "name 必填"
	}
	if n := len([]rune(name)); n > maxNameLen {
		return nil, fmt.Sprintf("name 过长：%d 字符，上限 %d", n, maxNameLen)
	}
	typ = strings.TrimSpace(typ)
	if !notify.KnownType(typ) {
		return nil, "不支持的渠道类型：" + typ
	}
	clean := map[string]string{}
	for k, v := range cfg {
		clean[strings.TrimSpace(k)] = v
	}
	if err := notify.Validate(typ, clean); err != nil {
		return nil, err.Error()
	}
	if b, err := json.Marshal(clean); err != nil || len(b) > 8192 {
		return nil, "渠道配置过大：上限 8192 字节"
	}
	return &store.Channel{ID: id, Name: name, Type: typ, Config: clean, Enabled: enabled}, ""
}

// normalizeNotify 校验任务的通知绑定：渠道必须存在，事件只允许 success / failure。
func (s *Server) normalizeNotify(in []store.NotifyBinding) ([]store.NotifyBinding, error) {
	if len(in) > 20 {
		return nil, fmt.Errorf("通知渠道过多：%d，上限 20", len(in))
	}
	out := make([]store.NotifyBinding, 0, len(in))
	seen := map[int64]bool{}
	for _, b := range in {
		if b.ChannelID <= 0 {
			return nil, fmt.Errorf("channel_id 非法")
		}
		if seen[b.ChannelID] {
			return nil, fmt.Errorf("渠道 %d 重复绑定", b.ChannelID)
		}
		seen[b.ChannelID] = true
		ch, err := s.st.GetChannel(b.ChannelID)
		if err != nil {
			return nil, err
		}
		if ch == nil {
			return nil, fmt.Errorf("渠道 %d 不存在", b.ChannelID)
		}
		events := make([]string, 0, len(b.Events))
		evSeen := map[string]bool{}
		for _, e := range b.Events {
			if e != "success" && e != "failure" {
				return nil, fmt.Errorf("事件只支持 success / failure，收到 %q", e)
			}
			if !evSeen[e] {
				events = append(events, e)
				evSeen[e] = true
			}
		}
		out = append(out, store.NotifyBinding{ChannelID: b.ChannelID, Events: events})
	}
	return out, nil
}

// ---------- 作用域辅助 ----------

// hasTokens 服务端是否已建过启用中的令牌（true 表示访问控制已生效）。
func (s *Server) hasTokens() bool {
	n, err := s.st.CountTokens()
	return err == nil && n > 0
}

// scopedEndpoints 按令牌的 endpoints 白名单过滤。Scopes 为空则不限制。
func (s *Server) scopedEndpoints(p *principal, eps []store.Endpoint) []store.Endpoint {
	if p == nil || len(p.Scopes) == 0 {
		return eps
	}
	out := make([]store.Endpoint, 0, len(eps))
	for _, e := range eps {
		if p.scopeOK(e.Slug) {
			out = append(out, e)
		}
	}
	return out
}
