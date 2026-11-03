package session

import (
	"context"
	"fmt"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"os"
	"strings"
	"sync"
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
		c.FactoryWorkRefusal = func() string {
			if stopped.Load() {
				return factory.PersonStopReason
			}
			return ""
		}
	})
	armed := t.TempDir() + "/armed"
	startJob(t, a, "trap '' TERM; echo ready > "+armed+"; while :; do sleep 1; done")
	waitFor(t, "first command installs its TERM trap", func() bool { _, err := os.Stat(armed); return err == nil })
	second := startJob(t, a, "sleep 30")
	stopped.Store(true)
	select {
	case <-a.jobs.find(second).done:
	case <-time.After(750 * time.Millisecond):
		t.Fatal("second manager command kept running through the first command's TERM grace")
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
				c.FactoryWorkRefusal = func() string {
					if stopped {
						return factory.PersonStopReason
					}
					return ""
				}
			})
			var words strings.Builder
			for _, e := range collect(t, mustSubmit(t, a, "Why is this job waiting?")) {
				if e.Kind == EventTextDelta {
					words.WriteString(e.Text)
				}
			}
			if strings.Contains(words.String(), factory.PersonStopReason) != stopped {
				t.Fatalf("stopped=%v current Stop receipt missing or stale: %q", stopped, words.String())
			}
			if !strings.Contains(words.String(), "model's explanation") || model.requests() != 1 {
				t.Fatalf("person answer was blocked: %q calls=%d", words.String(), model.requests())
			}
		})
	}
}

func TestLateStopSampleDoesNotCancelNewManagerWork(t *testing.T) {
	var armed atomic.Bool
	var once sync.Once
	sampled, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.JobBudget = &liveBudgetStub{cap: 10}
		c.FactoryWorkRefusal = func() string {
			if armed.CompareAndSwap(true, false) {
				once.Do(func() { close(sampled) })
				<-release
				return factory.PersonStopReason
			}
			return ""
		}
	})
	old := startJob(t, a, "sleep 30")
	armed.Store(true)
	select {
	case <-sampled:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not sample Stop")
	}
	fresh := startJob(t, a, "sleep 30")
	unblock()
	select {
	case <-a.jobs.find(old).done:
	case <-time.After(2 * time.Second):
		t.Fatal("old work was not cancelled")
	}
	if !a.jobs.find(fresh).running() {
		t.Fatal("late Stop sample cancelled work started after the sample")
	}
}

func TestAuthorityReadErrorDoesNotCancelManagerWork(t *testing.T) {
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.JobBudget = &liveBudgetStub{cap: 10}
		c.FactoryWorkRefusal = func() string { return "job could not be read; automatic work stopped" }
	})
	id := startJob(t, a, "sleep 30")
	time.Sleep(350 * time.Millisecond)
	if !a.jobs.find(id).running() {
		t.Fatal("read error cancelled an unstopped manager's work")
	}
}

func TestLateStopSampleDoesNotCancelRetriedManagerTask(t *testing.T) {
	var armed atomic.Bool
	sampled, release, sampledAgain := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce, againOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	var sampling atomic.Bool
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.JobBudget = &liveBudgetStub{cap: 10}
		c.FactoryWorkRefusal = func() string {
			if armed.CompareAndSwap(true, false) {
				sampling.Store(true)
				close(sampled)
				<-release
				return factory.PersonStopReason
			}
			if sampling.Load() {
				againOnce.Do(func() { close(sampledAgain) })
			}
			return ""
		}
	})
	graph := a.graph()
	graph.run = func(*TaskNode) {}
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "proof", brief: "prove the change", acceptance: "tests pass", depth: 1})
	old := startJob(t, a, "sleep 30")
	armed.Store(true)
	select {
	case <-sampled:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not sample Stop")
	}
	if _, err := a.Cancel(fmt.Sprintf("task:%d", id)); err != nil {
		t.Fatal(err)
	}
	if err := a.RetryTask(id); err != nil {
		t.Fatal(err)
	}
	unblock()
	select {
	case <-sampledAgain:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not finish old Stop")
	}
	select {
	case <-a.jobs.find(old).done:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not stop the old producer")
	}
	if got := graph.node(id).notice(); got.State != TaskRunning || got.Stopped {
		t.Fatalf("late Stop cancelled retried task: %+v", got)
	}
}

type sampledJobBudget struct {
	liveBudgetStub
	read func() string
}

func (b *sampledJobBudget) Refusal() string { return b.read() }

func TestLateBudgetSampleDoesNotStopResumedManager(t *testing.T) {
	var armed atomic.Bool
	sampled, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	budget := &sampledJobBudget{read: func() string {
		if armed.CompareAndSwap(true, false) {
			close(sampled)
			<-release
			return "job budget reached"
		}
		return ""
	}}
	entered, finish := make(chan struct{}), make(chan struct{})
	model := &scriptedCompleter{steps: []step{func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		close(entered)
		select {
		case <-finish:
			provider.Emit(ctx, provider.StreamDelta, "resumed")
			return textResponse("resumed"), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}}
	a, _ := newTestAgent(t, model, func(c *Config) { c.JobBudget = budget })
	armed.Store(true)
	select {
	case <-sampled:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not sample budget")
	}
	if err := a.StopWork(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "person Stop settles", func() bool { a.mu.Lock(); defer a.mu.Unlock(); return !a.workStopping })
	events := mustSubmit(t, a, "Start fresh work after raising the budget")
	<-entered
	unblock()
	time.Sleep(250 * time.Millisecond)
	a.mu.Lock()
	stopped := a.workStopped
	a.mu.Unlock()
	if stopped {
		t.Error("late budget sample stopped the resumed manager")
	}
	close(finish)
	var words strings.Builder
	for _, e := range collect(t, events) {
		if e.Kind == EventTextDelta {
			words.WriteString(e.Text)
		}
	}
	if !strings.Contains(words.String(), "resumed") {
		t.Fatalf("resumed manager did not finish: %q", words.String())
	}
}
