package head

import (
	"strings"
	"testing"
)

// The correction that names its work in its own words wins over the job that
// merely spoke most recently. Before this, adjacency led and returned on its
// first hit, so the vocabulary arm was unreachable whenever anything had
// narrated — and a correction aimed at the auth fix bought a paid revision of
// whatever job happened to have a heartbeat in the window.
func TestDescribedWorkBeatsTheWorkThatSpokeLast(t *testing.T) {
	graph := openHeadStore(t)
	deliverJob(t, graph, "resolve", "auth-fix", "Auth token refresh",
		"patch the auth token refresh", "The refresh path now retries once and the tests pass.")
	deliverJob(t, graph, "resolve", "parser-fix", "Parser rewrite",
		"rewrite the config parser", "The parser accepts trailing commas now.")

	answerWith(t, graph, "resolve", "actually the auth refresh is still broken", `{"reply":"noted"}`)

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
}

// With nothing in the sentence to go on, position is the whole signal and it is
// the one a person would use: the follow-up answers whatever just spoke.
func TestContentlessFollowUpAnswersTheWorkThatSpokeLast(t *testing.T) {
	graph := openHeadStore(t)
	deliverJob(t, graph, "adjacent", "auth-fix", "Auth token refresh",
		"patch the auth token refresh", "The refresh path now retries once.")
	deliverJob(t, graph, "adjacent", "parser-fix", "Parser rewrite",
		"rewrite the config parser", "The parser accepts trailing commas now.")

	answerWith(t, graph, "adjacent", "actually that needs to say much more about the trade-offs",
		`{"reply":"noted"}`)

	commands := pendingCommandsOf(t, graph)
	if len(commands) != 1 || commands[0].Target != "parser-fix" {
		t.Fatalf("the follow-up did not answer the last thing said: %+v", commands)
	}
}

// When the words name more than one delivered job, nothing about the ranking is
// a decision. One short question in plain words settles it, naming the jobs the
// way the user would recognise them — and nothing is journaled until they answer.
func TestGenuineAmbiguityAsksOnePlainQuestion(t *testing.T) {
	graph := openHeadStore(t)
	deliverJob(t, graph, "ambiguous", "auth-fix", "Auth token refresh",
		"fix the auth token refresh", "The refresh path retries once.")
	deliverJob(t, graph, "ambiguous", "auth-login", "Auth login page",
		"fix the auth login page", "The login page validates the session cookie.")

	user := answerWith(t, graph, "ambiguous", "actually the auth fix is wrong", `{"reply":"noted"}`)

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

// And the answer to that question is the revision. The option carries the job
// it names, so choosing settles the referent rather than re-guessing it.
func TestAnsweringTheAmbiguityQuestionJournalsTheCorrection(t *testing.T) {
	graph := openHeadStore(t)
	deliverJob(t, graph, "settle", "auth-fix", "Auth token refresh",
		"fix the auth token refresh", "The refresh path retries once.")
	deliverJob(t, graph, "settle", "auth-login", "Auth login page",
		"fix the auth login page", "The login page validates the session cookie.")

	answerWith(t, graph, "settle", "actually the auth fix is wrong", `{"reply":"noted"}`)
	if commands := pendingCommandsOf(t, graph); len(commands) != 0 {
		t.Fatalf("the question was skipped: %+v", commands)
	}

	answerWith(t, graph, "settle", "2", `{"reply":"noted"}`)
	commands := pendingCommandsOf(t, graph)
	if len(commands) != 1 {
		t.Fatalf("answering journaled %d commands: %+v", len(commands), commands)
	}
	if commands[0].Target != "auth-login" || !IsCorrection(commands[0].Instruction) {
		t.Fatalf("the chosen job did not become the revision: %+v", commands[0])
	}
	if !strings.Contains(commands[0].Instruction, "actually the auth fix is wrong") {
		t.Fatalf("the answer replaced the critique with a digit:\n%s", commands[0].Instruction)
	}
}
