package remote

// tasklane.go is THE STANDING TASK LANE, ACROSS A CONNECTION.
//
// A task is a node in the conversation's graph, and its life — admitted,
// queued, running, done — is emitted on a subscription that outlives every
// turn (internal/session's [Agent.WatchTaskUpdates]). A local surface holds
// that subscription for the whole session and draws its rail from it.
//
// A HOSTED SURFACE HELD NOTHING, AND THAT WAS THE BUG. Version 6 gave the
// surface the task DOOR — it could commission work on the far machine — and no
// lane to watch it on. The engine's own emit puts every update on two lanes:
// the running turn's hub, and the standing subscription. Only the first crossed
// the wire, so the only task updates a hosted rail ever saw were the ones that
// happened to be raised inside a turn. `/task solo …` typed by hand raises none:
// the door is a call, not a turn, so the far worker ran to completion and the
// person who started it watched an empty column.
//
// The lane is per-SURFACE and not per-session, and that is the whole reason the
// roster arrives at all: [Agent.WatchTaskUpdates] replays the graph's rows onto
// each lane as it opens, so a window that attached an hour into the work learns
// every node rather than only the ones that moved after it arrived. One session
// with three windows on it opens three subscriptions and each gets its own
// replay, which is exactly what three local surfaces on one agent would do.
//
// IT IS OPENED ON DEMAND AND NEVER AT THE DOOR. The surface says
// [MethodTaskWatch] when it takes up a conversation, which is after its own
// half of the lane exists — so the roster replay cannot arrive before there is
// anything to draw it into. Saying it again replaces the subscription rather
// than adding a second, which is how `/new` over a connection gets a fresh
// roster for the conversation that replaced the old one.
//
// THE COST IS ONE SUBSCRIPTION PER WINDOW AND ONE FRAME PER EVENT. No call is
// made on a frame and none on a pointer (PERF.md's connection laws); the only
// call this file adds to a repaint is none.

import (
	"encoding/json"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── the engine half ─────────────────────────────────────────────────────────

// taskLaneAgent is the standing task subscription, asserted rather than
// required of [WrappedAgent] for the reason the task door is: an engine built
// around an agent that has no graph has no lane to offer, and a capability that
// cannot work is absent rather than broken. A surface watching such an engine
// draws the rail it has always drawn — nothing — and no sentence lies about it.
type taskLaneAgent interface {
	WatchTaskUpdates() (<-chan session.Event, func())
}

// taskFeed is one surface's subscription and the way out of it. It is a pointer
// held in two places — the session's map, and the pump reading it — so the pump
// can tell "still mine" from "replaced under me" by comparing the two.
type taskFeed struct {
	stop func()
	once sync.Once
}

// leave ends one feed. It is idempotent because every road out of a connection
// may reach it and a swap reaches it for a surface that is leaving anyway.
func (f *taskFeed) leave() {
	if f == nil {
		return
	}
	f.once.Do(func() {
		if f.stop != nil {
			f.stop()
		}
	})
}

// watchTasks opens this surface's lane, replacing whatever it held.
//
// THE OLD ONE IS LEFT BEFORE THE NEW ONE IS OPENED, because the two would
// otherwise both be pumping the same rows onto one screen — and after a swap the
// older lane belongs to a conversation this surface has already been told it is
// no longer in.
func (sess *Session) watchTasks(s *server) {
	sess.mu.Lock()
	agent, generation, closed := sess.agent, sess.generation, sess.closed
	watching := false
	if _, here := sess.surfaces[s]; here {
		watching = true
	}
	previous := sess.tasklanes[s]
	delete(sess.tasklanes, s)
	sess.mu.Unlock()

	previous.leave()
	if closed || !watching || agent == nil {
		return
	}
	door, ok := agent.(taskLaneAgent)
	if !ok {
		return
	}

	// The subscription is opened OUTSIDE the session lock: it walks the graph to
	// build the roster replay, and the graph has locks of its own.
	lane, stop := door.WatchTaskUpdates()
	feed := &taskFeed{stop: stop}

	sess.mu.Lock()
	_, here := sess.surfaces[s]
	// A conversation that was swapped or closed while the roster was being built
	// gets nothing: this lane is already about the wrong agent.
	if sess.closed || !here || sess.generation != generation {
		sess.mu.Unlock()
		feed.leave()
		return
	}
	if sess.tasklanes == nil {
		sess.tasklanes = map[*server]*taskFeed{}
	}
	sess.tasklanes[s] = feed
	sess.mu.Unlock()

	go sess.pumpTasks(s, generation, lane, feed)
}

// pumpTasks puts one surface's lane on its wire, one frame per event.
//
// IT KEEPS DRAINING WHATEVER HAPPENS, on [Session.pump]'s law: a lane left with
// a writer parked on it is a goroutine that never ends, so a stale generation
// and a dead pipe both stop the SENDING and neither stops the reading. The range
// ends when [taskFeed.leave] closes the subscription, which every road out of a
// connection reaches.
func (sess *Session) pumpTasks(s *server, generation uint64, lane <-chan session.Event, feed *taskFeed) {
	defer guard.Recover("remote/engine task lane")
	quiet := false
	for event := range lane {
		if quiet {
			continue
		}
		payload, err := json.Marshal(WireEvent(event))
		if err != nil {
			// An event that cannot be encoded is a field somebody added that
			// does not survive JSON. Dropping the one row keeps the lane alive,
			// which is better than ending it over a field nobody reads.
			continue
		}
		sess.mu.Lock()
		stale := sess.generation != generation || sess.tasklanes[s] != feed
		sess.mu.Unlock()
		if stale {
			quiet = true
			continue
		}
		if err := s.send(Frame{Kind: "task", Payload: payload}); err != nil {
			quiet = true
		}
	}
}

// dropTaskLane ends one surface's lane. It runs where a surface leaves the room.
func (sess *Session) dropTaskLane(s *server) {
	sess.mu.Lock()
	feed := sess.tasklanes[s]
	delete(sess.tasklanes, s)
	sess.mu.Unlock()
	feed.leave()
}

// retakeTaskLanes is what a session SWAP owes every rail in the room.
//
// The old subscriptions belong to the agent that is being closed, and a lane
// that kept delivering off it would draw the previous conversation's rows into
// the one that replaced it. So each is left and reopened on the new agent, which
// replays the new conversation's roster — usually nothing at all, which is the
// correct picture of a conversation that has just been started.
func (sess *Session) retakeTaskLanes() {
	sess.mu.Lock()
	surfaces := make([]*server, 0, len(sess.tasklanes))
	for surface := range sess.tasklanes {
		surfaces = append(surfaces, surface)
	}
	sess.mu.Unlock()
	for _, surface := range surfaces {
		sess.watchTasks(surface)
	}
}

// closeTaskLanes ends every lane in the room. It runs where the conversation
// does.
func (sess *Session) closeTaskLanes() {
	sess.mu.Lock()
	feeds := make([]*taskFeed, 0, len(sess.tasklanes))
	for surface, feed := range sess.tasklanes {
		feeds = append(feeds, feed)
		delete(sess.tasklanes, surface)
	}
	sess.mu.Unlock()
	for _, feed := range feeds {
		feed.leave()
	}
}

// pendingTasks is the far end's open proposals, or the honest refusal that this
// engine has no graph to have any.
func (sess *Session) pendingTasks() ([]uint64, bool) {
	agent := sess.current()
	door, ok := agent.(interface{ PendingTasks() []uint64 })
	if !ok {
		return nil, false
	}
	return door.PendingTasks(), true
}

// ── the surface half ────────────────────────────────────────────────────────

// taskFrame folds one "task" frame into whichever lane the surface is holding.
//
// A frame with no lane behind it is DROPPED and not queued. The only window in
// which that happens is between a surface leaving one lane and opening the next,
// and the next opens with the whole roster replayed — so nothing that is still
// true can be lost in it.
func (c *Client) taskFrame(payload json.RawMessage) {
	c.mu.Lock()
	lane := c.tasks
	c.mu.Unlock()
	if lane == nil {
		return
	}
	// Seq zero: the engine does not number this lane, because there is nothing
	// to resume. A redial reopens the subscription and the roster is replayed
	// whole, and the surface's own (id, state) de-dup is what makes a replayed
	// row and a live row the same row (internal/tui3's taskUpdate).
	lane.push(0, payload)
}

// buryTasks ends the lane when the connection does, so a surface pumping it
// learns the lane is over rather than waiting on a channel nobody will write.
func (c *Client) buryTasks() {
	c.mu.Lock()
	lane := c.tasks
	c.tasks = nil
	c.mu.Unlock()
	if lane != nil {
		lane.finish()
	}
}

// WatchTaskUpdates is this surface's standing subscription to the far
// conversation's task lane, and the way out of it.
//
// IT IS A FRESH CHANNEL EVERY TIME, replacing the one before it. The surface
// opens a lane per conversation it takes up (internal/tui3's [app.watchTasks]),
// and handing back the same channel twice would leave two pumps reading one
// channel — each event delivered to whichever won the race, which is a rail that
// silently drops half of what it is told.
func (a *Agent) WatchTaskUpdates() (<-chan session.Event, func()) {
	c := a.c
	lane := newStream()

	c.mu.Lock()
	previous, dead := c.tasks, c.dead
	c.tasks = lane
	c.mu.Unlock()
	if previous != nil {
		previous.finish()
	}
	if dead != nil {
		// A connection that is already gone answers with a lane that has already
		// ended, rather than one that will never speak: the surface reads a
		// closed lane as a lane it no longer has.
		lane.finish()
		return lane.events(), func() {}
	}

	// THE SUBSCRIPTION IS ASKED FOR OFF THE LOOP. The lane above already exists,
	// so nothing the engine sends can arrive before there is somewhere to put
	// it; and a call made on the surface's update loop would put an ssh round
	// trip between a keystroke and the frame that answers it.
	go func() {
		defer guard.Recover("remote/task watch")
		_, _ = c.call(nil, MethodTaskWatch, nil)
	}()

	var once sync.Once
	return lane.events(), func() {
		once.Do(func() {
			c.mu.Lock()
			if c.tasks == lane {
				c.tasks = nil
			}
			c.mu.Unlock()
			lane.finish()
		})
	}
}

// TaskUpdates is the same lane for a caller that has no way to leave it. It
// exists because [tui3.taskAgent] asks for this shape and the leavable one is
// asserted on top of it; every door in this build takes the leavable road.
func (a *Agent) TaskUpdates() <-chan session.Event {
	lane, _ := a.WatchTaskUpdates()
	return lane
}

// ResolveTask answers one proposal on the far machine.
//
// IT IS A CALL WITH NOTHING COMING BACK, exactly as the four other resolve doors
// on this wire are: what happens next is a turn resuming or a task starting, and
// both of those arrive as events. It is made off the update loop for the reason
// every write on this wire is — the keystroke that pressed `y` must not wait on
// an ssh round trip before the row on screen changes.
func (a *Agent) ResolveTask(id uint64, answer session.TaskAnswer) {
	args := TaskResolveArgs{
		ID: id, Approved: answer.Approved, Redirect: answer.Redirect, Model: answer.Model,
	}
	c := a.c
	go func() {
		defer guard.Recover("remote/task resolve")
		_, _ = c.call(nil, MethodTaskResolve, args)
	}()
}

// HoldTask stops one proposal clock on the far machine without making a key
// wait for the connection. The engine broadcasts the zero-deadline proposal
// back to every surface after it accepts the hold.
func (a *Agent) HoldTask(id uint64) {
	c := a.c
	go func() {
		defer guard.Recover("remote/task hold")
		_, _ = c.call(nil, MethodTaskHold, TaskHoldArgs{ID: id})
	}()
}

// TaskProposalsPending is the far engine's open proposals, AND WHETHER IT SAID.
//
// IT IS ASKED AND NOT REPLICATED because of when it is asked: a turn ending
// with a proposal card still on screen, which is neither a frame nor a pointer
// and happens once per turn at most (internal/tui3's [app.syncTaskAsk]).
//
// THE SECOND ANSWER IS THE WHOLE POINT OF THE PAIR. The surface reads a missing
// id as "nobody is asking this any more" and writes a verdict on the card, so a
// call that timed out or a link that died would retire a question that is still
// open on the far machine. False is "this engine did not say", and the surface
// leaves the card exactly as it found it.
func (a *Agent) TaskProposalsPending() ([]uint64, bool) {
	payload, err := a.c.call(nil, MethodTaskPending, nil)
	if err != nil {
		return nil, false
	}
	var pending TaskPending
	if err := json.Unmarshal(payload, &pending); err != nil {
		return nil, false
	}
	return pending.IDs, true
}

// PendingTasks is the same reading for [tui3.taskAgent], whose shape has no room
// for the second answer. IT IS DELIBERATELY THE PESSIMISTIC HALF: an engine that
// did not answer is reported as having nothing outstanding, which is why the
// surface asserts [Agent.TaskProposalsPending] first and only falls back here.
func (a *Agent) PendingTasks() []uint64 {
	ids, _ := a.TaskProposalsPending()
	return ids
}
