package head

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// seedLiveRedirectJob is the shape the live failure had: one job of the user's
// own, two workers inside it mid-turn, and a brief that shares a word with what
// the user is about to type — so the redirection anchors without a question.
func seedLiveRedirectJob(t *testing.T, graph *store.Store) {
	t.Helper()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "parser", Title: "the parser rewrite", Brief: "understand the repo and rewrite the parser", Stage: 2},
		{ID: "parser-a", Parent: "parser", Title: "Survey", Brief: "read the repo", Stage: 1},
		{ID: "parser-b", Parent: "parser", Title: "Rewrite", Brief: "rewrite the parser", Stage: 1},
	}}, store.Provenance{
		Origin: store.OriginUser, SessionID: "steer", Intent: "understand the repo and rewrite the parser",
	}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"parser-a", "parser-b"} {
		claim, won, err := graph.Claim(id, "worker-"+id)
		if err != nil || !won {
			t.Fatalf("claim %s won=%t err=%v", id, won, err)
		}
		if err := graph.Start(claim); err != nil {
			t.Fatal(err)
		}
	}
}

// overStudyMessage is the sentence from the journal, verbatim. It steers the
// running job and teaches something meant to outlast it, in one breath.
const overStudyMessage = "you seem to be wasting a lot of time on understanding the repo instead of " +
	"working fast, just learn how to do it better dont change anything else in running one now " +
	"i am just asking you to learn"

func agentReplies(t *testing.T, graph *store.Store, session string) []store.Message {
	t.Helper()
	messages, err := graph.Messages(session, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	replies := make([]store.Message, 0, len(messages))
	for _, message := range messages {
		if message.Role == store.RoleAgent && strings.TrimSpace(message.NodeID) == "" {
			replies = append(replies, message)
		}
	}
	return replies
}

// The live bug. A redirection informed two running workers, the reconciler filed
// its receipt under the job's own card, and the thread said nothing at all — so
// the user sat watching a still conversation wondering whether their words had
// landed anywhere. Every redirection now ends in one visible line.
func TestRedirectAlwaysAnswersInTheThread(t *testing.T) {
	graph := openHeadStore(t)
	seedLiveRedirectJob(t, graph)
	client := &fakeClient{responses: []string{
		`{"reply":"Passed that to the two workers on the parser rewrite — I'll say what changed once it lands.","remember":null}`,
	}}
	user := postUser(t, graph, "steer", overStudyMessage)

	handled, err := New(client, graph).manageRedirect(context.Background(), user)
	if err != nil || !handled {
		t.Fatalf("redirect not handled: handled=%t err=%v", handled, err)
	}
	commands := pendingCommandsOf(t, graph)
	if len(commands) != 1 || commands[0].Kind != store.CommandRedirect || commands[0].Target != "parser" {
		t.Fatalf("redirect command = %+v", commands)
	}
	replies := agentReplies(t, graph, "steer")
	if len(replies) != 1 {
		t.Fatalf("the redirect posted %d replies, want exactly one", len(replies))
	}
	if strings.TrimSpace(replies[0].Body) == "" {
		t.Fatal("the redirect posted an empty body, which is silence wearing a message id")
	}
	// The reply is tied to the durable work it acknowledges, so the thread can
	// show it beside the change rather than as a loose remark.
	if replies[0].CommandSeq != commands[0].Seq {
		t.Fatalf("reply command seq = %d, want %d", replies[0].CommandSeq, commands[0].Seq)
	}
	// The composer was given the real audience, not a guess.
	if prompt := client.systemPrompt(); !strings.Contains(prompt, revisionVoicePrompt) {
		t.Fatalf("the redirect reply did not go through the voice path: %q", prompt)
	}
	if len(client.seen) < 2 || !strings.Contains(client.seen[1].Content[0].Text,
		"Steps of that work already under way, which hear their words verbatim: 2") {
		t.Fatalf("the composer was not told who is mid-turn: %+v", client.seen)
	}
}

// Position by volatility, not by semantic category. The audience count reads as
// a fact about the work, which is why it was written up beside the work's name;
// it is a count of steps that are mid-turn AT THIS INSTANT, and it moves as
// workers finish their turns. So it rides at the bottom, just above the message
// it is about, where a new value invalidates nothing but itself — while the
// job's name, what was done with the user's words, and the appending thread
// hold the front.
func TestRevisionAudienceCountSitsJustAboveTheMessage(t *testing.T) {
	graph := openHeadStore(t)
	seedLiveRedirectJob(t, graph)
	client := &fakeClient{responses: []string{`{"reply":"Taking that to the parser rewrite.","remember":null}`}}
	user := postUser(t, graph, "steer", overStudyMessage)

	if handled, err := New(client, graph).manageRedirect(context.Background(), user); err != nil || !handled {
		t.Fatalf("redirect not handled: handled=%t err=%v", handled, err)
	}
	prompt := client.userPrompt()
	work := strings.Index(prompt, "The work: ")
	thread := strings.Index(prompt, "Recent thread before this message:")
	notebook := strings.Index(prompt, "Notebook (durable memory")
	audience := strings.Index(prompt, "Steps of that work already under way")
	message := strings.Index(prompt, "Current user message (verbatim):")
	for name, index := range map[string]int{
		"the work": work, "the thread": thread, "the notebook": notebook,
		"the audience count": audience, "the message": message,
	} {
		if index < 0 {
			t.Fatalf("%s never reached the composer:\n%s", name, prompt)
		}
	}
	if !(work < thread && thread < notebook && notebook < audience && audience < message) {
		t.Fatalf("the composer's prompt is not ordered by volatility "+
			"(work=%d thread=%d notebook=%d audience=%d message=%d):\n%s",
			work, thread, notebook, audience, message, prompt)
	}
}

// A composer that fails is not a licence to go quiet: the command is already
// journaled when it runs, so the plain sentence goes out in its place.
func TestRedirectSpeaksEvenWhenTheComposerFails(t *testing.T) {
	graph := openHeadStore(t)
	seedLiveRedirectJob(t, graph)
	// No responses left, so every call errors.
	client := &fakeClient{}
	user := postUser(t, graph, "steer", overStudyMessage)

	if handled, err := New(client, graph).manageRedirect(context.Background(), user); err != nil || !handled {
		t.Fatalf("redirect not handled: handled=%t err=%v", handled, err)
	}
	replies := agentReplies(t, graph, "steer")
	if len(replies) != 1 || strings.TrimSpace(replies[0].Body) == "" {
		t.Fatalf("a failed composer left the thread silent: %+v", replies)
	}
	if !strings.Contains(replies[0].Body, "the parser rewrite") ||
		!strings.Contains(replies[0].Body, "2 steps already under way") {
		t.Fatalf("the fallback said nothing true about the work: %q", replies[0].Body)
	}
}

// The teaching half. "i am just asking you to learn" is a redirection AND a
// lesson, and the lesson used to be whispered into two workers that would die
// with it while the notebook stayed empty. It lands as a durable belief now, on
// the stated channel, through the same seam the router captures with.
func TestRedirectCapturesTheLessonItCarries(t *testing.T) {
	graph := openHeadStore(t)
	seedLiveRedirectJob(t, graph)
	client := &fakeClient{responses: []string{
		`{"reply":"Passing that to the two workers on the parser rewrite, and I've noted the pacing lesson.",` +
			`"remember":{"scope":"user","kind":"lesson",` +
			`"body":"Spend far less time studying a repository before acting; start work early and learn from the change."}}`,
	}}
	user := postUser(t, graph, "steer", overStudyMessage)

	if handled, err := New(client, graph).manageRedirect(context.Background(), user); err != nil || !handled {
		t.Fatalf("redirect not handled: handled=%t err=%v", handled, err)
	}
	facts, err := graph.RecentFacts(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 {
		t.Fatalf("the redirect recorded %d beliefs, want the one it was taught: %+v", len(facts), facts)
	}
	fact := facts[0]
	if fact.Kind != store.FactLesson || fact.Channel != store.FactChannelStated || fact.Scope != "user" {
		t.Fatalf("captured belief = %+v", fact)
	}
	// Durable guidance, not the pointing sentence that carried it.
	if !strings.Contains(fact.Body, "studying a repository") ||
		strings.Contains(fact.Body, "running one now") {
		t.Fatalf("the lesson was captured deictically: %q", fact.Body)
	}
	if replies := agentReplies(t, graph, "steer"); len(replies) != 1 ||
		!strings.Contains(replies[0].Body, "noted") {
		t.Fatalf("the reply never confirmed what it recorded: %+v", replies)
	}
}

// And the other direction, which matters as much: an ordinary change of course
// teaches nothing, the model says so, and the notebook stays empty. Capture is
// the model's judgment about this sentence, never a word it happens to contain.
func TestRedirectWithNothingToTeachRecordsNothing(t *testing.T) {
	graph := openHeadStore(t)
	seedLiveRedirectJob(t, graph)
	client := &fakeClient{responses: []string{
		`{"reply":"Taking that to the parser rewrite — both workers have it.","remember":null}`,
	}}
	user := postUser(t, graph, "steer", "focus on the tokenizer instead of the parser")

	if handled, err := New(client, graph).manageRedirect(context.Background(), user); err != nil || !handled {
		t.Fatalf("redirect not handled: handled=%t err=%v", handled, err)
	}
	facts, err := graph.RecentFacts(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 0 {
		t.Fatalf("a redirection that taught nothing wrote to the notebook: %+v", facts)
	}
	if replies := agentReplies(t, graph, "steer"); len(replies) != 1 {
		t.Fatalf("replies = %+v, want exactly one", replies)
	}
}

// The floor itself. Every route that acts on the graph ends in this call, so an
// empty body has to become an honest try-again here rather than a message id
// carrying nothing — that is what makes a silent route structurally impossible
// rather than merely fixed once.
func TestPostAgentFloorNeverPostsAnEmptyBody(t *testing.T) {
	graph := openHeadStore(t)
	head := New(&fakeClient{}, graph)
	if err := head.postAgentFloor("floor", "   \n ", 0, ""); err != nil {
		t.Fatal(err)
	}
	if err := head.postAgentFloor("floor", "a real line", 0, ""); err != nil {
		t.Fatal(err)
	}
	replies := agentReplies(t, graph, "floor")
	if len(replies) != 2 {
		t.Fatalf("replies = %+v", replies)
	}
	if replies[0].Body != providerErrorReply {
		t.Fatalf("the empty body was posted as-is: %q", replies[0].Body)
	}
	if replies[1].Body != "a real line" {
		t.Fatalf("a real line was rewritten: %q", replies[1].Body)
	}
}
