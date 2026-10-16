package session

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/teams"
)

// turnOrigin is the two facts that say which turn caused a line of team
// traffic: the run that is writing it and the person's message that OPENED that
// run. It is built once, at the moment a turn starts, and never changed.
//
// IT IS CARRIED ON THE TURN'S CONTEXT AND NEVER READ FROM THE AGENT. A line is
// stamped from the context of the call that produced it, or from a copy taken
// when a write was queued, so a turn that was abandoned and replaced, a late
// callback and a steer that landed mid-turn can never charge their line to the
// turn that happens to be current when the write finally runs. The same law as
// [withRunIdentity], one value wider.
type turnOrigin struct {
	// run is the turn's run identity, the id its journal records as `usage_run`.
	run string
	// turn is the durable id of the person's message that opened the turn, ""
	// when no person message opened it or none can be named exactly.
	turn string
}

type turnOriginKey struct{}

// withTurnOrigin hangs origin on ctx for the life of the turn.
func withTurnOrigin(ctx context.Context, origin turnOrigin) context.Context {
	return context.WithValue(ctx, turnOriginKey{}, origin)
}

// turnOriginFrom is the origin ctx carries, and the zero value when it carries
// none (a call made outside any turn), which stamps nothing.
func turnOriginFrom(ctx context.Context) turnOrigin {
	origin, _ := ctx.Value(turnOriginKey{}).(turnOrigin)
	return origin
}

// provenance is the origin as a traffic line carries it. runBy is the
// conversation key the line's team knows this conversation by, which only the
// writer of that line knows. A turn with no run stamps nothing at all: a
// message id with no run behind it would be a claim nothing recorded.
func (o turnOrigin) provenance(runBy string) teams.Provenance {
	if o.run == "" {
		return teams.Provenance{}
	}
	return teams.Provenance{Turn: o.turn, Run: o.run, RunBy: runBy}
}

// personMessageID is the durable id of the person's message that user is, ""
// for anything that is not one: the session's own notes, a woken turn's empty
// opening, and a line written before message ids existed. Only the id the
// message was RECORDED with counts, so the caller passes the message after
// [ensureUserIdentity], which is the copy that reaches the journal.
func personMessageID(user userMessage) string {
	if user.authored || user.wake || user.resumed || user.person == nil || user.person.Composed == nil {
		return ""
	}
	return user.person.Composed.MessageID
}

// openingMessage is the person's message a turn is stamped as opened by: the
// message it was started on, or, for the empty turn started to answer a line
// that landed in the last seconds of the one before, the one line that drain
// landed ([userMessage.answersMessage]). It is "" whenever neither is exact.
func openingMessage(user userMessage) string {
	if id := personMessageID(user); id != "" {
		return id
	}
	if user.wake && user.answersPerson {
		return user.answersMessage
	}
	return ""
}
