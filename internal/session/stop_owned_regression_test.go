package session

import (
	"context"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestReviewStopCancelsOwnedManagerBackgroundWork(t *testing.T) {
	var stopped atomic.Bool
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.JobBudget = &liveBudgetStub{cap: 10}
		c.FactoryWorkRefusal = func() string {
			if stopped.Load() {
				return factory.PersonStopReason
			}
			return ""
		}
	})
	id := startJob(t, a, "sleep 30")
	stopped.Store(true)
	if a.automaticFactoryRefusal() == "" {
		t.Fatal("the durable Stop was not visible to the manager")
	}
	// The existing budget watcher polls every 100 ms. Give it three polls.
	time.Sleep(350 * time.Millisecond)
	if a.jobs.find(id).running() {
		t.Fatal("person Stop is visible, but the manager's owned background command still runs")
	}
}

func TestPersonStopPreservesManagerPersonChat(t *testing.T) {
	var stopped atomic.Bool
	entered, release := make(chan struct{}), make(chan struct{})
	model := &scriptedCompleter{steps: []step{func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		close(entered)
		select {
		case <-release:
			return textResponse("this job is stopped; a person presses Start"), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}}
	a, _ := newTestAgent(t, model, func(c *Config) {
		c.JobBudget = &liveBudgetStub{cap: 10}
		c.FactoryWorkRefusal = func() string {
			if stopped.Load() {
				return factory.PersonStopReason
			}
			return ""
		}
	})
	id := startJob(t, a, "sleep 30")
	events := mustSubmit(t, a, "Why is this job waiting?")
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("person chat did not enter the provider")
	}
	stopped.Store(true)
	time.Sleep(350 * time.Millisecond)
	if a.jobs.find(id).running() {
		t.Error("person chat kept the manager background command alive after Stop")
	}
	close(release)
	got := collect(t, events)
	var words strings.Builder
	for _, e := range got {
		if e.Kind == EventTextDelta {
			words.WriteString(e.Text)
		}
	}
	if !strings.Contains(words.String(), "this job is stopped") {
		t.Fatalf("person chat was cut off: %q", words.String())
	}
}

func TestManagerBackgroundCommandContinuesWithoutPersonStop(t *testing.T) {
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.JobBudget = &liveBudgetStub{cap: 10}
		c.FactoryWorkRefusal = func() string { return "" }
	})
	id := startJob(t, a, "sleep 30")
	time.Sleep(350 * time.Millisecond)
	if !a.jobs.find(id).running() {
		t.Fatal("healthy manager background command was cut off")
	}
}
