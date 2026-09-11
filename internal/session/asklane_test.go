package session

// The laws of the ask lane: an answer is a message, and the three things that
// fall out of that (docs/design/questions/DESIGN.md, "An answer is a message").

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

// askAnswerRead is the [Answer] out of what the model reads. The rendering is a
// sentence and then the answer ([askAnswerText]), so a test reads it the way the
// model does: the fact first, the object under it.
func askAnswerRead(t *testing.T, text string) Answer {
	t.Helper()
	at := strings.IndexByte(text, '\n')
	if at < 0 {
		t.Fatalf("the answer carries no object under its sentence: %q", text)
	}
	var answer Answer
	if err := json.Unmarshal([]byte(text[at+1:]), &answer); err != nil {
		t.Fatalf("the answer's object does not read: %v · %q", err, text)
	}
	return answer
}

// askDelivered is every answer this conversation has been handed with nobody
// parked on it: what is still on the queue, and what a turn has already taken
// off it — a wake starts a turn the instant the note lands, so a test that only
// read the queue would be reading a race.
func askDelivered(a *Agent) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	notes := make([]string, 0, len(a.steering)+len(a.messages))
	for _, message := range a.messages {
		if message.Role != "user" {
			continue
		}
		if text := partsText(message); strings.HasPrefix(text, askAnsweredWord) || strings.HasPrefix(text, askChangedWord) {
			notes = append(notes, text)
		}
	}
	for _, message := range a.steering {
		notes = append(notes, message.text())
	}
	return notes
}

// waitForOneQuestion is the question this conversation has just put, without a
// sleep of its own worth of guessing.
func waitForOneQuestion(t *testing.T, a *Agent) Question {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		if open := a.OpenQuestions(); len(open) > 0 {
			return open[0]
		}
		select {
		case <-deadline:
			t.Fatal("no question was ever put")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

const askNotWaiting = `{"head":"Which storage shape should this use?","kind":"choice","reason":"two shapes fit and the record does not choose","stakes":"reversible","blocking":{"turn":false},"options":[{"key":"1","label":"sqlite"},{"key":"2","label":"jsonl"}],"pick":{"key":"1","reason":"the readers we have are sqlite"}}`

const askWaiting = `{"head":"Which storage shape should this use?","kind":"choice","reason":"two shapes fit and the record does not choose","stakes":"reversible","options":[{"key":"1","label":"sqlite"},{"key":"2","label":"jsonl"}],"pick":{"key":"1","reason":"the readers we have are sqlite"}}`

// AN ASK THAT SAYS THE TURN DOES NOT WAIT COMES BACK AT ONCE, and the question
// it raised is still standing when it does. It is the whole of D8: the model
// carries on with what does not depend on the answer.
func TestAnAskThatDoesNotWaitComesBackAtOnceAndTheQuestionStands(t *testing.T) {
	a := askTestAgent(t, true)
	done := make(chan string, 1)
	go func() {
		text, _, _ := a.executeAsk(context.Background(), json.RawMessage(askNotWaiting))
		done <- text
	}()
	var text string
	select {
	case text = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("an ask that does not wait waited")
	}
	if !strings.HasPrefix(text, askAskedWord+" · question 1 · ") {
		t.Fatalf("the asker was not told what happened: %q", text)
	}
	if !strings.Contains(text, askAnsweredWord) {
		t.Fatalf("the asker was not told what its answer will look like: %q", text)
	}
	open := a.OpenQuestions()
	if len(open) != 1 || open[0].ID != 1 {
		t.Fatalf("the question did not stay open: %+v", open)
	}
	if open[0].Blocking.Turn {
		t.Fatal("the question says the turn is waiting on it, and it is not")
	}
}

// THE ANSWER READS THE SAME WHETHER A CALL WAS PARKED OR NOT. A model that
// asked without waiting must be handed exactly what a model that waited is
// handed, or there are two contracts and only one of them was ever tested.
func TestTheAnswerReadsTheSameWhetherTheTurnWaitedOrNot(t *testing.T) {
	answered := func(raw string, parked bool) string {
		t.Helper()
		a := askTestAgent(t, true)
		done := make(chan string, 1)
		go func() {
			text, _, _ := a.executeAsk(context.Background(), json.RawMessage(raw))
			done <- text
		}()
		q := waitForOneQuestion(t, a)
		if !parked {
			<-done
		}
		answer := Answer{Kind: QuestionAsk, ID: q.ID, Key: "2", Change: "but keep the sqlite file"}
		if err := a.ResolveQuestion(answer); err != nil {
			t.Fatal(err)
		}
		if parked {
			select {
			case text := <-done:
				return text
			case <-time.After(2 * time.Second):
				t.Fatal("the parked call was never handed its answer")
			}
		}
		deadline := time.After(2 * time.Second)
		for {
			if notes := askDelivered(a); len(notes) > 0 {
				return notes[len(notes)-1]
			}
			select {
			case <-deadline:
				t.Fatal("the answer never reached the conversation")
			default:
				time.Sleep(time.Millisecond)
			}
		}
	}
	waited := answered(askWaiting, true)
	carried := answered(askNotWaiting, false)
	// The answers were given a moment apart, so the one field that cannot be
	// equal is the instant; everything else is the same bytes.
	strip := func(text string) string {
		answer := askAnswerRead(t, text)
		answer.At = time.Time{}
		at := strings.IndexByte(text, '\n')
		return text[:at+1] + answerJSON(answer)
	}
	if strip(waited) != strip(carried) {
		t.Fatalf("the two forms read differently:\n parked: %s\n message: %s", strip(waited), strip(carried))
	}
	if !strings.Contains(carried, "but keep the sqlite file") {
		t.Fatalf("the person's own words did not travel: %q", carried)
	}
}

// A PICK THE CLOCK TAKES IS PROVISIONAL AND SAYS SO. The person has not said
// this; a model that read it as settled would never mention it again.
func TestAPickTakenByTheClockReadsAsProvisionalAndRunsWithNobodyParked(t *testing.T) {
	a := askTestAgent(t, true)
	if err := a.SetAutonomy(AskChoice, Policy{Kind: PolicyRecommendThenAuto, After: time.Hour}); err != nil {
		t.Fatal(err)
	}
	text, _, _ := a.executeAsk(context.Background(), json.RawMessage(askNotWaiting))
	if !strings.HasPrefix(text, askAskedWord) {
		t.Fatalf("the ask did not come back at once: %q", text)
	}
	q := waitForOneQuestion(t, a)
	// THE CLOCK IS ARMED ON A QUESTION NOBODY IS PARKED ON, which is the half
	// that used to be impossible: it lived inside the call's own select.
	a.mu.Lock()
	open := a.asked.atLocked(q.ID)
	armed := open != nil && open.clock != nil
	a.mu.Unlock()
	if !armed {
		t.Fatal("no clock was armed on a question the call did not wait for")
	}
	if q.Deadline.IsZero() {
		t.Fatal("the question carries no deadline for a surface to draw")
	}
	// AND IT IS RUN RATHER THAN WAITED FOR. An hour is on the dial above, so
	// what this asserts is the clock's own road and not the speed of this box;
	// a test that slept would be a wall-clock gate, which PERF.md bans.
	a.askClockRanOut(q.ID)
	notes := askDelivered(a)
	if len(notes) == 0 {
		t.Fatal("the clock never took the pick on a question nobody was parked on")
	}
	note := notes[len(notes)-1]
	if !strings.Contains(note, "provisional") {
		t.Fatalf("the pick was not marked provisional: %q", note)
	}
	answer := askAnswerRead(t, note)
	if answer.DecidedBy != DecidedByDial || answer.FirstKey() != "1" {
		t.Fatalf("the clock did not take the asker's own pick: %+v", answer)
	}
}

// A KEY ON THE QUESTION STOPS THE CLOCK IN THE ENGINE, not only in the drawing.
// The deadline is the one source of truth for the countdown, so it goes, and the
// question is said again with it gone.
func TestAHoldStopsTheClockAndClearsTheDeadline(t *testing.T) {
	a := askTestAgent(t, true)
	if err := a.SetAutonomy(AskChoice, Policy{Kind: PolicyRecommendThenAuto, After: time.Hour}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.executeAsk(context.Background(), json.RawMessage(askNotWaiting)); err != nil {
		t.Fatal(err)
	}
	q := waitForOneQuestion(t, a)
	if q.Deadline.IsZero() {
		t.Fatal("a recommend-then-auto question carries no deadline for a surface to draw")
	}
	a.HoldQuestion(QuestionAsk, q.Token())
	held := waitForOneQuestion(t, a)
	if !held.Deadline.IsZero() {
		t.Fatalf("the deadline survived the hold: %v", held.Deadline)
	}
	// AND A CLOCK THAT RAN OUT ANYWAY DECIDES NOTHING. Running it is what
	// proves the hold reached the engine rather than only the drawing — a sleep
	// would prove only that this box was quick enough.
	a.askClockRanOut(q.ID)
	if notes := askDelivered(a); len(notes) > 0 {
		t.Fatalf("the clock answered a question somebody was reading: %v", notes)
	}
	if open := a.OpenQuestions(); len(open) != 1 {
		t.Fatalf("the held question is not still open: %+v", open)
	}
}

// A SETTLED REVERSIBLE DECISION CAN BE CHANGED, and the correction says what it
// was changed from — because whatever was done on the old answer is now the
// thing to look at.
func TestChangingAnAnswerReachesTheModelAsACorrection(t *testing.T) {
	a := askTestAgent(t, true)
	if _, _, err := a.executeAsk(context.Background(), json.RawMessage(askNotWaiting)); err != nil {
		t.Fatal(err)
	}
	q := waitForOneQuestion(t, a)
	if err := a.ResolveQuestion(Answer{Kind: QuestionAsk, ID: q.ID, Key: "1"}); err != nil {
		t.Fatal(err)
	}
	if err := a.ResolveQuestion(Answer{Kind: QuestionAsk, ID: q.ID, Key: "2", Revises: true}); err != nil {
		t.Fatal(err)
	}
	notes := askDelivered(a)
	if len(notes) != 2 {
		t.Fatalf("the answer and the change did not both reach the conversation: %v", notes)
	}
	change := notes[1]
	if !strings.HasPrefix(change, askChangedWord) || !strings.Contains(change, "was sqlite, now jsonl") {
		t.Fatalf("the correction does not say what changed: %q", change)
	}
	// AND THE RECORD KEEPS THE NEWER DECISION, which is what the gate reads —
	// with the clause that keeps two lines about one head from reading as a
	// contradiction.
	records := a.Decisions()
	if len(records) != 2 || records[1].Words() != "jsonl" {
		t.Fatalf("the record does not end on the changed answer: %+v", records)
	}
	if line := records[1].Line(); !strings.Contains(line, "changed from sqlite") {
		t.Fatalf("the changed line does not say what it replaced: %q", line)
	}
	decided, ok := decidedAlready(records, q)
	if !ok || decided.Words() != "jsonl" {
		t.Fatalf("the gate would answer the next ask with the old decision: %+v", decided)
	}
}

// AN IRREVERSIBLE DECISION IS NOT CHANGED FROM A RECEIPT. What it allowed has
// already happened, and a key that pretended otherwise would be a lie on the
// screen.
func TestAnIrreversibleDecisionCannotBeChanged(t *testing.T) {
	a := askTestAgent(t, true)
	raw := `{"head":"Delete the release branch?","kind":"confirmation","reason":"it cannot be recovered","stakes":"irreversible","blocking":{"turn":false},"options":[{"key":"1","label":"delete it"},{"key":"2","label":"keep it","safe":true}]}`
	if _, _, err := a.executeAsk(context.Background(), json.RawMessage(raw)); err != nil {
		t.Fatal(err)
	}
	q := waitForOneQuestion(t, a)
	if err := a.ResolveQuestion(Answer{Kind: QuestionAsk, ID: q.ID, Key: "1"}); err != nil {
		t.Fatal(err)
	}
	err := a.ResolveQuestion(Answer{Kind: QuestionAsk, ID: q.ID, Key: "2", Revises: true})
	if err == nil {
		t.Fatal("an irreversible decision was changed")
	}
	if !strings.Contains(err.Error(), "cannot be changed") {
		t.Fatalf("the refusal does not say why: %v", err)
	}
	if notes := askDelivered(a); len(notes) != 1 {
		t.Fatalf("the refused change still reached the model: %v", notes)
	}
}

// A REVISION OF A QUESTION THIS CONVERSATION NEVER ASKED IS REFUSED rather than
// silently dropped: a person who pressed a key is owed the sentence.
func TestAChangeToAQuestionTheLaneNeverHeldIsRefused(t *testing.T) {
	a := askTestAgent(t, true)
	err := a.ResolveQuestion(Answer{Kind: QuestionAsk, ID: 9, Key: "1", Revises: true})
	if err == nil || !strings.Contains(err.Error(), "earlier run") {
		t.Fatalf("a change to nothing was taken: %v", err)
	}
	if err := a.ResolveQuestion(Answer{Kind: QuestionStanding, ID: 1, Key: "1", Revises: true}); err == nil {
		t.Fatal("a lane that cannot take a change took one")
	}
}

// A WIDENING YES TAKEN BACK IS A PERMISSION REVOKED: the gate asks again.
func TestChangingAnAlwaysRevokesThePermission(t *testing.T) {
	a := askTestAgent(t, true)
	a.rememberConsent("bash", true)
	a.recordDecision(DecisionRecord{
		ID: 4, Kind: QuestionConsent, Ask: AskPermission, Head: "needs your ok to run bash",
		Subject: SubjectRef{Kind: SubjectCall, Name: "bash"},
		Picked:  []string{"2"}, By: DecidedByPerson, Stakes: StakesCostly, At: time.Now(),
	})
	if err := a.ResolveQuestion(Answer{Kind: QuestionConsent, ID: 4, Key: "3", Revises: true}); err != nil {
		t.Fatal(err)
	}
	if _, known := a.rememberedConsent("bash"); known {
		t.Fatal("the permission still stands after it was taken back")
	}
}

// AN ANSWER TO A QUESTION THIS LANE IS NOT HOLDING IS REFUSED IN WORDS. It is
// the row left on a screen by a turn that was interrupted, or by a session host
// that has restarted since — and the person pressing the key is owed the
// sentence rather than a key that does nothing.
func TestAnAnswerToAQuestionTheLaneNoLongerHoldsIsRefusedInWords(t *testing.T) {
	a := askTestAgent(t, true)
	err := a.ResolveQuestion(Answer{Kind: QuestionAsk, ID: 4, Key: "1"})
	if err == nil {
		t.Fatal("an answer to a question nothing is asking was taken in silence")
	}
	if !strings.Contains(err.Error(), "restarted") {
		t.Fatalf("the refusal does not say what happened: %v", err)
	}
	// AND A SECOND CLICK ON AN ANSWERED QUESTION IS STILL SILENT, which is the
	// law this one must not have broken (answers.go).
	if _, _, err := a.executeAsk(context.Background(), json.RawMessage(askNotWaiting)); err != nil {
		t.Fatal(err)
	}
	q := waitForOneQuestion(t, a)
	if err := a.ResolveQuestion(Answer{Kind: QuestionAsk, ID: q.ID, Key: "1"}); err != nil {
		t.Fatal(err)
	}
	if err := a.ResolveQuestion(Answer{Kind: QuestionAsk, ID: q.ID, Key: "2"}); err != nil {
		t.Fatalf("a second click was refused: %v", err)
	}
}

// AND A CHANGE AFTER A TURN WAITED FOR ITS ANSWER IS STILL A MESSAGE. The call
// that was parked has taken its answer and gone; the correction cannot be handed
// to it, and a channel nobody reads is where this was lost on the Spark before
// the wait was claimed on the handover (askwait.go).
func TestChangingAnAnswerTheTurnWaitedForReachesTheModel(t *testing.T) {
	a := askTestAgent(t, true)
	done := make(chan string, 1)
	go func() {
		text, _, _ := a.executeAsk(context.Background(), json.RawMessage(askWaiting))
		done <- text
	}()
	q := waitForOneQuestion(t, a)
	if err := a.ResolveQuestion(Answer{Kind: QuestionAsk, ID: q.ID, Key: "1"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the parked call was never handed its answer")
	}
	if err := a.ResolveQuestion(Answer{Kind: QuestionAsk, ID: q.ID, Key: "2", Revises: true}); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(2 * time.Second)
	for {
		notes := askDelivered(a)
		if len(notes) > 0 {
			if !strings.HasPrefix(notes[len(notes)-1], askChangedWord) {
				t.Fatalf("what reached the model is not the correction: %q", notes[len(notes)-1])
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("the change never reached the model")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

// deskRows is how many question rows this session is publishing for another
// window to read (taskpresence.go's desk).
func deskRows(a *Agent) int {
	if a.presence == nil {
		return 0
	}
	a.presence.mu.Lock()
	defer a.presence.mu.Unlock()
	return len(a.presence.asks)
}

// AN ANSWERED QUESTION LEAVES THE DESK, and it is the answer that takes it off
// rather than the call returning.
//
// Measured on the Spark, 2026-09-11: with the let-go still deferred in the call,
// a question the call did not wait for kept its presence row for the rest of the
// session — so home drew that answered row as the thing this conversation was
// waiting for, every time any other question came up, and a key pressed on it
// did nothing at all.
func TestAnAnswerTakesTheQuestionOffTheDeskWhenNobodyWasParked(t *testing.T) {
	a := askTestAgent(t, true)
	if _, _, err := a.executeAsk(context.Background(), json.RawMessage(askNotWaiting)); err != nil {
		t.Fatal(err)
	}
	q := waitForOneQuestion(t, a)
	if rows := deskRows(a); rows != 1 {
		t.Fatalf("the question never reached the desk another window reads: %d rows", rows)
	}
	if err := a.ResolveQuestion(Answer{Kind: QuestionAsk, ID: q.ID, Key: "2"}); err != nil {
		t.Fatal(err)
	}
	if rows := deskRows(a); rows != 0 {
		t.Fatalf("an answered question is still on the desk: %d rows", rows)
	}
	if open := a.OpenQuestions(); len(open) != 0 {
		t.Fatalf("an answered question is still open: %+v", open)
	}
}

// A QUESTION NOTHING WAITS ON DOES NOT SAY THIS CONVERSATION IS WAITING ON YOU.
// It is working, with a question standing beside the work — which is the whole
// of what asking without waiting means, and what every other window and the home
// page read off the presence file.
func TestAQuestionTheTurnDoesNotWaitForIsNotWaitingOnYou(t *testing.T) {
	a := askTestAgent(t, true)
	if _, _, err := a.executeAsk(context.Background(), json.RawMessage(askNotWaiting)); err != nil {
		t.Fatal(err)
	}
	waitForOneQuestion(t, a)
	if waiting := a.waitingOnPerson(); waiting.waiting {
		t.Fatalf("a question the turn does not wait for made the conversation say it was waiting: %+v", waiting)
	}
}

// NOTHING ARMED OUTLIVES THE SESSION THAT ARMED IT. A clock that fired after the
// door shut would write a decision into the record with nobody left to read the
// answer — and the next run of this conversation reads that record as something
// the person settled.
func TestAClockThatRunsOutAfterTheSessionClosedDecidesNothing(t *testing.T) {
	a := askTestAgent(t, true)
	if err := a.SetAutonomy(AskChoice, Policy{Kind: PolicyRecommendThenAuto, After: time.Hour}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.executeAsk(context.Background(), json.RawMessage(askNotWaiting)); err != nil {
		t.Fatal(err)
	}
	q := waitForOneQuestion(t, a)
	before := len(a.Decisions())
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	a.askClockRanOut(q.ID)
	if after := len(a.Decisions()); after != before {
		t.Fatalf("the clock wrote a decision after the session closed: %d → %d", before, after)
	}
}

// AND AN ANSWER NOBODY CAN READ IS NOT A DECISION EITHER. A person's key press
// that lands on a conversation which has since closed is refused rather than
// recorded, for the same reason: what makes a decision real is that the model
// was told.
func TestAnAnswerToAClosedConversationIsRefusedAndRecordsNothing(t *testing.T) {
	a := askTestAgent(t, true)
	if _, _, err := a.executeAsk(context.Background(), json.RawMessage(askNotWaiting)); err != nil {
		t.Fatal(err)
	}
	q := waitForOneQuestion(t, a)
	before := len(a.Decisions())
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if err := a.ResolveQuestion(Answer{Kind: QuestionAsk, ID: q.ID, Key: "2"}); err == nil {
		t.Fatal("an answer to a closed conversation was taken as though it had been read")
	}
	if after := len(a.Decisions()); after != before {
		t.Fatalf("a decision was recorded for an answer nothing read: %d → %d", before, after)
	}
}

// THE FIRST ANSWER WINS AND THE SECOND CHANGES NOTHING — including the record.
// The clock and a person can reach this door in the same instant, and a loser
// that wrote its own record left the windows and the transcript showing one
// answer while the model had been told the other.
func TestTheSecondAnswerToOneQuestionRecordsNothing(t *testing.T) {
	a := askTestAgent(t, true)
	if _, _, err := a.executeAsk(context.Background(), json.RawMessage(askNotWaiting)); err != nil {
		t.Fatal(err)
	}
	q := waitForOneQuestion(t, a)
	if err := a.ResolveQuestion(Answer{Kind: QuestionAsk, ID: q.ID, Key: "2"}); err != nil {
		t.Fatal(err)
	}
	records := a.Decisions()
	if len(records) != 1 {
		t.Fatalf("one answer wrote %d records", len(records))
	}
	// The loser says nothing to anybody: a second key press on a question that
	// is already answered is not a fault.
	if err := a.ResolveQuestion(Answer{Kind: QuestionAsk, ID: q.ID, Key: "1"}); err != nil {
		t.Fatalf("a second answer was reported as a failure: %v", err)
	}
	after := a.Decisions()
	if len(after) != 1 {
		t.Fatalf("the loser of the race wrote a record: %+v", after)
	}
	if after[0].Words() != records[0].Words() {
		t.Fatalf("the record changed under the winner: %q → %q", records[0].Words(), after[0].Words())
	}
	notes := askDelivered(a)
	if len(notes) != 1 {
		t.Fatalf("the model was told about a question twice: %v", notes)
	}
}

// A RATIFY WAITS ON NOBODY, which is its own definition ("something reversible
// was DONE … nothing waits on the answer") and could not be true until an answer
// could arrive with nobody parked. So the call comes back at once, the row stands
// for as long as somebody wants to unwind the thing, and no window says this
// conversation is waiting on them.
func TestARatifyDoesNotStopTheTurnAndIsNotWaitingOnYou(t *testing.T) {
	a := askTestAgent(t, true)
	const ratify = `{"head":"I renamed the two fixtures to match the test names","kind":"ratify","reason":"it was reversible and the names were already wrong","stakes":"reversible","options":[{"key":"1","label":"fine"},{"key":"2","label":"put it back","safe":true}]}`
	done := make(chan string, 1)
	go func() {
		text, _, _ := a.executeAsk(context.Background(), json.RawMessage(ratify))
		done <- text
	}()
	select {
	case text := <-done:
		if !strings.HasPrefix(text, askShownLead) {
			t.Fatalf("a ratify did not come back as a standing question: %q", text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the turn parked on a question its own rung says nobody waits for")
	}
	q := waitForOneQuestion(t, a)
	if q.Blocking.Blocks() {
		t.Fatalf("a ratify says something is waiting on it: %+v", q.Blocking)
	}
	if waiting := a.waitingOnPerson(); waiting.waiting {
		t.Fatal("a ratify put `waiting on you` on the presence file")
	}
	// AND IT IS THE RUNG'S OWN ANSWER, NOT THE ASKER'S ([AskKind.Waits]): a
	// ratify that writes `blocking: {turn: true}` still stops nothing, which is
	// pinned from the other end by TestARatifyThatAskedToStopTheTurnStopsNothing.
}

// THE BOOK DOES NOT GROW FOR THE LIFE OF A SESSION. A settled question is kept
// so its answer can be changed while the receipt offering that is on screen, and
// the oldest go once more are kept than can ever be drawn — otherwise every
// question ever answered in a long conversation stays in the one book.
func TestTheBookKeepsOnlyTheNewestAnsweredQuestions(t *testing.T) {
	a := askTestAgent(t, true)
	const rounds = askSettledKept + 3
	for round := range rounds {
		// A DIFFERENT QUESTION EACH TIME, because the gate refuses one a record
		// has already answered — which is the ladder's first rung and not this
		// law's subject.
		raw := strings.Replace(askNotWaiting, "Which storage shape should this use?",
			"Which storage shape should round "+strconv.Itoa(round)+" use?", 1)
		if _, _, err := a.executeAsk(context.Background(), json.RawMessage(raw)); err != nil {
			t.Fatal(err)
		}
		q := waitForOneQuestion(t, a)
		if err := a.ResolveQuestion(Answer{Kind: QuestionAsk, ID: q.ID, Key: "1"}); err != nil {
			t.Fatal(err)
		}
	}
	a.mu.Lock()
	held := len(a.asked.parked)
	newest := a.asked.atLocked(uint64(rounds))
	oldest := a.asked.atLocked(1)
	a.mu.Unlock()
	if held != askSettledKept {
		t.Fatalf("the book holds %d answered questions, want %d", held, askSettledKept)
	}
	if newest == nil {
		t.Fatal("the newest decision is not there to be changed")
	}
	if oldest != nil {
		t.Fatal("the oldest decision is still in the book")
	}
	// AND THE ONE THE RECEIPT IS ABOUT CAN STILL BE CHANGED, which is the whole
	// reason any of them are kept.
	change := Answer{Kind: QuestionAsk, ID: uint64(rounds), Key: "2", Revises: true}
	if err := a.ResolveQuestion(change); err != nil {
		t.Fatalf("the newest decision could not be changed: %v", err)
	}
	// A decision that has left the book is refused in words rather than silently
	// applied to nothing.
	if err := a.ResolveQuestion(Answer{Kind: QuestionAsk, ID: 1, Key: "2", Revises: true}); err == nil {
		t.Fatal("a decision the book no longer holds was changed anyway")
	}
}

// AND NOTHING SETTLED OUTLIVES THE SESSION. A receipt kept so its decision could
// be changed is one nobody can change once the door has shut.
//
// AND [Agent.Close] IS WHAT DOES IT, which is the half this test could not
// reach while `agent.go` had no call in it: it now says
// `a.stopAskClocksLocked()` beside `a.stopSteerGraceLocked()`, because the two
// are one law (steer_grace.go: NOTHING ARMED OUTLIVES THE SESSION THAT ARMED
// IT). Nothing here reaches into the book itself any more — Close is asked to
// do it and the book is only read afterwards.
func TestClosingTheSessionClearsWhatWasKeptForARevision(t *testing.T) {
	a := askTestAgent(t, true)
	if _, _, err := a.executeAsk(context.Background(), json.RawMessage(askNotWaiting)); err != nil {
		t.Fatal(err)
	}
	q := waitForOneQuestion(t, a)
	if err := a.ResolveQuestion(Answer{Kind: QuestionAsk, ID: q.ID, Key: "1"}); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	kept := a.asked.atLocked(q.ID) != nil
	a.mu.Unlock()
	if !kept {
		t.Fatal("the decision was not kept for a change while the session was open")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	left := a.asked.atLocked(q.ID)
	a.mu.Unlock()
	if left != nil {
		t.Fatal("a settled question outlived the session that answered it")
	}
}

// AND THE CLOCK ON A QUESTION NOBODY ANSWERED IS STOPPED BY THE SAME LINE.
//
// The question itself is left exactly where it is — a session leaving does not
// answer it, and the next one has its own book — so what has to be proved is
// about the TIMER and not about the entry. `time.Timer.Stop` reports whether it
// was the call that stopped a timer still running, so a second Stop answering
// false is the proof that Close already made the first one: an armed clock would
// answer true here, and it would go on to fire on a session with nobody left to
// read what it decided.
//
// [TestAClockThatRunsOutAfterTheSessionClosedDecidesNothing] is the other half
// and they are not the same fact. That one pins the GUARD — a clock that fires
// anyway writes nothing. This one pins that it does not fire at all, which is
// what stops a closed session holding a goroutine and a timer for an hour.
func TestClosingTheSessionStopsTheClockOnAQuestionNobodyAnswered(t *testing.T) {
	a := askTestAgent(t, true)
	if err := a.SetAutonomy(AskChoice, Policy{Kind: PolicyRecommendThenAuto, After: time.Hour}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.executeAsk(context.Background(), json.RawMessage(askNotWaiting)); err != nil {
		t.Fatal(err)
	}
	q := waitForOneQuestion(t, a)
	a.mu.Lock()
	armed := a.asked.atLocked(q.ID)
	running := armed != nil && armed.clock != nil
	a.mu.Unlock()
	if !running {
		t.Fatal("no clock was armed, so this test would pass on a question that never had one")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	open := a.asked.atLocked(q.ID)
	a.mu.Unlock()
	if open == nil || open.clock == nil {
		t.Fatal("an open question nobody answered was dropped by the close; only settled ones are")
	}
	if open.clock.Stop() {
		t.Fatal("the clock was still running after the session closed")
	}
}
