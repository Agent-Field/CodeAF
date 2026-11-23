package session

// askclock.go — A CLOCK THAT WAITS FOR ITS QUESTION TO BE SEEN.
//
// A choice with a suggested answer runs on a clock, and the clock starts when
// the question is raised ([Agent.startAskClock]). That is right for a surface
// that draws every question the moment it is raised. A surface that holds a
// question nothing waits on until the person pauses — so a reply carrying on
// in the background does not interrupt them mid-word — would let the clock take
// the suggestion for somebody who never saw it.
//
// So a surface may DEFER the clock of a question it is holding, and START it
// when the question is finally shown, for the policy's own length counted from
// then. It is not [Agent.HoldQuestion]: that is a person's key on a question
// they are reading and is never undone, while this is a surface saying it has
// not shown the question yet, and is undone the moment it has.
//
// IT IS BOUNDED. A deferred clock starts by itself after [askClockDeferLimit],
// so a surface that deferred and then went away cannot leave a suggestion
// unclocked for the life of the conversation.

import (
	"strconv"
	"strings"
	"time"
)

// askClockDeferLimit is the longest a surface may keep a question's clock from
// starting. It is the same five minutes a surface holds a question for at most
// (docs/design/app-v3 V84), so the two bounds end together.
//
// It is a var only so the tests can run it out in a millisecond. NOTHING IN A
// BUILD WRITES IT.
var askClockDeferLimit = 5 * time.Minute

// DeferQuestionClock stops the clock of one open question a surface has not
// shown yet, and reports whether it did. It does nothing to a question with no
// running clock, one a person held, one already answered or deferred, and to
// every lane but the model's own question, which is the only lane whose clock
// lives here.
//
// AND NOTHING TO A QUESTION THAT BLOCKS. A turn parked on it or work named as
// waiting on it ([Blocking.Blocks]) is exactly what must reach the person at
// once, so the engine refuses to defer it whatever the surface believed.
func (a *Agent) DeferQuestionClock(kind QuestionKind, token string) bool {
	if child, answer, ok := a.discussionAnswer(Answer{Kind: kind, Ref: token}); ok {
		return child.DeferQuestionClock(kind, answerToken(answer))
	}
	id, ok := askClockID(kind, token)
	if !ok {
		return false
	}
	a.mu.Lock()
	open := a.asked.atLocked(id)
	if open == nil || open.settled != nil || open.held || open.deferred || open.clock == nil || open.q.Deadline.IsZero() || open.q.Blocking.Blocks() || a.closed {
		a.mu.Unlock()
		return false
	}
	open.deferred = true
	open.q.Deadline = time.Time{}
	// THE BOUND IS THE SAME TIMER, so a deferred question carries one timer
	// exactly as a clocked one does and Close stops it the same way.
	armAskClockLocked(open, askClockDeferLimit, func(gen uint64) { a.startDeferredClock(id, gen, true) })
	q := open.q
	a.mu.Unlock()
	a.restateQuestion(q)
	return true
}

// StartQuestionClock starts a deferred clock now, for the policy's own length,
// and reports whether it did. A question that was not deferred is untouched:
// its clock is already running, or a person held it, or it never had one.
func (a *Agent) StartQuestionClock(kind QuestionKind, token string) bool {
	if child, answer, ok := a.discussionAnswer(Answer{Kind: kind, Ref: token}); ok {
		return child.StartQuestionClock(kind, answerToken(answer))
	}
	id, ok := askClockID(kind, token)
	if !ok {
		return false
	}
	return a.startDeferredClock(id, 0, false)
}

// startDeferredClock is [Agent.StartQuestionClock] by id; armed is the bound's
// own timer, which starts nothing unless it is still the live arming — a bound
// from an earlier deferral must not start a later one early.
func (a *Agent) startDeferredClock(id, gen uint64, armed bool) bool {
	a.mu.Lock()
	open := a.asked.atLocked(id)
	if open == nil || open.settled != nil || open.held || !open.deferred || a.closed || (armed && open.clockGen != gen) {
		a.mu.Unlock()
		return false
	}
	open.deferred = false
	after := open.q.Policy.After
	open.q.Deadline = time.Now().Add(after)
	armAskClockLocked(open, after, func(gen uint64) { a.askClockEnded(id, gen, true) })
	q := open.q
	a.mu.Unlock()
	a.restateQuestion(q)
	return true
}

// askClockID reads the model's own question's token; every other lane's clock
// is not this file's.
func askClockID(kind QuestionKind, token string) (uint64, bool) {
	if kind != QuestionAsk {
		return 0, false
	}
	id, err := strconv.ParseUint(strings.TrimSpace(token), 10, 64)
	return id, err == nil
}
