package session

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A refused turn waits for the pass already buying room, then sends the same
// question. The blocked model call makes the order observable without a clock.
func TestTurnWaitsForInFlightSummaryBeforeRecovering(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	refused := make(chan struct{})
	var requests atomic.Int32
	model := &summarizer{answer: func(_ int, messages []ai.Message) (*ai.Response, error) {
		if strings.HasPrefix(messageText(messages[0]), "You are compacting") {
			close(started)
			<-release
			return textResponse(summaryWords), nil
		}
		if requests.Add(1) == 1 {
			close(refused)
			return nil, refusalOver(16_384, 2_000)
		}
		return textResponse("ANSWER AFTER COMPACT"), nil
	}}
	agent, _ := newTestAgent(t, model, func(c *Config) { c.ContextWindow = 16_384 })
	personHeavy(agent, 10, 6_000)
	compacted := make(chan error, 1)
	go func() { compacted <- agent.Compact(context.Background()) }()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("summary did not start")
	}
	events := make(chan []Event, 1)
	go func() { events <- collect(t, mustSubmit(t, agent, "QUESTION DURING COMPACT")) }()
	select {
	case <-refused:
	case <-time.After(10 * time.Second):
		close(release)
		t.Fatal("turn never reached its first request")
	}
	select {
	case got := <-events:
		close(release)
		t.Fatalf("turn ended before the in-flight pass landed: %v", got)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-compacted; err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-events:
		for _, event := range got {
			if event.Kind == EventError {
				t.Fatalf("turn errored after the pass landed: %v", event.Err)
			}
		}
	case <-time.After(10 * time.Second):
		t.Fatal("turn did not finish after the pass landed")
	}
	if requests.Load() != 2 || model.calls() != 3 || !holdsText(liveTranscript(agent), "ANSWER AFTER COMPACT") {
		t.Fatalf("requests=%d total calls=%d; want one summary and an answered retry", requests.Load(), model.calls())
	}
}

func TestCancelledTurnStopsWaitingForInFlightSummary(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	refused := make(chan struct{})
	model := &summarizer{answer: func(_ int, messages []ai.Message) (*ai.Response, error) {
		if strings.HasPrefix(messageText(messages[0]), "You are compacting") {
			close(started)
			<-release
			return textResponse(summaryWords), nil
		}
		close(refused)
		return nil, refusalOver(16_384, 2_000)
	}}
	agent, _ := newTestAgent(t, model, func(c *Config) { c.ContextWindow = 16_384 })
	personHeavy(agent, 10, 6_000)
	compacted := make(chan error, 1)
	go func() { compacted <- agent.Compact(context.Background()) }()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	events, err := agent.Submit(ctx, "CANCEL DURING COMPACT")
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		for range events {
		}
		close(done)
	}()
	<-refused
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		close(release)
		t.Fatal("cancelled turn stayed behind the summary")
	}
	close(release)
	if err := <-compacted; err != nil {
		t.Fatal(err)
	}
}

// The screen joins scrollback above the live transcript's floor. A turn that
// finished during the summary must remain on that joined page after each pass.
