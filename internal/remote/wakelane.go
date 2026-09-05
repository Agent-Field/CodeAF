package remote

// wakelane.go is THE TURNS THE CONVERSATION STARTS ON ITS OWN, ACROSS A
// CONNECTION.
//
// A wake is the turn a landed task, an exited job or a fired watch starts with
// nobody having typed anything (internal/session's [Agent.wakeLocked]). The
// session hands each one to its wake lanes BEFORE the turn's first event, and a
// local surface holds one for its whole life and draws the turn from its first
// delta (internal/tui3's watchWakes). A surface arriving mid-wake is handed the
// stream by its welcome ([Welcome.Live]), and a returning one by the replay.
//
// A HOSTED ENGINE HELD NOTHING, AND THAT WAS THE BUG — tasklane.go's, one lane
// over. The turn still ran: the hosted engine is a *session.Agent whose own
// wake declines nothing, so the report reached the model and the answer reached
// the journal, retries and all. But no stream was minted for the turn, no
// "turn" frame said it had begun, and no event crossed the wire, so a surface
// attached to that conversation sat reading idle while the engine spent
// minutes on a provider fault, and the answer that finally landed never reached
// the screen. It was in the transcript for whoever opened the conversation
// NEXT, and invisible to the person sitting in front of it — a task card that
// said done, and no word about it.
//
// THE FIX IS THE ADOPTION A SECOND WINDOW ALREADY GETS, AND NOTHING MORE. A
// wake turn is a turn this window did not start, which is the one shape the
// wire has drawn since version 4 ([Turn], [Client.Follow]): mint the stream so
// an arriving surface finds it in its welcome, tell the room so every surface
// binds the stream before its first event, and hand the events to
// [Session.pump], which numbers them, fans them out to everybody watching and
// closes them exactly as it does for a turn somebody submitted. No new frame
// kind, no new method, and a surface cannot tell a wake from a watched turn —
// which is the point.
//
// THE SUBSCRIPTION IS ONE PER CONVERSATION AND NOT ONE PER WINDOW, unlike the
// task lane's, because the fan-out is the session's own: one minted stream
// reaches every surface through [Session.emit], and a second subscription would
// mint a second stream for the same turn and draw it twice. It is opened at
// [NewSession] rather than on demand for the same reason the ring is opened at
// the mint: a conversation that wakes while NOBODY is attached still journals
// its answer, and the wake that is running when a surface arrives is the one
// fact the welcome must be able to name.

import (
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// wakeLaneAgent is the session's own-turn subscription, asserted rather than
// required of [WrappedAgent] for tasklane.go's reason: an engine built around a
// scripted agent has no turns of its own to offer, and a lane that cannot carry
// anything is absent rather than broken.
type wakeLaneAgent interface {
	WatchWakes() (<-chan (<-chan session.Event), func())
}

// wakeWatch is the conversation's one subscription and the way out of it. The
// once is for the same reason taskFeed's is: a swap, a close and the agent's
// own ending may all reach the same lane, and a stop that ran twice would close
// a channel twice.
type wakeWatch struct {
	stop func()
	once sync.Once
}

func (w *wakeWatch) leave() {
	if w == nil {
		return
	}
	w.once.Do(func() {
		if w.stop != nil {
			w.stop()
		}
	})
}

// watchOwnTurns opens this conversation's subscription to the turns it starts
// itself, and is a no-op when it already holds one or its agent has no wake
// lane to hold.
//
// THE LANE IS OPENED OUTSIDE THE SESSION LOCK, for [Session.watchTasks]'
// reason: taking it means taking the agent's own lock, and the two must never
// be held in one order here and the other order there. What the re-take under
// the lock then checks is that the conversation this lane is about is still the
// one being served — a swap or a close that raced the open leaves the lane
// closed and the conversation unwatched, and the swap's own retake is what
// opens the next one.
func (sess *Session) watchOwnTurns() {
	sess.mu.Lock()
	if sess.closed || sess.wakeStop != nil || sess.agent == nil {
		sess.mu.Unlock()
		return
	}
	agent, generation := sess.agent, sess.generation
	sess.mu.Unlock()

	door, ok := agent.(wakeLaneAgent)
	if !ok {
		return
	}

	lane, stop := door.WatchWakes()
	watch := &wakeWatch{stop: stop}

	sess.mu.Lock()
	// A conversation that was swapped or closed while the lane was being
	// opened gets nothing: this lane is already about the wrong agent.
	if sess.closed || sess.wakeStop != nil || sess.generation != generation {
		sess.mu.Unlock()
		watch.leave()
		return
	}
	sess.wakeStop = watch
	sess.pumps.Add(1)
	sess.mu.Unlock()
	go sess.pumpOwnTurns(lane, generation)
}

// pumpOwnTurns mints and pumps every turn the conversation starts on its own.
//
// THE ORDER IS [server.release]'s, with nobody to except: the room is told
// about the stream before the pump that fills it starts, so a surface binds the
// stream before its first event rather than learning about a turn from a frame
// it cannot place. The range ends when the lane does — a stop, a swap's retake,
// or the conversation closing — and a stream already handed over keeps its own
// pump, which goes quiet by generation exactly as a swapped turn's does.
//
// A WAKE THAT OUTLIVES ITS CONVERSATION IS DRAINED AND NEVER MINTED. The lane
// is buffered, so a stream can be sitting on it when the conversation is
// swapped or closed underneath: the generation this loop was opened under is
// the one its lane belongs to, and the question "is that still the
// conversation being served" is asked in the same hold of the lock that would
// mint — anything else names a stream in the new conversation for events the
// old one produced. The stream itself is still drained, because the channel
// has a session writing into it and an unread one parks that writer's pump
// forever ([Agent.wakeLocked]'s sink exists for exactly this).
func (sess *Session) pumpOwnTurns(lane <-chan (<-chan session.Event), generation uint64) {
	defer sess.pumps.Done()
	defer guard.Recover("remote/engine wake lane")
	for events := range lane {
		sess.mu.Lock()
		stale := sess.closed || sess.generation != generation
		var id uint64
		if !stale {
			// The generation minted under the lock is the one this loop was
			// opened with — that is what the check above proved — so it is
			// dropped rather than re-bound, and a later iteration compares
			// against the generation it captured, not one it overwrote.
			id, _ = sess.mintLocked()
		}
		sess.mu.Unlock()
		if stale {
			go func() {
				for range events { //nolint:revive // draining is the point
				}
			}()
			continue
		}
		// A wake opens on nothing: the note that woke it is drained and
		// journaled inside the turn, so there is no sentence to carry and a
		// surface draws the turn's work and its answer, exactly as a local
		// surface watching its own session does.
		sess.tellTurn(Turn{Stream: id}, nil)
		sess.pumps.Add(1)
		go sess.pump(id, generation, events)
	}
}

// retakeWakeLane is what a session SWAP owes this lane: the subscription
// belonged to the agent that is being closed, and a lane that kept running off
// it would draw the previous conversation's turns into the one that replaced
// it. The old one is left and the new one opened, beside the task lanes it
// retakes with.
func (sess *Session) retakeWakeLane() {
	sess.stopWakeLane()
	sess.watchOwnTurns()
}

// stopWakeLane ends this conversation's subscription, and is what a conversation
// ending owes it: the lane is left BEFORE the agent is closed, on the rails'
// own law, so nothing is still listening off a conversation that is being
// flushed and shut.
func (sess *Session) stopWakeLane() {
	sess.mu.Lock()
	watch := sess.wakeStop
	sess.wakeStop = nil
	sess.mu.Unlock()
	watch.leave()
}
