package notify

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	os.Setenv("HOOKDOCK_TEST_ALLOW_PRIVATE_WEBHOOK", "1") // httptest 全在 127.0.0.1
	os.Exit(m.Run())
}

func testClient() *http.Client { return &http.Client{Timeout: 5 * time.Second} }

func int64p(v int64) *int64 { return &v }

func sampleMessage() Message {
	return Message{
		Title: "[hookdock] 任务失败 · 备份", Body: "任务：备份\n状态：失败（重试已用尽）\n退出码：1",
		Event: "failure", Status: "dead", JobID: "job_1", JobName: "备份", RunID: "run_1",
		ExitCode: int64p(1), Error: "退出码 1", Output: "boom", Attempt: 3, MaxAttempts: 3,
		DurationSec: 2, FinishedAt: time.Now().Unix(),
	}
}

func TestBark(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/push" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"code":200,"message":"success"}`))
	}))
	defer srv.Close()

	ch := Channel{Type: "bark", Config: map[string]string{"server": srv.URL, "key": "k123", "group": "hookdock", "level": "active"}}
	if err := Send(context.Background(), testClient(), ch, sampleMessage()); err != nil {
		t.Fatal(err)
	}
	if got["device_key"] != "k123" || got["group"] != "hookdock" || got["level"] != "active" {
		t.Fatalf("unexpected payload: %#v", got)
	}
	if got["title"] != sampleMessage().Title {
		t.Errorf("title = %v", got["title"])
	}
}

func TestWeCom(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer srv.Close()

	ch := Channel{Type: "wecom", Config: map[string]string{"webhook_url": srv.URL}}
	if err := Send(context.Background(), testClient(), ch, sampleMessage()); err != nil {
		t.Fatal(err)
	}
	if got["msgtype"] != "markdown" {
		t.Fatalf("unexpected payload: %#v", got)
	}
	md := got["markdown"].(map[string]any)
	if !strings.Contains(md["content"].(string), "任务失败") {
		t.Errorf("content missing title: %v", md["content"])
	}
}

func TestWeComBusinessError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errcode":40001,"errmsg":"invalid webhook url"}`))
	}))
	defer srv.Close()
	ch := Channel{Type: "wecom", Config: map[string]string{"webhook_url": srv.URL}}
	err := Send(context.Background(), testClient(), ch, sampleMessage())
	if err == nil || !strings.Contains(err.Error(), "40001") {
		t.Fatalf("want business error, got %v", err)
	}
}

func TestDingTalkSign(t *testing.T) {
	var q url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.Query()
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer srv.Close()

	secret := "SECabcdef"
	ch := Channel{Type: "dingtalk", Config: map[string]string{"webhook_url": srv.URL + "?access_token=x", "secret": secret}}
	if err := Send(context.Background(), testClient(), ch, sampleMessage()); err != nil {
		t.Fatal(err)
	}
	ts, sign := q.Get("timestamp"), q.Get("sign")
	if ts == "" || sign == "" {
		t.Fatalf("missing sign params: %v", q)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "\n" + secret))
	if want := base64.StdEncoding.EncodeToString(mac.Sum(nil)); want != sign {
		t.Fatalf("sign mismatch: got %s want %s", sign, want)
	}
	if q.Get("access_token") != "x" {
		t.Fatalf("access_token lost: %v", q)
	}
}

func TestFeishu(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"code":0,"msg":"success"}`))
	}))
	defer srv.Close()
	ch := Channel{Type: "feishu", Config: map[string]string{"webhook_url": srv.URL}}
	if err := Send(context.Background(), testClient(), ch, sampleMessage()); err != nil {
		t.Fatal(err)
	}
	if got["msg_type"] != "text" {
		t.Fatalf("unexpected payload: %#v", got)
	}
}

func TestServerChan(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/SCT123.send" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"code":0,"message":""}`))
	}))
	defer srv.Close()
	old := serverChanBase
	serverChanBase = srv.URL
	defer func() { serverChanBase = old }()

	ch := Channel{Type: "serverchan", Config: map[string]string{"sendkey": "SCT123"}}
	if err := Send(context.Background(), testClient(), ch, sampleMessage()); err != nil {
		t.Fatal(err)
	}
	if got["title"] != sampleMessage().Title {
		t.Fatalf("unexpected payload: %#v", got)
	}
}

func TestPushPlus(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"code":200,"msg":"ok"}`))
	}))
	defer srv.Close()
	old := pushPlusURL
	pushPlusURL = srv.URL
	defer func() { pushPlusURL = old }()

	ch := Channel{Type: "pushplus", Config: map[string]string{"token": "t", "channel": "mail"}}
	if err := Send(context.Background(), testClient(), ch, sampleMessage()); err != nil {
		t.Fatal(err)
	}
	if got["token"] != "t" || got["channel"] != "mail" {
		t.Fatalf("unexpected payload: %#v", got)
	}
}

func TestTelegram(t *testing.T) {
	var got map[string]any
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	old := telegramAPIBase
	telegramAPIBase = srv.URL
	defer func() { telegramAPIBase = old }()

	ch := Channel{Type: "telegram", Config: map[string]string{"bot_token": "123:ABC", "chat_id": "-100"}}
	if err := Send(context.Background(), testClient(), ch, sampleMessage()); err != nil {
		t.Fatal(err)
	}
	if path != "/bot123:ABC/sendMessage" {
		t.Fatalf("path = %s", path)
	}
	if got["chat_id"] != "-100" {
		t.Fatalf("unexpected payload: %#v", got)
	}
}

func TestSlack(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	ch := Channel{Type: "slack", Config: map[string]string{"webhook_url": srv.URL}}
	if err := Send(context.Background(), testClient(), ch, sampleMessage()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got["text"].(string), "任务失败") {
		t.Fatalf("unexpected payload: %#v", got)
	}
}

func TestDiscord(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	ch := Channel{Type: "discord", Config: map[string]string{"webhook_url": srv.URL}}
	if err := Send(context.Background(), testClient(), ch, sampleMessage()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got["content"].(string), "任务失败") {
		t.Fatalf("unexpected payload: %#v", got)
	}
}

func TestNtfy(t *testing.T) {
	var got map[string]any
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"id":"x"}`))
	}))
	defer srv.Close()
	ch := Channel{Type: "ntfy", Config: map[string]string{"server": srv.URL, "topic": "hook", "token": "tk", "priority": "5", "tags": "warning,robot"}}
	if err := Send(context.Background(), testClient(), ch, sampleMessage()); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer tk" {
		t.Fatalf("auth = %q", auth)
	}
	if got["topic"] != "hook" || got["priority"].(float64) != 5 {
		t.Fatalf("unexpected payload: %#v", got)
	}
}

func TestGotify(t *testing.T) {
	var got map[string]any
	var token string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token = r.URL.Query().Get("token")
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer srv.Close()
	ch := Channel{Type: "gotify", Config: map[string]string{"server": srv.URL, "token": "abc", "priority": "8"}}
	if err := Send(context.Background(), testClient(), ch, sampleMessage()); err != nil {
		t.Fatal(err)
	}
	if token != "abc" {
		t.Fatalf("token = %q", token)
	}
	if got["priority"].(float64) != 8 {
		t.Fatalf("unexpected payload: %#v", got)
	}
}

func TestWebhook(t *testing.T) {
	var got map[string]any
	var hdr string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdr = r.Header.Get("X-Custom")
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	ch := Channel{Type: "webhook", Config: map[string]string{
		"url": srv.URL, "method": "POST", "headers": `{"X-Custom":"yes"}`,
	}}
	if err := Send(context.Background(), testClient(), ch, sampleMessage()); err != nil {
		t.Fatal(err)
	}
	if hdr != "yes" {
		t.Fatalf("header = %q", hdr)
	}
	if got["status"] != "dead" || got["event"] != "failure" {
		t.Fatalf("unexpected payload: %#v", got)
	}
	job := got["job"].(map[string]any)
	if job["name"] != "备份" {
		t.Fatalf("unexpected job: %#v", job)
	}
}

func TestEmail(t *testing.T) {
	addr, ch := startFakeSMTP(t)
	host, port, _ := net.SplitHostPort(addr)
	c := Channel{Type: "email", Config: map[string]string{
		"smtp_host": host, "smtp_port": port, "security": "none",
		"from": "hookdock@example.com", "to": "a@example.com,b@example.com",
		"subject_prefix": "[hookdock]",
	}}
	if err := Send(context.Background(), testClient(), c, sampleMessage()); err != nil {
		t.Fatal(err)
	}
	select {
	case data := <-ch:
		if !strings.Contains(data, "Subject: =?UTF-8?") {
			t.Errorf("subject not encoded: %q", data)
		}
		if !strings.Contains(data, "任务：备份") {
			t.Errorf("body missing: %q", data)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no mail data")
	}
}

func TestValidate(t *testing.T) {
	if err := Validate("bark", map[string]string{}); err == nil {
		t.Fatal("bark without key should fail")
	}
	if err := Validate("nope", nil); err == nil {
		t.Fatal("unknown type should fail")
	}
	if err := Validate("webhook", map[string]string{"url": "ftp://x"}); err == nil {
		t.Fatal("non-http url should fail")
	}
	if err := Validate("telegram", map[string]string{"bot_token": "t", "chat_id": "1"}); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogComplete(t *testing.T) {
	for _, d := range Catalog() {
		if d.Type == "" || d.Label == "" || d.Group == "" {
			t.Fatalf("incomplete type def: %#v", d)
		}
		for _, f := range d.Fields {
			if f.Key == "" || f.Label == "" {
				t.Fatalf("incomplete field in %s: %#v", d.Type, f)
			}
		}
	}
}

// ---------- fake SMTP ----------

func startFakeSMTP(t *testing.T) (string, chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	data := make(chan string, 1)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go fakeSMTPConn(conn, data)
		}
	}()
	return ln.Addr().String(), data
}

func fakeSMTPConn(conn net.Conn, data chan<- string) {
	defer conn.Close()
	br := bufio.NewReader(conn)
	bw := bufio.NewWriter(conn)
	write := func(s string) { _, _ = bw.WriteString(s); _ = bw.Flush() }
	write("220 fake ESMTP\r\n")
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"):
			write("250-fake\r\n250 OK\r\n")
		case strings.HasPrefix(cmd, "HELO"):
			write("250 OK\r\n")
		case strings.HasPrefix(cmd, "MAIL FROM"), strings.HasPrefix(cmd, "RCPT TO"), cmd == "RSET", cmd == "NOOP":
			write("250 OK\r\n")
		case cmd == "DATA":
			write("354 End data with <CR><LF>.<CR><LF>\r\n")
			var b strings.Builder
			for {
				l, err := br.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimRight(l, "\r\n") == "." {
					break
				}
				_, _ = io.WriteString(&b, l)
			}
			select {
			case data <- b.String():
			default:
			}
			write("250 OK\r\n")
		case cmd == "QUIT":
			write("221 Bye\r\n")
			return
		default:
			write("250 OK\r\n")
		}
	}
}

// 邮件头注入：from/to 里的 CR/LF 必须在拼信头前被剥掉，
// 否则配置里一个换行就能伪造 Bcc、拆出全新信头。
func TestBuildMailStripsHeaderInjection(t *testing.T) {
	raw := string(buildMail("a@b.c\r\nBcc: evil@x.y", []string{"d@e.f\nX-Evil: 1"}, "标题", "正文"))
	if strings.Contains(raw, "Bcc:") {
		t.Fatalf("Bcc 注入没被剥掉: %q", raw)
	}
	if strings.Contains(raw, "X-Evil:") {
		t.Fatalf("X-Evil 注入没被剥掉: %q", raw)
	}
	head := raw[:strings.Index(raw, "\r\n\r\n")]
	if n := strings.Count(head, "\r\n"); n != 6 {
		t.Fatalf("信头行数 %d，期望 6（From/To/Subject/Date/MIME-Version/Content-Type/Content-Transfer-Encoding 共 7 行→6 个分隔）: %q", n, head)
	}
}

// webhook 地址指向内网/本机必须拒（盲 SSRF）。
func TestWebhookPrivateHostRejected(t *testing.T) {
	os.Unsetenv("HOOKDOCK_TEST_ALLOW_PRIVATE_WEBHOOK")
	defer os.Setenv("HOOKDOCK_TEST_ALLOW_PRIVATE_WEBHOOK", "1")
	for _, u := range []string{
		"http://127.0.0.1:9999/x", "http://192.168.50.1/x", "http://10.0.0.1/x",
		"http://169.254.169.254/latest/meta-data", "http://localhost:8080/x",
		"http://nas.local/x", "http://foo.internal/x",
	} {
		ch := Channel{Type: "webhook", Config: map[string]string{"url": u}}
		if err := Send(context.Background(), testClient(), ch, sampleMessage()); err == nil {
			t.Fatalf("%s 应被拒但放行了", u)
		}
	}
	if err := pubHost("https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=x"); err != nil {
		t.Fatalf("公网地址被误拒: %v", err)
	}
}
