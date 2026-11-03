package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"hookdock/internal/notify"
	"hookdock/internal/scheduler"
	"hookdock/internal/store"
)

func newTestServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv := New(st, scheduler.NewHub(), notify.NewNotifier(st))
	mux := http.NewServeMux()
	srv.Routes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, st
}

func call(t *testing.T, ts *httptest.Server, method, path, token string, body any) (int, []byte) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, ts.URL+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("X-Api-Token", token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func decode(t *testing.T, b []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("decode %s: %v", b, err)
	}
}

func TestChannelAPIAndTestSend(t *testing.T) {
	ts, _ := newTestServer(t)

	var hits int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer receiver.Close()

	code, b := call(t, ts, http.MethodPost, "/api/channels", "", map[string]any{
		"name": "本地回调", "type": "webhook",
		"config": map[string]string{"url": receiver.URL, "method": "POST"},
	})
	if code != 200 {
		t.Fatalf("create channel: %d %s", code, b)
	}
	var ch store.Channel
	decode(t, b, &ch)
	if ch.ID == 0 || ch.Type != "webhook" {
		t.Fatalf("unexpected channel: %#v", ch)
	}

	code, b = call(t, ts, http.MethodGet, "/api/channels", "", nil)
	if code != 200 {
		t.Fatalf("list: %d %s", code, b)
	}
	var list []store.Channel
	decode(t, b, &list)
	if len(list) != 1 || list[0].Config["url"] != receiver.URL {
		t.Fatalf("unexpected list: %#v", list)
	}

	code, b = call(t, ts, http.MethodPut, "/api/channels/"+strconv.FormatInt(ch.ID, 10), "", map[string]any{
		"name": "改名后的回调",
	})
	if code != 200 {
		t.Fatalf("update: %d %s", code, b)
	}
	var up store.Channel
	decode(t, b, &up)
	if up.Name != "改名后的回调" || up.Config["url"] != receiver.URL {
		t.Fatalf("merge failed: %#v", up)
	}

	code, b = call(t, ts, http.MethodPost, "/api/channels/"+strconv.FormatInt(ch.ID, 10)+"/test", "", nil)
	if code != 200 {
		t.Fatalf("test send: %d %s", code, b)
	}
	waitFor(t, func() bool { return atomic.LoadInt32(&hits) == 1 })

	code, _ = call(t, ts, http.MethodDelete, "/api/channels/"+strconv.FormatInt(ch.ID, 10), "", nil)
	if code != 200 {
		t.Fatalf("delete: %d", code)
	}
}

func TestChannelValidation(t *testing.T) {
	ts, _ := newTestServer(t)
	cases := []struct {
		name string
		body map[string]any
		want int
	}{
		{"missing name", map[string]any{"type": "bark", "config": map[string]string{"key": "x"}}, 400},
		{"unknown type", map[string]any{"name": "x", "type": "nope", "config": map[string]string{}}, 400},
		{"missing required", map[string]any{"name": "x", "type": "bark", "config": map[string]string{}}, 400},
		{"bad url", map[string]any{"name": "x", "type": "webhook", "config": map[string]string{"url": "ftp://x"}}, 400},
	}
	for _, c := range cases {
		code, b := call(t, ts, http.MethodPost, "/api/channels", "", c.body)
		if code != c.want {
			t.Errorf("%s: got %d %s want %d", c.name, code, b, c.want)
		}
	}
}

func TestJobNotifyBinding(t *testing.T) {
	ts, _ := newTestServer(t)
	code, b := call(t, ts, http.MethodPost, "/api/channels", "", map[string]any{
		"name": "回调", "type": "webhook", "config": map[string]string{"url": "http://127.0.0.1:1/x"},
	})
	if code != 200 {
		t.Fatalf("channel: %d %s", code, b)
	}
	var ch store.Channel
	decode(t, b, &ch)

	code, b = call(t, ts, http.MethodPost, "/api/jobs", "", map[string]any{
		"name": "任务", "schedule": "@every 5m", "exec_argv": `["true"]`,
		"notify": []map[string]any{{"channel_id": ch.ID, "events": []string{"failure"}}},
	})
	if code != 200 {
		t.Fatalf("job: %d %s", code, b)
	}
	var job store.Job
	decode(t, b, &job)
	if len(job.Notify) != 1 || job.Notify[0].ChannelID != ch.ID || job.Notify[0].Events[0] != "failure" {
		t.Fatalf("notify not saved: %#v", job.Notify)
	}

	code, b = call(t, ts, http.MethodGet, "/api/jobs/"+job.ID, "", nil)
	if code != 200 {
		t.Fatalf("get job: %d %s", code, b)
	}
	var got store.Job
	decode(t, b, &got)
	if len(got.Notify) != 1 {
		t.Fatalf("notify not returned: %#v", got)
	}

	bad := []map[string]any{
		{"channel_id": 99999, "events": []string{"failure"}},
		{"channel_id": ch.ID, "events": []string{"nope"}},
	}
	for _, n := range bad {
		code, b = call(t, ts, http.MethodPost, "/api/jobs", "", map[string]any{
			"name": "bad", "schedule": "@every 5m", "exec_argv": `["true"]`, "notify": []map[string]any{n},
		})
		if code != 400 {
			t.Errorf("bad binding %v: got %d %s", n, code, b)
		}
	}
}

func TestChannelConfigMaskedForNonManager(t *testing.T) {
	ts, st := newTestServer(t)
	code, b := call(t, ts, http.MethodPost, "/api/channels", "", map[string]any{
		"name": "回调", "type": "webhook", "config": map[string]string{"url": "http://127.0.0.1:1/x"},
	})
	if code != 200 {
		t.Fatalf("channel: %d %s", code, b)
	}

	plain := "hk_test_operator"
	sum := sha256.Sum256([]byte(plain))
	tok := &store.Token{Name: "值班", Role: "operator", Enabled: true}
	if err := st.CreateToken(tok, hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}

	code, b = call(t, ts, http.MethodGet, "/api/channels", plain, nil)
	if code != 200 {
		t.Fatalf("list as operator: %d %s", code, b)
	}
	var list []store.Channel
	decode(t, b, &list)
	if len(list) != 1 || len(list[0].Config) != 0 {
		t.Fatalf("config should be masked: %#v", list)
	}

	code, b = call(t, ts, http.MethodPost, "/api/channels", plain, map[string]any{
		"name": "x", "type": "bark", "config": map[string]string{"key": "k"},
	})
	if code != 403 {
		t.Fatalf("operator create should be 403, got %d %s", code, b)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}
