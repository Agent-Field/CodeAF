package resident

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func askBlockingQuestion(t *testing.T, graph *store.Store, reconciler *Reconciler, session, text string) store.AgentQuestion {
	t.Helper()
	question, err := reconciler.AskQuestion(store.AgentQuestion{
		SessionID: session, Text: text, Urgency: store.QuestionBlocking,
	})
	if err != nil {
		t.Fatalf("ask %q: %v", text, err)
	}
	if question.Status != store.QuestionAsked {
		t.Fatalf("blocking question was not surfaced: %+v", question)
	}
	return question
}

func postAs(t *testing.T, graph *store.Store, session string, role store.Role, body string) store.Message {
	t.Helper()
	message, err := graph.PostMessage(store.Message{SessionID: session, Role: role, Body: body})
	if err != nil {
		t.Fatalf("post %q: %v", body, err)
	}
	return message
}

func questionPosts(t *testing.T, graph *store.Store, session string, seq int64) int {
	t.Helper()
	messages, err := graph.Messages(session, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	posts := 0
	for _, message := range messages {
		if message.QuestionSeq == seq {
			posts++
		}
	}
	return posts
}

// The second half of the blocker. Two questions were open; the user answered
// the newer one, and that answer is itself an intervening user turn for the
// older one — so the guard that keeps a question from capturing unrelated chat
// fired against it forever. The words stayed on screen, the worker stayed
// blocked, and nothing ever said so.
func TestAQuestionSteppedOverComesBack(t *testing.T) {
	graph := openStore(t)
	reconciler := New(graph, nil, nil)
	if err := reconciler.SessionOpened(context.Background(), "stranded", "tui", time.Hour); err != nil {
		t.Fatal(err)
	}
	older := askBlockingQuestion(t, graph, reconciler, "stranded", "Which branch should the fix target?")
	newer := askBlockingQuestion(t, graph, reconciler, "stranded", "Start the migration plan at $4.10?")

	// The user types one digit; by recency it lands on the newer question.
	answer := postAs(t, graph, "stranded", store.RoleUser, "1")
	if err := graph.ResolveQuestion(newer.Seq, store.QuestionAnswered, "start it", answer.Seq); err != nil {
		t.Fatal(err)
	}
	postAs(t, graph, "stranded", store.RoleAgent, "Got it — starting it now.")

	// That is the stranding: nothing the user types can reach the older one.
	probe := postAs(t, graph, "stranded", store.RoleUser, "main")
	if reachable, ok, err := graph.QuestionForAnswer("stranded", probe.Seq, 0); err != nil || ok {
		t.Fatalf("the fixture is not the failure: %+v ok=%t err=%v", reachable, ok, err)
	}
	postAs(t, graph, "stranded", store.RoleAgent, "Noted.")

	reconciler.now = func() time.Time { return time.Now().Add(2 * questionResurfaceQuiet) }
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if posts := questionPosts(t, graph, "stranded", older.Seq); posts != 2 {
		t.Fatalf("the stepped-over question was posted %d times, want it asked again", posts)
	}
	back := postAs(t, graph, "stranded", store.RoleUser, "main")
	reachable, ok, err := graph.QuestionForAnswer("stranded", back.Seq, 0)
	if err != nil || !ok || reachable.Seq != older.Seq {
		t.Fatalf("the question that came back is still unanswerable: %+v ok=%t err=%v", reachable, ok, err)
	}
}

// It comes back, it does not nag: a question re-asked a moment ago waits, and
// a conversation the user is still mid-turn in is not the moment.
func TestAQuestionThatCameBackDoesNotComeBackAgainAtOnce(t *testing.T) {
	graph := openStore(t)
	reconciler := New(graph, nil, nil)
	if err := reconciler.SessionOpened(context.Background(), "quiet-return", "tui", time.Hour); err != nil {
		t.Fatal(err)
	}
	question := askBlockingQuestion(t, graph, reconciler, "quiet-return", "Which branch should the fix target?")
	postAs(t, graph, "quiet-return", store.RoleUser, "something else entirely")

	// The user's turn has not been answered yet: cutting in here is not a
	// natural moment, it is an interruption.
	reconciler.now = func() time.Time { return time.Now().Add(2 * questionResurfaceQuiet) }
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if posts := questionPosts(t, graph, "quiet-return", question.Seq); posts != 1 {
		t.Fatalf("the question cut into a turn in progress: %d posts", posts)
	}

	postAs(t, graph, "quiet-return", store.RoleAgent, "Here you go.")
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if posts := questionPosts(t, graph, "quiet-return", question.Seq); posts != 2 {
		t.Fatalf("the question did not come back once the thread moved on: %d posts", posts)
	}
	// Straight away again is nagging: it has only just been asked, so it waits.
	reconciler.now = time.Now
	postAs(t, graph, "quiet-return", store.RoleUser, "and another thing")
	postAs(t, graph, "quiet-return", store.RoleAgent, "Noted.")
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if posts := questionPosts(t, graph, "quiet-return", question.Seq); posts != 2 {
		t.Fatalf("the question nagged: %d posts", posts)
	}
}

// And when a stepped-over question does run out its own window, it goes loudly:
// what was asked, and that the request behind it never went ahead.
func TestAStrandedQuestionThatAgesOutSaysSo(t *testing.T) {
	graph := openStore(t)
	compile := func(context.Context, string, string) (Compiled, error) {
		return Compiled{Question: "Which airport — LHR or LGW?"}, nil
	}
	reconciler := New(graph, compile, nil)
	if err := reconciler.SessionOpened(context.Background(), "lapse-stranded", "tui", time.Hour); err != nil {
		t.Fatal(err)
	}
	question := askCompileQuestion(t, graph, reconciler, "lapse-stranded")
	other := askBlockingQuestion(t, graph, reconciler, "lapse-stranded", "Start the migration plan at $4.10?")

	answer := postAs(t, graph, "lapse-stranded", store.RoleUser, "1")
	if err := graph.ResolveQuestion(other.Seq, store.QuestionAnswered, "start it", answer.Seq); err != nil {
		t.Fatal(err)
	}
	postAs(t, graph, "lapse-stranded", store.RoleAgent, "Got it — starting it now.")

	reconciler.now = func() time.Time { return time.Now().Add(4 * compileAskWindow) }
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	settled, found, err := graph.AgentQuestionBySeq(question.Seq)
	if err != nil || !found || settled.Status != store.QuestionExpired {
		t.Fatalf("stranded question after its window = %+v found=%t err=%v", settled, found, err)
	}
	messages, err := graph.Messages("lapse-stranded", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	said := false
	for _, message := range messages {
		if strings.Contains(message.Body, "lapsed unanswered") &&
			strings.Contains(message.Body, "never went ahead") &&
			strings.Contains(message.Body, "Which airport") {
			said = true
		}
	}
	if !said {
		t.Fatalf("a stranded request lapsed silently: %+v", messages)
	}
}
