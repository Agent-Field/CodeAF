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

// THE LANDING THAT SOMEBODY ANSWERED CARRIES THE ANSWER'S FATE WHEN IT IS
// RAISED AGAIN (#1077): the twelfth identical card read as though the first
// eleven were ignored, so a re-raise over a recorded answer leads with what
// became of it — `accepted 18:20 · nobody could check it`.
func TestAReRaisedLandingCarriesItsAnswerFate(t *testing.T) {
	agent, _ := questionSession(t, "landing-fate", nil)
	lane, stop := agent.WatchQuestions()
	defer stop()

	notice := TaskNotice{ID: 1, Title: "write the sheet", State: TaskUnverified}
	agent.publishLandingQuestion(notice)
	first := <-lane
	if first.Kind != EventQuestion || first.Question == nil || first.Question.Kind != QuestionLanding {
		t.Fatalf("the landing did not raise on the landing lane: %+v", first)
	}
	if strings.Contains(first.Question.Reason, "accepted") {
		t.Fatalf("a raise nobody answered already carries a stamp: %q", first.Question.Reason)
	}

	// The answer lands on the record the way the lane writes it, and the
	// answer's own claim takes the bank down. The minute is now's own, so the
	// stamp reads as today's however many days after this the test runs — the
	// stamp says its day when the answer is from another one.
	at := time.Now().Truncate(time.Minute)
	q := agent.landingQuestion(PendingDecision{Notice: notice})
	agent.recordDecision(decisionRecordOf(q, Answer{
		At:        at,
		Kind:      QuestionLanding,
		ID:        1,
		Key:       LandingYesKey,
		DecidedBy: DecidedByPerson,
	}))
	agent.claimQuestion(QuestionLanding, "1", false)

	agent.publishLandingQuestion(notice)
	again := <-lane
	if again.Kind != EventQuestion || again.Question == nil {
		t.Fatalf("the re-raise did not go out: %+v", again)
	}
	if !strings.HasPrefix(again.Question.Reason, "accepted "+at.Format("15:04")) {
		t.Fatalf("the re-raised card does not lead with the answer's fate: %q", again.Question.Reason)
	}
}

// AN IN-FLIGHT SETTLE HOLDS THE QUESTION DOWN, and the settle's own terminal
// notice still raises (#1077): the card that came back within seconds was the
// answer's own work asking again.
func TestAnInFlightSettleHoldsTheQuestionDown(t *testing.T) {
	agent := &Agent{}
	lane, stop := agent.WatchQuestions()
	defer stop()

	agent.publishLandingQuestion(TaskNotice{
		ID: 1, Title: "write the sheet", State: TaskUnverified, Settling: "your accept",
	})
	select {
	case ev := <-lane:
		t.Fatalf("an in-flight settle raised %v", ev.Kind)
	case <-time.After(100 * time.Millisecond):
	}
	if kind := agent.landingAsked(1); kind != "" {
		t.Fatalf("a held-down publish still banked a %q question", kind)
	}

	// AND THE TERMINAL NOTICE RAISES: resettle hands the claim back inside its
	// locked write, before the notice is built, so the settle's own last word
	// is not blanketed by the hold-down.
	agent.publishLandingQuestion(TaskNotice{ID: 1, Title: "write the sheet", State: TaskUnverified})
	ev := <-lane
	if ev.Kind != EventQuestion || ev.Question == nil {
		t.Fatalf("the terminal notice did not raise: %v", ev.Kind)
	}
}

// A DECIDER CHANGE DURING A FLIGHT REDRAWS THE CARD (#1077's review round 1):
// `let codeaf decide` pressed mid-re-audit must reach every window at once, and
// the redraw must mint the NEW shape — the standing bank would return the old
// card unchanged.
func TestADeciderChangeDuringAFlightRedrawsTheCard(t *testing.T) {
	agent := &Agent{}
	lane, stop := agent.WatchQuestions()
	defer stop()

	agent.publishLandingQuestion(TaskNotice{ID: 1, Title: "write the sheet", State: TaskUnverified})
	first := <-lane
	agent.publishLandingQuestion(TaskNotice{
		ID: 1, Title: "write the sheet", State: TaskUnverified,
		Settling: "your accept", Decider: TaskAskOwnerModel,
	})
	redrawn := <-lane
	if redrawn.Kind != EventQuestion || redrawn.Question == nil {
		t.Fatalf("a changed holder mid-flight raised %v", redrawn.Kind)
	}
	if redrawn.Question.Policy == first.Question.Policy {
		t.Fatalf("the redraw banked the standing card: policy %q did not move", redrawn.Question.Policy)
	}
}

// THE FLIGHT STAMP DIES WITH THE FLIGHT (#1077's review round 2): the redraw
// during a settle banks `handed it to codeaf 18:20 · still working on it`, and
// the settle's terminal notice must not re-emit that bank verbatim — a card
// saying the work is still running after it finished is the stale-card class
// the fix is about. The terminal raise mints fresh: the record's fate, the
// ask's own reason, no flight stamp.
func TestTheTerminalNoticeRetiresTheFlightStamp(t *testing.T) {
	agent, _ := questionSession(t, "landing-flight-stamp", nil)
	lane, stop := agent.WatchQuestions()
	defer stop()

	notice := TaskNotice{ID: 1, Title: "write the sheet", State: TaskUnverified}
	agent.publishLandingQuestion(notice)
	<-lane

	at := time.Now().Truncate(time.Minute)
	q := agent.landingQuestion(PendingDecision{Notice: notice})
	agent.recordDecision(decisionRecordOf(q, Answer{
		At:        at,
		Kind:      QuestionLanding,
		ID:        1,
		Key:       LandingDecideKey,
		DecidedBy: DecidedByPerson,
	}))

	// the decider change during the flight redraws with the flight stamp
	agent.publishLandingQuestion(TaskNotice{
		ID: 1, Title: "write the sheet", State: TaskUnverified,
		Settling: "your accept", Decider: TaskAskOwnerModel,
	})
	redrawn := <-lane
	if redrawn.Kind != EventQuestion || redrawn.Question == nil {
		t.Fatalf("the redraw did not go out: %+v", redrawn)
	}
	if !strings.Contains(redrawn.Question.Reason, "still working on it") {
		t.Fatalf("the redraw during the flight does not carry the flight stamp: %q", redrawn.Question.Reason)
	}

	// and the terminal notice retires it
	agent.publishLandingQuestion(TaskNotice{
		ID: 1, Title: "write the sheet", State: TaskUnverified, Decider: TaskAskOwnerModel,
	})
	terminal := <-lane
	if terminal.Kind != EventQuestion || terminal.Question == nil {
		t.Fatalf("the terminal raise did not go out: %+v", terminal)
	}
	if strings.Contains(terminal.Question.Reason, "still working on it") {
		t.Fatalf("the flight stamp outlived the flight: %q", terminal.Question.Reason)
	}
	if !strings.HasPrefix(terminal.Question.Reason, "handed it to codeaf "+at.Format("15:04")) {
		t.Fatalf("the terminal card does not lead with the answer's fate: %q", terminal.Question.Reason)
	}
}

// A STAMP FROM ANOTHER DAY SAYS ITS DAY (#1077's Opus review): `accepted
// 18:20` on a card drawn the next morning reads as an hour ago, and the stamp
// is the one place the card says when.
func TestTheStampSaysItsDayWhenItIsNotToday(t *testing.T) {
	today := time.Now()
	stamp := landingAnsweredStamp(DecisionRecord{
		Kind: QuestionLanding, Picked: []string{LandingYesKey},
		By: DecidedByPerson, At: today.Add(-26 * time.Hour),
	})
	if !strings.Contains(stamp, today.Add(-26*time.Hour).Format("Jan 2")) {
		t.Fatalf("a stamp from another day does not say its day: %q", stamp)
	}
	if landingAnsweredStamp(DecisionRecord{
		Kind: QuestionLanding, Picked: []string{LandingYesKey},
		By: DecidedByPerson, At: today,
	}) != "accepted "+today.Format("15:04") {
		t.Fatalf("a stamp from today says more than its hour")
	}
}
