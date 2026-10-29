package remote

import (
	"reflect"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// A STREAM'S TEXT IS FOLDED BEFORE IT IS FRAMED, AND ONLY ITS TEXT. When a
// conversation's engine sets [Engine.DeltaWindow], each stream's pump sends a
// text or reasoning delta at once when none went out within the window, and
// otherwise holds it, folding the chunks that follow with identical metadata,
// until the window ends or the run reaches DeltaBytes. Anything that is not
// streamed text first sends the held text and then goes out itself, at once,
// and ends the window, so a tool call, a question, a stop or the turn's end is
// never late and the words after it start at once. The channel closing sends
// the held text before the stream's "closed" frame. A folded run is one event,
// one seq and one ring entry, so replay and resume see exactly what was sent.
//
// DefaultDeltaWindow spans two frames at 60 Hz because a one-frame window
// cannot fold the desktop's text deltas arriving 25 milliseconds apart.
const DefaultDeltaWindow = 33 * time.Millisecond

// DeltaBytes bounds a pending run so a fast producer cannot grow a frame for
// the whole window. It matches the app outbox's 4 KiB text budget.
const DeltaBytes = 4 << 10

func streamedText(kind session.EventKind) bool {
	return kind == session.EventTextDelta || kind == session.EventReasoning
}

func textContinues(prev, next session.Event) bool {
	if prev.Kind != next.Kind || !streamedText(prev.Kind) {
		return false
	}
	// Metadata belongs to the run, including fields added to Event later. Only
	// the text may differ without changing what the surface was told.
	prev.Text, next.Text = "", ""
	return reflect.DeepEqual(prev, next)
}

// deltaWindow belongs to one pump so independent streams never hold each
// other's text or share a timer.
type deltaWindow struct {
	window  time.Duration
	emit    func(session.Event)
	pending *session.Event
	until   time.Time
}

func (w *deltaWindow) send(ev session.Event) {
	w.emit(ev)
	w.until = time.Now().Add(w.window)
}

func (w *deltaWindow) flush() {
	if w.pending == nil {
		return
	}
	ev := *w.pending
	w.pending = nil
	w.send(ev)
}

func (w *deltaWindow) add(ev session.Event) {
	if !streamedText(ev.Kind) {
		w.flush()
		w.emit(ev)
		// Tool calls, questions and every other event end the burst, so the next
		// text reaches the person at once rather than inheriting the old window.
		w.until = time.Time{}
		return
	}
	if w.pending != nil && !time.Now().Before(w.until) {
		w.flush()
	}
	// A large producer chunk is already a full frame and must travel alone.
	if len(ev.Text) >= DeltaBytes {
		w.flush()
		w.send(ev)
		return
	}
	if w.pending != nil && textContinues(*w.pending, ev) {
		w.pending.Text += ev.Text
		if len(w.pending.Text) >= DeltaBytes {
			w.flush()
		}
		return
	}
	w.flush()
	if !time.Now().Before(w.until) {
		w.send(ev)
		return
	}
	w.pending = &ev
}

func pumpDeltaWindow(window time.Duration, events <-chan session.Event, emit func(session.Event)) {
	w := deltaWindow{window: window, emit: emit}
	var timer *time.Timer
	var armedUntil time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		var fire <-chan time.Time
		if w.pending != nil {
			// Reset only when the deadline changes. In particular, flushing a run for
			// changed metadata must move the timer too, rather than wake at its old end.
			if timer == nil {
				timer = time.NewTimer(time.Until(w.until))
			} else if !armedUntil.Equal(w.until) {
				timer.Reset(time.Until(w.until))
			}
			armedUntil = w.until
			fire = timer.C
		} else if !armedUntil.IsZero() {
			timer.Stop()
			armedUntil = time.Time{}
		}
		select {
		case ev, ok := <-events:
			if !ok {
				w.flush()
				return
			}
			w.add(ev)
		case <-fire:
			armedUntil = time.Time{}
			w.flush()
		}
	}
}
