package session

import (
	"context"
	"errors"
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

// The cancellation reservation exists before the pass takes its compaction
// gate. A second caller must not steal that gap and start uncancellable work.
func TestManualSummaryReservationRejectsAnotherPassBeforeStart(t *testing.T) {
	model := &summarizer{}
	agent, _ := summaryAgent(t, model)
	journaledPersonHeavy(agent, 1, 12, 20_000)
	agent.mu.Lock()
	agent.manualPass = &manualPassCut{cancel: func() {}}
	agent.mu.Unlock()
	defer func() {
		agent.mu.Lock()
		agent.manualPass = nil
		agent.mu.Unlock()
	}()
	if err := agent.CompactObserved(context.Background(), nil); !errors.Is(err, ErrCompactionInFlight) {
		t.Fatalf("second manual pass = %v, want ErrCompactionInFlight", err)
	}
	if n := model.calls(); n != 0 {
		t.Fatalf("second caller reached provider %d times before first pass started", n)
	}
}

// Cancellation can return before a provider goroutine has handed its error
// back. Close must give that pass the same bounded journal grace as a turn.
type closingSummaryModel struct {
	started chan struct{}
	cut     chan struct{}
	release chan struct{}
}

func (m *closingSummaryModel) CompleteWithMessages(ctx context.Context, _ []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	close(m.started)
	<-ctx.Done()
	close(m.cut)
	<-m.release
	return nil, ctx.Err()
}

func TestCloseJoinsAManualSummaryBeforeClosingItsJournal(t *testing.T) {
	model := &closingSummaryModel{started: make(chan struct{}), cut: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(model.release) }) }
	defer release()
	agent, path := summaryAgent(t, model)
	journaledPersonHeavy(agent, 1, 12, 20_000)
	compacted := make(chan error, 1)
	go func() { compacted <- agent.CompactObserved(context.Background(), nil) }()
	select {
	case <-model.started:
	case <-time.After(5 * time.Second):
		t.Fatal("manual summary did not start")
	}
	closed := make(chan error, 1)
	go func() { closed <- agent.Close() }()
	select {
	case <-model.cut:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not cancel the summary request")
	}
	select {
	case err := <-closed:
		t.Fatalf("Close returned before the summary could journal its final fact: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	release()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not join the canceled summary")
	}
	select {
	case <-compacted:
	case <-time.After(5 * time.Second):
		t.Fatal("manual summary did not finish")
	}
	record := ReadTranscript(path)
	if len(record.Passes) == 0 || record.Passes[len(record.Passes)-1].Failed != SummaryFailedInterrupted {
		t.Fatal("the canceled summary's final fact was lost at Close")
	}
}
