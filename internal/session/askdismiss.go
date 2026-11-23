package session

import (
	"strconv"
	"strings"
)

// A PERSON WHO DISMISSES A QUESTION HAS DECIDED NOTHING, AND SAID SO.
//
// The Inbox's Dismiss (the owner's decision of 2026-11-16) takes one of the
// model's own questions down without an answer. It is the talked-past ending
// for one id ([askedOfThePerson.dismissLocked]): the entry and its words are
// claimed under one lock, the question is withdrawn from every surface with the
// person's own act as the reason, and the model is told it was dismissed so it
// can ask again later if it still needs to. No [Answer] is made and no decision
// is recorded, because none was.
//
// THE MODEL HEARS IT ONE OF TWO WAYS. A call parked on the question comes back
// with [askDismissed] as its result. A question the turn did not wait for —
// [Question.Later], one a task is stopped on, a ratify, one they asked back on
// — has no call left, so the same sentence is queued as step guidance that
// never starts a turn ([Agent.enqueueNextTurnNote]); the model reads it at its
// next step, or at the first step of the next turn when none is running.

// askDismissed is what the model reads about a question the person dismissed.
const askDismissed = "the person dismissed this question without answering it: nothing was decided. Carry on without the answer; ask again later only if you still need it."

// askDismissedReason is why the question stopped standing, in the person's
// words about their own act.
const askDismissedReason = "you dismissed it"

// DismissQuestion takes down one of the model's own open questions, by its
// token, because the person dismissed it. It refuses a token this conversation
// is not asking any more — answered, withdrawn, or from before a restart — in
// the same words an answer to it would meet.
func (a *Agent) DismissQuestion(token string) error {
	id, err := strconv.ParseUint(strings.TrimSpace(token), 10, 64)
	if err != nil {
		return errAnswerGone
	}
	a.mu.Lock()
	open, called, ok := a.asked.dismissLocked(id)
	var q Question
	said := false
	if ok {
		q, said = a.claimQuestionLocked(QuestionAsk, strconv.FormatUint(id, 10), false)
	}
	a.mu.Unlock()
	if !ok {
		return errAnswerGone
	}
	if !said {
		q = open.q
	}
	a.sayWithdrawn(q, askDismissedReason)
	// A CALL WAS PARKED ON IT: the closed wait hands it [askDismissed] and the
	// call takes its own presence row down, so there is nothing more to do.
	if called {
		return nil
	}
	if open.letGo != nil {
		// The lane's own way down takes the presence row; the words are already
		// claimed, so it says nothing a second time.
		open.letGo()
	}
	a.enqueueNextTurnNote("Your question \"" + strings.TrimSpace(q.Head) + "\": " + askDismissed)
	return nil
}
