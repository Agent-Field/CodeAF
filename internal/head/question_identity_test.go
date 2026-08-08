package head

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func askOpenQuestion(t *testing.T, graph *store.Store, session, prompt string, labels ...string) store.AgentQuestion {
	t.Helper()
	options := make([]store.QuestionOption, 0, len(labels))
	for _, label := range labels {
		options = append(options, store.QuestionOption{Label: label})
	}
	question, err := graph.AskQuestion(store.AgentQuestion{
		SessionID: session, Text: store.QuestionMessageBody(prompt, options),
		Urgency: store.QuestionBlocking, Options: options,
	})
	if err != nil {
		t.Fatalf("ask %q: %v", prompt, err)
	}
	if _, err := graph.SurfaceQuestion(question.Seq); err != nil {
		t.Fatalf("surface %q: %v", prompt, err)
	}
	return question
}

func questionStatus(t *testing.T, graph *store.Store, seq int64) store.AgentQuestion {
	t.Helper()
	question, found, err := graph.AgentQuestionBySeq(seq)
	if err != nil || !found {
		t.Fatalf("question %d: found=%t err=%v", seq, found, err)
	}
	return question
}

func lastAgentLine(t *testing.T, graph *store.Store, session string) string {
	t.Helper()
	messages, err := graph.Messages(session, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == store.RoleAgent {
			return messages[index].Body
		}
	}
	return ""
}

// One question open is not ambiguity: the digit answers it, exactly as before.
func TestOneOpenQuestionStillTakesABareDigit(t *testing.T) {
	graph := openHeadStore(t)
	question := askOpenQuestion(t, graph, "one-question",
		"Which branch should the fix target?", "main", "release/2.4")
	user := postUser(t, graph, "one-question", "1")

	handled, err := New(nil, graph).answerAgentQuestion(context.Background(), user)
	if err != nil || !handled {
		t.Fatalf("bare digit handled=%t err=%v", handled, err)
	}
	settled := questionStatus(t, graph, question.Seq)
	if settled.Status != store.QuestionAnswered || settled.Resolution != "main" {
		t.Fatalf("single open question = %+v", settled)
	}
}

// The blocker: with two questions open, "newest wins" approved a plan the user
// never read and spent the question they meant to answer. Now it asks — once,
// in plain words, with no picker and no error.
func TestTwoOpenQuestionsAskWhichOneABareDigitIsFor(t *testing.T) {
	graph := openHeadStore(t)
	auth := askOpenQuestion(t, graph, "two-questions",
		"Which branch should the auth fix target?", "main", "release/2.4")
	deploy := askOpenQuestion(t, graph, "two-questions",
		"Start the deploy migration at $4.10?", "start it", "hold it")
	user := postUser(t, graph, "two-questions", "1")

	conversational := New(nil, graph)
	handled, err := conversational.answerAgentQuestion(context.Background(), user)
	if err != nil || !handled {
		t.Fatalf("ambiguous digit handled=%t err=%v", handled, err)
	}
	if settled := questionStatus(t, graph, deploy.Seq); settled.Status != store.QuestionAsked {
		t.Fatalf("the newest question was spent on a guess: %+v", settled)
	}
	if settled := questionStatus(t, graph, auth.Seq); settled.Status != store.QuestionAsked {
		t.Fatalf("the older question was spent on a guess: %+v", settled)
	}
	asked := lastAgentLine(t, graph, "two-questions")
	if !strings.HasSuffix(strings.TrimSpace(asked), questionChoiceTail) {
		t.Fatalf("no plain question was asked: %q", asked)
	}
	if !strings.Contains(asked, "auth fix") || !strings.Contains(asked, "deploy migration") {
		t.Fatalf("the ask did not name both questions: %q", asked)
	}
	if strings.Contains(asked, "1.") || strings.Contains(asked, "```") {
		t.Fatalf("the ask became a picker: %q", asked)
	}

	// And the words that name one settle it, with the answer already given.
	choice := postUser(t, graph, "two-questions", "the auth one")
	handled, err = conversational.answerAgentQuestion(context.Background(), choice)
	if err != nil || !handled {
		t.Fatalf("named choice handled=%t err=%v", handled, err)
	}
	settled := questionStatus(t, graph, auth.Seq)
	if settled.Status != store.QuestionAnswered || settled.Resolution != "main" {
		t.Fatalf("the named question did not take the answer already given: %+v", settled)
	}
	if other := questionStatus(t, graph, deploy.Seq); other.Status != store.QuestionAsked {
		t.Fatalf("the other question was consumed too: %+v", other)
	}
}

// People answer by position as readily as by name, and the sentence they were
// just shown is the only ordering they have.
func TestTheChoiceCanBeAnsweredByPosition(t *testing.T) {
	graph := openHeadStore(t)
	auth := askOpenQuestion(t, graph, "ordinal",
		"Which branch should the auth fix target?", "main", "release/2.4")
	deploy := askOpenQuestion(t, graph, "ordinal",
		"Start the deploy migration at $4.10?", "start it", "hold it")
	conversational := New(nil, graph)
	user := postUser(t, graph, "ordinal", "2")
	if handled, err := conversational.answerAgentQuestion(context.Background(), user); err != nil || !handled {
		t.Fatalf("ambiguous digit handled=%t err=%v", handled, err)
	}
	choice := postUser(t, graph, "ordinal", "the second one")
	if handled, err := conversational.answerAgentQuestion(context.Background(), choice); err != nil || !handled {
		t.Fatalf("positional choice handled=%t err=%v", handled, err)
	}
	if settled := questionStatus(t, graph, deploy.Seq); settled.Status != store.QuestionAnswered ||
		settled.Resolution != "hold it" {
		t.Fatalf("the second question named = %+v", settled)
	}
	if other := questionStatus(t, graph, auth.Seq); other.Status != store.QuestionAsked {
		t.Fatalf("the first question was consumed: %+v", other)
	}
}

// A reply only one open question could be reading is not ambiguous, whichever
// one happens to be newest. That is the same evidence a person would use.
func TestAReplyOnlyOneOpenQuestionAcceptsGoesThere(t *testing.T) {
	graph := openHeadStore(t)
	auth := askOpenQuestion(t, graph, "aimed",
		"Which branch should the auth fix target?", "main", "release/2.4")
	deploy := askOpenQuestion(t, graph, "aimed",
		"Start the deploy migration at $4.10?", "start it", "hold it")
	user := postUser(t, graph, "aimed", "release/2.4")

	if handled, err := New(nil, graph).answerAgentQuestion(context.Background(), user); err != nil || !handled {
		t.Fatalf("aimed answer handled=%t err=%v", handled, err)
	}
	if settled := questionStatus(t, graph, auth.Seq); settled.Status != store.QuestionAnswered ||
		settled.Resolution != "release/2.4" {
		t.Fatalf("the question those words belong to = %+v", settled)
	}
	if other := questionStatus(t, graph, deploy.Seq); other.Status != store.QuestionAsked {
		t.Fatalf("the newest question took an answer meant for another: %+v", other)
	}
}

// An explicit reference from a surface that knows what it pointed at never
// reaches the ambiguity path at all.
func TestANamedQuestionIsNeverAskedAbout(t *testing.T) {
	graph := openHeadStore(t)
	auth := askOpenQuestion(t, graph, "named",
		"Which branch should the auth fix target?", "main", "release/2.4")
	deploy := askOpenQuestion(t, graph, "named",
		"Start the deploy migration at $4.10?", "start it", "hold it")
	user, err := graph.PostMessage(store.Message{
		SessionID: "named", Role: store.RoleUser, Body: "1", QuestionSeq: auth.Seq,
	})
	if err != nil {
		t.Fatal(err)
	}
	if handled, err := New(nil, graph).answerAgentQuestion(context.Background(), user); err != nil || !handled {
		t.Fatalf("named answer handled=%t err=%v", handled, err)
	}
	if settled := questionStatus(t, graph, auth.Seq); settled.Status != store.QuestionAnswered ||
		settled.Resolution != "main" {
		t.Fatalf("the named question = %+v", settled)
	}
	if other := questionStatus(t, graph, deploy.Seq); other.Status != store.QuestionAsked {
		t.Fatalf("a named answer reached the wrong question: %+v", other)
	}
	if asked := lastAgentLine(t, graph, "named"); strings.HasSuffix(strings.TrimSpace(asked), questionChoiceTail) {
		t.Fatalf("a named answer was asked about anyway: %q", asked)
	}
}

// A question the words could never have settled is not a candidate. A charter
// waiting to be ratified lets ordinary words go by, so with one of those open
// beside a job's askback there is nothing to choose between: the reply goes to
// the one question that would have taken it, and nobody is asked anything.
func TestAQuestionThatWouldIgnoreTheWordsIsNotACandidate(t *testing.T) {
	graph := openHeadStore(t)
	branch := askOpenQuestion(t, graph, "one-real",
		"Which branch should the auth fix target?", "main", "release/2.4")
	charter, err := graph.AskQuestion(store.AgentQuestion{
		SessionID: "one-real", Text: "Stand this up as a rule?", Urgency: store.QuestionBlocking,
		Options: []store.QuestionOption{
			{Label: "yes, stand it up", Value: "charter:ratify:daily-digest"},
			{Label: "just once", Value: "charter:once:daily-digest"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.SurfaceQuestion(charter.Seq); err != nil {
		t.Fatal(err)
	}
	user := postUser(t, graph, "one-real", "release/2.4")

	handled, err := New(nil, graph).answerAgentQuestion(context.Background(), user)
	if err != nil || !handled {
		t.Fatalf("aimed answer handled=%t err=%v", handled, err)
	}
	if settled := questionStatus(t, graph, branch.Seq); settled.Status != store.QuestionAnswered {
		t.Fatalf("the one question those words fit = %+v", settled)
	}
	if other := questionStatus(t, graph, charter.Seq); other.Status != store.QuestionAsked {
		t.Fatalf("a charter was ratified by unrelated words: %+v", other)
	}
	if asked := lastAgentLine(t, graph, "one-real"); strings.HasSuffix(strings.TrimSpace(asked), questionChoiceTail) {
		t.Fatalf("an unambiguous answer was asked about: %q", asked)
	}
}

// Free text is an answer too — the turn after a question is the answer to it,
// which is the law the whole queue runs on. So free text with two questions
// open is exactly as ambiguous as a digit, and gets the same one question back
// instead of silently settling the newer one.
func TestFreeTextWithTwoOpenQuestionsIsAskedAboutToo(t *testing.T) {
	graph := openHeadStore(t)
	auth := askOpenQuestion(t, graph, "free-text",
		"Which branch should the auth fix target?", "main", "release/2.4")
	deploy := askOpenQuestion(t, graph, "free-text",
		"Start the deploy migration at $4.10?", "start it", "hold it")
	user := postUser(t, graph, "free-text", "whatever you think is safest")

	handled, err := New(nil, graph).answerAgentQuestion(context.Background(), user)
	if err != nil || !handled {
		t.Fatalf("ambiguous free text handled=%t err=%v", handled, err)
	}
	if settled := questionStatus(t, graph, deploy.Seq); settled.Status != store.QuestionAsked {
		t.Fatalf("free text settled the newest question on a guess: %+v", settled)
	}
	if settled := questionStatus(t, graph, auth.Seq); settled.Status != store.QuestionAsked {
		t.Fatalf("free text settled the older question on a guess: %+v", settled)
	}
	if asked := lastAgentLine(t, graph, "free-text"); !strings.HasSuffix(strings.TrimSpace(asked), questionChoiceTail) {
		t.Fatalf("free text was answered into one of them silently: %q", asked)
	}
}
