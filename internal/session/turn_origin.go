package session

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/teams"
)

// turnOrigin holds the run, the person's message that opened it for team
// traffic, and the message it answers for client tools. It is built once, at
// the moment a turn starts, and never changed.
//
// IT IS CARRIED ON THE TURN'S CONTEXT AND NEVER READ FROM THE AGENT. A line is
// stamped from the context of the call that produced it, or from a copy taken
// when a write was queued, so a turn that was abandoned and replaced, a late
// callback and a steer that landed mid-turn can never charge their line to the
// turn that happens to be current when the write finally runs. The same law as
// [withRunIdentity], with the message identities beside it.
type turnOrigin struct {
	// run is the turn's run identity, the id its journal records as `usage_run`.
	run string
	// turn is the durable id of the person's message that opened the turn, ""
	// when no person message opened it or none can be named exactly.
	turn string
	// answers is the durable id of the message this turn answers. Resuming an
	// answer can name it without changing who opened the run for crew traffic.
	answers string
}

type turnOriginKey struct{}

// turnOriginLocked captures the message being answered before the turn runs.
// A new person's opening also records its run on that line, so a later carry-on
// can prove which message opened the interrupted run without guessing by recency.
func (a *Agent) turnOriginLocked(user *userMessage, run string) turnOrigin {
	origin := turnOrigin{run: run, turn: openingMessage(*user), answers: openingMessage(*user)}
	if personMessageID(*user) != "" {
		mark := *user.person
		mark.RunID = run
		user.person = &mark
	} else if user.resumed {
		origin.answers = user.sourceMessage
	} else if user.wake && !user.answersPerson && len(a.steering) == 1 && len(a.ambient) == 0 {
		// A continuation note must be the whole opening. Mixed or coalesced
		// news names no single message, even if one note knows an earlier id.
		origin.answers = a.steering[0].sourceMessage
	}
	return origin
}

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
