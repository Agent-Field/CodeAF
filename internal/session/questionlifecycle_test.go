package session

// The three moments of a question, for the lanes that used to have only one:
// raised, then answered or withdrawn, on the questions lane every window reads.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
)

// A CONNECT OFFER IS A QUESTION LIKE ANY OTHER. It reached the questions lane
// only through [Agent.OpenQuestions]'s replay before this, so a second window
// learned of it by attaching and was never told it had been answered: the offer
// stood on that screen for the rest of the conversation.
func TestTheConnectOfferIsRaisedAnsweredAndWithdrawnOnTheQuestionsLane(t *testing.T) {
	agent, _ := questionSession(t, "cccc1111cccc1111", nil)
	hub := watched(agent)
	go func() {
		for range hub {
		}
	}()
	asks, stopAsking := agent.WatchQuestions()
	defer stopAsking()

	done := make(chan struct{})
	go func() {
		_, _ = agent.askConnect(context.Background(), connectStatus{ID: "notion", Name: "Notion"})
		close(done)
	}()

	raised := waitForAsk(t, asks, EventQuestion)
	if raised.Question.Kind != QuestionConnect {
		t.Fatalf("the questions lane raised %v", raised.Question.Kind)
	}
	ref := raised.Question.Ref
	if ref == "" {
		t.Fatal("the connect question carries no token to answer it by")
	}

	if err := agent.ResolveQuestion(Answer{Kind: QuestionConnect, Ref: ref, Key: "1"}); err != nil {
		t.Fatalf("ResolveQuestion: %v", err)
	}
	<-done
	answered := waitForAsk(t, asks, EventQuestionAnswered)
	if answered.Question.Ref != ref {
		t.Fatalf("the answered event is about %q, want %q", answered.Question.Ref, ref)
	}
	// AND THE RECORD KEEPS IT, which no family of this lane had before: an
	// answer with no banked words leaves nothing anybody can read afterwards.
	decisions := agent.Decisions()
	if len(decisions) == 0 || !strings.Contains(decisions[len(decisions)-1].Head, "Notion") {
		t.Fatalf("the answer left no record: %+v", decisions)
	}
}

// AND A LANE THAT STOPS WAITING WITHDRAWS ITS QUESTION, which is the other half
// of the same law: the turn ended, so the offer comes off every screen.
func TestTheConnectOfferIsWithdrawnWhenTheTurnEnds(t *testing.T) {
	agent, _ := questionSession(t, "cccc2222cccc2222", nil)
	hub := watched(agent)
	go func() {
		for range hub {
		}
	}()
	asks, stopAsking := agent.WatchQuestions()
	defer stopAsking()

	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_, _ = agent.askConnect(ctx, connectStatus{ID: "notion", Name: "Notion"})
		close(done)
	}()
	waitForAsk(t, asks, EventQuestion)
	stop()
	<-done

	gone := waitForAsk(t, asks, EventQuestionWithdrawn)
	if gone.Question.Withdrawn == nil || gone.Question.Withdrawn.Reason == "" {
		t.Fatalf("the withdrawal says nothing about why: %+v", gone.Question.Withdrawn)
	}
}

// NOTHING WAITS ON A RATIFY. It is the cheapest rung of the ladder — something
// reversible was done and this is the chance to unwind it — so the call comes
// straight back, the turn carries on, and the question stands on its own until
// somebody looks at it.
func TestARatifyIsShownAndWaitsOnNobody(t *testing.T) {
	agent, dir := questionSession(t, "rtfy1111rtfy1111", func(config *Config) { config.Interactive = true })
	asks, stopAsking := agent.WatchQuestions()
	defer stopAsking()
	raw := json.RawMessage(`{"head":"renamed the column to slug","kind":"ratify","reason":"it was reversible and the name was wrong","stakes":"reversible","options":[{"key":"1","label":"fine","safe":true},{"key":"2","label":"put it back"}]}`)

	text, isError, err := agent.executeAsk(context.Background(), raw)
	if err != nil || isError {
		t.Fatalf("executeAsk: %q err=%v isError=%v", text, err, isError)
	}
	if !strings.HasPrefix(text, askShownLead) {
		t.Fatalf("the call did not come straight back: %q", text)
	}
	waitForAsk(t, asks, EventQuestion)

	// IT IS STILL OPEN, AND IT IS NOT THIS SESSION BEING STOPPED ON SOMEBODY.
	if open := agent.OpenQuestions(); len(open) != 1 || open[0].Ask != AskRatify {
		t.Fatalf("OpenQuestions() = %+v", open)
	}
	if agent.NeedsPerson() {
		t.Fatalf("a ratify made the session say it was waiting: %q", agent.WaitingOn())
	}

	// AND THE ANSWER REACHES THE MODEL AS A MESSAGE, because the call it came
	// from is long gone.
	id := agent.OpenQuestions()[0].ID
	if err := agent.ResolveQuestion(Answer{Kind: QuestionAsk, ID: id, Key: "2"}); err != nil {
		t.Fatalf("ResolveQuestion: %v", err)
	}
	waitForAsk(t, asks, EventQuestionAnswered)
	if note := waitForNote(t, agent, askAnsweredWord); !strings.Contains(note, "put it back") {
		t.Fatalf("the answer did not reach the model: %q", note)
	}
	if records, _ := ReadDecisions(dir); len(records) != 1 {
		t.Fatalf("the ratify left %d records, want 1", len(records))
	}
}

// ASKING BACK IS NOT ANSWERING. The call returns so the model can say something
// — a model parked in a tool cannot — and the question stays open on every
// screen it is drawn on, with the decision still the person's.
func TestAskingBackLeavesTheQuestionOpenAndTheAnswerArrivesLater(t *testing.T) {
	agent, _ := questionSession(t, "askb1111askb1111", func(config *Config) { config.Interactive = true })
	asks, stopAsking := agent.WatchQuestions()
	defer stopAsking()
	raw := json.RawMessage(`{"head":"Which storage shape?","kind":"choice","reason":"the record does not choose","stakes":"reversible","options":[{"key":"1","label":"sqlite"},{"key":"2","label":"jsonl"}]}`)
	returned := make(chan string, 1)
	go func() {
		text, _, _ := agent.executeAsk(context.Background(), raw)
		returned <- text
	}()
	raised := waitForAsk(t, asks, EventQuestion)
	id := raised.Question.ID

	if err := agent.ResolveQuestion(Answer{Kind: QuestionAsk, ID: id,
		AskedBack: []Exchange{{Asked: "what does jsonl cost on a big file?"}}}); err != nil {
		t.Fatalf("ResolveQuestion: %v", err)
	}
	text := <-returned
	if !strings.HasPrefix(text, askedBackLead) || !strings.Contains(text, "what does jsonl cost") {
		t.Fatalf("the call did not come back with what they asked: %q", text)
	}
	// NOTHING WAS DECIDED: no record, and no answered event closing it in other
	// windows.
	if open := agent.OpenQuestions(); len(open) != 1 || open[0].ID != id {
		t.Fatalf("the question did not stay open: %+v", open)
	}
	if decisions := agent.Decisions(); len(decisions) != 0 {
		t.Fatalf("asking back was recorded as a decision: %+v", decisions)
	}
	if agent.NeedsPerson() {
		t.Fatal("a question the model is answering said the session was stopped on somebody")
	}

	// AND THEIR ANSWER, WHEN IT COMES, REACHES THE MODEL AS A MESSAGE.
	if err := agent.ResolveQuestion(Answer{Kind: QuestionAsk, ID: id, Key: "2"}); err != nil {
		t.Fatalf("ResolveQuestion: %v", err)
	}
	waitForAsk(t, asks, EventQuestionAnswered)
	if note := waitForNote(t, agent, askAnsweredWord); !strings.Contains(note, "jsonl") {
		t.Fatalf("the answer never reached the model: %q", note)
	}
	if len(agent.OpenQuestions()) != 0 {
		t.Fatalf("the question is still open after it was answered: %+v", agent.OpenQuestions())
	}
}

// waitForNote polls the queues a message to the model lands on for one carrying
// this opening.
func waitForNote(t *testing.T, agent *Agent, lead string) string {
	t.Helper()
	// THE TRANSCRIPT IS LOOKED AT AS WELL AS THE QUEUES, and by CONTAINS rather
	// than by prefix. A note that is owed an answer wakes the conversation
	// ([Agent.enqueueNote]), the turn it wakes drains the queue into the
	// transcript, and what lands there is the note under the session's own
	// `while you worked:` lead. Which of the two holds it when this looks is a
	// question about how busy the box is — a test that read only the queues, and
	// only from the first byte, passed alone and failed beside a full suite.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, text := range everythingSaidTo(agent) {
			if at := strings.Index(text, lead); at >= 0 {
				return text[at:]
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no message to the model carried %q; everything said is %q", lead, everythingSaidTo(agent))
	return ""
}

// everythingSaidTo is every line this session has put in front of the model:
// what is queued, what is ambient, and what a woken turn has already spliced
// into the transcript.
func everythingSaidTo(agent *Agent) []string {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	var said []string
	for _, message := range append(append([]userMessage{}, agent.steering...), agent.ambient...) {
		said = append(said, message.text())
	}
	for _, message := range agent.messages {
		said = append(said, messageText(message))
	}
	return said
}

// TWO QUESTIONS RAISED BY ONE TOOL BATCH SAY SO. A model that calls three tools
// at once can put three approvals on a screen in the same instant, and those are
// one thing to answer rather than three — the surface groups them by this and
// fans one command out over the set.
func TestQuestionsFromOneStepShareTheirBatchToken(t *testing.T) {
	agent, _ := questionSession(t, "btch1111btch1111", nil)
	episode := agent.newEpisode()

	episode.decisionBegins()
	first := agent.consentAsk(1, toolCallNamed("c1", "bash"), approval.Decision{Rule: "bash"})
	second := agent.consentAsk(2, toolCallNamed("c2", "write"), approval.Decision{Rule: "write"})
	if first.Batch == "" || first.Batch != second.Batch {
		t.Fatalf("two questions from one step wear %q and %q", first.Batch, second.Batch)
	}

	// AND THE NEXT STEP IS A DIFFERENT MOMENT.
	episode.decisionBegins()
	third := agent.consentAsk(3, toolCallNamed("c3", "bash"), approval.Decision{Rule: "bash"})
	if third.Batch == first.Batch {
		t.Fatalf("a question from the next step wears the same token %q", third.Batch)
	}
}

func toolCallNamed(id, name string) ai.ToolCall {
	call := ai.ToolCall{ID: id}
	call.Function.Name = name
	return call
}
