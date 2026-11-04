package scheduler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"hookdock/internal/notify"
	"hookdock/internal/store"
)

func init() { os.Setenv("HOOKDOCK_TEST_ALLOW_PRIVATE_WEBHOOK", "1") }

func TestSchedulerDispatchesOnlyOnFinalFailure(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "sched.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	var mu sync.Mutex
	var events []map[string]any
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		events = append(events, body)
		mu.Unlock()
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer receiver.Close()

	ch := &store.Channel{Name: "回调", Type: "webhook", Config: map[string]string{"url": receiver.URL}, Enabled: true}
	if err := st.CreateChannel(ch); err != nil {
		t.Fatal(err)
	}
	job := &store.Job{
		Name: "会失败的任务", Schedule: "@every 1m", ExecArgv: `["/bin/sh","-c","exit 3"]`,
		Payload: "{}", TimeoutSec: 10, MaxAttempts: 2, Enabled: true, NextRunAt: time.Now().Unix() - 1,
		Notify: []store.NotifyBinding{{ChannelID: ch.ID, Events: []string{"failure"}}},
	}
	if err := st.CreateJob(job); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := New(st, NewHub(), 2, notify.NewNotifier(st))
	s.Start(ctx)
	defer func() {
		cancel()
		s.Stop(10 * time.Second)
	}()

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(events)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 1 {
		t.Fatalf("want exactly 1 notification after retries exhausted, got %d", len(events))
	}
	if events[0]["event"] != "failure" || events[0]["status"] != "dead" {
		t.Fatalf("unexpected notification: %#v", events[0])
	}
	run := events[0]["run"].(map[string]any)
	if run["attempt"].(float64) != 2 {
		t.Fatalf("attempt = %v, want 2 (only final attempt notifies)", run["attempt"])
	}
}

func TestSchedulerDispatchesSuccess(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "sched2.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	got := make(chan string, 4)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		ev, _ := body["event"].(string)
		select {
		case got <- ev:
		default:
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer receiver.Close()

	ch := &store.Channel{Name: "回调", Type: "webhook", Config: map[string]string{"url": receiver.URL}, Enabled: true}
	_ = st.CreateChannel(ch)
	job := &store.Job{
		Name: "会成功的任务", Schedule: "@every 1m", ExecArgv: `["/bin/sh","-c","echo hello"]`,
		Payload: "{}", TimeoutSec: 10, MaxAttempts: 1, Enabled: true, NextRunAt: time.Now().Unix() - 1,
		Notify: []store.NotifyBinding{{ChannelID: ch.ID, Events: []string{"success"}}},
	}
	if err := st.CreateJob(job); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := New(st, NewHub(), 2, notify.NewNotifier(st))
	s.Start(ctx)
	defer func() {
		cancel()
		s.Stop(10 * time.Second)
	}()

	select {
	case ev := <-got:
		if ev != "success" {
			t.Fatalf("event = %s", ev)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("no success notification")
	}
}
