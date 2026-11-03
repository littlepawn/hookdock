package notify

import (
	"context"
	"log"
	"net/http"
	"time"

	"hookdock/internal/store"
)

// Notifier 从库里读渠道配置并投递。发送异步进行，不阻塞任务执行。
type Notifier struct {
	st      *store.Store
	client  *http.Client
	timeout time.Duration
}

func NewNotifier(st *store.Store) *Notifier {
	return &Notifier{
		st:      st,
		client:  &http.Client{Timeout: 15 * time.Second},
		timeout: 20 * time.Second,
	}
}

// Dispatch 按任务绑定的渠道与事件发送通知。调用方只传终态（succeeded / dead）。
func (n *Notifier) Dispatch(job *store.Job, run *store.Run, status string, exitCode *int64,
	errMsg, output string, startedAt, finishedAt int64) {
	if n == nil || job == nil || len(job.Notify) == 0 {
		return
	}
	event := "success"
	if status != "succeeded" {
		event = "failure"
	}
	msg := BuildRunMessage(job.Name, job.ID, run.ID, status, exitCode, errMsg, output,
		run.Attempt, run.MaxAttempts, startedAt, finishedAt)
	for _, b := range job.Notify {
		if !eventAllowed(b.Events, event) {
			continue
		}
		ch, err := n.st.GetChannel(b.ChannelID)
		if err != nil || ch == nil || !ch.Enabled {
			continue
		}
		go n.sendOne(ch, msg)
	}
}

// Test 同步发送一条测试消息，供 /api/channels/{id}/test 直接返回结果。
func (n *Notifier) Test(ch *store.Channel) error {
	if n == nil || ch == nil {
		return nil
	}
	msg := TestMessage(ch.Name)
	ctx, cancel := context.WithTimeout(context.Background(), n.timeout)
	defer cancel()
	err := Send(ctx, n.client, toChannel(ch), msg)
	errText := ""
	if err != nil {
		errText = err.Error()
	}
	n.st.RecordChannelResult(ch.ID, errText)
	return err
}

func (n *Notifier) sendOne(ch *store.Channel, msg Message) {
	ctx, cancel := context.WithTimeout(context.Background(), n.timeout)
	defer cancel()
	err := Send(ctx, n.client, toChannel(ch), msg)
	errText := ""
	if err != nil {
		errText = err.Error()
		// 只记录渠道名与错误，不打印配置（可能含 key）
		log.Printf("推送失败 %s(%s): %v", ch.Name, ch.Type, err)
	}
	n.st.RecordChannelResult(ch.ID, errText)
}

func toChannel(ch *store.Channel) Channel {
	return Channel{ID: ch.ID, Name: ch.Name, Type: ch.Type, Config: ch.Config, Enabled: ch.Enabled}
}

func eventAllowed(events []string, event string) bool {
	if len(events) == 0 {
		return true
	}
	for _, e := range events {
		if e == event {
			return true
		}
	}
	return false
}
