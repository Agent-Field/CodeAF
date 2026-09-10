package remote

// questionlane.go is the surface's half of version 14's QUESTIONS LANE.
//
// A question is one decision handed to a person with its evidence attached
// (docs/design/questions/DESIGN.md), and internal/session speaks every one of
// them on a subscription that outlives the turn — [session.Agent.WatchQuestions]
// — which replays whatever is still open the moment a surface attaches. Until
// this file that subscription had no frame on this wire, and the consequence was
// not a degraded drawing but SILENCE: internal/tui3 asserts the questions half
// of its agent as one interface (its questionAgent), a *Agent carried
// ResolveQuestion and neither of the other two doors, so the assertion failed,
// app.questionDoors() was false, and an `ask` on the road a plain `aforge`
// takes stopped the turn with no block, no chip and no row on any screen. The
// answer's own door had been here all along, carrying [session.Answer] whole,
// which is exactly why nobody noticed: the half a person presses worked and the
// half that puts the question in front of them did not exist.
//
// IT IS clientlanes.go's FOUR PIECES, and it has them for that file's reason —
// fold a frame into the lane, end the lane when the connection does, hand out a
// fresh subscription per conversation, and answer what the lane raises. The
// answering door is [Agent.ResolveQuestion] and was already written.
//
// ── WHY [Agent.OpenQuestions] IS A REPLICA AND NOT A CALL ────────────────────
//
// The engine already states this fact unasked. Every question that is raised,
// withdrawn or answered crosses as a frame, and everything still open is
// replayed onto each new subscription — so a surface holding the lane has been
// TOLD what is open, and a call asking the same thing would be the round trip
// version 4 turned this protocol around to stop making (wire.go: intent goes up,
// facts come down). The replica is moved on the reader goroutine, before the
// event reaches the surface, so the list and the block a person is looking at
// cannot disagree.
//
// AND IT IS NOT A SECOND STORE, on the terms internal/session's question.go
// states that law: the engine derives its answer from the lanes' own waits,
// this end holds only what the engine SAID, and a link that ends throws the
// whole of it away rather than keeping a row nothing can take back off
// ([Client.buryLanes]).

import (
	"encoding/json"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// questionsOpen is what one surface has been told is still open, oldest first —
// the order [session.Agent.OpenQuestions] answers in and the order the replay
// arrives in, kept rather than re-derived so a rebuilt list cannot re-order the
// rows somebody is reading.
type questionsOpen struct {
	mu   sync.Mutex
	open []session.Question
}

// take folds one of the lane's events in. It is the whole of the replica: a
// question arrives open, and it stops being open when it is withdrawn or
// answered — which are the only three kinds this lane carries.
//
// A QUESTION ALREADY HERE IS REPLACED WHERE IT STANDS. The replay after a redial
// hands back everything still open, and a surface that appended would then draw
// the same decision twice under two rows.
func (q *questionsOpen) take(event session.Event) {
	asked := event.Question
	if asked == nil {
		return
	}
	token := questionToken(*asked)
	q.mu.Lock()
	defer q.mu.Unlock()
	switch event.Kind {
	case session.EventQuestion:
		for at := range q.open {
			if questionToken(q.open[at]) == token {
				q.open[at] = *asked
				return
			}
		}
		q.open = append(q.open, *asked)
	case session.EventQuestionWithdrawn, session.EventQuestionAnswered:
		kept := q.open[:0]
		for _, standing := range q.open {
			if questionToken(standing) == token {
				continue
			}
			kept = append(kept, standing)
		}
		q.open = kept
	}
}

// list is what is open right now, as a copy: the caller is a surface that will
// hold it across frames, and the reader goroutine goes on moving this one.
func (q *questionsOpen) list() []session.Question {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.open) == 0 {
		return nil
	}
	open := make([]session.Question, len(q.open))
	copy(open, q.open)
	return open
}

// forgetAll drops the lot, which is what a new subscription and a dead
// connection both owe: everything true is about to be replayed, or nothing is
// ever coming.
func (q *questionsOpen) forgetAll() {
	q.mu.Lock()
	q.open = nil
	q.mu.Unlock()
}

// questionToken names one question the way every other place in this product
// names it — the lane it belongs to and the lane's own token, which is the Ref
// where that lane's is a string and the id otherwise ([session.Question.Token]).
func questionToken(q session.Question) string {
	return string(q.Kind) + ":" + q.Token()
}

// questionFrame takes one event off the questions lane: the replica first, the
// surface second. The order is the point — a surface woken by this event reads
// [Agent.OpenQuestions] on the very next frame, and a list a beat behind the
// event announcing it is the shape of bug clientlanes.go's title lane already
// states.
func (c *Client) questionFrame(payload json.RawMessage) {
	var wired EventWire
	if err := json.Unmarshal(payload, &wired); err == nil {
		c.asked.take(wired.Unwire())
	}
	c.laneFrame(laneQuestion, payload)
}

// WatchQuestions is this surface's subscription to every question the far
// conversation raises, withdraws or has answered, with the whole object on each
// event. It hands back the lane and the way out of it.
//
// WHAT IS OPEN IS FORGOTTEN FIRST. The engine replays it onto the subscription
// this call opens, so the replica is rebuilt from the engine's own account —
// and a surface taking up a SECOND conversation must not carry the first one's
// questions into it.
func (a *Agent) WatchQuestions() (<-chan session.Event, func()) {
	a.c.asked.forgetAll()
	return a.watchLane(laneQuestion, MethodQuestionWatch)
}

// OpenQuestions is every decision the far conversation is waiting on somebody
// for, oldest first, answered from what the lane has already said (the file
// header says why that is a reading and not a call).
//
// A SURFACE THAT NEVER OPENED THE LANE IS TOLD NOTHING RATHER THAN NOTHING
// TRUE: the list is empty, exactly as it is for a conversation with no
// questions, because a window that does not draw questions has not been sent
// any and has nothing to report about them.
func (a *Agent) OpenQuestions() []session.Question { return a.c.asked.list() }
