package session

import "time"

// jobWakeDelay bounds how long an idle session gathers unowed process endings.
// THE FIRST ENDING OWNS THE WINDOW: later arrivals never push its deadline out.
const jobWakeDelay = 2 * time.Second

// jobWakeWindow is the identity of one pending wake, not a batch of notes.
// Notes keep their ordinary queue order and their individual surface events.
type jobWakeWindow struct {
	stop func()
}

// deferJobWakeLocked schedules only the idle wake, with a.mu held. Owed news
// still starts its turn immediately through the ordinary notification lane.
func (a *Agent) deferJobWakeLocked() {
	a.jobWakePending = true
	if a.jobWake != nil || a.closed || a.workStopped || !a.opened || (a.config.InTask && !a.config.roomThread) {
		return
	}
	after := a.jobWakeAfter
	if after == nil {
		after = func(delay time.Duration, fire func()) func() {
			timer := time.AfterFunc(delay, fire)
			return func() { timer.Stop() }
		}
	}
	window := &jobWakeWindow{}
	a.jobWake = window
	window.stop = after(jobWakeDelay, func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		// A TURN OR CLOSE MAY HAVE WON THE LOCK FIRST. Even a callback already
		// dispatched by the clock becomes inert once its window is withdrawn.
		if a.jobWake != window {
			return
		}
		a.jobWake = nil
		a.wakeLocked()
	})
}

// stopJobWakeLocked withdraws only the clock. Its notes remain pending until
// the queue records them, even when the clock fired while a wake was refused.
func (a *Agent) stopJobWakeLocked() {
	if window := a.jobWake; window != nil {
		a.jobWake = nil
		window.stop()
	}
}
