package tui

import (
	"fmt"
	"strings"
)

// Residency is what a window knows about which process is running the brain.
//
// Only one aforge on a store answers, reconciles and executes; every other
// window is a surface over the same journal. That is a correct arrangement and
// an invisible one, which is the whole failure this exists to end: a second
// window that says nothing about being second is indistinguishable from an
// application that has died — no reply arrives, and there is no way to tell
// whether that is because someone else is answering or because nothing is.
//
// Commander.Residency is how a window learns all of this, and it is called from
// the poll goroutine on every cycle, quiet ones included: the resident can die
// while nothing at all changes in the store, so the journal watermark cannot
// speak for it. That makes it a probe and only a probe — anything that takes
// real time, promotion above all, belongs to a goroutine the implementation
// starts for itself.
//
// Its second return is how a window changes what it is. A visitor's commander
// holds no model clients and no head; the one that promotes holds both. Rather
// than making every capability mutable behind a lock, the surface hands back a
// replacement commander at the moment the role moves, and the window adopts it
// whole. A commander with nothing to say about the role answers the zero
// Residency and no successor, which is the ordinary single-window journey.
type Residency struct {
	// Visitor says this window is not the one running the brain. The zero
	// value is the ordinary single-window journey, which renders exactly as it
	// always has.
	Visitor bool
	// PID names the process that does hold the role, when it is known.
	PID int
	// Note replaces the whole label while something is in motion — a promotion
	// under way, a handover asked for. It is a short phrase, not a sentence.
	Note string
}

// label is the header's one quiet segment about all of this: what this window
// is, and then whatever is currently true about the role — which process holds
// it, or what is being done about that. A resident says nothing unless
// something odd is going on, because being the one that answers is the ordinary
// case and the ordinary case earns no ink.
func (r Residency) label() string {
	note := strings.TrimSpace(r.Note)
	if !r.Visitor {
		return note
	}
	if note == "" && r.PID > 0 {
		note = fmt.Sprintf("resident is pid %d", r.PID)
	}
	if note == "" {
		return "second window"
	}
	return "second window · " + note
}

// shortLabel is what survives a narrow frame: the fact, without the tail.
func (r Residency) shortLabel() string {
	if !r.Visitor {
		return strings.TrimSpace(r.Note)
	}
	return "second window"
}
