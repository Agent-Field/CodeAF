package main

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/lease"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

func TestSentinelClientCallParsesYesAndNo(t *testing.T) {
	for _, test := range []struct {
		response string
		yes      bool
		line     string
	}{
		{response: "yes — a release landed", yes: true, line: "a release landed"},
		{response: "no — nothing changed", line: "nothing changed"},
	} {
		capture := &gateCaptureClient{model: "talk/model", response: test.response}
		settings := config.Config{Model: capture.model}
		client := adoptLiveClient(settings, capture.model, capture)
		verdict, err := checkSentinel(settings, client)(context.Background(), resident.SentinelPrompt{
			CharterID: "release-watch", Invariant: "Keep release notes current.",
			SentinelHint: "Did a release land?", Evidence: "poll due",
		})
		if err != nil {
			t.Fatal(err)
		}
		if verdict.Yes != test.yes || verdict.Line != test.line {
			t.Fatalf("verdict = %+v", verdict)
		}
		if len(capture.messages) != 2 || capture.messages[0].Content[0].Text != sentinelSystemPrompt ||
			!strings.Contains(capture.messages[1].Content[0].Text, "Keep release notes current.") {
			t.Fatalf("sentinel messages = %+v", capture.messages)
		}
	}
}

func TestWakeCommandRunsOnePassAndExits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wake.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	charter, err := store.NewCharter("wake-command", "Keep release notes current.", store.WatchSpec{
		Kind: store.WatchPoll, Poll: &store.PollWatch{Condition: "look for a release", Cadence: time.Hour},
	}, "Did a release land?", store.CharterAction{Template: "Update the release notes"},
		store.CharterRails{PerFiringBudgetUSD: 0.1, MaxFiringsPerDay: 2}, store.CharterActive,
		store.Ratification{Origin: store.OriginUser, SessionID: "wake-session", Evidence: "yes"})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.CreateCharter(charter); err != nil {
		t.Fatal(err)
	}
	if err := graph.PromoteCharter(charter.ID, "test fixture exercises tenured wake behavior", false); err != nil {
		t.Fatal(err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}

	calls := 0
	builder := func(graph *store.Store, _ string) (*resident.Reconciler, error) {
		return resident.New(graph,
			func(_ context.Context, instruction, _ string) (resident.Compiled, error) {
				return resident.Compiled{Goal: "Re-grounded: " + instruction}, nil
			}, nil,
		).WithWatchEngine(0, func(_ context.Context, _ resident.SentinelPrompt) (resident.SentinelVerdict, error) {
			calls++
			return resident.SentinelVerdict{Yes: true, Line: "release found"}, nil
		}), nil
	}
	var output bytes.Buffer
	if err := runWakeWith([]string{"--db", path}, &output, builder); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !strings.Contains(output.String(), "checked 1, fired 1") {
		t.Fatalf("wake output = %q calls=%d", output.String(), calls)
	}

	graph, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	wake, wakeFound, err := graph.LastStandingWake()
	if err != nil || !wakeFound || wake.IsZero() {
		t.Fatalf("wake pass was not journaled: %s found=%t err=%v", wake, wakeFound, err)
	}
	nodes, err := graph.Nodes()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, node := range nodes {
		if node.Provenance.CharterID != charter.ID {
			continue
		}
		found = true
		if node.Provenance.Origin != store.OriginTrigger || node.Provenance.Intent != "Re-grounded: "+charter.Action.Template {
			t.Fatalf("wake job provenance = %+v", node.Provenance)
		}
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("wake command did not splice trigger work")
	}

	output.Reset()
	if err := runWakeWith([]string{"--db", path}, &output, builder); err != nil {
		t.Fatal(err)
	}
	// A SECOND PASS OVER THE SAME STORE FINDS NOTHING, AND SAYS SO IN A
	// SENTENCE. It used to read `examined 0, checked 0, …` — eight figures
	// asserting a measurement where nothing happened (wakePassWords).
	if calls != 1 || !strings.Contains(output.String(), "nothing was waiting to be looked at.") {
		t.Fatalf("second one-pass output = %q calls=%d", output.String(), calls)
	}
}

func TestWakeFastExitsWhenResidentLeaseHeld(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wake.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}
	release, heldBy, err := lease.AcquireResident(path, "chat")
	if err != nil || release == nil || heldBy != nil {
		t.Fatalf("hold resident lease = release %v, held %+v, err %v", release != nil, heldBy, err)
	}
	defer release()

	built := false
	started := time.Now()
	var output bytes.Buffer
	err = runWakeWith([]string{"--db", path}, &output, func(*store.Store, string) (*resident.Reconciler, error) {
		built = true
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if built {
		t.Fatal("wake built a reconciler while another resident was alive")
	}
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("fast exit took %s", elapsed)
	}
	if !strings.Contains(output.String(), "resident alive (pid ") || !strings.Contains(output.String(), "— skipping wake") {
		t.Fatalf("fast-exit output = %q", output.String())
	}
}

func TestWakeFullPassFiresDuePracticeCandidate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "practice-wake.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	seedWakePracticeCandidate(t, graph, "repo:/work/parser", "build parser and run go test")
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	builder := func(graph *store.Store, _ string) (*resident.Reconciler, error) {
		return resident.New(graph, nil, nil).WithPracticeLoop(2, 0), nil
	}
	if err := runWakeWith([]string{"--db", path}, &output, builder); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "practice 1") {
		t.Fatalf("wake summary = %q", output.String())
	}

	graph, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	nodes, err := graph.Nodes()
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range nodes {
		if node.Group == store.PracticeGroup && node.Status == store.Pending &&
			node.Provenance.Origin == store.OriginSelf {
			return
		}
	}
	t.Fatalf("wake did not admit a pending self-origin practice node: %+v", nodes)
}

func seedWakePracticeCandidate(t *testing.T, graph *store.Store, scope, intent string) {
	t.Helper()
	for job := 1; job <= 2; job++ {
		rootID := fmt.Sprintf("wake-history-%d", job)
		nodes := []store.NodeSpec{{ID: rootID, Brief: intent, Stage: 2}}
		for leaf := 1; leaf <= 3; leaf++ {
			nodes = append(nodes, store.NodeSpec{
				ID: fmt.Sprintf("%s-leaf-%d", rootID, leaf), Parent: rootID,
				Brief: intent, Stage: 1,
			})
		}
		if err := graph.Splice(store.RootID, store.Subtree{Nodes: nodes}, store.Provenance{
			Origin: store.OriginUser, SessionID: "wake-practice", Intent: intent,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := graph.RecordFact(rootID, scope, store.FactLesson, intent); err != nil {
			t.Fatal(err)
		}
		// Improving residuals across the two jobs: the learning-progress
		// allocator funds curiosity only where practice is paying off.
		surprise := 2.0
		if job == 2 {
			surprise = 1.0
		}
		for leaf := 1; leaf <= 3; leaf++ {
			completeWakePracticeNode(t, graph, fmt.Sprintf("%s-leaf-%d", rootID, leaf), surprise)
		}
		completeWakePracticeNode(t, graph, rootID, surprise)
	}
}

func completeWakePracticeNode(t *testing.T, graph *store.Store, id string, surprise float64) {
	t.Helper()
	claim, won, err := graph.Claim(id, "wake-practice-history")
	if err != nil || !won {
		t.Fatalf("claim %s: won=%t err=%v", id, won, err)
	}
	if err := graph.RecordSurprise(store.NodeSurprise{
		NodeID: id, ActualTokens: 200, ExpectedTokens: 100, Surprise: surprise,
	}); err != nil {
		t.Fatal(err)
	}
	if err := graph.Complete(claim, "go test passed"); err != nil {
		t.Fatal(err)
	}
}

// A resident whose loop died silently keeps its lease for as long as its
// process lives, and every wake used to defer to it forever — so no standing
// watch, charter or practice ever fired again while it printed "alive".
func TestWakeRunsAnywayWhenTheHolderStoppedTicking(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wake.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}
	release, heldBy, err := lease.AcquireResident(path, "chat")
	if err != nil || release == nil || heldBy != nil {
		t.Fatalf("hold resident lease = release %v, held %+v, err %v", release != nil, heldBy, err)
	}
	defer release()
	if err := lease.NoteResidentTick(path, time.Now().Add(-2*lease.StuckAfter)); err != nil {
		t.Fatal(err)
	}

	built := false
	var output bytes.Buffer
	err = runWakeWith([]string{"--db", path}, &output, func(graph *store.Store, _ string) (*resident.Reconciler, error) {
		built = true
		return resident.New(graph, nil, nil), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !built {
		t.Fatalf("wake deferred to a resident that had stopped ticking: %q", output.String())
	}
	if !strings.Contains(output.String(), "has not ticked since") {
		t.Fatalf("wake took the role back silently: %q", output.String())
	}
}
