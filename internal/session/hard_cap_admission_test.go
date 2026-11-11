package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// Recording at the job-budget admission boundary queues the ledger wake while
// Submit holds the turn lock. PrepareModel then holds the running turn before
// its first provider admission, so the watcher must leave that admission alone.
type recordingAdmissionBudget struct {
	once   sync.Once
	record func()
}

func (*recordingAdmissionBudget) Spend(float64) {}
func (b *recordingAdmissionBudget) Refusal() string {
	b.once.Do(b.record)
	return ""
}

func TestHardCapPreAdmissionWakeDoesNotLatchStop(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	synctest.Test(t, func(t *testing.T) {
		t.Cleanup(func() { StopUsageWriter(UsageLedgerPath()) })
		prepared, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		defer unblock()
		budget := &recordingAdmissionBudget{record: func() {
			RecordUsage(UsageLedgerPath(), UsageLine{At: time.Now(), Model: "earlier work", Calls: 1, USD: 1.5})
			if !StopUsageWriter(UsageLedgerPath()) {
				t.Fatal("recording did not reach the ledger")
			}
		}}
		model := &scriptedCompleter{}
		a, _ := newTestAgent(t, model, func(c *Config) {
			c.OneModel = true
			c.Routing = provider.RoutingOff
			c.DailyBudgetResolved, c.DailyBudgetUSD = true, 1
			c.PrepareModel = func(string, modelsource.Set) (string, string, error) {
				close(prepared)
				<-release
				return "", "", nil
			}
		})
		// The ledger injection belongs to turn admission, after the startup snapshot.
		a.config.JobBudget = budget
		events := mustSubmit(t, a, "work")
		<-prepared
		synctest.Wait()
		unblock()
		got := collect(t, events)
		if len(got) != 1 || !errors.Is(got[0].Err, ErrSpendRail) {
			t.Errorf("turn admission: events=%v, want one ErrSpendRail", kinds(got))
		}
		synctest.Wait()
		events, err := a.Submit(context.Background(), "work")
		if err != nil {
			t.Fatalf("Submit after recording: %v; want ErrSpendRail in the event stream", err)
		}
		got = collect(t, events)
		if len(got) != 1 || !errors.Is(got[0].Err, ErrSpendRail) || model.requests() != 0 {
			t.Fatalf("daily refusal: events=%v calls=%d", got, model.requests())
		}
	})
}

func TestHardCapIdleChildDoesNotOwnParentWork(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	synctest.Test(t, func(t *testing.T) {
		t.Cleanup(func() { StopUsageWriter(UsageLedgerPath()) })
		parent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
			c.SpendPreauthorized = true
			c.Routing = provider.RoutingOff
			c.DailyBudgetResolved = true
			c.DailyBudgetUSD = 1
		})
		graph := parent.graph()
		graph.run = func(*TaskNode) {}
		id := graph.reserve()
		graph.admit(id, taskSpec{title: "work", brief: "answer", acceptance: "done"})
		child, err := parent.newTaskAgent(context.Background(), t.TempDir(), graph.node(id), "")
		if err != nil {
			t.Fatal(err)
		}
		// Child constructors choose their own routing; keep network refreshes
		// outside this admission-only scheduler.
		child.stopLaneBeat()
		child.config.SpendPreauthorized = false
		t.Cleanup(child.SettleWrites)
		t.Cleanup(func() { _ = child.Close() })
		RecordUsage(UsageLedgerPath(), UsageLine{At: time.Now(), Model: "earlier work", Calls: 1, USD: 1.5})
		if !StopUsageWriter(UsageLedgerPath()) {
			t.Fatal("recording did not reach the ledger")
		}
		synctest.Wait()
		events, err := child.Submit(context.Background(), "work")
		if err != nil {
			t.Fatalf("Submit after recording: %v; want ErrSpendRail in the event stream", err)
		}
		got := collect(t, events)
		if len(got) != 1 || !errors.Is(got[0].Err, ErrSpendRail) {
			t.Fatalf("daily refusal: events=%v", got)
		}
	})
}
