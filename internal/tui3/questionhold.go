package tui3

// Stopping a clock that ANSWERS.
//
// This surface has always held its own reading clock: a countdown that never
// answers and only ever stops (question.go's [app.tickQuestion]). The other kind
// belongs to the engine — a question under `recommend-then-auto` takes the
// asker's own pick when its deadline passes — and a key pressed on one of those
// has to reach the engine, because the drawing stopping while the engine went on
// counting is the pick taken under somebody's hand with `paused` on the screen
// above it.
//
// The task proposal's card had this door first ([app.holdTask]); this is the
// same act for every lane, hung off the question's own properties rather than
// off its name.

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
)

// questionHolder is the door that stops a clock, when the agent under this
// surface has one. It is asked for separately from [questionAgent] on that
// interface's own terms: an engine can apply an answer without being able to
// hold a clock, and a capability that cannot work is absent rather than broken.
type questionHolder interface {
	// HoldQuestion stops the clock on the question this lane and token name,
	// without answering it. It never blocks: what a person sees is the question
	// said again with its deadline gone.
	HoldQuestion(session.QuestionKind, string)
}

// questionClockAnswers reports whether this question's clock will DECIDE
// something when it runs out — the one kind a key has to stop in the engine.
func questionClockAnswers(q session.Question) bool {
	return q.Policy.Kind == session.PolicyRecommendThenAuto && !q.Deadline.IsZero()
}

// questionHold is the hook [app.holdQuestionClocks] calls for one question, or
// nil where this question has no clock that answers.
func (a *app) questionHold(q session.Question) func() {
	if !questionClockAnswers(q) {
		return nil
	}
	kind, token, drawn := q.Kind, q.Token(), questionTokenOf(q)
	return func() { a.holdQuestionClock(kind, token, drawn) }
}

// holdQuestionClock takes the countdown off this window at once and tells the
// engine to stop counting.
//
// THE DRAWING GOES FIRST FOR [app.holdTask]'S REASON: the person pressed a key
// and the row has to stop counting on that frame, not on the round trip. The
// engine says the question again with its deadline gone (session's
// [Agent.restateQuestion]), and the object that arrives says the same thing this
// window already drew.
func (a *app) holdQuestionClock(kind session.QuestionKind, token, drawn string) {
	for i := range a.questions {
		if a.questions[i].token() != drawn {
			continue
		}
		a.questions[i].question.Deadline = time.Time{}
		a.touch()
	}
	doors, ok := a.agent.(questionHolder)
	if !ok || doors == nil {
		return
	}
	// AND THE DOOR IS ASKED FROM A COMMAND AND NEVER FROM INSIDE Update. It is a
	// call over a connection, and the one thing this window may not do while it
	// holds the update loop is wait on the engine (remote's callclass.go says
	// what that costs). [app.takeQuestionHolds] is where it is handed back to
	// bubbletea, on the one line every message passes through.
	a.questionHolds = append(a.questionHolds, func() tea.Msg {
		doors.HoldQuestion(kind, token)
		return nil
	})
}

// takeQuestionHolds is every hold this frame asked for, as one command.
//
// IT IS DRAINED IN [app.Update] AND NOWHERE ELSE, because a hold is asked for
// from inside a key routine that answers `taken` or `not taken` — and a command
// returned beside `not taken` is a command the caller throws away. The one place
// that sees every message is the one place that can hand this to bubbletea
// whatever the key turned out to mean.
func (a *app) takeQuestionHolds() tea.Cmd {
	if len(a.questionHolds) == 0 {
		return nil
	}
	holds := a.questionHolds
	a.questionHolds = nil
	return tea.Batch(holds...)
}
