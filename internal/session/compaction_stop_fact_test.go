package session

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A manual retry has no Submit context. Stop must still cancel the real model
// request, rather than only changing what the surface draws while it keeps running.
type stoppableSummaryModel struct {
	once    sync.Once
	started chan struct{}
}

func (m *stoppableSummaryModel) CompleteWithMessages(ctx context.Context, _ []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	m.once.Do(func() { close(m.started) })
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestStopCancelsAManualSummaryRetry(t *testing.T) {
	model := &stoppableSummaryModel{started: make(chan struct{})}
	agent, _ := summaryAgent(t, model)
	journaledPersonHeavy(agent, 1, 12, 20_000)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- agent.CompactObserved(ctx, nil) }()
	select {
	case <-model.started:
	case <-time.After(5 * time.Second):
		t.Fatal("manual summary did not reach the provider")
	}
	agent.Interrupt()
	select {
	case <-done:
	case <-time.After(time.Second):
		cancel()
		<-done
		t.Fatal("Stop left the manual summary request running")
	}
	if agent.ContextUse().Summarizing {
		t.Fatal("manual summary still reports running after Stop")
	}
}
