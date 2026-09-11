package tui3

import (
	"sync"

	tea "charm.land/bubbletea/v2"
)

// ── THE DOORBELL: THE ONE DOOR INTO THE LOOP FROM ANOTHER GOROUTINE ─────────────
//
// THE DEFECT THIS FIXES, measured to the microsecond. A person pressed enter on
// a question and the window froze for ten seconds, then said "the engine did not
// answer in time" about an answer the engine had already applied. The engine
// answers a question in about a millisecond, and the ten seconds were a
// DEADLOCK that only the wire's deadline broke:
//
//  1. The answer woke the model's turn, and the turn said so — a "phase" frame
//     (connecting, thinking) that the engine writes before its reply to the
//     answer, because the turn's goroutine gets there first.
//  2. The client's reader goroutine took that frame and handed it to the phase
//     reader this package registered, which called Bubble Tea's Program.Send.
//  3. Program.Send puts a message on an UNBUFFERED channel whose only reader is
//     the update loop. The update loop was busy — it was the thing waiting for
//     the reply to the answer.
//  4. So the reader waited for the loop, the loop waited for the reply, and the
//     reply sat on the pipe behind the reader until the ten-second deadline.
//
// internal/session's news seam has always said its reader must never block
// (phasenews.go's postPhaseNews). This package's reader broke that contract
// whenever the loop was busy, and a busy loop is exactly when news arrives.
//
// SO NOTHING OUTSIDE THE LOOP EVER WAITS FOR THE LOOP. A goroutine that has
// something for the surface puts it where the surface reads it — a desk, a
// replica — and then RINGS: it drops one token into a slot of one and returns,
// whatever the loop is doing. The loop keeps exactly one command parked on that
// slot ([doorbell.waitRing]) and parks it again in the same Update that takes the
// message, so a ring is never lost: after the last ring there is either a token
// in the slot, which the parked command will take, or the command took it after
// the ring, and the message it returns is still on its way. A ring that finds a
// token already in the slot is the same request already made, and is dropped —
// that is the coalescing, and it is why a burst of a hundred phases costs one
// frame rather than a hundred.
//
// IT IS THE ONLY WAY IN THAT CARRIES NOTHING, which is the precise claim and
// the one worth making. Program.Send appears nowhere in this package
// (doorbell_test.go's [TestNothingInTheSurfaceCallsProgramSend] holds it),
// because every caller of it is a goroutine that can be made to wait on a busy
// loop, and a goroutine that can be made to wait is a deadlock waiting for its
// partner.
//
// THE OTHER NON-BLOCKING WAKE IS [behindWatch.stir] (keeper.go), and it is
// deliberately not this. It says "read THIS conversation's agent", so it carries
// a key — and a wake that carries content cannot coalesce the way this one does,
// because two rings with different keys are two messages. It keeps its own
// buffered lane and its own per-key dedup arm instead. What the two share is the
// only law that matters here and neither may ever break: a wake never waits for
// the loop. It has a `default:` on its send for exactly the reason this door has
// a one-slot channel.

// doorbell is one door into the update loop: one message, one slot, never waited on.
//
// It carries the message it delivers rather than being told one on each ring,
// because a ring that carried content would not be coalescable — two rings
// with different content are two messages — and every caller here has already
// put its content somewhere the loop reads it.
type doorbell struct {
	msg  tea.Msg
	rung chan struct{}
	// done ends the parked command when the program does, so a surface that has
	// stopped leaves no goroutine sitting on a slot nobody will ring.
	done chan struct{}
	once sync.Once
}

// newDoorbell is a door that delivers msg.
func newDoorbell(msg tea.Msg) *doorbell {
	return &doorbell{msg: msg, rung: make(chan struct{}, 1), done: make(chan struct{})}
}

// ring asks the loop for msg and returns at once. It is safe from any goroutine,
// on a nil door, and on a door whose program has stopped.
func (w *doorbell) ring() {
	if w == nil {
		return
	}
	select {
	case w.rung <- struct{}{}:
	default:
		// A token is already in the slot: the loop is owed this message and
		// will be handed it. A second token would say the same thing twice.
	}
}

// waitRing is the command the loop keeps parked on the door. The Update that
// takes the message it returns MUST return waitRing again, which is what keeps
// exactly one parked at all times.
func (w *doorbell) waitRing() tea.Cmd {
	if w == nil {
		return nil
	}
	return func() tea.Msg {
		select {
		case <-w.rung:
			return w.msg
		case <-w.done:
			return nil
		}
	}
}

// close ends the door. A ring after it is harmless and delivers nothing.
func (w *doorbell) close() {
	if w == nil {
		return
	}
	w.once.Do(func() { close(w.done) })
}
