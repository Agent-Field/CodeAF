package resident

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// Journey #12 driven the way the binary drives it, end to end: a session that
// was open and closed, work that lands while nobody is watching, and a fresh
// attach that goes AttachSession → SessionOpening → deliver, exactly as
// cmd/aforge does. The pieces are each covered on their own; what was never
// asserted is that the sequence composes — that the detach edge the TUI writes
// on its way out becomes the "since" of the brief the next launch reads, and
// that the absence threshold is crossed by a real gap rather than by zero.
func TestClosingAndReturningLandsOneBriefForWhatHappenedWhileAway(t *testing.T) {
	graph := openStore(t)

	// The session that was here before, opening and then closing the window.
	before := New(graph, nil, nil)
	if err := before.AttachSession("yesterday"); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.TouchSeen("tui", "yesterday", store.SeenAttached); err != nil {
		t.Fatal(err)
	}
	if err := before.SessionClosed("yesterday", "tui"); err != nil {
		t.Fatal(err)
	}

	// While the terminal was closed: an overnight job lands, and a standing
	// charter fires a question that is still waiting.
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "overnight", Brief: "Rebuild the release report", Title: "Release report", Stage: 1,
	}}}, store.Provenance{
		Origin: store.OriginTrigger, SessionID: "yesterday", Intent: "rebuild the report",
	}); err != nil {
		t.Fatal(err)
	}
	claim, won, err := graph.Claim("overnight", "worker")
	if err != nil || !won {
		t.Fatalf("claim won=%t err=%v", won, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	if err := graph.Complete(claim, "Release report landed in /tmp/report.md"); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordUsage(store.NodeUsage{NodeID: "overnight", Cost: 0.82}); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.PostMessage(store.Message{
		SessionID: "yesterday", Role: store.RoleAgent,
		Body:    "Which region should receive the rollout?",
		Options: []store.QuestionOption{{Label: "Toronto"}, {Label: "Virginia"}},
	}); err != nil {
		t.Fatal(err)
	}

	// The return. A positive threshold, so the absence is measured rather than
	// waved through, and the two halves are called in the binary's order.
	var seen BriefActivity
	reconciler := New(graph, nil, nil).WithBriefComposer(
		func(_ context.Context, activity BriefActivity) (BriefDraft, error) {
			seen = activity
			items := make([]BriefDraftItem, 0, len(activity.Events))
			for _, event := range activity.Events {
				items = append(items, BriefDraftItem{Seq: event.Seq, Body: event.Text})
			}
			return BriefDraft{
				Headline: "While you were away: the release report landed and one choice is waiting.",
				Items:    items,
			}, nil
		})
	if err := reconciler.AttachSession("today"); err != nil {
		t.Fatal(err)
	}
	deliver, err := reconciler.SessionOpening("today", "tui", time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	if deliver == nil {
		t.Fatal("a return after real activity had nothing to deliver")
	}
	if err := deliver(context.Background()); err != nil {
		t.Fatal(err)
	}

	if seen.Done != 1 || seen.Questions != 1 || seen.CostUSD == 0 {
		t.Fatalf("the brief's window missed the absence: %+v", seen)
	}
	messages, err := graph.Messages("today", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	briefs := make([]store.Message, 0, 1)
	for _, message := range messages {
		if message.Brief != nil {
			briefs = append(briefs, message)
		}
	}
	if len(briefs) != 1 {
		t.Fatalf("a return posted %d briefs into the thread: %+v", len(briefs), messages)
	}
	brief := briefs[0]
	if brief.Role != store.RoleAgent || brief.NodeID != "" {
		t.Fatalf("the brief is not a thread-level agent line: role=%s node=%q", brief.Role, brief.NodeID)
	}
	if !strings.HasPrefix(brief.Body, "While you were away:") {
		t.Fatalf("the brief opens with %q", brief.Body)
	}
	if brief.Brief.Done != 1 || brief.Brief.Questions != 1 {
		t.Fatalf("the fold's counts do not match the absence: %+v", brief.Brief)
	}

	// And it is said once. Re-attaching in the same session has nothing new to
	// report, so returning twice does not repeat the news.
	again, err := reconciler.SessionOpening("today", "tui", time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	if again != nil {
		if err := again(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	messages, err = graph.Messages("today", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	repeats := 0
	for _, message := range messages {
		if message.Brief != nil {
			repeats++
		}
	}
	if repeats != 1 {
		t.Fatalf("a second attach brought the brief back: %d in the thread", repeats)
	}
}
