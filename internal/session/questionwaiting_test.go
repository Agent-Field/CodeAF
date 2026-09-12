package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// ratifyCall and waitingCall are the two shapes these tests need: one that waits
// on nobody and one that stops the turn.
const (
	ratifyCall = `{"head":"renamed the column to slug","kind":"ratify",` +
		`"reason":"it was reversible and the name was wrong","stakes":"reversible",` +
		`"options":[{"key":"1","label":"fine","safe":true},{"key":"2","label":"put it back"}]}`
	choiceCall = `{"head":"which storage shape should this use?","kind":"choice",` +
		`"reason":"two shapes are viable and the record does not choose","stakes":"costly",` +
		`"options":[{"key":"1","label":"sqlite"},{"key":"2","label":"jsonl"}]}`
)

// THE ROW A PERSON IS TOLD THEY ARE NEEDED FOR IS ONE THAT IS WAITING FOR THEM.
//
// The waiting desk answers with its OLDEST question, and that row is the line
// home and every other window draw beside `waiting on you` — and the question a
// key press there answers. A ratify keeps its row until somebody looks at it,
// correctly, because it is still answerable; so a ratify raised before a real
// question put the RATIFY's sentence under `waiting on you` and answered the
// ratify when the key was pressed (2026-09-11). Both halves now read the one
// predicate ([Question.Waiting]).
func TestTheWaitingDeskSkipsAQuestionNothingWaitsOn(t *testing.T) {
	agent, _ := questionSession(t, "wait1111wait1111", func(config *Config) { config.Interactive = true })
	asks, stopAsking := agent.WatchQuestions()
	defer stopAsking()

	if _, _, err := agent.executeAsk(context.Background(), json.RawMessage(ratifyCall)); err != nil {
		t.Fatalf("the ratify: %v", err)
	}
	waitForAsk(t, asks, EventQuestion)
	// NOTHING IS WAITING YET. The ratify is open, drawn and answerable, and the
	// session is not stopped on anybody.
	if agent.NeedsPerson() {
		t.Fatalf("a ratify alone made the session say it was waiting: %q", agent.WaitingOn())
	}

	// And now a real question, raised SECOND — which is what made the desk answer
	// with the wrong one.
	asked := make(chan string, 1)
	go func() {
		out, _, _ := agent.executeAsk(context.Background(), json.RawMessage(choiceCall))
		asked <- out
	}()
	waitForAsk(t, asks, EventQuestion)

	waitUntil(t, agent.NeedsPerson)
	if on := agent.WaitingOn(); !strings.Contains(on, "storage shape") {
		t.Fatalf("home would draw %q under `waiting on you` — that is the ratify's line, and a key press there answers the ratify", on)
	}

	// Tidy: answer the choice so the goroutine ends.
	for _, open := range agent.OpenQuestions() {
		if open.Ask == AskChoice {
			_ = agent.ResolveQuestion(Answer{Kind: QuestionAsk, ID: open.ID, Key: "1"})
		}
	}
	<-asked
}

// A QUESTION THAT OUTLIVED ITS CALL IS RETIRED WITH ITS TURN, unless it said it
// would outlive it.
//
// [askOpen] is taken off the model's book only when somebody answers, so a
// ratify nobody looked at and a question somebody asked back on and never
// returned to had no withdrawal trigger at all: they stood in OpenQuestions, on
// the presence desk and against [QuestionCap] for the rest of the session, about
// a turn that ended long ago.
func TestAQuestionNothingWillCarryIsRetiredWhenTheTurnEnds(t *testing.T) {
	agent, _ := questionSession(t, "rtre1111rtre1111", func(config *Config) { config.Interactive = true })
	asks, stopAsking := agent.WatchQuestions()
	defer stopAsking()

	if _, _, err := agent.executeAsk(context.Background(), json.RawMessage(ratifyCall)); err != nil {
		t.Fatalf("the ratify: %v", err)
	}
	waitForAsk(t, asks, EventQuestion)
	if open := agent.OpenQuestions(); len(open) != 1 {
		t.Fatalf("OpenQuestions() = %+v", open)
	}

	agent.retireTurnQuestions()

	gone := waitForAsk(t, asks, EventQuestionWithdrawn)
	if gone.Question.Withdrawn == nil || gone.Question.Withdrawn.Reason == "" {
		t.Fatalf("the withdrawal says nothing about why: %+v", gone.Question.Withdrawn)
	}
	if open := agent.OpenQuestions(); len(open) != 0 {
		t.Fatalf("the ratify outlived its turn: %+v", open)
	}
	if agent.NeedsPerson() {
		t.Fatalf("the desk still says somebody is needed: %q", agent.WaitingOn())
	}
}

// AND A QUESTION WHOSE ASKER SAID IT WOULD READ THE ANSWER WHENEVER IT CAME IS
// NOT RETIRED. That is what `blocking: {turn: false}` means on the model's own
// `ask`, and it is the one thing [questionOutlivesTurn] is for — the rule is one
// predicate rather than a list of kinds, so a lane that learns a new shape of
// waiting does not have to be added to a switch.
func TestTheWithdrawalRuleIsOnePredicateAboutTheQuestion(t *testing.T) {
	for _, row := range []struct {
		what    string
		q       Question
		outlive bool
	}{
		{"a ratify", Question{Ask: AskRatify}, false},
		{"an ordinary ask", Question{Ask: AskChoice, Blocking: Blocking{Turn: true}}, false},
		{"one the asker is not waiting for", Question{Ask: AskChoice, Later: true}, true},
		{"one a task is waiting on", Question{Ask: AskChoice, Blocking: Blocking{Tasks: []string{"port the parser"}}}, true},
	} {
		if got := questionOutlivesTurn(row.q); got != row.outlive {
			t.Errorf("questionOutlivesTurn(%s) = %v, want %v", row.what, got, row.outlive)
		}
	}

	// AND THE TWO PREDICATES ARE NOT ONE. "May the turn end without this" and
	// "is anything stopped on this" are different questions, and a question that
	// nothing waits on may perfectly well outlive its turn — which is exactly
	// what a non-blocking ask is.
	later := Question{Ask: AskChoice, Later: true}
	if later.Waiting() {
		t.Error("a question nothing is stopped on read as waiting on somebody")
	}
	if !questionOutlivesTurn(later) {
		t.Error("a question the asker is not waiting for was retired with its turn")
	}
}

// AND THE MODEL'S OWN WORD IS WHAT SETS IT.
func TestAnAskThatBlocksNothingSaysItWillOutliveTheTurn(t *testing.T) {
	agent, _ := questionSession(t, "latr1111latr1111", func(config *Config) { config.Interactive = true })
	asks, stopAsking := agent.WatchQuestions()
	defer stopAsking()

	go func() {
		_, _, _ = agent.executeAsk(context.Background(), json.RawMessage(
			`{"head":"which storage shape?","kind":"choice","reason":"two shapes are viable",`+
				`"stakes":"reversible","blocking":{"turn":false},`+
				`"options":[{"key":"1","label":"sqlite"},{"key":"2","label":"jsonl"}]}`))
	}()
	raised := waitForAsk(t, asks, EventQuestion)
	if !raised.Question.Later {
		t.Fatalf("an ask that blocks no turn does not say it will outlive it: %+v", raised.Question)
	}

	// A ratify never carries it: it belongs to the turn that did the thing.
	if _, _, err := agent.executeAsk(context.Background(), json.RawMessage(ratifyCall)); err != nil {
		t.Fatalf("the ratify: %v", err)
	}
	for _, open := range agent.OpenQuestions() {
		if open.Ask == AskRatify && open.Later {
			t.Fatalf("a ratify said it would outlive its turn: %+v", open)
		}
	}
}

// AND A RATIFY THAT ASKED TO STOP THE TURN STILL STOPS NOTHING.
//
// This is the case a reading of `blocking` alone gets wrong, and it is why
// [AskKind.Waits] is a term of [Question.Waiting] rather than an older spelling
// of it. `blocking.turn` is DERIVED from whether this engine actually parks the
// call and is never the model's claim (tools_ask.go), so a model that writes
// `blocking: {turn: true}` beside `kind: ratify` cannot put a row on the waiting
// desk about a turn that is carrying on — and even if some other lane built the
// question by hand with that field set, the kind alone would keep it off.
func TestARatifyThatAskedToStopTheTurnStopsNothing(t *testing.T) {
	agent, _ := questionSession(t, "rtby1111rtby1111", func(config *Config) { config.Interactive = true })
	asks, stopAsking := agent.WatchQuestions()
	defer stopAsking()

	if _, _, err := agent.executeAsk(context.Background(), json.RawMessage(
		`{"head":"renamed the column to slug","kind":"ratify","reason":"it was reversible",`+
			`"stakes":"reversible","blocking":{"turn":true},`+
			`"options":[{"key":"1","label":"fine","safe":true},{"key":"2","label":"put it back"}]}`)); err != nil {
		t.Fatalf("the ratify: %v", err)
	}
	raised := waitForAsk(t, asks, EventQuestion)
	if raised.Question.Blocking.Turn {
		t.Fatalf("the ratify says the turn is stopped on it, which the model asked for and this engine did not do: %+v", raised.Question.Blocking)
	}
	if raised.Question.Waiting() {
		t.Fatal("a ratify read as waiting on somebody")
	}
	if agent.NeedsPerson() {
		t.Fatalf("home would say `waiting on you` and draw the ratify's line: %q", agent.WaitingOn())
	}
	// AND THE KIND ALONE WOULD DO IT, for a question some other lane built by
	// hand. Both terms, so neither reading can put this row on the desk.
	byHand := Question{Ask: AskRatify, Blocking: Blocking{Turn: true}}
	if byHand.Waiting() {
		t.Fatal("a ratify carrying a blocking of its own read as waiting on somebody")
	}
}

// THE MIRROR, ON THE WITHDRAWAL SIDE. A ratify is retired with the turn that
// raised it; a question the asker said it was not waiting for is not.
func TestTheTurnRetiresARatifyAndKeepsAQuestionTheAskerWillReadLater(t *testing.T) {
	agent, _ := questionSession(t, "mirr1111mirr1111", func(config *Config) { config.Interactive = true })
	ratify := &askOpen{q: Question{ID: 1, Kind: QuestionAsk, Ask: AskRatify, Head: "renamed the column"}}
	later := &askOpen{q: Question{ID: 2, Kind: QuestionAsk, Ask: AskChoice, Later: true, Head: "which storage shape?"}}
	onATask := &askOpen{q: Question{ID: 3, Kind: QuestionAsk, Ask: AskChoice, Head: "merge it?",
		Blocking: Blocking{Tasks: []string{"port the parser"}}}}

	agent.mu.Lock()
	for _, open := range []*askOpen{ratify, later, onATask} {
		agent.asked.parkLocked(open)
		// Neither has a lane reading it: both are questions that outlived the
		// call that asked them, which is the whole case this sweep is for.
		agent.asked.parked[open.q.ID].wait = nil
	}
	gone := agent.asked.retireLocked()
	standing := agent.asked.openLocked()
	agent.mu.Unlock()

	if len(gone) != 1 || gone[0].q.ID != ratify.q.ID {
		t.Fatalf("the turn's end retired %d questions, want only the ratify", len(gone))
	}
	if len(standing) != 2 {
		t.Fatalf("%d questions are still standing, want the two that outlive their turn", len(standing))
	}
}
