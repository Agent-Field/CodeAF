package remote

// ── ROAMING ─────────────────────────────────────────────────────────────────
//
// redial.go is the difference between a conversation that lives on a link and
// one that lives on a machine. The engine's session is on the far machine and
// the journal is on the far machine's disk; the pipe between here and there is
// the one part of the arrangement that a café's wifi, a VPN's flap or a closed
// lid can take away. So when it goes, the surface opens another one and says
// where it got to ([Hello.Resume]) — the conversation never noticed.
//
// IT IS THE READER GOROUTINE THAT REDIALS, and that is not a detail: a reader
// whose link has died has nothing to read, so there is no second goroutine to
// invent and no window in which two of them could both be handshaking. The law
// client.go states — one writer, one reader, one pump per stream — survives the
// whole of this file.
//
// THE ROAMING IS BOUNDED AND THE BOUND IS SAID OUT LOUD. It keeps trying for
// [RoamWindow] and no longer, the note on the screen names that span while it
// is trying, and when it gives up the sentence a person reads is the one this
// package has always said about a lost connection — `the connection to devbox
// is gone — run the same command to pick the conversation back up`. There is no
// second vocabulary for a link that ended, because there is no second thing
// that happened.
//
// AND IT PROMISES NOTHING THE FAR END DOES NOT. A redial gets the turn back
// only if the engine is a persistent one ([Welcome.Persistent]); against the
// other honest shape — `aforge engine` on a pipe — a redial gets a FRESH engine
// and the turn that was in flight is over. [Client.reconcile] says so on the
// turn itself rather than leaving a person to work it out from a reply that
// stopped mid-sentence, and it says so again if the engine came back with a
// different conversation open than the one this window left.

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/guard"
)

const (
	// RoamWindow is how long a dropped link is redialled before the connection
	// is declared gone. It is the honest bound: five minutes covers the things
	// that actually interrupt a connection while somebody is still sitting
	// there — a wifi handover, a tunnel, a VPN reconnect, a lid closed over a
	// walk to another desk — and stops well short of pretending a laptop shut
	// for the night is still attached to anything.
	//
	// GIVING UP COSTS NOTHING BUT THE WINDOW, which is why the number can be
	// this modest: the engine journals every turn as it happens, so the
	// conversation is on the far machine's disk either way and the same command
	// opens it again. Roaming buys the person not having to type it.
	RoamWindow = 5 * time.Minute

	// firstBackoff is the pause before the first retry and slowestBackoff the
	// ceiling it doubles up to. The first is short because most drops are a
	// blip and the pipe is there a second later; the ceiling exists because
	// every attempt spawns an ssh process on this machine and a hundred of them
	// against a machine that is switched off is a person's laptop working for
	// nothing.
	firstBackoff   = 1 * time.Second
	slowestBackoff = 15 * time.Second

	// resumeTail is how long a resumed stream may stay silent before it is
	// treated as a turn that ended while nobody was watching. See
	// [Client.watchTail] for the whole of why it exists.
	resumeTail = 3 * time.Second
)

// Dialer opens ONE fresh transport to the engine. It is the seam the redial
// loop turns on: the spawning of ssh belongs to the door (cmd/aforge, which
// owns processes and flags) and this package must be able to ask for it again
// without knowing what it is.
type Dialer func() (io.ReadWriteCloser, error)

// Roaming is the policy a client redials by.
type Roaming struct {
	// Dial opens the next link. Required.
	Dial Dialer
	// Window is how long to keep trying, and zero is [RoamWindow].
	Window time.Duration
}

func (r *Roaming) window() time.Duration {
	if r == nil || r.Window <= 0 {
		return RoamWindow
	}
	return r.Window
}

// Roam dials the engine and returns a client that redials for itself.
//
// It is [Dial] with a dialer instead of a pipe, and the first attempt is NOT
// roamed: a machine that cannot be reached at all, an aforge that is not
// installed there, a version that does not match — those are things the door
// has to be able to say plainly on a terminal that is still the person's (the
// prompt law in cmd/aforge's chatv3_host.go), and quietly retrying them for
// five minutes would replace an answerable sentence with a hang.
func Roam(host string, hello Hello, roam Roaming) (*Client, error) {
	if roam.Dial == nil {
		return nil, errors.New("remote: roaming needs a dialer")
	}
	conn, err := roam.Dial()
	if err != nil {
		return nil, err
	}
	c := newClient(host, hello)
	c.roam = &roam
	if _, err := c.attach(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	go c.read()
	return c, nil
}

// ── the sentences ───────────────────────────────────────────────────────────

// roamingNote is what the screen says WHILE the redialling happens: quiet, true,
// and bounded. It names the machine for [Client.gone]'s reason — a person with
// three windows open needs to know which one lost its link — and it names the
// span because a person watching a status line has every right to know whether
// this is going to resolve itself or is going to end.
func (c *Client) roamingNote() string {
	return fmt.Sprintf("reconnecting to %s — trying for up to %s", c.where(), roamSpan(c.roam.window()))
}

// roamingRefusal is what a CALL made during the gap says. It is the same first
// half as the note, because it is the same fact, and it ends with the one thing
// that is worth doing about it.
func (c *Client) roamingRefusal() string {
	return fmt.Sprintf("reconnecting to %s — try that again in a moment", c.where())
}

// roamSpan writes a duration the way a sentence wants it. It is derived from
// the window rather than spelled beside it, because a number that appears in
// two places drifts.
func roamSpan(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d < time.Second:
		return "a moment"
	case d < time.Minute:
		return plural(int(d/time.Second), "second")
	case d%time.Minute == 0:
		return plural(int(d/time.Minute), "minute")
	default:
		return fmt.Sprintf("%s %s", plural(int(d/time.Minute), "minute"), plural(int((d%time.Minute)/time.Second), "second"))
	}
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// ── the loop ────────────────────────────────────────────────────────────────

// lost is what the reader does when the link under it stops, and it answers
// whether reading may carry on. True means a live pipe is in place and the
// stream state has been reconciled with what the engine says; false means this
// client is over and [Client.bury] has already said why.
func (c *Client) lost(cause error) bool {
	c.closeObservers()
	c.mu.Lock()
	roam, closing, dead := c.roam, c.closing, c.dead
	left := c.welcome.SessionFile
	c.mu.Unlock()
	if roam == nil || closing || dead != nil || c.stopped() {
		c.bury(cause)
		return false
	}

	// THE CALLS IN FLIGHT FAIL AND THE STREAMS DO NOT. A call is a round trip
	// whose answer was on the pipe that just died, and its caller is waiting on
	// a deadline; a stream is a TURN, which is running on the far machine and
	// has not stopped because this laptop's wifi did. That difference is the
	// whole reason the surface can be redialled under a person without the
	// conversation noticing.
	c.mu.Lock()
	c.reconnecting = true
	waiting := c.calls
	c.calls = map[uint64]chan result{}
	c.mu.Unlock()
	refusal := errors.New(c.roamingRefusal())
	for _, answer := range waiting {
		answer <- result{err: refusal}
	}

	welcome, err := c.redial(roam)
	c.mu.Lock()
	c.reconnecting = false
	c.mu.Unlock()
	if err != nil {
		// A refusal the far end MADE is worth repeating; a redial that simply
		// ran out of window is not, and the sentence for it is the one this
		// package has always said about the link that died in the first place.
		var spoken spokenError
		if errors.As(err, &spoken) {
			c.bury(err)
		} else {
			c.bury(cause)
		}
		return false
	}
	c.reconcile(left, welcome)
	c.retakeTitle(left, welcome)
	return true
}

// redial is the backoff loop: a pause, an attempt, a longer pause, until the
// engine answers or the window runs out.
func (c *Client) redial(roam *Roaming) (Welcome, error) {
	deadline := time.Now().Add(roam.window())
	wait := firstBackoff
	for {
		if c.stopped() {
			return Welcome{}, errors.New("this connection is closed")
		}
		if time.Now().After(deadline) {
			return Welcome{}, errors.New("the redialling ran out of time")
		}
		timer := time.NewTimer(wait)
		select {
		case <-c.stop:
			timer.Stop()
			return Welcome{}, errors.New("this connection is closed")
		case <-timer.C:
		}

		conn, err := roam.Dial()
		if err == nil {
			welcome, err := c.attach(conn)
			if err == nil {
				return welcome, nil
			}
			_ = conn.Close()
			// A REFUSAL IS NOT A BLIP. A far end that answered and said no —
			// another protocol version, a fatal frame — will say the same no in
			// fifteen seconds, so the loop stops rather than spending the whole
			// window rediscovering it.
			var spoken spokenError
			if errors.As(err, &spoken) {
				return Welcome{}, err
			}
		}
		wait *= 2
		if wait > slowestBackoff {
			wait = slowestBackoff
		}
	}
}

// ── what the engine came back as ────────────────────────────────────────────

// reconcile is the honesty crux of roaming: the link is back, and now the
// surface has to be told which of three things that means.
//
//   - The engine kept the session AND the turn. Nothing is said, because
//     nothing happened that a person needs to know about: the gap's events
//     arrive as ordinary event frames and the duplicates client.go drops are
//     invisible by construction.
//   - The engine came back with a DIFFERENT conversation open than the one this
//     window left. The transcript on screen belongs to the old one, so the swap
//     is announced rather than made under the person, and the streams of the
//     conversation we left are ended.
//   - The far end is not persistent ([Welcome.Persistent] false), so a redial
//     got a FRESH engine. The conversation is still on that machine's disk and
//     the journal has everything that reached it, but the turn that was in
//     flight is over and nothing is going to finish it.
//
// A STREAM IS NEVER LEFT WAITING ON A TURN NOBODY IS RUNNING. Every road here
// either keeps a stream because the engine still holds it, ends it with the
// sentence that says why, or hands it to [Client.watchTail].
func (c *Client) reconcile(left string, welcome Welcome) {
	switched := strings.TrimSpace(left) != "" && strings.TrimSpace(welcome.SessionFile) != strings.TrimSpace(left)
	switch {
	case switched:
		c.note(fmt.Sprintf("%s came back with a different conversation open than the one this window left — what is above is the old one, and anything from here on belongs to the new one", c.where()))
		c.forgetStreams(fmt.Sprintf("%s opened a different conversation, so this turn is not coming back", c.where()))

	case !welcome.Persistent:
		// The sentence is only true if there was a turn to lose. An idle
		// session redialled onto a fresh engine lost nothing at all, and a
		// notice about it would be the screen inventing a loss.
		if c.forgetStreams(fmt.Sprintf("the connection came back, but %s does not keep a turn running while nothing is attached — that answer stopped when the link dropped, and asking again is the way back to it", c.where())) {
			c.note(fmt.Sprintf("%s does not keep a turn running while nothing is attached, so the turn that was in flight did not survive the drop", c.where()))
		}

	default:
		for id, s := range c.openStreams() {
			if id == welcome.Live {
				continue
			}
			c.watchTail(id, s)
		}
	}
}

// openStreams is every stream the surface still has a turn open on.
func (c *Client) openStreams() map[uint64]*stream {
	c.mu.Lock()
	defer c.mu.Unlock()
	open := map[uint64]*stream{}
	for id, s := range c.streams {
		if _, alive := s.cursor(); alive {
			open[id] = s
		}
	}
	return open
}

// forgetStreams ends every open turn with one sentence and empties the routing
// map, and says whether there was anything to end.
//
// THE MAP IS EMPTIED AND NOT JUST THE OPEN ONES, because the engine on the
// other end of the new pipe is a different engine: it mints stream ids from one
// again, and a get-or-create that found this connection's old stream 1 would
// hand a live turn's events to a channel that has already closed.
func (c *Client) forgetStreams(sentence string) bool {
	c.mu.Lock()
	streams := c.streams
	c.streams = map[uint64]*stream{}
	c.mu.Unlock()
	ended := false
	for _, s := range streams {
		if _, alive := s.cursor(); !alive {
			continue
		}
		ended = true
		// The turn is told WHY on the stream before it closes, which is where
		// the surface draws a turn's failure — the same road client.go's bury
		// takes, for the same reason.
		s.fail(errors.New(sentence))
		s.finish()
	}
	return ended
}

// watchTail bounds a resumed stream the engine did not name as live.
//
// A REATTACHED SURFACE MUST NOT SPIN ON A TURN NOBODY IS RUNNING. The engine
// answers a resume cursor for a stream it still holds by replaying the gap and
// carrying on; a stream it no longer holds is answered with NOTHING at all
// (wire.go says so, and it is the right answer — the transcript is the
// authority on a turn that ended). Those two look identical for the first
// instant, so the stream is given [resumeTail] of quiet: anything at all
// arriving on it means the engine is still speaking about that turn and it is
// left alone, and total silence means the turn ended while nobody was watching
// and the channel is closed so the surface stops waiting for it.
//
// IT CLOSES WITHOUT A SENTENCE. Nothing failed — the turn finished, on the far
// machine, and its whole text is in the journal — so an error event here would
// be the screen reporting a fault that did not happen.
//
// AND IT WAITS OUT THE REPLAY RATHER THAN RACING IT. An engine that still holds
// a finished turn replays its tail the instant the welcome is out, so the quiet
// is measured from the LAST thing said and not from the moment of reattaching:
// anything arriving buys another [resumeTail], and the stream closes when the
// far end has stopped speaking about it. Only a stream [Welcome.Live] does not
// name is watched this way, which is what makes the rule safe — the turn that is
// actually running is the one the engine names, and a running turn is allowed to
// be quiet for as long as the tool it is waiting on takes.
func (c *Client) watchTail(id uint64, s *stream) {
	guard.Go("remote/resume-tail", func() {
		before := s.delivered()
		for {
			select {
			case <-time.After(resumeTail):
			case <-c.stop:
				return
			}
			if _, alive := s.cursor(); !alive {
				return
			}
			if said := s.delivered(); said != before {
				before = said
				continue
			}
			c.mu.Lock()
			delete(c.streams, id)
			c.mu.Unlock()
			s.finish()
			return
		}
	})
}
