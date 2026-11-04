package scheduler

import (
	"context"
	"encoding/json"
	"log"
	"math/rand"
	"sync"
	"time"

	"hookdock/internal/executor"
	"hookdock/internal/notify"
	"hookdock/internal/store"
)

type Hub struct {
	mu   sync.Mutex
	subs map[chan string]struct{}
}

func NewHub() *Hub { return &Hub{subs: map[chan string]struct{}{}} }

func (h *Hub) Sub() chan string {
	ch := make(chan string, 64)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *Hub) Unsub(ch chan string) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
	close(ch)
}

func (h *Hub) Publish(event string, payload any) {
	b, _ := json.Marshal(map[string]any{"type": event, "data": payload, "at": time.Now().Unix()})
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- string(b):
		default: // 慢客户端丢弃，不阻塞调度
		}
	}
}

type Scheduler struct {
	st       *store.Store
	hub      *Hub
	workers  int
	notifier *notify.Notifier
	inflight sync.WaitGroup // 正在执行的 run
	loopWG   sync.WaitGroup // tickLoop/runLoop/pruneLoop 本身
	stopped  chan struct{}
	stopOnce sync.Once
}

func New(st *store.Store, hub *Hub, workers int, notifier *notify.Notifier) *Scheduler {
	if workers <= 0 {
		workers = 4
	}
	return &Scheduler{st: st, hub: hub, workers: workers, notifier: notifier, stopped: make(chan struct{})}
}

func (s *Scheduler) Start(ctx context.Context) {
	s.loopWG.Add(3)
	go func() { defer s.loopWG.Done(); s.tickLoop(ctx) }()
	go func() { defer s.loopWG.Done(); s.runLoop(ctx) }()
	go func() { defer s.loopWG.Done(); s.pruneLoop(ctx) }()
}

// Stop 先停派发循环，再等正在跑的 run 收尾。
// 三个循环本身也要等：只 close(stopped)+inflight.Wait 的话，
// runLoop 可能在 Wait 之后又抢到一条 run 并 Add(1)，Wait 已经返回，
// 进程直接退出留下 running 行。带超时是因为执行器可能卡在被托管进程上。
func (s *Scheduler) Stop(timeout time.Duration) {
	s.stopOnce.Do(func() { close(s.stopped) })
	loopDone := make(chan struct{})
	go func() { s.loopWG.Wait(); close(loopDone) }()
	select {
	case <-loopDone:
	case <-time.After(timeout):
		log.Printf("调度循环 %s 内没退干净，继续收尾", timeout)
	}
	inflightDone := make(chan struct{})
	go func() { s.inflight.Wait(); close(inflightDone) }()
	select {
	case <-inflightDone:
	case <-time.After(timeout):
		log.Printf("还有执行未收尾（超过 %s），交给下一次启动的 running 恢复处理", timeout)
	}
}

// tickLoop: 每秒扫到期 job → 入队 run；顺带处理 misfire 补跑
func (s *Scheduler) tickLoop(ctx context.Context) {
	tk := time.NewTicker(time.Second)
	defer tk.Stop()
	// 启动即补跑一次（misfire）
	s.fireDue()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopped:
			return
		case <-tk.C:
			s.fireDue()
		}
	}
}

func (s *Scheduler) fireDue() {
	nowT := time.Now()
	jobs, err := s.st.DueJobs(nowT.Unix())
	if err != nil {
		log.Printf("due jobs: %v", err)
		return
	}
	for _, j := range jobs {
		sch, err := Parse(j.Schedule)
		if err != nil {
			log.Printf("job %s 非法 schedule %q: %v", j.ID, j.Schedule, err)
			_ = s.st.SetJobEnabled(j.ID, false, 0)
			continue
		}
		// 先推进 next_run_at，避免重复触发
		next := sch.Next(nowT)
		_ = s.st.SetJobEnabled(j.ID, j.Enabled, next.Unix())

		runID := store.NewID("run")
		if err := s.st.EnqueueRun(runID, j.ID, j.MaxAttempts); err != nil {
			log.Printf("enqueue run: %v", err)
			continue
		}
		s.hub.Publish("run.queued", map[string]any{"run_id": runID, "job_id": j.ID, "job_name": j.Name})
	}
}

// runLoop: 消费 queued run
func (s *Scheduler) runLoop(ctx context.Context) {
	sem := make(chan struct{}, s.workers)
	tk := time.NewTicker(500 * time.Millisecond)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopped:
			return
		case <-tk.C:
		}
		runs, err := s.st.DueRuns(time.Now().Unix())
		if err != nil {
			log.Printf("due runs: %v", err)
			continue
		}
		for _, r := range runs {
			ok, err := s.st.ClaimRun(r.ID)
			if err != nil || !ok {
				continue
			}
			sem <- struct{}{}
			s.inflight.Add(1)
			go func(run store.Run) {
				defer func() { <-sem; s.inflight.Done() }()
				s.execute(ctx, run)
			}(r)
		}
	}
}

func (s *Scheduler) execute(ctx context.Context, run store.Run) {
	job, err := s.st.GetJob(run.JobID)
	if err != nil || job == nil {
		_ = s.st.FinishRun(run.ID, "failed", nil, "", false, "任务不存在")
		s.hub.Publish("run.finished", map[string]any{"run_id": run.ID, "status": "failed"})
		return
	}
	var argv []string
	if err := json.Unmarshal([]byte(job.ExecArgv), &argv); err != nil || len(argv) == 0 {
		_ = s.st.FinishRun(run.ID, "failed", nil, "", false, "exec_argv 非法")
		s.hub.Publish("run.finished", map[string]any{"run_id": run.ID, "status": "failed"})
		return
	}

	vars := map[string]any{}
	if job.Payload != "" && job.Payload != "{}" {
		var p map[string]any
		if err := json.Unmarshal([]byte(job.Payload), &p); err == nil {
			vars["payload"] = p
		}
	}
	vars["job"] = map[string]any{"id": job.ID, "name": job.Name}
	vars["run"] = map[string]any{"id": run.ID, "attempt": float64(run.Attempt)}

	expanded := executor.Expand(argv, vars)
	env := map[string]string{
		"HOOKDOCK_JOB_ID":   job.ID,
		"HOOKDOCK_JOB_NAME": job.Name,
		"HOOKDOCK_RUN_ID":   run.ID,
		"HOOKDOCK_ATTEMPT":  itoa(run.Attempt),
		"HOOKDOCK_PAYLOAD":  job.Payload,
	}

	s.hub.Publish("run.started", map[string]any{"run_id": run.ID, "job_id": job.ID, "job_name": job.Name, "attempt": run.Attempt})
	startedAt := time.Now().Unix()
	res := executor.Run(ctx, expanded, env, job.TimeoutSec)
	finishedAt := time.Now().Unix()

	status := "succeeded"
	if res.TimedOut {
		status = "timeout"
	} else if res.ExitCode != 0 || res.Err != "" {
		status = "failed"
	}

	if status != "succeeded" {
		if run.Attempt < run.MaxAttempts {
			backoff := int64(1) << uint(run.Attempt) // 2,4,8...
			backoff += int64(rand.Intn(3))
			nextAt := time.Now().Unix() + backoff
			_ = s.st.RetryRun(run.ID, run.Attempt+1, nextAt, res.Err, res.Output)
			s.hub.Publish("run.retry", map[string]any{
				"run_id": run.ID, "status": status, "attempt": run.Attempt,
				"next_attempt_at": nextAt, "error": res.Err,
			})
			return
		}
		status = "dead"
	}

	var code *int64
	c := res.ExitCode
	code = &c
	_ = s.st.FinishRun(run.ID, status, code, res.Output, res.Truncated, res.Err)
	s.hub.Publish("run.finished", map[string]any{
		"run_id": run.ID, "job_id": job.ID, "job_name": job.Name,
		"status": status, "exit_code": res.ExitCode, "error": res.Err,
	})

	// 只在终态推送：成功，或重试用尽后的死信。
	if status == "succeeded" || status == "dead" {
		s.notifier.Dispatch(job, &run, status, code, res.Err, res.Output, startedAt, finishedAt)
	}

	// 一次性任务：终态（成功或耗尽重试）即自动停用，不再按 schedule 重复
	if job.OneShot && (status == "succeeded" || status == "dead") {
		_ = s.st.SetJobEnabled(job.ID, false, 0)
		s.hub.Publish("job.disabled", map[string]any{
			"job_id": job.ID, "job_name": job.Name, "reason": "oneshot 已完成",
		})
	}
}

func (s *Scheduler) pruneLoop(ctx context.Context) {
	tk := time.NewTicker(time.Hour)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopped:
			return
		case <-tk.C:
			if n, err := s.st.Prune(); err == nil && n > 0 {
				log.Printf("pruned %d requests", n)
			}
		}
	}
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
