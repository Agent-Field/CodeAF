package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// stalledErrand is ofetch v4-flash s13 at the end of its wall: one top-level job
// still moving, a growth journal that shows what a round of it costs, and not
// one delivery judgement in the whole store.
func stalledErrand(t *testing.T, session string) *store.Store {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { graph.Close() })
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-2", Brief: "add a per-origin circuit breaker"},
		{ID: "task-2-n1", Parent: "task-2", Brief: "a piece"},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: session,
		Intent: "add a per-origin circuit breaker"}); err != nil {
		t.Fatal(err)
	}
	return graph
}

// A RUN MAY NOT END WITH NOTHING JUDGED.
//
// The governor refuses growth when the wall is near, but a job only asks to grow
// when something in it ends; a job whose queued leaves keep starting never asks.
// ofetch s13 spent 5407 seconds, 422 model calls and eight growth rounds and
// journaled zero delivery judgements. The watcher holds the same grip and uses
// it on the clock alone.
func TestTheWatcherForcesAVerdictBeforeTheWall(t *testing.T) {
	session := "headless-forced"
	graph := stalledErrand(t, session)
	// Two admitted rounds far enough apart to give the job a measured pace.
	for round := 1; round <= 2; round++ {
		if err := graph.RecordJobGrowth("task-2", store.JobGrowth{
			Reason: "gap", Lineage: "task-2", Round: round, Allowed: true,
		}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(60 * time.Millisecond)
	}

	var progress strings.Builder
	var closedJob string
	watcher := &settlementWatch{
		graph: graph, session: session, progress: &progress, started: time.Now(),
		closeOut: func(jobRoot, keep, reason string) int { closedJob = jobRoot; return 2 },
	}

	// A wall with room to spare takes nothing away.
	roomy, cancelRoomy := context.WithTimeout(context.Background(), time.Hour)
	defer cancelRoomy()
	watcher.forceJudgement(roomy)
	if closedJob != "" {
		t.Fatalf("a run with an hour left was cut short over %q", closedJob)
	}

	// A wall shorter than a round of this job is judged now or never.
	tight, cancelTight := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancelTight()
	watcher.forceJudgement(tight)
	if closedJob != "task-2" {
		t.Fatalf("the run reached its wall with nothing judged: closed %q", closedJob)
	}
	if !strings.Contains(progress.String(), "still time") {
		t.Fatalf("the person watching was told nothing:\n%s", progress.String())
	}

	// And it happens once. A job already driven to a verdict is not driven again
	// on the next beat.
	closedJob = ""
	watcher.forceJudgement(tight)
	if closedJob != "" {
		t.Fatalf("the same job was closed out twice: %q", closedJob)
	}
}

// A job that has already been judged keeps its last minutes. The guarantee is
// that a verdict EXISTS, not that a second one is bought at the price of the
// work that would have earned it.
func TestTheWatcherLeavesAJudgedJobAlone(t *testing.T) {
	session := "headless-judged"
	graph := stalledErrand(t, session)
	for round := 1; round <= 2; round++ {
		if err := graph.RecordJobGrowth("task-2", store.JobGrowth{
			Reason: "gap", Lineage: "task-2", Round: round, Allowed: true,
		}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(60 * time.Millisecond)
	}
	if err := graph.RecordDeliveryGate("task-2", store.DeliveryGate{Pass: true}); err != nil {
		t.Fatal(err)
	}

	closed := ""
	watcher := &settlementWatch{
		graph: graph, session: session, started: time.Now(),
		closeOut: func(jobRoot, keep, reason string) int { closed = jobRoot; return 1 },
	}
	tight, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	watcher.forceJudgement(tight)
	if closed != "" {
		t.Fatalf("a job with a verdict on it was cut short: %q", closed)
	}
}
