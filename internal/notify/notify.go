package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Channel 是一条推送渠道的运行时形态。Config 的键随 Type 变化，见 Catalog()。
type Channel struct {
	ID      int64             `json:"id"`
	Name    string            `json:"name"`
	Type    string            `json:"type"`
	Config  map[string]string `json:"config"`
	Enabled bool              `json:"enabled"`
}

// Message 是推送内容。各渠道负责把它翻译成自己的消息格式。
type Message struct {
	Title       string `json:"title"`
	Body        string `json:"body"`
	Event       string `json:"event"`  // success | failure
	Status      string `json:"status"` // succeeded | dead | test
	JobID       string `json:"job_id"`
	JobName     string `json:"job_name"`
	RunID       string `json:"run_id"`
	ExitCode    *int64 `json:"exit_code"`
	Error       string `json:"error"`
	Output      string `json:"output"`
	Attempt     int64  `json:"attempt"`
	MaxAttempts int64  `json:"max_attempts"`
	DurationSec int64  `json:"duration_sec"`
	FinishedAt  int64  `json:"finished_at"`
}

// Field 描述一个渠道配置字段，前端据此动态渲染表单。
type Field struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Placeholder string   `json:"placeholder,omitempty"`
	Help        string   `json:"help,omitempty"`
	Type        string   `json:"type,omitempty"` // text | password | number | select | textarea | url
	Required    bool     `json:"required,omitempty"`
	Secret      bool     `json:"secret,omitempty"`
	Default     string   `json:"default,omitempty"`
	Options     []string `json:"options,omitempty"`
}

// TypeDef 一种渠道类型的元数据。
type TypeDef struct {
	Type   string  `json:"type"`
	Label  string  `json:"label"`
	Group  string  `json:"group"` // domestic | international | generic
	Desc   string  `json:"desc"`
	Fields []Field `json:"fields"`
}

var catalog = []TypeDef{
	{
		Type: "bark", Label: "Bark（iOS）", Group: "domestic",
		Desc: "Bark 官方或自建服务，推送到 iPhone。",
		Fields: []Field{
			{Key: "key", Label: "Device Key", Type: "password", Required: true, Secret: true, Placeholder: "Bark App 里的一串 key"},
			{Key: "server", Label: "服务地址", Type: "url", Default: "https://api.day.app", Help: "自建 Bark 服务时改这里"},
			{Key: "group", Label: "分组", Type: "text", Placeholder: "hookdock"},
			{Key: "sound", Label: "提示音", Type: "text", Placeholder: "default"},
			{Key: "level", Label: "时效级别", Type: "select", Options: []string{"active", "timeSensitive", "passive", "critical"}},
			{Key: "icon", Label: "图标 URL", Type: "url"},
			{Key: "click", Label: "点击跳转 URL", Type: "url", Help: "可选。留空则用 HOOKDOCK_PUBLIC_URL 拼任务详情"},
		},
	},
	{
		Type: "wecom", Label: "企业微信群机器人", Group: "domestic",
		Desc: "企业微信群 → 添加群机器人 → 复制 Webhook 地址。",
		Fields: []Field{
			{Key: "webhook_url", Label: "Webhook 地址", Type: "password", Required: true, Secret: true, Placeholder: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=..."},
		},
	},
	{
		Type: "dingtalk", Label: "钉钉群机器人", Group: "domestic",
		Desc: "钉钉群 → 群设置 → 机器人 → 自定义机器人。加签机器人需填 secret。",
		Fields: []Field{
			{Key: "webhook_url", Label: "Webhook 地址", Type: "password", Required: true, Secret: true, Placeholder: "https://oapi.dingtalk.com/robot/send?access_token=..."},
			{Key: "secret", Label: "加签 Secret", Type: "password", Secret: true, Help: "安全设置选「加签」时才需要"},
		},
	},
	{
		Type: "feishu", Label: "飞书群机器人", Group: "domestic",
		Desc: "飞书群 → 设置 → 群机器人 → 自定义机器人。",
		Fields: []Field{
			{Key: "webhook_url", Label: "Webhook 地址", Type: "password", Required: true, Secret: true, Placeholder: "https://open.feishu.cn/open-apis/bot/v2/hook/..."},
			{Key: "secret", Label: "签名校验 Secret", Type: "password", Secret: true, Help: "可选，机器人开启签名校验时填写"},
		},
	},
	{
		Type: "serverchan", Label: "Server 酱", Group: "domestic",
		Desc: "Server 酱 Turbo，推送到微信。",
		Fields: []Field{
			{Key: "sendkey", Label: "SendKey", Type: "password", Required: true, Secret: true, Placeholder: "SCT..."},
		},
	},
	{
		Type: "pushplus", Label: "PushPlus", Group: "domestic",
		Desc: "PushPlus 推送加，支持微信/邮件/Webhook 等通道。",
		Fields: []Field{
			{Key: "token", Label: "Token", Type: "password", Required: true, Secret: true},
			{Key: "channel", Label: "发送通道", Type: "select", Options: []string{"wechat", "webhook", "mail", "sms"}, Help: "留空用账号默认通道"},
		},
	},
	{
		Type: "telegram", Label: "Telegram Bot", Group: "international",
		Desc: "用 @BotFather 建机器人，把 bot 拉进会话或先私聊一次拿 chat_id。",
		Fields: []Field{
			{Key: "bot_token", Label: "Bot Token", Type: "password", Required: true, Secret: true, Placeholder: "123456:ABC..."},
			{Key: "chat_id", Label: "Chat ID", Type: "text", Required: true, Placeholder: "123456789 或 @channel"},
		},
	},
	{
		Type: "slack", Label: "Slack", Group: "international",
		Desc: "Slack App → Incoming Webhooks → 复制 Webhook URL。",
		Fields: []Field{
			{Key: "webhook_url", Label: "Webhook 地址", Type: "password", Required: true, Secret: true, Placeholder: "https://hooks.slack.com/services/..."},
		},
	},
	{
		Type: "discord", Label: "Discord", Group: "international",
		Desc: "频道设置 → 整合 → Webhook → 复制 Webhook URL。",
		Fields: []Field{
			{Key: "webhook_url", Label: "Webhook 地址", Type: "password", Required: true, Secret: true, Placeholder: "https://discord.com/api/webhooks/..."},
		},
	},
	{
		Type: "ntfy", Label: "ntfy", Group: "international",
		Desc: "开源自托管推送，支持 Android/iOS/Web。",
		Fields: []Field{
			{Key: "server", Label: "服务地址", Type: "url", Default: "https://ntfy.sh"},
			{Key: "topic", Label: "Topic", Type: "text", Required: true},
			{Key: "token", Label: "访问 Token", Type: "password", Secret: true, Help: "公共 topic 可留空"},
			{Key: "priority", Label: "优先级", Type: "number", Default: "3", Help: "1~5，5 最高"},
			{Key: "tags", Label: "标签", Type: "text", Placeholder: "warning,robot"},
		},
	},
	{
		Type: "gotify", Label: "Gotify", Group: "international",
		Desc: "开源自托管推送，Android App 接收。",
		Fields: []Field{
			{Key: "server", Label: "服务地址", Type: "url", Required: true, Placeholder: "https://gotify.example.com"},
			{Key: "token", Label: "应用 Token", Type: "password", Required: true, Secret: true},
			{Key: "priority", Label: "优先级", Type: "number", Default: "5"},
		},
	},
	{
		Type: "email", Label: "邮件（SMTP）", Group: "generic",
		Desc: "任意 SMTP 服务，支持 SSL / STARTTLS / 明文。",
		Fields: []Field{
			{Key: "smtp_host", Label: "SMTP 主机", Type: "text", Required: true, Placeholder: "smtp.example.com"},
			{Key: "smtp_port", Label: "端口", Type: "number", Default: "465"},
			{Key: "security", Label: "加密方式", Type: "select", Default: "ssl", Options: []string{"ssl", "starttls", "none"}},
			{Key: "username", Label: "用户名", Type: "text"},
			{Key: "password", Label: "密码 / 授权码", Type: "password", Secret: true},
			{Key: "from", Label: "发件人", Type: "text", Required: true, Placeholder: "hookdock@example.com"},
			{Key: "to", Label: "收件人", Type: "text", Required: true, Placeholder: "a@example.com,b@example.com"},
			{Key: "subject_prefix", Label: "标题前缀", Type: "text", Default: "[hookdock]"},
		},
	},
	{
		Type: "webhook", Label: "通用 Webhook", Group: "generic",
		Desc: "自定义 HTTP 回调，POST/PUT 一段结构化 JSON。",
		Fields: []Field{
			{Key: "url", Label: "回调地址", Type: "url", Required: true},
			{Key: "method", Label: "方法", Type: "select", Default: "POST", Options: []string{"POST", "PUT"}},
			{Key: "headers", Label: "附加请求头（JSON）", Type: "textarea", Placeholder: "{\"Authorization\":\"Bearer xxx\"}"},
		},
	},
}

var typeIndex = func() map[string]TypeDef {
	m := make(map[string]TypeDef, len(catalog))
	for _, d := range catalog {
		m[d.Type] = d
	}
	return m
}()

// Catalog 返回所有渠道类型的元数据（供 UI 动态渲染）。
func Catalog() []TypeDef { return catalog }

// KnownType 判断渠道类型是否受支持。
func KnownType(typ string) bool {
	_, ok := typeIndex[typ]
	return ok
}

// TypeLabel 返回渠道类型的中文名。
func TypeLabel(typ string) string {
	if d, ok := typeIndex[typ]; ok {
		return d.Label
	}
	return typ
}

// Validate 校验渠道配置。只校验必填项与 URL scheme，不校验外部服务可达性。
func Validate(typ string, cfg map[string]string) error {
	def, ok := typeIndex[typ]
	if !ok {
		return fmt.Errorf("不支持的渠道类型 %q", typ)
	}
	for _, f := range def.Fields {
		v := strings.TrimSpace(cfg[f.Key])
		if f.Required && v == "" {
			return fmt.Errorf("%s 必填", f.Label)
		}
		if v != "" && f.Type == "url" {
			u, err := url.Parse(v)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return fmt.Errorf("%s 必须是 http(s) 地址", f.Label)
			}
		}
	}
	return nil
}

// BuildRunMessage 把一次运行组装成推送消息。
func BuildRunMessage(jobName, jobID, runID string, status string, exitCode *int64, errMsg, output string,
	attempt, maxAttempts int64, startedAt, finishedAt int64) Message {
	event := "success"
	label := "成功"
	if status != "succeeded" {
		event = "failure"
		label = "失败"
	}
	title := fmt.Sprintf("[hookdock] 任务%s · %s", label, jobName)

	var b strings.Builder
	fmt.Fprintf(&b, "任务：%s\n", jobName)
	fmt.Fprintf(&b, "状态：%s\n", statusLabel(status))
	if exitCode != nil {
		fmt.Fprintf(&b, "退出码：%d\n", *exitCode)
	}
	fmt.Fprintf(&b, "尝试：%d/%d\n", attempt, maxAttempts)
	if startedAt > 0 && finishedAt >= startedAt {
		fmt.Fprintf(&b, "耗时：%ds\n", finishedAt-startedAt)
	}
	if finishedAt > 0 {
		fmt.Fprintf(&b, "时间：%s\n", time.Unix(finishedAt, 0).Format("2006-01-02 15:04:05"))
	}
	if strings.TrimSpace(errMsg) != "" {
		fmt.Fprintf(&b, "错误：%s\n", errMsg)
	}
	if out := tailText(output, 1200); out != "" {
		fmt.Fprintf(&b, "\n输出：\n%s\n", out)
	}
	if base := publicURL(); base != "" && jobID != "" {
		fmt.Fprintf(&b, "\n详情：%s/jobs/%s\n", base, url.PathEscape(jobID))
	}
	return Message{
		Title: title, Body: strings.TrimRight(b.String(), "\n"), Event: event, Status: status,
		JobID: jobID, JobName: jobName, RunID: runID, ExitCode: exitCode, Error: errMsg,
		Output: output, Attempt: attempt, MaxAttempts: maxAttempts,
		DurationSec: maxInt64(finishedAt-startedAt, 0), FinishedAt: finishedAt,
	}
}

// TestMessage 用于「发送测试」。
func TestMessage(channelName string) Message {
	now := time.Now()
	return Message{
		Title: "[hookdock] 推送测试",
		Body:  fmt.Sprintf("渠道「%s」配置可用。\n时间：%s", channelName, now.Format("2006-01-02 15:04:05")),
		Event: "test", Status: "test", FinishedAt: now.Unix(),
	}
}

func statusLabel(status string) string {
	switch status {
	case "succeeded":
		return "成功"
	case "dead":
		return "失败（重试已用尽）"
	case "failed":
		return "失败"
	case "timeout":
		return "超时"
	case "test":
		return "测试"
	default:
		return status
	}
}

func publicURL() string {
	return strings.TrimRight(strings.TrimSpace(getenv("HOOKDOCK_PUBLIC_URL")), "/")
}

// getenv 单独抽出来便于测试注入。
var getenv = os.Getenv

// 固定服务地址抽成变量，便于测试里替换成本地 httptest。
var (
	telegramAPIBase = "https://api.telegram.org"
	serverChanBase  = "https://sctapi.ftqq.com"
	pushPlusURL     = "https://www.pushplus.plus/send"
)

// Send 按渠道类型投递消息。网络错误与业务错误都以 error 返回，便于测试接口展示。
func Send(ctx context.Context, client *http.Client, ch Channel, msg Message) error {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	cfg := ch.Config
	if cfg == nil {
		cfg = map[string]string{}
	}
	title := msg.Title
	body := msg.Body
	switch ch.Type {
	case "bark":
		return sendBark(ctx, client, cfg, msg)
	case "wecom":
		return sendWeCom(ctx, client, cfg, title, body)
	case "dingtalk":
		return sendDingTalk(ctx, client, cfg, title, body)
	case "feishu":
		return sendFeishu(ctx, client, cfg, title, body)
	case "serverchan":
		return sendServerChan(ctx, client, cfg, title, body)
	case "pushplus":
		return sendPushPlus(ctx, client, cfg, title, body)
	case "telegram":
		return sendTelegram(ctx, client, cfg, title, body)
	case "slack":
		return sendSlack(ctx, client, cfg, title, body)
	case "discord":
		return sendDiscord(ctx, client, cfg, title, body)
	case "ntfy":
		return sendNtfy(ctx, client, cfg, title, body)
	case "gotify":
		return sendGotify(ctx, client, cfg, title, body)
	case "email":
		return sendEmail(ctx, cfg, title, body)
	case "webhook":
		return sendWebhook(ctx, client, cfg, msg)
	default:
		return fmt.Errorf("不支持的渠道类型 %q", ch.Type)
	}
}

// ---------- HTTP 辅助 ----------

func postJSON(ctx context.Context, client *http.Client, rawURL string, payload any, headers map[string]string) ([]byte, int, error) {
	return doJSON(ctx, client, http.MethodPost, rawURL, payload, headers)
}

func doJSON(ctx context.Context, client *http.Client, method, rawURL string, payload any, headers map[string]string) ([]byte, int, error) {
	var rd io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, rd)
	if err != nil {
		return nil, 0, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	req.Header.Set("User-Agent", "hookdock/0.2")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 32*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return b, resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet(b))
	}
	return b, resp.StatusCode, nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	if s == "" {
		return "(空响应)"
	}
	return s
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func tailText(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return "…" + string(r[len(r)-n:])
}

func cfgOr(m map[string]string, key, def string) string {
	if v := strings.TrimSpace(m[key]); v != "" {
		return v
	}
	return def
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// ---------- 各渠道 ----------

func sendBark(ctx context.Context, client *http.Client, cfg map[string]string, msg Message) error {
	server := strings.TrimRight(cfgOr(cfg, "server", "https://api.day.app"), "/")
	payload := map[string]any{
		"device_key": strings.TrimSpace(cfg["key"]),
		"title":      msg.Title,
		"body":       msg.Body,
	}
	for k, dst := range map[string]string{"group": "group", "sound": "sound", "level": "level", "icon": "icon"} {
		if v := strings.TrimSpace(cfg[k]); v != "" {
			payload[dst] = v
		}
	}
	if v := strings.TrimSpace(cfg["click"]); v != "" {
		payload["url"] = v
	} else if base := publicURL(); base != "" && msg.JobID != "" {
		payload["url"] = base + "/jobs/" + url.PathEscape(msg.JobID)
	}
	b, _, err := postJSON(ctx, client, server+"/push", payload, nil)
	if err != nil {
		return fmt.Errorf("bark: %w", err)
	}
	var r struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(b, &r) == nil && r.Code != 0 && r.Code != 200 {
		return fmt.Errorf("bark: code=%d %s", r.Code, r.Message)
	}
	return nil
}

func sendWeCom(ctx context.Context, client *http.Client, cfg map[string]string, title, body string) error {
	content := truncate(title+"\n"+body, 4000)
	payload := map[string]any{"msgtype": "markdown", "markdown": map[string]any{"content": content}}
	b, _, err := postJSON(ctx, client, strings.TrimSpace(cfg["webhook_url"]), payload, nil)
	if err != nil {
		return fmt.Errorf("企业微信: %w", err)
	}
	var r struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if json.Unmarshal(b, &r) == nil && r.ErrCode != 0 {
		return fmt.Errorf("企业微信: errcode=%d %s", r.ErrCode, r.ErrMsg)
	}
	return nil
}

func sendDingTalk(ctx context.Context, client *http.Client, cfg map[string]string, title, body string) error {
	rawURL := strings.TrimSpace(cfg["webhook_url"])
	if secret := strings.TrimSpace(cfg["secret"]); secret != "" {
		u, err := url.Parse(rawURL)
		if err != nil {
			return fmt.Errorf("钉钉: webhook 地址非法")
		}
		ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(ts + "\n" + secret))
		sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))
		q := u.Query()
		q.Set("timestamp", ts)
		q.Set("sign", sign)
		u.RawQuery = q.Encode()
		rawURL = u.String()
	}
	payload := map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]any{
			"title": title,
			"text":  truncate("### "+title+"\n\n"+body, 4000),
		},
	}
	b, _, err := postJSON(ctx, client, rawURL, payload, nil)
	if err != nil {
		return fmt.Errorf("钉钉: %w", err)
	}
	var r struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if json.Unmarshal(b, &r) == nil && r.ErrCode != 0 {
		return fmt.Errorf("钉钉: errcode=%d %s", r.ErrCode, r.ErrMsg)
	}
	return nil
}

func sendFeishu(ctx context.Context, client *http.Client, cfg map[string]string, title, body string) error {
	payload := map[string]any{
		"msg_type": "text",
		"content":  map[string]any{"text": title + "\n" + body},
	}
	if secret := strings.TrimSpace(cfg["secret"]); secret != "" {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		mac := hmac.New(sha256.New, []byte(ts+"\n"+secret))
		mac.Write(nil)
		payload["timestamp"] = ts
		payload["sign"] = base64.StdEncoding.EncodeToString(mac.Sum(nil))
	}
	b, _, err := postJSON(ctx, client, strings.TrimSpace(cfg["webhook_url"]), payload, nil)
	if err != nil {
		return fmt.Errorf("飞书: %w", err)
	}
	var r struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if json.Unmarshal(b, &r) == nil && r.Code != 0 {
		return fmt.Errorf("飞书: code=%d %s", r.Code, r.Msg)
	}
	return nil
}

func sendServerChan(ctx context.Context, client *http.Client, cfg map[string]string, title, body string) error {
	key := strings.TrimSpace(cfg["sendkey"])
	payload := map[string]any{"title": title, "desp": body}
	b, _, err := postJSON(ctx, client, strings.TrimRight(serverChanBase, "/")+"/"+url.PathEscape(key)+".send", payload, nil)
	if err != nil {
		return fmt.Errorf("Server酱: %w", err)
	}
	var r struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(b, &r) == nil && r.Code != 0 {
		return fmt.Errorf("Server酱: code=%d %s", r.Code, r.Message)
	}
	return nil
}

func sendPushPlus(ctx context.Context, client *http.Client, cfg map[string]string, title, body string) error {
	payload := map[string]any{
		"token":    strings.TrimSpace(cfg["token"]),
		"title":    title,
		"content":  body,
		"template": "txt",
	}
	if ch := strings.TrimSpace(cfg["channel"]); ch != "" {
		payload["channel"] = ch
	}
	b, _, err := postJSON(ctx, client, pushPlusURL, payload, nil)
	if err != nil {
		return fmt.Errorf("PushPlus: %w", err)
	}
	var r struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if json.Unmarshal(b, &r) == nil && r.Code != 200 && r.Code != 0 {
		return fmt.Errorf("PushPlus: code=%d %s", r.Code, r.Msg)
	}
	return nil
}

func sendTelegram(ctx context.Context, client *http.Client, cfg map[string]string, title, body string) error {
	u := strings.TrimRight(telegramAPIBase, "/") + "/bot" + strings.TrimSpace(cfg["bot_token"]) + "/sendMessage"
	payload := map[string]any{
		"chat_id":                  strings.TrimSpace(cfg["chat_id"]),
		"text":                     truncate(title+"\n"+body, 4000),
		"disable_web_page_preview": true,
	}
	b, _, err := postJSON(ctx, client, u, payload, nil)
	if err != nil {
		return fmt.Errorf("Telegram: %w", err)
	}
	var r struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if json.Unmarshal(b, &r) == nil && !r.OK {
		return fmt.Errorf("Telegram: %s", r.Description)
	}
	return nil
}

func sendSlack(ctx context.Context, client *http.Client, cfg map[string]string, title, body string) error {
	payload := map[string]any{"text": truncate(title+"\n"+body, 3500)}
	b, _, err := postJSON(ctx, client, strings.TrimSpace(cfg["webhook_url"]), payload, nil)
	if err != nil {
		return fmt.Errorf("Slack: %w", err)
	}
	if s := strings.TrimSpace(string(b)); s != "" && !strings.HasPrefix(s, "ok") {
		return fmt.Errorf("Slack: %s", snippet(b))
	}
	return nil
}

func sendDiscord(ctx context.Context, client *http.Client, cfg map[string]string, title, body string) error {
	payload := map[string]any{"content": truncate(title+"\n"+body, 1900)}
	if _, _, err := postJSON(ctx, client, strings.TrimSpace(cfg["webhook_url"]), payload, nil); err != nil {
		return fmt.Errorf("Discord: %w", err)
	}
	return nil
}

func sendNtfy(ctx context.Context, client *http.Client, cfg map[string]string, title, body string) error {
	server := strings.TrimRight(cfgOr(cfg, "server", "https://ntfy.sh"), "/")
	priority := 3
	if v, err := strconv.Atoi(strings.TrimSpace(cfg["priority"])); err == nil && v >= 1 && v <= 5 {
		priority = v
	}
	payload := map[string]any{
		"topic":    strings.TrimSpace(cfg["topic"]),
		"title":    title,
		"message":  body,
		"priority": priority,
	}
	if tags := strings.TrimSpace(cfg["tags"]); tags != "" {
		var out []string
		for _, t := range strings.Split(tags, ",") {
			if t = strings.TrimSpace(t); t != "" {
				out = append(out, t)
			}
		}
		if len(out) > 0 {
			payload["tags"] = out
		}
	}
	headers := map[string]string{}
	if tok := strings.TrimSpace(cfg["token"]); tok != "" {
		headers["Authorization"] = "Bearer " + tok
	}
	if _, _, err := postJSON(ctx, client, server, payload, headers); err != nil {
		return fmt.Errorf("ntfy: %w", err)
	}
	return nil
}

func sendGotify(ctx context.Context, client *http.Client, cfg map[string]string, title, body string) error {
	server := strings.TrimRight(strings.TrimSpace(cfg["server"]), "/")
	priority := 5
	if v, err := strconv.Atoi(strings.TrimSpace(cfg["priority"])); err == nil && v >= 0 && v <= 10 {
		priority = v
	}
	u := server + "/message?token=" + url.QueryEscape(strings.TrimSpace(cfg["token"]))
	payload := map[string]any{"title": title, "message": body, "priority": priority}
	if _, _, err := postJSON(ctx, client, u, payload, nil); err != nil {
		return fmt.Errorf("Gotify: %w", err)
	}
	return nil
}

// pubHost 判断 webhook 地址的 host 是否为公网域名/IP。
// 渠道配置只有 channel.manage 令牌能改，但任务输出会经 /api/channels/{id}/test
// 回显目标响应，盲 SSRF 的代价不该由 webhook 承担，直接拒掉私网目标。
func pubHost(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("webhook 地址必须是 http(s)")
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			return fmt.Errorf("webhook 地址不能指向内网或本机地址")
		}
		return nil
	}
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	if h == "localhost" || strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".internal") {
		return fmt.Errorf("webhook 地址不能指向内网域名")
	}
	return nil
}

// allowPrivateWebhook 仅供测试注入（httptest 监听 127.0.0.1）。
// 每次调用时读取而不是 init 读死：跨包测试（scheduler_test）设环境变量的
// init 晚于本包的 var 初始化，读死会让旁路永远不生效。
func allowPrivateWebhook() bool {
	return os.Getenv("HOOKDOCK_TEST_ALLOW_PRIVATE_WEBHOOK") == "1"
}

func sendWebhook(ctx context.Context, client *http.Client, cfg map[string]string, msg Message) error {
	if !allowPrivateWebhook() {
		if err := pubHost(cfg["url"]); err != nil {
			return fmt.Errorf("通用 Webhook: %w", err)
		}
	}
	method := strings.ToUpper(strings.TrimSpace(cfg["method"]))
	if method != http.MethodPut {
		method = http.MethodPost
	}
	headers := map[string]string{}
	if raw := strings.TrimSpace(cfg["headers"]); raw != "" {
		if err := json.Unmarshal([]byte(raw), &headers); err != nil {
			return fmt.Errorf("通用 Webhook: headers 不是合法 JSON 对象")
		}
	}
	payload := map[string]any{
		"title":   msg.Title,
		"message": msg.Body,
		"event":   msg.Event,
		"status":  msg.Status,
		"job":     map[string]any{"id": msg.JobID, "name": msg.JobName},
		"run": map[string]any{
			"id": msg.RunID, "attempt": msg.Attempt, "max_attempts": msg.MaxAttempts,
			"exit_code": msg.ExitCode, "error": msg.Error, "output": msg.Output,
			"duration_sec": msg.DurationSec,
		},
		"finished_at": msg.FinishedAt,
	}
	if _, _, err := doJSON(ctx, client, method, strings.TrimSpace(cfg["url"]), payload, headers); err != nil {
		return fmt.Errorf("通用 Webhook: %w", err)
	}
	return nil
}

// ---------- SMTP ----------

func sendEmail(ctx context.Context, cfg map[string]string, subject, body string) error {
	host := strings.TrimSpace(cfg["smtp_host"])
	port := cfgOr(cfg, "smtp_port", "465")
	from := strings.TrimSpace(cfg["from"])
	user := strings.TrimSpace(cfg["username"])
	pass := cfg["password"]
	if _, err := strconv.Atoi(port); err != nil {
		return fmt.Errorf("邮件: smtp_port 非法")
	}
	to := splitAddrs(cfg["to"])
	if len(to) == 0 {
		return fmt.Errorf("邮件: to 为空")
	}
	if prefix := strings.TrimSpace(cfg["subject_prefix"]); prefix != "" {
		subject = prefix + " " + subject
	}
	raw := buildMail(from, to, subject, body)
	addr := net.JoinHostPort(host, port)
	var auth smtp.Auth
	if user != "" {
		auth = smtp.PlainAuth("", user, pass, host)
	}
	security := strings.ToLower(cfgOr(cfg, "security", "ssl"))
	d := net.Dialer{Timeout: 10 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("邮件: 连接 %s 失败: %w", addr, err)
	}
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	if security == "ssl" {
		tconn := tls.Client(conn, &tls.Config{ServerName: host})
		if err := tconn.Handshake(); err != nil {
			_ = conn.Close()
			return fmt.Errorf("邮件: TLS 握手失败: %w", err)
		}
		conn = tconn
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("邮件: SMTP 握手失败: %w", err)
	}
	defer c.Close()
	if security == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return fmt.Errorf("邮件: 服务器不支持 STARTTLS")
		}
		if err := c.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return fmt.Errorf("邮件: STARTTLS 失败: %w", err)
		}
	}
	if auth != nil {
		if ok, _ := c.Extension("AUTH"); ok {
			if err := c.Auth(auth); err != nil {
				return fmt.Errorf("邮件: 认证失败: %w", err)
			}
		}
	}
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("邮件: MAIL FROM 失败: %w", err)
	}
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			return fmt.Errorf("邮件: RCPT TO 失败: %w", err)
		}
	}
	wc, err := c.Data()
	if err != nil {
		return fmt.Errorf("邮件: DATA 失败: %w", err)
	}
	if _, err := wc.Write(raw); err != nil {
		_ = wc.Close()
		return fmt.Errorf("邮件: 写入正文失败: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("邮件: 发送失败: %w", err)
	}
	return c.Quit()
}

// stripHeaderUnsafe 把值截到第一个 CR/LF/NUL 之前：这些字节进 "From:"/"To:" 行
// 就是邮件头注入（可伪造 Bcc、拆出全新信头）。只删字符不够——剩下的尾巴会
// 拼成一个畸形地址（a@b.cBcc: evil@x.y）；截断才是"只取第一行"的正确语义。
func stripHeaderUnsafe(s string) string {
	if i := strings.IndexAny(s, "\r\n\x00"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func splitAddrs(v string) []string {
	var out []string
	for _, p := range strings.Split(stripHeaderUnsafe(v), ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func buildMail(from string, to []string, subject, body string) []byte {
	var b bytes.Buffer
	b.WriteString("From: " + stripHeaderUnsafe(from) + "\r\n")
	// 每个收件人单独剥：buildMail 是公开构造点，不能假设调用方已经过 splitAddrs。
	safeTo := make([]string, 0, len(to))
	for _, a := range to {
		if a = stripHeaderUnsafe(a); a != "" {
			safeTo = append(safeTo, a)
		}
	}
	b.WriteString("To: " + strings.Join(safeTo, ", ") + "\r\n")
	b.WriteString("Subject: " + mime.BEncoding.Encode("UTF-8", subject) + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n"))
	b.WriteString("\r\n")
	return b.Bytes()
}
