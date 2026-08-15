package head

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// ── the scope boundary, asked once and then learned ─────────────────────────

// "Do you want the quick look now, or the proper job?" is the one boundary the
// head genuinely cannot read off the words, and the product's answer to a
// genuine judgment call is: ask a cheap structured question, then let repeated
// answers turn into a default. The machinery for the second half already
// existed for compile assumptions; these tests pin that the ask tool now
// reaches it, and that reaching it costs the loop nothing it used to have.

func scopeOptions() []store.QuestionOption {
	return []store.QuestionOption{
		{Label: "Quick answer from what I have", Value: askOptionPrefix + "1"},
		{Label: "A proper researched report", Value: askOptionPrefix + "2"},
	}
}

// answerScopeQuestion seeds one settled scope ask the way the tool mints it, so
// the acceptance projection reads exactly the rows it will read in production.
func answerScopeQuestion(t *testing.T, graph *store.Store, session, resolution string) {
	t.Helper()
	question, err := graph.AskQuestion(store.AgentQuestion{
		SessionID: session, Text: "Quick answer, or the proper job?",
		Urgency: store.QuestionBlocking, Category: store.QuestionCategoryScope,
		DefaultAnswer: "1", Options: scopeOptions(),
	})
	if err != nil {
		t.Fatalf("seed scope question: %v", err)
	}
	if _, err := graph.SurfaceQuestion(question.Seq); err != nil {
		t.Fatalf("surface scope question: %v", err)
	}
	answer := postUser(t, graph, session, resolution)
	if err := graph.ResolveQuestion(question.Seq, store.QuestionAnswered, resolution, answer.Seq); err != nil {
		t.Fatalf("settle scope question: %v", err)
	}
}

func askScope(t *testing.T, run *beltRun, question string) (string, bool) {
	t.Helper()
	return run.execute(beltToolAsk, beltArguments(t, map[string]any{
		"question": question,
		"options":  []string{"Quick answer from what I have", "A proper researched report"},
		"category": "scope",
	}))
}

// A scope ask is a durable row, because a row is the only thing the meta loop
// can count — and it is still the loop's question, so the answer must reach the
// loop rather than being spent by the gates on its way past.
func TestScopeAskIsCategorizedAndItsAnswerStillReachesTheLoop(t *testing.T) {
	graph := openHeadStore(t)
	session := "scope-ask"
	head := New(nil, graph)
	run := &beltRun{head: head, user: postUser(t, graph, session, "look at the pricing thing")}

	message, failed := askScope(t, run, "Quick answer now, or the proper job?")
	if failed {
		t.Fatalf("the scope question could not be asked: %s", message)
	}
	if !run.spoke {
		t.Fatal("a posted question did not claim the turn, so the loop would speak over it")
	}

	open, err := graph.OpenQuestions(session, 10)
	if err != nil || len(open) != 1 {
		t.Fatalf("open questions = %+v err=%v", open, err)
	}
	if open[0].Category != store.QuestionCategoryScope {
		t.Fatalf("the ask was not journaled as a scope question: %q", open[0].Category)
	}
	if open[0].DefaultAnswer != "1" {
		t.Fatalf("no default was offered, so nothing can ever be learned: %q", open[0].DefaultAnswer)
	}

	answer := postUser(t, graph, session, "1")
	handled, err := head.answerAgentQuestion(context.Background(), answer)
	if err != nil {
		t.Fatal(err)
	}
	if handled {
		t.Fatal("the gates spent the answer, so the loop that asked would never hear it")
	}
	handled, err = head.answerPendingQuestion(context.Background(), answer)
	if err != nil {
		t.Fatal(err)
	}
	if handled {
		t.Fatal("the selectable path spent the answer instead of handing it to the loop")
	}

	stat, err := graph.QuestionCategoryStats(store.QuestionCategoryScope)
	if err != nil {
		t.Fatal(err)
	}
	if stat.N != 1 || stat.Accepted != 1 {
		t.Fatalf("the answer never reached the projection, so nothing can be learned: %+v", stat)
	}
}

// Once the answer has been the same often enough that asking has stopped paying,
// the question is not put at all: the tool hands the learned default back and the
// turn CARRIES ON, because nothing was said to the person yet.
func TestScopeAskFlipsToAssumeAndTheTurnContinues(t *testing.T) {
	graph := openHeadStore(t)
	session := "scope-learned"
	for index := 0; index < store.VOIMinSamples; index++ {
		answerScopeQuestion(t, graph, session, askOptionPrefix+"1")
	}
	if ask, _, err := graph.ShouldAsk(store.QuestionCategoryScope); err != nil || ask {
		t.Fatalf("the gate never closed on a consistently answered scope ask: ask=%t err=%v", ask, err)
	}

	head := New(nil, graph)
	before, err := graph.Messages(session, 0, 200)
	if err != nil {
		t.Fatal(err)
	}
	run := &beltRun{head: head, user: postUser(t, graph, session, "have a look at the pricing thing")}

	result, failed := askScope(t, run, "Quick answer now, or the proper job?")
	if failed {
		t.Fatalf("the assumed ask reported failure: %s", result)
	}
	if run.spoke {
		t.Fatal("an assumption ended the turn; the loop must carry on and act on it")
	}
	if !strings.Contains(result, "assumed: Quick answer from what I have") {
		t.Fatalf("the tool did not name the default it assumed: %q", result)
	}
	if !strings.Contains(result, "learned from 8 earlier answers") {
		t.Fatalf("the tool did not say what the assumption rests on: %q", result)
	}
	if !strings.Contains(result, "say so in one clause") {
		t.Fatalf("the loop was not told to carry the assumption out loud: %q", result)
	}

	after, err := graph.Messages(session, 0, 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before)+1 {
		t.Fatalf("something was posted to the person: %d messages before, %d after", len(before), len(after))
	}
	if open, err := graph.OpenQuestions(session, 10); err != nil || len(open) != 0 {
		t.Fatalf("a question was put after the gate closed: %+v err=%v", open, err)
	}

	// The assumption is journaled, which is what lets a correction on the next
	// turn be counted against it and reopen the gate.
	events, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	journaled := false
	for _, event := range events {
		if event.Kind != store.EventAssumedWithDefault {
			continue
		}
		if strings.Contains(string(event.Payload), string(store.QuestionCategoryScope)) &&
			strings.Contains(string(event.Payload), "Quick answer from what I have") {
			journaled = true
		}
	}
	if !journaled {
		t.Fatal("the skipped ask was never journaled, so a correction could never match back")
	}
}

// A category this build has never heard of is not an error. The model still
// asked a perfectly good question, and refusing it would cost the person their
// answer to protect a statistic — so it degrades to the ordinary askback.
func TestUnknownAskCategoryDegradesToAnOrdinaryAsk(t *testing.T) {
	graph := openHeadStore(t)
	session := "scope-unknown"
	head := New(nil, graph)
	run := &beltRun{head: head, user: postUser(t, graph, session, "cancel it")}

	message, failed := run.execute(beltToolAsk, beltArguments(t, map[string]any{
		"question": "Which one do you mean?",
		"options":  []string{"Ledger audit", "Market research"},
		"category": "wobble",
	}))
	if failed {
		t.Fatalf("an unknown category refused the question outright: %s", message)
	}
	if !run.spoke {
		t.Fatal("the fallback ask did not claim the turn")
	}
	if open, err := graph.OpenQuestions(session, 10); err != nil || len(open) != 0 {
		t.Fatalf("an unmeasurable category still minted a durable row: %+v err=%v", open, err)
	}
	messages, err := graph.Messages(session, run.user.Seq, 10)
	if err != nil || len(messages) != 1 || len(messages[0].Options) != 2 {
		t.Fatalf("the question was not asked as rows a person can pick: %+v err=%v", messages, err)
	}
}
