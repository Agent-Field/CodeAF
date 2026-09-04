package remote

// clientlanes.go is the surface's half of version 11's harness lane.
//
// IT IS THE SAME FOUR PIECES tasklane.go's surface half has — fold a frame into
// the lane, end the lane when the connection does, hand out a fresh subscription
// per conversation, and answer the question the lane raises — because
// internal/tui3 asserts a lane and its answer as ONE interface. A wire holding
// the events and not the answer leaves a card drawn and unanswerable, which is
// the fault the task rail was found in.
//
// THE METHOD NAMES ARE THE SESSION'S, so a hosted surface satisfies the same
// optional interfaces a local one does and internal/tui3 needs no line about
// which kind of agent it is holding.

import (
	"encoding/json"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// laneFrame folds one lane frame into whichever lane the surface holds. A frame
// with no lane behind it is DROPPED and not queued, on [Client.taskFrame]'s
// reasoning: the only window in which that happens is between a surface leaving
// one conversation's lane and opening the next one's, and nothing on these lanes
// is a row that stays true across that boundary.
func (c *Client) laneFrame(name laneName, payload json.RawMessage) {
	c.mu.Lock()
	lane := c.laneLocked(name)
	c.mu.Unlock()
	if lane == nil {
		return
	}
	// Seq zero: the engine does not number this lane. A redial reopens the
	// subscription, and internal/session replays whatever is still standing onto
	// it — which is what carries a card raised while the link was down, and why
	// an answered one does not come back.
	lane.push(0, payload)
}

// laneLocked is the stream one lane is delivering onto, called with c.mu held.
// One lane crosses today; the switch is where a second would join it.
func (c *Client) laneLocked(name laneName) *stream {
	if name == laneDesign {
		return c.designs
	}
	return nil
}

// setLaneLocked points one lane at a stream and answers the one it replaced.
func (c *Client) setLaneLocked(name laneName, lane *stream) *stream {
	previous := c.laneLocked(name)
	if name == laneDesign {
		c.designs = lane
	}
	return previous
}

// buryLanes ends the lane when the connection does, so a surface pumping it
// learns it is over rather than waiting on a channel nobody will write.
func (c *Client) buryLanes() {
	c.mu.Lock()
	lane := c.designs
	c.designs = nil
	c.mu.Unlock()
	if lane != nil {
		lane.finish()
	}
}

// watchLane is one subscription to one lane, and the way out of it.
//
// IT IS A FRESH CHANNEL EVERY TIME, replacing the one before it, for
// [Agent.WatchTaskUpdates]'s reason: a surface opens a lane per conversation it
// takes up, and handing back the same channel twice would leave two pumps
// reading one channel, each event going to whichever won the race.
func (a *Agent) watchLane(name laneName, method string) (<-chan session.Event, func()) {
	c := a.c
	lane := newStream()

	c.mu.Lock()
	previous := c.setLaneLocked(name, lane)
	dead := c.dead
	c.mu.Unlock()
	if previous != nil {
		previous.finish()
	}
	if dead != nil {
		// A connection that is already gone answers with a lane that has already
		// ended rather than one that will never speak.
		lane.finish()
		return lane.events(), func() {}
	}

	// THE SUBSCRIPTION IS ASKED FOR OFF THE LOOP. The lane above already exists,
	// so nothing the engine sends can arrive before there is somewhere to put
	// it, and a call made on the surface's update loop would put a round trip
	// between a keystroke and the frame that answers it.
	go func() {
		defer guard.Recover("remote/" + string(name) + " watch")
		_, _ = c.call(nil, method, nil)
	}()

	var once sync.Once
	return lane.events(), func() {
		once.Do(func() {
			c.mu.Lock()
			if c.laneLocked(name) == lane {
				c.setLaneLocked(name, nil)
			}
			c.mu.Unlock()
			lane.finish()
		})
	}
}

// WatchHarnessDesigns is this surface's subscription to the far conversation's
// harness lane: the design being written, the card that asks whether to keep
// it, and the intake card of a saved program the session is offering.
func (a *Agent) WatchHarnessDesigns() (<-chan session.Event, func()) {
	return a.watchLane(laneDesign, MethodDesignWatch)
}

// HarnessDesigns is the same lane for a caller with no way to leave it, which
// is the shape internal/tui3's designAgent asks for. Every door in this build
// takes the leavable road above.
func (a *Agent) HarnessDesigns() <-chan session.Event {
	lane, _ := a.WatchHarnessDesigns()
	return lane
}

// ResolveSubharness answers one intake card on the far machine.
//
// IT IS A CALL WITH NOTHING COMING BACK, as the other resolve doors on this
// wire are: what happens next is a program starting, and that arrives as events
// on the task rail. It is made off the update loop so the keystroke that pressed
// `y` does not wait on a round trip before the card comes down.
func (a *Agent) ResolveSubharness(id uint64, run bool, input json.RawMessage) {
	args := SubharnessResolveArgs{ID: id, Run: run, Input: input}
	c := a.c
	go func() {
		defer guard.Recover("remote/subharness resolve")
		_, _ = c.call(nil, MethodSubharnessResolve, args)
	}()
}
