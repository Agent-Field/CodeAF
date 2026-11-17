package session

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// Contract (b): an ordinary answered turn publishes the saved figure through
// the same hook as a late receipt, after its reply time and spend reach disk.
func TestAnsweredTurnPublishesItsSavedSpend(t *testing.T) {
	dir := t.TempDir()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Place = Place{Dir: dir, Workspace: config.Workspace}
	})
	agent.recordPresentedAssistant(textMessage("assistant", "done"), provider.MessageReasoning{}, humanPresentation("done"))
	agent.mu.Lock()
	agent.usage = Usage{Input: 10, Output: 5, CostUSD: .25, Calls: 1}
	agent.mu.Unlock()
	var news []SpendSaved
	t.Cleanup(OnSpendSaved(func(saved SpendSaved) {
		if saved.Dir == dir {
			meta, err := LoadMeta(dir)
			if err != nil || meta.SpentUSD != saved.SpentUSD || meta.LastReplyAt.IsZero() || meta.Tokens != 15 {
				t.Errorf("turn news preceded its combined metadata save: %+v, %+v, %v", saved, meta, err)
			}
			news = append(news, saved)
		}
	}))
	agent.stampSealed()
	if len(news) != 1 || news[0].SpentUSD != .25 {
		t.Fatalf("answered turn news = %+v, want one saved figure .25", news)
	}
}

// Contract (b), (c): a receipt recovered after cancellation saves X+L without
// another turn, says the saved figure once, and writes just one ledger row for L.
func TestLateRecoveredCancellationSavesCumulativeSpendOnce(t *testing.T) {
	dir := t.TempDir()
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	held := make(chan struct{})
	agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return priced(toolResponse("first", "ls", `{"path":"."}`), .25), nil
		},
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			close(held)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}}, func(config *Config) {
		config.Place = Place{Dir: dir, Workspace: config.Workspace}
		config.SessionFile = filepath.Join(dir, "transcript.jsonl")
		config.usageLedger = ledger
	})
	var mu sync.Mutex
	var news []SpendSaved
	t.Cleanup(OnSpendSaved(func(saved SpendSaved) {
		if saved.Dir == dir {
			meta, err := LoadMeta(dir)
			if err != nil || meta.SpentUSD != saved.SpentUSD {
				t.Errorf("spend was announced before it was saved: %+v, %+v, %v", saved, meta, err)
			}
			mu.Lock()
			news = append(news, saved)
			mu.Unlock()
		}
	}))
	events := mustSubmit(t, agent, "look around")
	waitMetaPatch(t, held)
	agent.Interrupt()
	collect(t, events)
	before := stampedMeta(t, agent, dir)
	if before.SpentUSD != .25 {
		t.Fatalf("the ended turn saved %v, want .25", before.SpentUSD)
	}
	mu.Lock()
	stamps := len(news)
	mu.Unlock()
	if stamps != 1 {
		t.Fatalf("the turn saved spend %d times, want once", stamps)
	}

	agent.reconciled(provider.Reconciled{
		Billed: provider.Billed{Model: "test/model", PromptTokens: 80, CompletionTokens: 20, Cost: .125},
		Ref:    "late-cancellation", Reason: "torn", Found: true,
	})
	after := stampedMeta(t, agent, dir)
	used := agent.Usage()
	if after.SpentUSD != .375 || after.SpentUSD != used.CostUSD || after.Tokens != used.Input+used.Output {
		t.Fatalf("late receipt saved %+v, meter %+v; want X+L=.375", after, used)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(news) != stamps+1 || news[stamps].SpentUSD != .375 {
		t.Fatalf("late spend news = %+v, want one new saved figure .375", news)
	}
	if usd, rows := ledgerUSD(t, ledger); usd != .375 || rows != 2 {
		t.Fatalf("ledger = %v in %d rows, want X+L in two rows", usd, rows)
	}
	lines, err := ReadUsage(ledger, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	late := 0
	for _, line := range lines {
		if line.GenerationID == "late-cancellation" {
			late++
			if line.USD != .125 || !line.Reconciled {
				t.Fatalf("late ledger row = %+v", line)
			}
		}
	}
	if late != 1 {
		t.Fatalf("late charge has %d ledger rows, want one", late)
	}
}

// Contract (c), fix round 1 finding 3: capture belongs inside the stamp lock,
// before either write. An older captured meter cannot be written after a newer
// stamp, and saved news follows the same .25 then .375 order.
func TestConcurrentTurnAndLateSpendStampsKeepTheLatestMeter(t *testing.T) {
	dir := t.TempDir()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Place = Place{Dir: dir, Workspace: config.Workspace}
		config.usageLedger = filepath.Join(t.TempDir(), UsageLedgerName)
	})
	agent.mu.Lock()
	agent.usage = Usage{Input: 10, Output: 2, CostUSD: .25, Calls: 1}
	agent.mu.Unlock()
	release, done := make(chan struct{}), make(chan struct{})
	type capture struct {
		spent  float64
		locked bool
	}
	captured := make(chan capture, 2)
	var captures atomic.Int32
	spendStampCaptured = func(a *Agent, spent float64) {
		if a != agent {
			return
		}
		// This assertion makes capture-before-lock fail deterministically,
		// without depending on whether the second stamp has been scheduled.
		locked := !a.spendStampMu.TryLock()
		if !locked {
			a.spendStampMu.Unlock()
		}
		n := captures.Add(1)
		captured <- capture{spent: spent, locked: locked}
		if n == 1 {
			<-release
		}
	}
	t.Cleanup(func() { spendStampCaptured = nil })
	var mu sync.Mutex
	var figures []float64
	t.Cleanup(OnSpendSaved(func(saved SpendSaved) {
		if saved.Dir != dir {
			return
		}
		mu.Lock()
		figures = append(figures, saved.SpentUSD)
		mu.Unlock()
	}))
	var once sync.Once
	letGo := func() { once.Do(func() { close(release) }) }
	t.Cleanup(letGo)
	go func() { agent.stampSealed(); close(done) }()
	t.Cleanup(func() { letGo(); waitMetaPatch(t, done) })
	select {
	case first := <-captured:
		if first.spent != .25 || !first.locked {
			t.Fatalf("first capture = %+v; want .25 with the stamp lock already held", first)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("older stamp did not reach its capture")
	}
	// Bank a late charge while A is between capture and write. The channel
	// handoff above guarantees A has released a.mu, so banking can proceed.
	agent.bank(bankedCall{
		used:  Usage{Input: 4, Output: 1, CostUSD: .125, Calls: 1},
		model: "test/model", generationID: "late-concurrent", late: true, ledger: true,
	})
	lateStarted, lateDone := make(chan struct{}), make(chan struct{})
	go func() {
		close(lateStarted)
		agent.stampSpend()
		close(lateDone)
	}()
	t.Cleanup(func() { letGo(); waitMetaPatch(t, lateDone) })
	waitMetaPatch(t, lateStarted)
	if got := captures.Load(); got != 1 {
		t.Fatalf("second stamp captured before the first wrote: %d captures", got)
	}
	select {
	case second := <-captured:
		t.Fatalf("second stamp captured while the first was held: %+v", second)
	default:
	}
	letGo()
	select {
	case second := <-captured:
		if second.spent != .375 || !second.locked {
			t.Fatalf("second capture = %+v; want .375 with the stamp lock held", second)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("newer stamp did not capture after releasing the older stamp")
	}
	waitMetaPatch(t, done)
	waitMetaPatch(t, lateDone)
	meta := stampedMeta(t, agent, dir)
	mu.Lock()
	defer mu.Unlock()
	if captures.Load() != 2 || meta.SpentUSD != .375 || meta.Tokens != 17 || len(figures) != 2 || figures[0] != .25 || figures[1] != .375 {
		t.Fatalf("concurrent stamps saved %+v and said %v, want .25 then .375", meta, figures)
	}
}
