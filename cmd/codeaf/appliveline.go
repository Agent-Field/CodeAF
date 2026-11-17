package main

import "time"

// ── an agent's live line, said as it moves ─────────────────────────────────
//
// An agent's `line` (appwire.Agent.Line) is the call its running turn is on:
// the same title the turn's tool row carries, so it is only ever something
// the agent actually did, never a guess at its intent. Lists and every
// agent.changed already carried it, but nothing said it again when the turn
// moved to its next call, so a window watching the agent read the line of
// whatever road last said the agent (often the turn's start: nothing) for the
// whole turn. A window that draws "what it is doing now" needs the line to
// move with the work.
//
// IT IS SAID AT MOST ONCE PER [appLineEvery] PER AGENT, AND THE NEWEST LINE IS
// ALWAYS SAID. A turn that makes ten quick calls says the first at once and
// the last when the interval has passed, never the eight between; a turn
// that then sits in one long call still shows that call. The turn's end says
// the agent again itself (with no line), so a flush due after it says nothing.

// appLineEvery is the most often one agent's live line is said while it moves.
const appLineEvery = time.Second

// appLineSay is one agent's live-line clock: when its line was last said, and
// the flush that will say a newer one, if one waits.
type appLineSay struct {
	at   time.Time
	stop func()
}

// stepMoved records the call a running turn is on as the agent's live line,
// and says the agent when the interval allows, else once it has passed.
func (b *appBackend) stepMoved(a *appAgent, live *appLive, title string) {
	say := false
	b.locked(func() {
		a.step = title
		if b.lineSays == nil {
			b.lineSays = map[*appAgent]*appLineSay{}
		}
		s := b.lineSays[a]
		if s == nil {
			s = &appLineSay{}
			b.lineSays[a] = s
		}
		if s.stop != nil {
			return // the flush already due says the newest line
		}
		now := b.now()
		if wait := appLineEvery - now.Sub(s.at); !s.at.IsZero() && wait > 0 {
			s.stop = b.after(wait, func() { b.lineFlush(a, live) })
			return
		}
		s.at, say = now, true
	})
	if say {
		b.sayAgent(a)
	}
}

// lineFlush says the line that waited for the interval, while the turn that
// moved it still runs.
func (b *appBackend) lineFlush(a *appAgent, live *appLive) {
	say := false
	b.locked(func() {
		s := b.lineSays[a]
		if s == nil || s.stop == nil {
			return
		}
		s.stop = nil
		if a.live != live || !live.running {
			return
		}
		s.at, say = b.now(), true
	})
	if say {
		b.sayAgent(a)
	}
}

// lineSettledLocked forgets an agent's live-line clock when its turn ends; the
// end says the agent itself. Callers hold b.mu.
func (b *appBackend) lineSettledLocked(a *appAgent) {
	if s := b.lineSays[a]; s != nil && s.stop != nil {
		s.stop()
	}
	delete(b.lineSays, a)
}
