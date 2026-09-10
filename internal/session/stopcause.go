package session

import (
	"context"
	"errors"
)

// ── EVERY CANCEL SAYS WHICH DOOR IT CAME THROUGH ────────────────────────────
//
// A TURN THAT ENDS WITH NOTHING SAID IS THE WORST THING THIS PACKAGE CAN DO,
// and until this file it could do it silently. `context.CancelFunc` carries no
// account of itself: every door here — the person's stop key, another window
// taking the conversation over, a tab closing, a hosted session being retired,
// the session quitting — cancelled the turn's context in exactly the same way,
// and what came back out the other end was the string `context canceled`. The
// model-call row said that. The loop read `ctx.Err() != nil`, wrote no journal
// line and sent no event. The surface, which had usually already looked away by
// then, drew nothing.
//
// The measured case was a chat turn on 2026-09-09 that thought for 108 seconds
// at full rate and then simply stopped: no answer, no error, no note, an idle
// status line, and nobody — the person, the log, or anyone reading the
// transcript afterwards — able to say what had ended it.
//
// SO EVERY DOOR NAMES ITSELF. The turn's context is made with
// [context.WithCancelCause] and each door cancels with one of the causes below;
// [context.Cause] reads it back at the boundary, the journal writes it down,
// the model-call row carries it instead of `context canceled`, and the surface
// is given ONE sentence — the person's own words, never the machine's.
//
// AND THE CAUSE DECIDES WHAT HAPPENS NEXT. A person who pressed stop is told
// their turn stopped and nothing else; that is what they asked for. Everything
// else is machinery, and machinery that takes a reply away from somebody owes
// them an account of it.

// StopDoor is what ended a turn, in machine words. It is a small closed set on
// purpose: a door that is not on it is a door nobody can autopsy.
type StopDoor string

const (
	// StopByPerson is the stop key and nothing else: the person asked.
	StopByPerson StopDoor = "person stopped"
	// StopByTakeover is another window taking this conversation over
	// (takeover.go). The turn dies where it stands, mid-reply or not.
	StopByTakeover StopDoor = "taken over"
	// StopByLeaving is the conversation going away under the turn — a tab
	// closing, a switch to another conversation, `/new`, filing an exchange.
	StopByLeaving StopDoor = "conversation left"
	// StopByClosing is the whole session shutting down.
	StopByClosing StopDoor = "session closed"
	// StopByAbandoned is [Agent.Abandon]: a stop that was not let go of in
	// time, so the session moved on from the turn (abandon.go).
	StopByAbandoned StopDoor = "abandoned"
	// StopByWorkStopped is [Agent.StopWork]: everything this conversation had
	// running was stopped at once (stopwork.go).
	StopByWorkStopped StopDoor = "work stopped"
	// StopByRetired is a hosted session let go of for want of anybody attached
	// to it (internal/remote).
	StopByRetired StopDoor = "session retired"
)

// turnStop is one door's cause. It is a type rather than a bare
// [errors.New] per door so that a reader can ask which door it was without
// comparing sentences.
type turnStop struct{ door StopDoor }

// Error is the machine's account, and it is what the model-call row carries in
// place of `context canceled`.
func (s *turnStop) Error() string { return "turn ended: " + string(s.door) }

// Unwrap keeps every `errors.Is(err, context.Canceled)` in the tree true. A
// cause is a cancellation with an account of itself attached, not a different
// kind of ending, and a caller that only wants to know whether the turn was
// cancelled must not have to learn this package's vocabulary to find out.
func (s *turnStop) Unwrap() error { return context.Canceled }

// stopFor is the cause for one door. Every door goes through it so that a cause
// is never built at a call site and therefore never built two ways.
func stopFor(door StopDoor) error { return &turnStop{door: door} }

// StoppedBy reads back which door ended a turn. It answers false for an error
// that is not one of this package's causes — including a plain
// [context.Canceled], which is what a caller's OWN context dying looks like and
// is not a door of ours.
func StoppedBy(err error) (StopDoor, bool) {
	var stop *turnStop
	if errors.As(err, &stop) && stop != nil {
		return stop.door, true
	}
	return "", false
}

// stopCause is the door a context was cancelled through, and whether anybody
// named one. A cancelled context with no cause of ours is the caller's own —
// a headless run's deadline, a parent shutting down — and it reads as
// [StopByClosing], which is the conservative answer: the session is going away
// and nothing is going to be re-asked.
func stopCause(ctx context.Context) (StopDoor, bool) {
	if ctx == nil || ctx.Err() == nil {
		return "", false
	}
	if door, ok := StoppedBy(context.Cause(ctx)); ok {
		return door, true
	}
	return StopByClosing, true
}

// ── WHAT A PERSON IS TOLD ───────────────────────────────────────────────────
//
// ONE SENTENCE, AND ONLY WHERE THE ANSWER NEVER ARRIVED. A turn that was
// answered says nothing; a turn the PERSON stopped says nothing either, because
// the surface already drew their stop and telling somebody what they just did
// is noise. Everything else took a reply away from somebody who was waiting for
// it, and this is the sentence that says so.
//
// THE WORDS ARE THE PERSON'S. No door names, no causes, no contexts — those are
// the machine's account and they live on the row and in the journal.

// stopSentence is what a person reads when a turn ended with no answer. It is
// empty for the one door that needs no sentence: their own stop.
func stopSentence(door StopDoor) string {
	switch door {
	case StopByPerson:
		return ""
	case StopByTakeover:
		return "this conversation was opened in another window, so the reply stopped here — ask again to pick it up"
	case StopByLeaving:
		return "the reply stopped when this conversation was left — ask again to pick it up"
	case StopByAbandoned:
		return "the reply was let go of after the stop took too long"
	case StopByWorkStopped:
		return "everything running here was stopped, the reply with it"
	case StopByRetired:
		return "nobody was left watching this conversation, so the reply stopped — ask again to pick it up"
	default:
		return "the reply stopped before it arrived — ask again to pick it up"
	}
}
