package session

import (
	"context"
	"errors"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/factory"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type liveBudgetStub struct {
	mu         sync.Mutex
	spent, cap float64
}

func (b *liveBudgetStub) Spend(usd float64) { b.mu.Lock(); defer b.mu.Unlock(); b.spent += usd }
func (b *liveBudgetStub) Refusal() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.spent >= b.cap {
		return "job budget reached"
	}
	return ""
}
func TestJobBudgetChargesDirectCallsNotFoldedUsage(t *testing.T) {
	b := &liveBudgetStub{cap: 5}
	a := &Agent{config: Config{JobBudget: b, usageLedger: filepath.Join(t.TempDir(), UsageLedgerName)}}
	// The asynchronous ledger must finish before its temporary directory is removed.
	t.Cleanup(func() {
		if !StopUsageWriter(a.config.usageLedger) {
			t.Error("usage writer did not stop")
		}
	})
	a.bank(bankedCall{used: Usage{CostUSD: 1}, ledger: true})
	a.bank(bankedCall{used: Usage{CostUSD: 1}, ledger: false})
	if b.spent != 1 {
		t.Fatalf("charged %v, want 1", b.spent)
	}
	b.cap = 1
	if !errors.Is(a.jobBudgetBlock(), ErrSpendRail) {
		t.Fatal("shared job budget did not refuse further calls")
	}
}
func TestJobBudgetCancelsAnInflightProviderPromptly(t *testing.T) {
	b := &liveBudgetStub{cap: 1}
	started := make(chan struct{})
	stopped := make(chan struct{})
	model := &scriptedCompleter{steps: []step{func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return nil, ctx.Err()
	}}}
	agent, _ := newTestAgent(t, model, func(c *Config) { c.JobBudget = b })
	events := mustSubmit(t, agent, "work until stopped")
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("provider did not start")
	}
	b.Spend(1)
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("budget did not cancel in-flight work")
	}
	collect(t, events)
	if agent.jobBudgetBlock() == nil {
		t.Fatal("budget admitted another request")
	}
}

func TestJobBudgetStopsBackgroundCommandsWhileForegroundIsIdle(t *testing.T) {
	b := &liveBudgetStub{cap: 1}
	a, _ := jobsAgent(t)
	a.config.JobBudget = b
	a.watchJobBudget()
	id := startJob(t, a, "sleep 30")
	b.Spend(1)
	waitFor(t, "budget stops owned background command", func() bool { return !a.jobs.find(id).running() })
	if _, err := a.jobs.start("true"); err == nil {
		t.Fatal("new background work admitted at cap")
	}
}
func TestJobBudgetCancelsQueuedAndRunningChildren(t *testing.T) {
	b := &liveBudgetStub{cap: 1}
	a, _ := jobsAgent(t)
	a.config.JobBudget = b
	graph := a.graph()
	id := graph.reserve()
	graph.mu.Lock()
	graph.nodes[id] = &TaskNode{graph: graph, id: id, spec: taskSpec{title: "owned child"}, state: TaskQueued, done: make(chan struct{})}
	graph.order = append(graph.order, id)
	graph.mu.Unlock()
	// A real owned process keeps the idle parent's work visible until cancellation.
	startJob(t, a, "sleep 30")
	a.watchJobBudget()
	b.Spend(1)
	waitFor(t, "budget cancels delegated child", func() bool { graph.mu.Lock(); defer graph.mu.Unlock(); return graph.nodes[id].stopped })
}

func TestJobBudgetManagerChangesCannotGoBelowSpend(t *testing.T) {
	it := factory.Item{Cap: 10, ChatSpent: 5.01}
	if _, err := ApplyItemChange(it, ItemChange{Cap: 5}, factory.Recipe{}); err == nil {
		t.Fatal("manager lowered budget below spend")
	}
	it.ChatSpent = 5
	next, err := ApplyItemChange(it, ItemChange{Cap: 5}, factory.Recipe{})
	if err != nil || next.Cap != 5 {
		t.Fatalf("equal cap refused: %v %+v", err, next)
	}
}

type namedBudgetStub struct {
	liveBudgetStub
	job string
}

func (b *namedBudgetStub) FactoryJob() string { return b.job }

// A factory job's family writes its job on every ledger row, priced or not, so
// the factory can say which models its jobs used.
func TestJobBudgetNamesTheFactoryJobOnLedgerRows(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	a := &Agent{config: Config{JobBudget: &namedBudgetStub{liveBudgetStub: liveBudgetStub{cap: 5}, job: "acme/api#7"}, usageLedger: ledger}}
	a.bank(bankedCall{model: "m-priced", used: Usage{Input: 10, Output: 4, CostUSD: 0.5, Calls: 1}, ledger: true})
	a.bank(bankedCall{model: "m-plan", used: Usage{Input: 3, Output: 2, Calls: 1}, ledger: true})
	plain := &Agent{config: Config{usageLedger: ledger}}
	plain.bank(bankedCall{model: "m-chat", used: Usage{Input: 1, Output: 1, CostUSD: 0.1, Calls: 1}, ledger: true})
	if !StopUsageWriter(ledger) {
		t.Fatal("usage writer did not stop")
	}
	lines, err := ReadUsage(ledger, time.Time{})
	if err != nil || len(lines) != 3 {
		t.Fatalf("lines = %d, %v", len(lines), err)
	}
	got := map[string]string{}
	for _, l := range lines {
		got[l.Model] = l.FactoryJob
	}
	if got["m-priced"] != "acme/api#7" || got["m-plan"] != "acme/api#7" || got["m-chat"] != "" {
		t.Fatalf("factory jobs on rows = %v", got)
	}
}
