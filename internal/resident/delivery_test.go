package resident

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// spliceOvernightJob is one top-level job created in a session that is gone by
// the time it lands — the shape of a charter firing and of any overnight work.
func spliceOvernightJob(t *testing.T, graph *store.Store, id, sessionID string) {
	t.Helper()
	err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: id, Brief: "Survey the field", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: sessionID, Intent: "survey the field"})
	if err != nil {
		t.Fatalf("splice %s: %v", id, err)
	}
}

func TestDeliverableSpeaksIntoTheAttachedSessionWhenItsOwnIsGone(t *testing.T) {
	graph := openStore(t)
	spliceOvernightJob(t, graph, "overnight", "yesterday")
	reconciler := New(graph, nil, nil)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	// The originating terminal is closed and a new one is open: one continuous
	// person, two session ids.
	if _, err := graph.TouchSeen("tui", "yesterday", store.SeenDetached); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.TouchSeen("tui", "today", store.SeenAttached); err != nil {
		t.Fatal(err)
	}
	landNode(t, graph, "overnight", "Three posts worth your time.")
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	dead, err := graph.Messages("yesterday", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(dead) != 0 {
		t.Fatalf("the answer was posted into the sealed session: %+v", dead)
	}
	live, err := graph.Messages("today", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 1 || live[0].Body != "Three posts worth your time." {
		t.Fatalf("the answer did not reach the live session: %+v", live)
	}
}

func TestDeliverableStaysInItsOwnSessionWhileSomebodyIsInIt(t *testing.T) {
	graph := openStore(t)
	spliceOvernightJob(t, graph, "here", "now")
	reconciler := New(graph, nil, nil)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.TouchSeen("tui", "now", store.SeenAttached); err != nil {
		t.Fatal(err)
	}
	landNode(t, graph, "here", "Done.")
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	messages, err := graph.Messages("now", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Body != "Done." {
		t.Fatalf("live originating session lost its own answer: %+v", messages)
	}
}

func TestJustDeliveredWorkStaysAddressableThroughTheGraceWindow(t *testing.T) {
	graph := openStore(t)
	spliceOvernightJob(t, graph, "fresh", "s")
	reconciler := New(graph, nil, nil)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	landNode(t, graph, "fresh", "Here it is.")
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	node, ok, err := graph.Node("fresh")
	if err != nil || !ok {
		t.Fatalf("node: ok=%t err=%v", ok, err)
	}
	if node.Folded || node.FoldRoot {
		t.Fatalf("the announce tick folded the job it had just announced: %+v", node)
	}
	// Addressable is the whole point of the window: surgery selects on folded=0.
	targets, err := graph.SearchSurgeryTargets("survey the field", true, store.Done)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, target := range targets {
		if target.Node.ID == "fresh" {
			found = true
		}
	}
	if !found {
		t.Fatalf("just-delivered job is not addressable: %+v", targets)
	}

	reconciler.now = func() time.Time { return time.Now().Add(2 * settledFoldGrace) }
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	node, _, err = graph.Node("fresh")
	if err != nil {
		t.Fatal(err)
	}
	if !node.FoldRoot {
		t.Fatalf("the grace window never closed: %+v", node)
	}
}

func TestArrivalBriefDoesNotCountTheQuestionsTheArrivalRaised(t *testing.T) {
	graph := openStore(t)
	reconciler := New(graph, nil, nil)
	// Something real happened while the user was away.
	spliceOvernightJob(t, graph, "away", "yesterday")
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A blocking question was raised while the user was still here, and
	// answered by nobody. The arrival rescues it into the live session — which
	// is a message the arrival itself writes, below the attach edge and inside
	// the naive window.
	question, err := graph.AskQuestion(store.AgentQuestion{
		SessionID: "yesterday", Text: "Which airport?", Urgency: store.QuestionBlocking,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.SurfaceQuestion(question.Seq); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.TouchSeen("tui", "yesterday", store.SeenDetached); err != nil {
		t.Fatal(err)
	}
	landNode(t, graph, "away", "It is surveyed.")
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	var seen BriefActivity
	reconciler = reconciler.WithBriefComposer(
		func(_ context.Context, activity BriefActivity) (BriefDraft, error) {
			seen = activity
			return BriefDraft{Headline: "Quiet night."}, nil
		})
	if err := reconciler.AttachSession("today"); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.SessionOpened(context.Background(), "today", "tui", 0); err != nil {
		t.Fatal(err)
	}
	if seen.Questions != 0 {
		t.Fatalf("the brief counted a question its own arrival surfaced: %+v", seen.Events)
	}
	if seen.Done != 1 {
		t.Fatalf("the brief lost the work that actually landed: %+v", seen.Events)
	}
}

func TestBriefSaysWhatIsWaitingOnTheUser(t *testing.T) {
	graph := openStore(t)
	spliceOvernightJob(t, graph, "blocked-job", "s")
	question, err := graph.AskQuestion(store.AgentQuestion{
		SessionID: "s", Text: "Which airport?", OriginNodeID: "blocked-job",
		Urgency: store.QuestionBlocking,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.SurfaceQuestion(question.Seq); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.TouchSeen("tui", "s", store.SeenDetached); err != nil {
		t.Fatal(err)
	}

	var seen BriefActivity
	reconciler := New(graph, nil, nil).WithBriefComposer(
		func(_ context.Context, activity BriefActivity) (BriefDraft, error) {
			seen = activity
			return BriefDraft{}, nil
		})
	reconciler.now = func() time.Time { return time.Now().Add(3 * 24 * time.Hour) }
	if err := reconciler.SessionOpened(context.Background(), "s2", "tui", 0); err != nil {
		t.Fatal(err)
	}
	if seen.Waiting != 1 {
		t.Fatalf("the brief reported nothing standing: %+v", seen.Events)
	}
	waiting := ""
	for _, event := range seen.Events {
		if event.Kind == store.BriefWaiting {
			waiting = event.Text
		}
	}
	if !strings.Contains(waiting, "stopped") || !strings.Contains(waiting, "waiting on one answer") {
		t.Fatalf("waiting row does not name the cost of the delay: %q", waiting)
	}
	if headline := defaultBriefHeadline(seen); !strings.Contains(headline, "1 waiting on you") {
		t.Fatalf("headline hides what is waiting: %q", headline)
	}
}

// A long deliverable is announced whole. The measured defect was a review that
// stopped mid-word at 4,096 bytes and never reached its verdict: the store
// bounded a settled node's summary with the bound meant for what a reader takes
// OUT of it, so the deliverable was already amputated by the time anything
// could announce or export it. The thread's own bound is the only one that
// belongs on this path.
func TestALongDeliverableIsAnnouncedWhole(t *testing.T) {
	graph := openStore(t)
	spliceOvernightJob(t, graph, "review", "now")
	reconciler := New(graph, nil, nil)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.TouchSeen("tui", "now", store.SeenAttached); err != nil {
		t.Fatal(err)
	}
	verdict := "VERDICT: request changes."
	deliverable := strings.Repeat("the parser change reads correctly and is tested. ", 200) + "\n" + verdict
	if len(deliverable) < 8<<10 {
		t.Fatalf("the fixture is only %d bytes — it has to be longer than the old bound", len(deliverable))
	}
	landNode(t, graph, "review", deliverable)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	node, ok, err := graph.Node("review")
	if err != nil || !ok {
		t.Fatalf("node: ok=%v err=%v", ok, err)
	}
	if !strings.HasSuffix(node.Summary, verdict) {
		t.Fatalf("the journal clipped the deliverable at %d bytes of %d", len(node.Summary), len(deliverable))
	}
	messages, err := graph.Messages("now", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 {
		t.Fatalf("want one announcement, got %d", len(messages))
	}
	if !strings.HasSuffix(messages[0].Body, verdict) {
		t.Fatalf("the announcement clipped the deliverable at %d bytes of %d",
			len(messages[0].Body), len(deliverable))
	}
}

func TestClippedResultKeepsItsFilePaths(t *testing.T) {
	prose := strings.Repeat("the report explains everything at length. ", 40)
	block := prose + "\nFiles:\n/tmp/ws/report.md\n/tmp/ws/data.csv"
	clipped := clipKeepingFiles(block, 300)
	for _, path := range []string{"/tmp/ws/report.md", "/tmp/ws/data.csv"} {
		if !strings.Contains(clipped, path) {
			t.Fatalf("clip dropped %s: %q", path, clipped)
		}
	}
	if len(clipped) > 300 {
		t.Fatalf("clip blew its budget: %d bytes", len(clipped))
	}
	if !strings.Contains(clipped, "the report explains") {
		t.Fatalf("clip kept no prose at all: %q", clipped)
	}
}
