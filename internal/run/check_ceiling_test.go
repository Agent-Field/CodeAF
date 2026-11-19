package run

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/session"
)

// run19Guard is the crew guard run19's research brief ran under, on stub
// prices: a checker with a $0.37 ceiling (three times its $0.124 estimate), a
// worker estimated at $0.0106, the $5 task limit, and no day cap.
func run19Guard(workEstimate float64) *session.SpendGuard {
	return &session.SpendGuard{
		Price:   func(string) (float64, float64, float64, bool) { return 0, 1e-6, 0, true },
		TaskCap: 5, TaskAction: config.CrewTaskCapAction(5),
		SeatCeilings:  map[crewroute.Seat]float64{crewroute.Checker: 0.373248},
		CeilingAction: config.CrewCheckCeilingAction,
		WorkEstimate:  workEstimate,
	}
}

// stubSeatCompleter answers every call at a fixed cost per model, the way the
// provider reported each of run19's calls: the worker's cheap ones and the
// checker's dearer ones over a growing transcript.
type stubSeatCompleter struct {
	mu    sync.Mutex
	costs map[string]float64
	calls map[string]int
}

func (p *stubSeatCompleter) forModel(model string) session.Completer {
	return stubModelCompleter{provider: p, model: model}
}

type stubModelCompleter struct {
	provider *stubSeatCompleter
	model    string
}

func (c stubModelCompleter) CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) {
	c.provider.mu.Lock()
	defer c.provider.mu.Unlock()
	c.provider.calls[c.model]++
	cost := c.provider.costs[c.model]
	return &ai.Response{Usage: &ai.Usage{Cost: &cost}}, nil
}

// run19Factory seats the run on the real crew factory over the guard: the root
// does the work itself on the worker seat and reports it built, and the review
// round's check reads on the checker seat until it either answers or is
// refused — the sequence run19 recorded.
func run19Factory(t *testing.T, store *plandb.Store, guard *session.SpendGuard, workerCalls int, checkCalls int) (WorkerFactory, *stubSeatCompleter) {
	t.Helper()
	provider := &stubSeatCompleter{
		costs: map[string]float64{"deepseek/deepseek-v4.1-flash": 0.0015, "google/gemini-4-carbon": 0.0225},
		calls: map[string]int{},
	}
	crew := CrewFactory(store, t.TempDir(), t.TempDir(), Seats{Work: "deepseek/deepseek-v4.1-flash", Plan: "deepseek/deepseek-v4.1-flash", Check: "google/gemini-4-carbon"}, "", false,
		func(model string) session.Completer { return guard.Wrap(model, provider.forModel(model)) })
	return func(task plandb.Task) Worker {
		worker := crew(task).(*BashWorker)
		return workerFunc(func(ctx context.Context, task plandb.Task) (Report, error) {
			if task.Role == plandb.RoleCheck {
				for i := 0; i < checkCalls; i++ {
					if _, err := worker.completer.CompleteWithMessages(ctx, nil); err != nil {
						return Report{Steps: i}, err
					}
				}
				return Report{Result: "holds: the brief reads the benchmark live", Steps: checkCalls}, nil
			}
			for i := 0; i < workerCalls; i++ {
				if _, err := worker.completer.CompleteWithMessages(ctx, nil); err != nil {
					return Report{}, err
				}
			}
			return Report{Result: "Built the vector-DB research brief", Steps: workerCalls}, nil
		})
	}, provider
}

// THE RUN19 SEQUENCE: the worker builds the brief and lands it, the review
// round's check reads it on a dear model, and the checker meets its own spend
// ceiling before it can answer. That is a fact about the checker. The run must
// answer done — the work is in — and carry the stop typed, so the person is
// asked about the work rather than told it failed. It used to fail the check,
// and CompleteRoot then failed the root over a finished brief.
func TestALandedWorksCheckMeetingItsCeilingIsANonAnswerNotAFailure(t *testing.T) {
	store := boundedRunStore(t, "Vector database research brief")
	// The worker cost what it was expected to, so nothing stretches the
	// checker's ceiling and it stops at $0.37 exactly as run19's did.
	guard := run19Guard(0.0106)
	factory, provider := run19Factory(t, store, guard, 7, 40)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	outcome, summary := Start(ctx, Spec{Store: store, Workspace: t.TempDir(), Slots: 1, Factory: factory, Limits: Limits{ReviewRound: true}})

	if outcome != OutcomeDone {
		t.Fatalf("outcome = %q, want %q: finished work whose check ran out of budget is not a failed run (summary %+v)", outcome, OutcomeDone, summary)
	}
	want := "the check stopped at its spend ceiling of $0.37 before it finished"
	if summary.Unchecked != want {
		t.Fatalf("summary.Unchecked = %q, want %q", summary.Unchecked, want)
	}
	if summary.Result != "Built the vector-DB research brief" {
		t.Fatalf("summary.Result = %q, want the worker's own report", summary.Result)
	}
	root := store.Task(store.RootID())
	if root.Status != plandb.StatusDone {
		t.Fatalf("root = %s (%q), want done", root.Status, root.Error)
	}
	checks := checkTasks(store)
	if len(checks) != 1 || checks[0].Status != plandb.StatusDone || checks[0].Result != want {
		t.Fatalf("check = %+v, want one check ended on the stop as its plain account", checks)
	}
	if strings.HasPrefix(checks[0].Result, "holds:") || strings.HasPrefix(checks[0].Result, "does not hold:") {
		t.Fatalf("check result %q reads as a conclusion it never reached", checks[0].Result)
	}
	// The ceiling is still a ceiling: the checker made the calls $0.37 buys
	// and not one more.
	if got := provider.calls["google/gemini-4-carbon"]; got != 16 {
		t.Fatalf("checker calls = %d, want 16 (16 × $0.0225 = $0.36; a seventeenth would pass $0.37)", got)
	}
}

// THE CHECK'S SIZE IS THE WORK'S SIZE. Run19's worker ran to 5.9 times its
// estimate; a check of that work reads that much more. With the worker's
// estimate on the guard, the checker's ceiling stretches with the work — by at
// most three times — and the same check that ran out at $0.37 now answers.
func TestAChecksCeilingStretchesWithWorkThatOverranItsEstimate(t *testing.T) {
	store := boundedRunStore(t, "Vector database research brief")
	// Seven worker calls at $0.0015 against a $0.0018 estimate: the work ran to
	// about six times what it was expected to cost, and the ceiling may grow
	// by three, to $1.12.
	guard := run19Guard(0.0018)
	factory, provider := run19Factory(t, store, guard, 7, 20)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	outcome, summary := Start(ctx, Spec{Store: store, Workspace: t.TempDir(), Slots: 1, Factory: factory, Limits: Limits{ReviewRound: true}})

	if outcome != OutcomeDone || summary.Unchecked != "" {
		t.Fatalf("outcome = %q, unchecked = %q; want a check that answered under its stretched ceiling", outcome, summary.Unchecked)
	}
	if got := provider.calls["google/gemini-4-carbon"]; got != 20 {
		t.Fatalf("checker calls = %d, want all 20 ($0.45) under the stretched ceiling", got)
	}
	checks := checkTasks(store)
	if len(checks) != 1 || !strings.HasPrefix(checks[0].Result, "holds:") {
		t.Fatalf("check = %+v, want it to have answered", checks)
	}
}

// A STRETCHED CEILING IS STILL A CEILING. However far the work overran, the
// checker stops at three times its stretched-by-at-most-three ceiling, and the
// work still stands.
func TestAStretchedCeilingStillStopsTheCheck(t *testing.T) {
	store := boundedRunStore(t, "Vector database research brief")
	guard := run19Guard(0.0018)
	factory, provider := run19Factory(t, store, guard, 7, 200)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	outcome, summary := Start(ctx, Spec{Store: store, Workspace: t.TempDir(), Slots: 1, Factory: factory, Limits: Limits{ReviewRound: true}})

	if outcome != OutcomeDone || summary.Unchecked != "the check stopped at its spend ceiling of $1.12 before it finished" {
		t.Fatalf("outcome = %q, unchecked = %q; want the stretched ceiling's stop", outcome, summary.Unchecked)
	}
	if got := provider.calls["google/gemini-4-carbon"]; got != 49 {
		t.Fatalf("checker calls = %d, want 49 (49 × $0.0225 = $1.10 under $1.12)", got)
	}
}

func checkTasks(store *plandb.Store) []*plandb.Task {
	var out []*plandb.Task
	for _, task := range store.Tasks() {
		if task.Role == plandb.RoleCheck {
			out = append(out, task)
		}
	}
	return out
}
