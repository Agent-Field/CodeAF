package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// THE INK RUN OF 2026-08-29, AT THE MOMENT ITS MONEY DISAPPEARED.
//
// `bench/deepswe/results/ink-grid-box-layout-…-s8` made a hundred and nine
// billed calls against `task-2` over seventeen minutes, wrote a twenty-six
// kilobyte patch, was given up on by its node watchdog, and left a store whose
// usage table held exactly one row — the planner's — because a leaf's spend
// reached the journal only through the outcome it hands back on the way out,
// and a leaf that is given up on hands back nothing. cost.json read $0.000228.
//
// The law this pins: EVERY BILLED RESPONSE WRITES ITS ROW WHEN THE RESPONSE
// ARRIVES. Two calls billed and then an ending that never lands still leaves
// two rows, and the landing roll-up that used to be the only record must not
// write a third over the top of them.
func TestTwoBilledCallsLeaveTwoUsageRowsWithoutALanding(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer graph.Close()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-2", Brief: "CSS Grid layout support", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "CSS Grid layout support"}); err != nil {
		t.Fatalf("splice: %v", err)
	}

	banker := newLeafBanker(graph, "task-2")
	// The context a leaf actually runs on, armed exactly as the attempt loop
	// arms it — so what is exercised here is the wiring, not a hand-rolled
	// imitation of it.
	ctx := armBilling(t.Context(), banker)
	sink := provider.BillingSinkFrom(ctx)
	if sink == nil {
		t.Fatal("armBilling armed nothing; a leaf's calls would go unbanked")
	}
	if node := provider.CallNodeFrom(ctx); node != "task-2" {
		t.Fatalf("the calls are filed under %q, want task-2", node)
	}

	sink(provider.Billed{
		Node: "task-2", Model: "deepseek/deepseek-v4-flash",
		PromptTokens: 4563, CompletionTokens: 610, CachedTokens: 3840, Cost: 0.000228,
	})
	sink(provider.Billed{
		Node: "task-2", Model: "deepseek/deepseek-v4-flash",
		PromptTokens: 11002, CompletionTokens: 318, CachedTokens: 8192, Cost: 0.00051,
	})
	// And then nothing: the watchdog gives up, the goroutine's outcome is
	// dropped, and no landing ever happens.

	rows, total := bankedRows(t, graph, "task-2")
	if rows != 2 {
		t.Fatalf("an interrupted leaf left %d usage rows, want the 2 calls it was billed for", rows)
	}
	if total != 4563+11002 {
		t.Fatalf("the banked rows sum to %d prompt tokens, want %d", total, 4563+11002)
	}
	// And the landing's roll-up is the REMAINDER of a total the banker has
	// already covered, which is nothing at all.
	landing := leafSpend(exec.Usage{
		PromptTokens: 4563 + 11002, CompletionTokens: 610 + 318,
		CachedTokens: 3840 + 8192, Cost: 0.000228 + 0.00051,
	}, nil, "deepseek/deepseek-v4-flash", banker.banked())
	if !landing.SpendBanked {
		t.Fatal("the landing does not know its money was banked")
	}
	if landing.PromptTokens != 0 || landing.CompletionTokens != 0 || landing.Cost != 0 {
		t.Fatalf("the landing would journal %d/%d prompt/completion tokens and $%v a second time",
			landing.PromptTokens, landing.CompletionTokens, landing.Cost)
	}
	// A worker whose calls this adapter cannot see — one that drives another
	// process — still journals what it spent, even after a banked attempt.
	escalated := leafSpend(exec.Usage{
		PromptTokens: 4563 + 11002 + 90000, CompletionTokens: 610 + 318 + 2000,
		CachedTokens: 3840 + 8192, Cost: 0.000228 + 0.00051 + 0.4,
	}, nil, "swe", banker.banked())
	if escalated.PromptTokens != 90000 || escalated.CompletionTokens != 2000 {
		t.Fatalf("an escalation's own spend came out as %d/%d, want the 90000/2000 nobody banked",
			escalated.PromptTokens, escalated.CompletionTokens)
	}
	// A call the provider did not price is not invented into the ledger.
	sink(provider.Billed{Node: "task-2", Model: "deepseek/deepseek-v4-flash"})
	if rows, _ = bankedRows(t, graph, "task-2"); rows != 2 {
		t.Fatalf("an unpriced response added a row; the table holds %d, want 2", rows)
	}
}

// bankedRows counts the usage rows filed under one node and sums their prompt
// tokens, reading the journal rather than any in-memory total — which is the
// whole point of the mechanism under test.
func bankedRows(t *testing.T, graph *store.Store, nodeID string) (int, int) {
	t.Helper()
	events, err := graph.Events(0, 500)
	if err != nil {
		t.Fatalf("read the journal: %v", err)
	}
	rows, total := 0, 0
	for _, event := range events {
		if event.Kind != store.EventUsageRecorded || event.NodeID != nodeID {
			continue
		}
		var usage store.NodeUsage
		if err := json.Unmarshal(event.Payload, &usage); err != nil {
			t.Fatalf("decode a usage row: %v", err)
		}
		if usage.Model != "deepseek/deepseek-v4-flash" {
			t.Fatalf("a banked row names %q, want the model that served it", usage.Model)
		}
		rows++
		total += usage.PromptTokens
	}
	return rows, total
}

// AND THE WATCHDOG ABOVE IT READS THE SAME EVIDENCE OF LIFE.
//
// ink s8's watchdog fired at seventeen minutes on a leaf that had made a model
// call or run a command in every one of them, said "the worker did not come
// back", and returned — leaving the goroutine running, still spending, still
// holding whatever children its last command had started. The window bounds
// SILENCE, exactly as the claim reaper's does, and a worker inside a call has
// answered it.
func TestTheWatchdogDoesNotGiveUpOnAWorkerThatIsWorking(t *testing.T) {
	working := &spanWorker{spans: 6, each: 40 * time.Millisecond, ended: make(chan struct{})}
	// A window far shorter than the work: on a flat timer this leaf would be
	// abandoned four times over.
	outcome, err := runLeafWithWatchdog(t.Context(), working, exec.Task{}, 60*time.Millisecond)
	if err != nil {
		t.Fatalf("a leaf that was demonstrably working was given up on: %v", err)
	}
	if outcome == nil || outcome.Text != "landed" {
		t.Fatalf("the leaf's own landing did not come back: %+v", outcome)
	}
}

// And a worker that shows no sign of life is STOPPED rather than walked away
// from — its context ends, which is what kills its commands and lets it land.
func TestTheWatchdogStopsTheWorkerItGivesUpOn(t *testing.T) {
	silent := &spanWorker{ended: make(chan struct{})}
	_, err := runLeafWithWatchdog(t.Context(), silent, exec.Task{}, 30*time.Millisecond)
	if _, gave := exec.RanOutOfRoom(err); !gave {
		t.Fatalf("a silent worker ended with %v, want the clock's own ending", err)
	}
	// The worker OBSERVES the stop. Asserting it after the watchdog has already
	// returned is the point: giving up used to be a departure, and the
	// goroutine went on spending against a context nobody had ended.
	select {
	case <-silent.ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the abandoned worker's context is still live; it would go on spending")
	}
}

// spanWorker opens `spans` liveness spans of `each` and then lands, or waits to
// be cancelled when it has none to open.
type spanWorker struct {
	spans int
	each  time.Duration
	once  sync.Once
	// ended is closed when the worker observes its context ending.
	ended chan struct{}
}

func (s *spanWorker) Subharness() string { return exec.LinearSubharness }

func (s *spanWorker) Run(ctx context.Context, _ exec.Task) (*exec.Outcome, error) {
	for range s.spans {
		done := exec.Working(ctx)
		select {
		case <-time.After(s.each):
		case <-ctx.Done():
			done()
			s.note()
			return &exec.Outcome{Text: "stopped"}, nil
		}
		done()
	}
	if s.spans == 0 {
		<-ctx.Done()
		s.note()
		// Deliberately never lands: this is the worker the grace period exists
		// to give up on, and it must not be rescued by returning.
		select {}
	}
	return &exec.Outcome{Text: "landed"}, nil
}

func (s *spanWorker) note() {
	s.once.Do(func() { close(s.ended) })
}
