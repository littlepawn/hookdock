package store

import (
	"path/filepath"
	"testing"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestChannelCRUD(t *testing.T) {
	s := openTest(t)
	ch := &Channel{Name: "我的 iPhone", Type: "bark", Config: map[string]string{"key": "abc"}, Enabled: true}
	if err := s.CreateChannel(ch); err != nil {
		t.Fatal(err)
	}
	if ch.ID == 0 {
		t.Fatal("id not set")
	}
	got, err := s.GetChannel(ch.ID)
	if err != nil || got == nil {
		t.Fatalf("get: %v %v", got, err)
	}
	if got.Name != ch.Name || got.Type != "bark" || got.Config["key"] != "abc" || !got.Enabled {
		t.Fatalf("unexpected channel: %#v", got)
	}

	got.Name = "改名"
	got.Config = map[string]string{"key": "xyz", "group": "ops"}
	got.Enabled = false
	if err := s.UpdateChannel(got); err != nil {
		t.Fatal(err)
	}
	again, _ := s.GetChannel(ch.ID)
	if again.Name != "改名" || again.Config["group"] != "ops" || again.Enabled {
		t.Fatalf("update not persisted: %#v", again)
	}

	s.RecordChannelResult(ch.ID, "")
	again, _ = s.GetChannel(ch.ID)
	if again.LastSentAt == 0 || again.LastError != "" {
		t.Fatalf("result not recorded: %#v", again)
	}
	s.RecordChannelResult(ch.ID, "boom")
	again, _ = s.GetChannel(ch.ID)
	if again.LastError != "boom" {
		t.Fatalf("error not recorded: %#v", again)
	}

	if err := s.DeleteChannel(ch.ID); err != nil {
		t.Fatal(err)
	}
	gone, _ := s.GetChannel(ch.ID)
	if gone != nil {
		t.Fatal("channel not deleted")
	}
}

func TestJobNotifyRoundtrip(t *testing.T) {
	s := openTest(t)
	j := &Job{
		Name: "备份", Schedule: "@every 5m", ExecArgv: `["/bin/true"]`, Payload: "{}",
		Notify: []NotifyBinding{
			{ChannelID: 1, Events: []string{"failure"}},
			{ChannelID: 2, Events: []string{"success", "failure"}},
		},
		Enabled: true,
	}
	if err := s.CreateJob(j); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetJob(j.ID)
	if err != nil || got == nil {
		t.Fatalf("get job: %v %v", got, err)
	}
	if len(got.Notify) != 2 || got.Notify[0].ChannelID != 1 || got.Notify[1].Events[1] != "failure" {
		t.Fatalf("notify not persisted: %#v", got.Notify)
	}
	got.Notify = []NotifyBinding{{ChannelID: 3, Events: []string{"success"}}}
	if err := s.UpdateJob(got); err != nil {
		t.Fatal(err)
	}
	again, _ := s.GetJob(j.ID)
	if len(again.Notify) != 1 || again.Notify[0].ChannelID != 3 {
		t.Fatalf("notify update not persisted: %#v", again.Notify)
	}

	jobs, err := s.ListJobs()
	if err != nil || len(jobs) != 1 || len(jobs[0].Notify) != 1 {
		t.Fatalf("list jobs notify missing: %#v %v", jobs, err)
	}
}

func TestParseNotifyJSON(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0}, {"null", 0}, {"[]", 0}, {"garbage", 0},
		{`[{"channel_id":1,"events":["failure"]}]`, 1},
	}
	for _, c := range cases {
		if got := ParseNotifyJSON(c.in); len(got) != c.want {
			t.Errorf("ParseNotifyJSON(%q) len=%d want %d", c.in, len(got), c.want)
		}
	}
}

func TestOpenTwiceMigratesIdempotently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "twice.db")
	s1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ch := &Channel{Name: "x", Type: "bark", Config: map[string]string{"key": "1"}, Enabled: true}
	if err := s1.CreateChannel(ch); err != nil {
		t.Fatal(err)
	}
	j := &Job{Name: "j", Schedule: "@every 1m", ExecArgv: `["true"]`, Payload: "{}", Enabled: true,
		Notify: []NotifyBinding{{ChannelID: ch.ID, Events: []string{"failure"}}}}
	if err := s1.CreateJob(j); err != nil {
		t.Fatal(err)
	}
	_ = s1.Close()

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	defer s2.Close()
	got, _ := s2.GetJob(j.ID)
	if got == nil || len(got.Notify) != 1 {
		t.Fatalf("data lost after reopen: %#v", got)
	}
}
