package session

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestBudgetStopExplainsIdleCrewOwner(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	budget := &liveBudgetStub{cap: 1}
	started := make(chan struct{})
	model := &scriptedCompleter{aside: func(messages []ai.Message) (*ai.Response, bool) {
		if isPlannerCall(messages) {
			return textResponse(`{"add":[{"id":"worker","title":"read notes","goal":"read notes"}]}`), true
		}
		if isNameCall(messages) || isSessionTitleCall(messages) {
			return textResponse("Read notes"), true
		}
		return nil, false
	}, steps: []step{func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}}}
	a, _ := newTestAgent(t, model, func(c *Config) { c.JobBudget = budget })
	if _, err := a.RunOrchestrate(context.Background(), "read notes", "", 5); err != nil {
		t.Fatal(err)
	}
	waitCloseRun(t, started, "worker")
	budget.Spend(2)
	waitFor(t, "crew stopped", func() bool { a.mu.Lock(); defer a.mu.Unlock(); return a.workStopped && !a.workStopping })
	for _, entry := range a.Transcript() {
		if strings.Contains(entry.Text, "job budget reached") {
			return
		}
	}
	t.Fatal("idle owner lost budget notice after worker stop")
}

func TestBudgetStopNoticeRecordedOnce(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		active, person, refused bool
		want                    int
	}{
		{name: "idle owner", want: 1},
		{name: "active owner", active: true, want: 1},
		{name: "owner call refuses first", active: true, refused: true, want: 1},
		{name: "person stops idle owner", person: true},
		{name: "person stops active owner", active: true, person: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CODEAF_HOME", t.TempDir())
			t.Setenv("HOME", t.TempDir())
			t.Setenv("CODEAF_DAILY_BUDGET", "")
			budget := &liveBudgetStub{cap: 1}
			worker, owner := make(chan struct{}), make(chan struct{})
			block := func(started chan struct{}) step {
				return func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
					close(started)
					<-ctx.Done()
					return nil, ctx.Err()
				}
			}
			ownerStep := block(owner)
			if tc.refused {
				ownerStep = func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
					close(owner)
					budget.Spend(2)
					return nil, spendRailReached{said: "job budget reached"}
				}
			}
			model := &scriptedCompleter{aside: func(messages []ai.Message) (*ai.Response, bool) {
				if isPlannerCall(messages) {
					return textResponse(`{"add":[{"id":"worker","title":"read notes","goal":"read notes"}]}`), true
				}
				if isNameCall(messages) || isSessionTitleCall(messages) {
					return textResponse("Read notes"), true
				}
				return nil, false
			}, steps: []step{block(worker), ownerStep}}
			a, _ := newTestAgent(t, model, func(c *Config) { c.JobBudget = budget })
			if _, err := a.RunOrchestrate(context.Background(), "read notes", "", 5); err != nil {
				t.Fatal(err)
			}
			waitCloseRun(t, worker, "worker")
			var events <-chan Event
			if tc.active {
				events = mustSubmit(t, a, "keep working")
				waitCloseRun(t, owner, "owner")
			}
			if tc.person {
				if err := a.StopWork(); err != nil {
					t.Fatal(err)
				}
			} else if !tc.refused {
				budget.Spend(2)
			}
			waitFor(t, "stop settles", func() bool {
				a.mu.Lock()
				defer a.mu.Unlock()
				return a.workStopped && !a.workStopping
			})
			if events != nil {
				collect(t, events)
			}
			if err := a.StopWork(); err != nil {
				t.Fatal(err)
			}
			got := 0
			for _, entry := range a.Transcript() {
				if strings.Contains(entry.Text, "job budget reached") {
					got++
				}
			}
			if got != tc.want {
				t.Fatalf("budget notices = %d, want %d", got, tc.want)
			}
		})
	}
}
