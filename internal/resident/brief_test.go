package resident

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func TestSessionOpenedComposesOneJournaledBriefFromAwayActivity(t *testing.T) {
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
	if err := graph.Complete(claim, "Release report landed in /tmp/report.md"); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordUsage(store.NodeUsage{NodeID: "report", Cost: 1.4}); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RecordFact("report", "repo:release", store.FactPlain, "The release branch is stable."); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.PostMessage(store.Message{
		SessionID: "old", Role: store.RoleAgent, Body: "Which region should receive the rollout?",
		Options: []store.QuestionOption{{Label: "Toronto"}, {Label: "Virginia"}},
	}); err != nil {
		t.Fatal(err)
	}

	calls := 0
	var received BriefActivity
	compose := func(_ context.Context, activity BriefActivity) (BriefDraft, error) {
		calls++
		received = activity
		items := make([]BriefDraftItem, 0, len(activity.Events))
		for _, event := range activity.Events {
			items = append(items, BriefDraftItem{Seq: event.Seq, Body: "calm · " + event.Text})
		}
		return BriefDraft{
			Headline: "While you were away: the report landed, one choice is waiting, and $1.40 was spent.",
			Items:    items,
		}, nil
	}
	reconciler := New(graph, nil, nil).WithBriefComposer(compose)
	if err := reconciler.SessionOpened(context.Background(), "arrival", "tui", 0); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || received.Done != 1 || received.Questions != 1 ||
		received.FactsLearned != 1 || received.CostUSD != 1.4 {
		t.Fatalf("composer calls=%d activity=%+v", calls, received)
	}
	messages, err := graph.Messages("arrival", 0, 0)
	if err != nil || len(messages) != 1 {
		t.Fatalf("arrival messages=%+v err=%v", messages, err)
	}
	brief := messages[0]
	if brief.Role != store.RoleAgent || brief.Brief == nil ||
		!strings.HasPrefix(brief.Body, "While you were away:") || len(brief.Brief.Items) != len(received.Events) {
		t.Fatalf("posted brief=%+v", brief)
	}
	if brief.Brief.Done != 1 || brief.Brief.Questions != 1 || brief.Brief.FactsLearned != 1 ||
		brief.Brief.CostUSD != 1.4 || brief.Brief.ThroughSeq <= brief.Brief.SinceSeq {
		t.Fatalf("brief metadata=%+v", brief.Brief)
	}
	for _, item := range brief.Brief.Items {
		if !strings.HasPrefix(item.Body, "calm · ") {
			t.Fatalf("item did not use composed voice: %+v", item)
		}
	}
}

func TestSessionOpenedStaysSilentWithoutAwayActivity(t *testing.T) {
	graph := openStore(t)
	calls := 0
	reconciler := New(graph, nil, nil).WithBriefComposer(func(_ context.Context, activity BriefActivity) (BriefDraft, error) {
		calls++
		return BriefDraft{}, fmt.Errorf("composer should not be called for %+v", activity)
	})

	// A first-ever attach establishes the watermark without inventing a brief.
	if err := reconciler.SessionOpened(context.Background(), "first", "tui", 0); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.SessionClosed("first", "tui"); err != nil {
		t.Fatal(err)
	}
	// Detach/attach events themselves are not background life.
	if err := reconciler.SessionOpened(context.Background(), "second", "tui", 0); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("silent opens called composer %d times", calls)
	}
	messages, err := graph.Messages("second", 0, 0)
	if err != nil || len(messages) != 0 {
		t.Fatalf("silent arrival posted messages=%+v err=%v", messages, err)
	}
}

func TestSessionOpenedHonorsAbsenceThreshold(t *testing.T) {
	graph := openStore(t)
	if _, err := graph.TouchSeen("tui", "old", store.SeenDetached); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.PostMessage(store.Message{
		SessionID: "old", Role: store.RoleAgent, Body: "Which region should receive the rollout?",
	}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	reconciler := New(graph, nil, nil).WithBriefComposer(func(context.Context, BriefActivity) (BriefDraft, error) {
		calls++
		return BriefDraft{}, nil
	})
	if err := reconciler.SessionOpened(context.Background(), "arrival", "tui", 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("short absence called the composer %d times", calls)
	}
	messages, err := graph.Messages("arrival", 0, 0)
	if err != nil || len(messages) != 0 {
		t.Fatalf("short absence posted brief=%+v err=%v", messages, err)
	}
}

func TestMaterializeBriefKeepsTheFoldToOneCanonicalSentence(t *testing.T) {
	message := materializeBrief(2, 7, BriefActivity{
		Events: []BriefEvent{{Seq: 5, Kind: store.BriefDone, Text: "Report landed."}},
		Done:   1,
	}, BriefDraft{
		Headline: "While you were away — the report landed. Here is a second dashboard sentence.",
	})
	if message.Body != "While you were away: the report landed." {
		t.Fatalf("fold headline = %q", message.Body)
	}
}

func TestSessionOpenedIncludesCancelledTopLevelWork(t *testing.T) {
	graph := openStore(t)
	if _, err := graph.TouchSeen("web", "old", store.SeenDetached); err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "obsolete", Brief: "Prepare the obsolete export", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, Intent: "prepare the export"}); err != nil {
		t.Fatal(err)
	}
	if err := graph.CancelPending("obsolete", "replaced by the new export"); err != nil {
		t.Fatal(err)
	}
	var got BriefActivity
	reconciler := New(graph, nil, nil).WithBriefComposer(func(_ context.Context, activity BriefActivity) (BriefDraft, error) {
		got = activity
		return BriefDraft{}, nil
	})
	if err := reconciler.SessionOpened(context.Background(), "arrival", "tui", 0); err != nil {
		t.Fatal(err)
	}
	if got.Cancelled != 1 || len(got.Events) != 1 || got.Events[0].Kind != store.BriefCancelled {
		t.Fatalf("cancelled activity = %+v", got)
	}
}
