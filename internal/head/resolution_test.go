package head

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The correction that names its work in its own words wins over the job that
// merely spoke most recently. Before this, adjacency led and returned on its
// first hit, so the vocabulary arm was unreachable whenever anything had
// narrated — and a correction aimed at the auth fix bought a paid revision of
// whatever job happened to have a heartbeat in the window.
//
// The anchor is the one referent no board read resolves — a delivered job is
// over — so it is computed before the loop runs and put in front of it as
// evidence. Acting on it is the correct tool, which hands the job its own
// previous version and the criticism together.
func TestDescribedWorkBeatsTheWorkThatSpokeLast(t *testing.T) {
	graph := openHeadStore(t)
	deliverJob(t, graph, "resolve", "auth-fix", "Auth token refresh",
		"patch the auth token refresh", "The refresh path now retries once and the tests pass.")
	deliverJob(t, graph, "resolve", "parser-fix", "Parser rewrite",
		"rewrite the config parser", "The parser accepts trailing commas now.")

	user := postUser(t, graph, "resolve", "actually the auth refresh is still broken")
	reading := deterministicReading(t, New(nil, graph), user)
	if !strings.Contains(reading, "reads as a correction") {
		t.Fatalf("the correction reading never reached the loop:\n%s", reading)
	}
	if !strings.Contains(reading, "the delivered work it most likely means is auth-fix") {
		t.Fatalf("the words did not outrank the job that spoke last:\n%s", reading)
	}

	head, _ := beltHead(graph, beltTurn{calls: []ai.ToolCall{
		beltCall("c1", beltToolCorrect, map[string]any{
			"job": "auth-fix", "words": user.Body})}}, beltTurn{})
	if err := head.answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}

	commands := pendingCommandsOf(t, graph)
	if len(commands) != 1 {
		t.Fatalf("journaled %d commands, want one revision: %+v", len(commands), commands)
	}
	if commands[0].Target != "auth-fix" {
		t.Fatalf("the correction landed on %q, want the job the words name", commands[0].Target)
	}
	if !IsCorrection(commands[0].Instruction) {
		t.Fatalf("the command is not a revision of the deliverable:\n%s", commands[0].Instruction)
	}
	if !strings.Contains(commands[0].Instruction, user.Body) {
		t.Fatalf("the criticism did not travel verbatim:\n%s", commands[0].Instruction)
	}
}

// With nothing in the sentence to go on, position is the whole signal and it is
// the one a person would use: the follow-up answers whatever just spoke.
func TestContentlessFollowUpAnswersTheWorkThatSpokeLast(t *testing.T) {
	graph := openHeadStore(t)
	deliverJob(t, graph, "adjacent", "auth-fix", "Auth token refresh",
		"patch the auth token refresh", "The refresh path now retries once.")
	deliverJob(t, graph, "adjacent", "parser-fix", "Parser rewrite",
		"rewrite the config parser", "The parser accepts trailing commas now.")

	user := postUser(t, graph, "adjacent", "actually that needs to say much more about the trade-offs")
	reading := deterministicReading(t, New(nil, graph), user)
	if !strings.Contains(reading, "the delivered work it most likely means is parser-fix") {
		t.Fatalf("the follow-up was not anchored to the last thing said:\n%s", reading)
	}

	head, _ := beltHead(graph, beltTurn{calls: []ai.ToolCall{
		beltCall("c1", beltToolCorrect, map[string]any{
			"job": "parser-fix", "words": user.Body})}}, beltTurn{})
	if err := head.answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	commands := pendingCommandsOf(t, graph)
	if len(commands) != 1 || commands[0].Target != "parser-fix" {
		t.Fatalf("the follow-up did not answer the last thing said: %+v", commands)
	}
}

// When the words name more than one delivered job, nothing about the ranking is
// a decision. The reading says so in as many words — the rivals, and an
// instruction not to choose between them — and one short question in plain words
// settles it, naming the jobs the way the user would recognise them. Nothing is
// journaled until they answer.
func TestGenuineAmbiguityAsksOnePlainQuestion(t *testing.T) {
	graph := openHeadStore(t)
	deliverJob(t, graph, "ambiguous", "auth-fix", "Auth token refresh",
		"fix the auth token refresh", "The refresh path retries once.")
	deliverJob(t, graph, "ambiguous", "auth-login", "Auth login page",
		"fix the auth login page", "The login page validates the session cookie.")

	user := postUser(t, graph, "ambiguous", "actually the auth fix is wrong")
	reading := deterministicReading(t, New(nil, graph), user)
	if !strings.Contains(reading, "more than one delivered job could be the one they mean") ||
		!strings.Contains(reading, "ask, do not choose") {
		t.Fatalf("the rivals were not reported to the loop:\n%s", reading)
	}
	for _, id := range []string{"auth-fix", "auth-login"} {
		if !strings.Contains(reading, id) {
			t.Fatalf("the reading dropped rival %q:\n%s", id, reading)
		}
	}

	head, _ := beltHead(graph, beltTurn{calls: []ai.ToolCall{
		beltCall("c1", beltToolAsk, map[string]any{
			"question": "Which one do you mean?",
			"options":  []string{"Auth token refresh", "Auth login page"}})}})
	if err := head.answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}

	if commands := pendingCommandsOf(t, graph); len(commands) != 0 {
		t.Fatalf("an ambiguous correction bought a revision before asking: %+v", commands)
	}
	reply := waitForAgentReply(t, graph, "ambiguous", user.Seq)
	if !strings.Contains(reply.Body, "Which one") {
		t.Fatalf("ambiguity did not produce one plain question: %q", reply.Body)
	}
	for _, label := range []string{"Auth token refresh", "Auth login page"} {
		if !strings.Contains(reply.Body, label) {
			t.Fatalf("the question does not offer %q: %q", label, reply.Body)
		}
	}
	if strings.Contains(reply.Body, "auth-fix") || strings.Contains(reply.Body, "auth-login") {
		t.Fatalf("the question named work by its id: %q", reply.Body)
	}
}

// And the answer to that question is the revision. The choice is not applied by
// the question machinery — it comes back as an ordinary turn — so what makes the
// critique survive is that the answering turn is given the whole exchange: the
// sentence, the question, and the digit. The digit is never what gets journaled.
func TestAnsweringTheAmbiguityQuestionJournalsTheCorrection(t *testing.T) {
	graph := openHeadStore(t)
	deliverJob(t, graph, "settle", "auth-fix", "Auth token refresh",
		"fix the auth token refresh", "The refresh path retries once.")
	deliverJob(t, graph, "settle", "auth-login", "Auth login page",
		"fix the auth login page", "The login page validates the session cookie.")

	const critique = "actually the auth fix is wrong"
	user := postUser(t, graph, "settle", critique)
	head, client := beltHead(graph, beltTurn{calls: []ai.ToolCall{
		beltCall("c1", beltToolAsk, map[string]any{
			"question": "Which one do you mean?",
			"options":  []string{"Auth token refresh", "Auth login page"}})}})
	if err := head.answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if commands := pendingCommandsOf(t, graph); len(commands) != 0 {
		t.Fatalf("the question was skipped: %+v", commands)
	}

	client.turns = append(client.turns, beltTurn{calls: []ai.ToolCall{
		beltCall("c2", beltToolCorrect, map[string]any{
			"job": "auth-login", "words": critique})}}, beltTurn{})
	answer := postUser(t, graph, "settle", "2")
	if err := head.answer(context.Background(), answer); err != nil {
		t.Fatal(err)
	}
	// The proof that the critique can survive: the turn answering "2" was handed
	// the sentence it is about and the question it answers, in one prompt.
	settling := latestPrompt(client)
	if !strings.Contains(settling, critique) || !strings.Contains(settling, "Which one do you mean?") {
		t.Fatalf("the answering turn was not given the exchange it settles:\n%s", settling)
	}

	commands := pendingCommandsOf(t, graph)
	if len(commands) != 1 {
		t.Fatalf("answering journaled %d commands: %+v", len(commands), commands)
	}
	if commands[0].Target != "auth-login" || !IsCorrection(commands[0].Instruction) {
		t.Fatalf("the chosen job did not become the revision: %+v", commands[0])
	}
	if !strings.Contains(commands[0].Instruction, critique) {
		t.Fatalf("the answer replaced the critique with a digit:\n%s", commands[0].Instruction)
	}
}
