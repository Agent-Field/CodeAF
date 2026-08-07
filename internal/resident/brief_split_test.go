package resident

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The split exists so a surface can journal the arrival immediately and compose
// the brief behind its first frame. The contract that makes it safe is that the
// attach edge and the brief's window are both fixed before the caller is handed
// anything to run, so a delayed composition still describes the same absence.
func TestSessionOpeningFixesTheWindowBeforeTheComposition(t *testing.T) {
	graph := openStore(t)
	if _, err := graph.TouchSeen("tui", "old", store.SeenDetached); err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "report", Brief: "Prepare the release report", Title: "Release report", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginTrigger, SessionID: "old", Intent: "prepare the report"}); err != nil {
		t.Fatal(err)
	}
	claim, won, err := graph.Claim("report", "worker")
	if err != nil || !won {
		t.Fatalf("claim won=%t err=%v", won, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	if err := graph.Complete(claim, "Release report landed."); err != nil {
		t.Fatal(err)
	}

	var received BriefActivity
	reconciler := New(graph, nil, nil).WithBriefComposer(
		func(_ context.Context, activity BriefActivity) (BriefDraft, error) {
			received = activity
			return BriefDraft{Headline: "the report landed."}, nil
		})

	deliver, err := reconciler.SessionOpening("arrival", "tui", 0)
	if err != nil {
		t.Fatal(err)
	}
	if deliver == nil {
		t.Fatal("qualifying activity should hand back a delivery")
	}

	// The attach edge is already journaled, and nothing has been said yet.
	if seen, found, err := graph.LastSeen(); err != nil || !found || seen.State != store.SeenAttached {
		t.Fatalf("attach edge not journaled: seen=%+v found=%t err=%v", seen, found, err)
	}
	messages, err := graph.Messages("arrival", 0, 0)
	if err != nil || len(messages) != 0 {
		t.Fatalf("nothing should be posted before delivery: %+v err=%v", messages, err)
	}

	// Work that lands after the window was fixed belongs to the session the
	// user is now watching, not to the absence they are being told about.
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "late", Brief: "Something after arrival", Title: "Late work", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginTrigger, SessionID: "arrival", Intent: "something after arrival"}); err != nil {
		t.Fatal(err)
	}
	lateClaim, won, err := graph.Claim("late", "worker")
	if err != nil || !won {
		t.Fatalf("claim late won=%t err=%v", won, err)
	}
	if err := graph.Start(lateClaim); err != nil {
		t.Fatal(err)
	}
	if err := graph.Complete(lateClaim, "Late work landed."); err != nil {
		t.Fatal(err)
	}

	if err := deliver(context.Background()); err != nil {
		t.Fatal(err)
	}
	if received.Done != 1 {
		t.Fatalf("the window widened after it was fixed: %+v", received)
	}
	for _, event := range received.Events {
		if strings.Contains(event.Text, "Late work") {
			t.Fatalf("brief picked up post-arrival work: %+v", event)
		}
	}
	messages, err = graph.Messages("arrival", 0, 0)
	if err != nil || len(messages) != 1 || messages[0].Brief == nil {
		t.Fatalf("delivery should post exactly one brief: %+v err=%v", messages, err)
	}
}

// SessionOpened keeps its old shape by running both halves.
func TestSessionOpeningReportsNothingToSayWithoutActivity(t *testing.T) {
	graph := openStore(t)
	reconciler := New(graph, nil, nil).WithBriefComposer(
		func(context.Context, BriefActivity) (BriefDraft, error) {
			t.Fatal("composer must not run without a previous watermark")
			return BriefDraft{}, nil
		})
	deliver, err := reconciler.SessionOpening("arrival", "tui", 0)
	if err != nil {
		t.Fatal(err)
	}
	if deliver != nil {
		t.Fatal("a first-ever session has no absence to describe")
	}
}
