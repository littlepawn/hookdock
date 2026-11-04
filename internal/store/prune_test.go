package store

import (
	"testing"
)

func TestPruneRemovesOldFinishedRuns(t *testing.T) {
	s := openTest(t)
	job := &Job{Name: "demo", Schedule: "* * * * *", ExecArgv: `["echo","hi"]`, Enabled: true}
	if err := s.CreateJob(job); err != nil {
		t.Fatal(err)
	}

	oldID := NewID("run")
	recentID := NewID("run")
	queuedID := NewID("run")
	if err := s.EnqueueRun(oldID, job.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.EnqueueRun(recentID, job.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.EnqueueRun(queuedID, job.ID, 1); err != nil {
		t.Fatal(err)
	}

	old := int64(31 * 86400)
	if _, err := s.db.Exec(`UPDATE runs SET created_at=?, status='succeeded', exit_code=0 WHERE id=?`, now()-old, oldID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE runs SET created_at=?, status='succeeded', exit_code=0 WHERE id=?`, now()-3*86400, recentID); err != nil {
		t.Fatal(err)
	}

	n, err := s.Prune()
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatalf("expected at least 1 pruned row, got %d", n)
	}

	var c int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM runs WHERE id=?`, oldID).Scan(&c); err != nil {
		t.Fatal(err)
	}
	if c != 0 {
		t.Fatal("old finished run should be pruned")
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM runs WHERE id=?`, recentID).Scan(&c); err != nil {
		t.Fatal(err)
	}
	if c != 1 {
		t.Fatal("recent run should be kept")
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM runs WHERE id=?`, queuedID).Scan(&c); err != nil {
		t.Fatal(err)
	}
	if c != 1 {
		t.Fatal("queued run should be kept regardless of age")
	}
}
