package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// WHAT THE SURFACE DOES WHEN A PERSON'S OWN WORD LET GO OF THE REQUEST.
//
// The engine cuts a request that has put nothing in front of anybody when the
// person names another model, and asks the same step again on theirs
// (internal/session's steer.go, THE PERSON'S WORD WINS). Two things are owed on
// this side of that, and until #935 the engine said nothing at all so neither
// happened: the clock the page is counting up belongs to a request that no
// longer exists and has to start again — it is the very `waiting · 13m 37s` the
// person spoke to be rid of — and anything the dead attempt drew has to come
// off, because the transcript no longer holds a word of it.

// personCut is the event the engine sends for it: a MOVE, with `Next` naming
// where the step is going, which is what every surface reads to tell a hop from
// a retry (session.RetryNews).
func personCut() session.Event {
	return session.Event{
		Kind: session.EventRetrying,
		Text: "you chose another model — asking it instead",
		Retry: &session.RetryNews{
			Model:  "moonshot/kimi-k3",
			Next:   "qwen/qwen3.8-flash",
			Reason: "you chose another model",
		},
	}
}

// TestThePersonsWordRestartsTheClockItWasSpokenToEnd. The wait a person is
// watching is the whole reason they spoke; a surface still counting the dead
// request's seconds would show them a number that says their word did nothing.
func TestThePersonsWordRestartsTheClockItWasSpokenToEnd(t *testing.T) {
	a := newWaitingApp(t, 13*time.Minute)
	if text := plain(mustLine(t, a)); !strings.Contains(text, "13m") {
		t.Fatalf("the fixture did not start on a long wait: %q", text)
	}
	drive(t, a, streamEventMsg{gen: a.gen, ev: personCut()})
	text := plain(mustLine(t, a))
	if strings.Contains(text, "13m") {
		t.Fatalf("the page kept counting the request the person's word let go of: %q", text)
	}
}

// TestThePersonsWordNamesWhereTheStepIsGoing. It is a move they made, so the
// line names the model they chose rather than a count of somebody's patience.
func TestThePersonsWordNamesWhereTheStepIsGoing(t *testing.T) {
	a := newWaitingApp(t, 13*time.Minute)
	drive(t, a, streamEventMsg{gen: a.gen, ev: personCut()})
	text := plain(mustLine(t, a))
	if !strings.Contains(text, "qwen3.8-flash") {
		t.Fatalf("the line never named the model the person chose: %q", text)
	}
	if strings.Contains(text, " of ") {
		t.Fatalf("a move a person made drew a count of tries: %q", text)
	}
	// AND IT IS NOT CALLED A RETRY. They chose a model; nothing failed, and the
	// row this line is composed from says `moving to` without "trying again"
	// beside it (failurerow.go). The two readings of one moment may not disagree.
	if strings.Contains(text, "trying again") {
		t.Fatalf("a person's own choice was reported to them as a failure being "+
			"recovered from: %q", text)
	}
}

// TestThePersonsWordTakesTheDeadAttemptOffThePage is the withdrawal. A thought
// and a tool row that was forming belong to a response nobody will ever be sent;
// the engine throws them away at the top of the next attempt and the page has to
// do the same, or the replacement appends its own underneath them.
func TestThePersonsWordTakesTheDeadAttemptOffThePage(t *testing.T) {
	a := newWaitingApp(t, 13*time.Minute)
	drive(t, a,
		streamEventMsg{gen: a.gen, ev: session.Event{
			Kind: session.EventReasoning, Text: "weighing up two ways to do this",
		}},
		streamEventMsg{gen: a.gen, ev: session.Event{
			Kind: session.EventToolForming, Tool: "bash", CallID: "c1", Bytes: 12,
		}},
	)
	before := strings.Join(plainRows(a), "\n")
	if !strings.Contains(before, "thought for") || !strings.Contains(before, "bash") {
		t.Fatalf("the fixture never drew the rows it is about:\n%s", before)
	}
	drive(t, a, streamEventMsg{gen: a.gen, ev: personCut()})
	after := strings.Join(plainRows(a), "\n")
	// The THINKING row is deliberately not asserted here, and the reason is the
	// engine's: a person's word can no longer let go of a request that has drawn
	// a visible thought at all (internal/session's [reachedThePerson] — a thought
	// on the page is something they have read), so this door cannot reach that
	// state. What [feed.retry] does with thinking on the rescue paths that CAN
	// reach it is that lane's law and not this one's.
	if strings.Contains(after, "bash") {
		t.Fatalf("a tool row that was still forming when the request was let go of stayed "+
			"on the page; the replacement's own calls will append underneath it:\n%s", after)
	}
}

// TestTheSurfaceNeverInventsAPersonsMove is the honesty half, and it is the one
// that would catch a surface drawing this off a guess: only an engine event says
// a person's word moved anything.
func TestTheSurfaceNeverInventsAPersonsMove(t *testing.T) {
	a := newWaitingApp(t, 13*time.Minute)
	if text := plain(mustLine(t, a)); strings.Contains(text, "qwen3.8-flash") {
		t.Fatalf("a long first attempt was drawn as somebody's move: %q", text)
	}
}
