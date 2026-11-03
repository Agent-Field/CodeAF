package session

import (
	"context"
 "fmt"
 "os"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/provider"
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
			provider.Emit(ctx, provider.StreamDelta, "this job is stopped; a person presses Start")
			return textResponse("this job is stopped; a person presses Start"), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}, func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		provider.Emit(ctx, provider.StreamDelta, "second person answer")
		return textResponse("second person answer"), nil
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
	follow, err := a.FollowUp("What should I press next?")
	if err != nil {
		t.Fatal(err)
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
	second := collect(t, follow)
	words.Reset()
	for _, e := range second {
		if e.Kind == EventTextDelta {
			words.WriteString(e.Text)
		}
	}
	if !strings.Contains(words.String(), "second person answer") {
		t.Fatalf("queued person chat was lost: %q", words.String())
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

func TestPersonStopCancelsEveryManagerCommandWithoutSerialGrace(t *testing.T) {
 var stopped atomic.Bool
 a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
  c.JobBudget = &liveBudgetStub{cap: 10}
  c.FactoryWorkRefusal = func() string { if stopped.Load() { return factory.PersonStopReason }; return "" }
 })
 armed := t.TempDir()+"/armed"
 startJob(t, a, "trap '' TERM; echo ready > "+armed+"; while :; do sleep 1; done")
 waitFor(t, "first command installs its TERM trap", func() bool { _, err := os.Stat(armed); return err == nil })
 second := startJob(t, a, "sleep 30")
 stopped.Store(true)
 select {
 case <-a.jobs.find(second).done:
 case <-time.After(750*time.Millisecond): t.Fatal("second manager command kept running through the first command's TERM grace")
 }
}

func TestStoppedManagerPersonChatShowsCurrentStopReceipt(t *testing.T) {
 for _, stopped := range []bool{false, true} {
  t.Run(fmt.Sprintf("stopped=%v", stopped), func(t *testing.T) {
   model := &scriptedCompleter{steps: []step{func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
    provider.Emit(ctx, provider.StreamDelta, "Here is the model's explanation.")
    return textResponse("Here is the model's explanation."), nil
   }}}
   a, _ := newTestAgent(t, model, func(c *Config) {
    c.JobBudget = &liveBudgetStub{cap: 10}
    c.FactoryWorkRefusal = func() string { if stopped { return factory.PersonStopReason }; return "" }
   })
   var words strings.Builder
   for _, e := range collect(t, mustSubmit(t, a, "Why is this job waiting?")) { if e.Kind == EventTextDelta { words.WriteString(e.Text) } }
   if strings.Contains(words.String(), factory.PersonStopReason) != stopped { t.Fatalf("stopped=%v current Stop receipt missing or stale: %q", stopped, words.String()) }
   if !strings.Contains(words.String(), "model's explanation") || model.requests() != 1 { t.Fatalf("person answer was blocked: %q calls=%d", words.String(), model.requests()) }
  })
 }
}
