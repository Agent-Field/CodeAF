package resident

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// askCompileQuestion drives the real command path, because the whole point of
// the finding is what the command path attaches to a compiler askback.
func askCompileQuestion(t *testing.T, graph *store.Store, reconciler *Reconciler, sessionID string) store.AgentQuestion {
	t.Helper()
	if _, err := graph.RequestCommand(store.Command{
		SessionID: sessionID, Kind: store.CommandSplice, Instruction: "book the flight",
	}); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	questions, err := graph.UnresolvedQuestions(50)
	if err != nil || len(questions) != 1 {
		t.Fatalf("compiler askback = %+v err=%v", questions, err)
	}
	return questions[0]
}

// "book the flight" → "which airport?" → the user quits. Next morning the
// question was gone from the dock, unanswerable, unexpirable and unmentioned:
// it had no node and no charter, so no origin could retire it, and it was
// already Asked, so the pending sweep skipped it forever.
func TestOrphanedBlockingQuestionIsCarriedIntoTheLiveSession(t *testing.T) {
	graph := openStore(t)
	compile := func(context.Context, string, string) (Compiled, error) {
		return Compiled{Question: "Which airport — LHR or LGW?"}, nil
	}
	first := New(graph, compile, nil)
	if err := first.SessionOpened(context.Background(), "last-night", "tui", time.Hour); err != nil {
		t.Fatal(err)
	}
	question := askCompileQuestion(t, graph, first, "last-night")
	if question.SessionID != "last-night" || question.Status != store.QuestionAsked {
		t.Fatalf("askback = %+v", question)
	}
	if question.ExpiresAt.IsZero() {
		t.Fatalf("compiler askback has no relevance window: %+v", question)
	}
	if err := first.SessionClosed("last-night", "tui"); err != nil {
		t.Fatal(err)
	}

	// A new morning is a new session id; the old one will never be attached to.
	morning := New(graph, compile, nil)
	if err := morning.SessionOpened(context.Background(), "this-morning", "tui", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := morning.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	carried, found, err := graph.AgentQuestionBySeq(question.Seq)
	if err != nil || !found || carried.SessionID != "this-morning" {
		t.Fatalf("orphaned question was not carried over: %+v found=%t err=%v", carried, found, err)
	}
	messages, err := graph.Messages("this-morning", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	reposted := 0
	for _, message := range messages {
		if message.QuestionSeq == question.Seq {
			reposted++
		}
	}
	if reposted != 1 {
		t.Fatalf("question was not re-asked exactly once: %+v", messages)
	}
	// And the user's answer can now actually reach it: the lookup is
	// session-filtered, so re-posting the words without moving the question
	// would have shown a prompt no reply could land on.
	reply, err := graph.PostMessage(store.Message{
		SessionID: "this-morning", Role: store.RoleUser, Body: "LHR",
	})
	if err != nil {
		t.Fatal(err)
	}
	if answerable, ok, err := graph.QuestionForAnswer("this-morning", reply.Seq, 0); err != nil || !ok ||
		answerable.Seq != question.Seq {
		t.Fatalf("carried question is still unanswerable: %+v ok=%t err=%v", answerable, ok, err)
	}
	// It settles: a second pass must not keep re-posting it.
	if err := morning.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	messages, err = graph.Messages("this-morning", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	reposted = 0
	for _, message := range messages {
		if message.QuestionSeq == question.Seq {
			reposted++
		}
	}
	if reposted != 1 {
		t.Fatalf("carried question was re-posted again: %+v", messages)
	}
}

// A blocking question is one the system said it could not go on without, so its
// lapse is the request's lapse — and that is news, not housekeeping.
func TestLapsedCompilerQuestionSaysSoInTheThread(t *testing.T) {
	graph := openStore(t)
	compile := func(context.Context, string, string) (Compiled, error) {
		return Compiled{Question: "Which airport — LHR or LGW?"}, nil
	}
	reconciler := New(graph, compile, nil)
	if err := reconciler.SessionOpened(context.Background(), "lapse", "tui", time.Hour); err != nil {
		t.Fatal(err)
	}
	question := askCompileQuestion(t, graph, reconciler, "lapse")

	reconciler.now = func() time.Time { return time.Now().Add(4 * compileAskWindow) }
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	settled, found, err := graph.AgentQuestionBySeq(question.Seq)
	if err != nil || !found || settled.Status != store.QuestionExpired {
		t.Fatalf("question after its window = %+v found=%t err=%v", settled, found, err)
	}
	messages, err := graph.Messages("lapse", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	said := false
	for _, message := range messages {
		if strings.Contains(message.Body, "lapsed unanswered") &&
			strings.Contains(message.Body, "never went ahead") {
			said = true
		}
	}
	if !said {
		t.Fatalf("the request lapsed silently: %+v", messages)
	}
}

// The loud lapse is narrow on purpose. A question retired because its job
// settled already has a visible reason in the thread, and saying the request
// "never went ahead" about a job that finished would be a lie.
func TestQuestionRetiredByItsOriginLapsesQuietly(t *testing.T) {
	graph := openStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "origin-job", Brief: "do the thing", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, SessionID: "quiet", Intent: "do the thing"}); err != nil {
		t.Fatal(err)
	}
	reconciler := New(graph, nil, nil)
	if err := reconciler.SessionOpened(context.Background(), "quiet", "tui", time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := reconciler.AskQuestion(store.AgentQuestion{
		SessionID: "quiet", Text: "keep going?", OriginNodeID: "origin-job",
		Urgency: store.QuestionBlocking,
	}); err != nil {
		t.Fatal(err)
	}
	claim, won, err := graph.Claim("origin-job", "worker")
	if err != nil || !won {
		t.Fatalf("claim: won=%t err=%v", won, err)
	}
	if err := graph.Complete(claim, "did the thing"); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	messages, err := graph.Messages("quiet", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range messages {
		if strings.Contains(message.Body, "lapsed unanswered") {
			t.Fatalf("an origin-retired question announced itself: %q", message.Body)
		}
	}
}

// A second window that is still being used is not an orphan; taking its
// question away would be the same theft in the other direction.
func TestALiveSecondWindowKeepsItsOwnQuestion(t *testing.T) {
	graph := openStore(t)
	compile := func(context.Context, string, string) (Compiled, error) {
		return Compiled{Question: "Which airport — LHR or LGW?"}, nil
	}
	reconciler := New(graph, compile, nil)
	if err := reconciler.SessionOpened(context.Background(), "window-a", "tui", time.Hour); err != nil {
		t.Fatal(err)
	}
	question := askCompileQuestion(t, graph, reconciler, "window-a")
	// The user keeps talking in window A after the question was asked.
	if _, err := graph.PostMessage(store.Message{
		SessionID: "window-a", Role: store.RoleUser, Body: "hold on, checking",
	}); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.SessionOpened(context.Background(), "window-b", "web", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	held, found, err := graph.AgentQuestionBySeq(question.Seq)
	if err != nil || !found || held.SessionID != "window-a" {
		t.Fatalf("a live window's question was taken from it: %+v found=%t err=%v", held, found, err)
	}
}
