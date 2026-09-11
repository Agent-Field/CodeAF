package remote

// standinglane.go carries the HARNESS LANE across a connection: the design
// card, the notes around it, and the subharness intake card that shares the
// subscription.
//
// WHY IT IS HERE. Those cards are raised on a subscription the session keeps for
// the whole conversation (internal/session's emitHarness) and never on a turn's
// stream, so before this file a hosted conversation was built with its designer
// nilled and its intake cards off — a card raised over a wire was work nobody
// would ever be shown.
//
// WHAT MAKES A DETACHED CARD SURVIVE is not this file: internal/session replays
// whatever is still standing onto each new subscription, and the engine opens a
// fresh subscription for every window that asks. So a card raised into an empty
// room reaches the next window, and an answered one does not. These cards are
// deliberately NOT also in the waiting room (held.go) — they would then be
// handed over twice.
//
// IT IS tasklane.go's SHAPE: one subscription per surface that asks, one frame
// per event, no call on a repaint, and a pump that keeps draining after it goes
// quiet so no goroutine is parked on a channel. The task rail is not rewritten
// onto this — it predates the lane and is tested as it stands.
//
// AND IT IS THE SHAPE EVERY STANDING LANE SINCE HAS TAKEN. The conversation's
// name rides it (version 12) and so do its QUESTIONS (version 14): each is a
// subscription internal/session keeps for the whole session, each replays what
// is still true onto a surface that has only just attached, and each was a
// capability that simply did not exist over a connection until its two lines
// were added to the two maps below.

import (
	"encoding/json"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// laneName is both the frame kind a lane's events travel under and the key its
// subscriptions are filed by. One word for both means a frame nobody has a lane
// for cannot be produced by a build that has one.
type laneName string

// laneDesign is the harness lane. The adaptive run lane is stated as unbuilt in
// lanes.go, with the exact reason.
const laneDesign laneName = "design"

// laneQuestion is the questions lane: every decision this conversation hands to
// a person, withdrawn or answered, with the whole object on each event
// (internal/session's question.go). It is a standing lane rather than a turn's
// event because most questions outlive the turn that raised them and many —
// a landed task's `your call`, a question withdrawn on the presence beat, an
// answer somebody left in another window — never had one.
const laneQuestion laneName = "question"

// laneTitle is the naming lane: the one event a session sends when it has named
// itself. It is a standing lane rather than a turn's event because the name is
// now minted beside the turn that bought it and very often lands after that
// turn has ended (internal/session's title.go).
const laneTitle laneName = "title"

// laneDoor is the slice of *session.Agent one lane needs, asserted rather than
// required of [WrappedAgent] for tasklane.go's reason: an engine whose agent has
// no designer and no runs has no lane to offer, and a capability that cannot
// work is absent rather than broken.
type laneDoor func(WrappedAgent) (<-chan session.Event, func(), bool)

// laneDoors is how each lane is opened on an agent that has it.
var laneDoors = map[laneName]laneDoor{
	laneDesign: func(agent WrappedAgent) (<-chan session.Event, func(), bool) {
		door, ok := agent.(interface {
			WatchHarnessDesigns() (<-chan session.Event, func())
		})
		if !ok {
			return nil, nil, false
		}
		lane, stop := door.WatchHarnessDesigns()
		return lane, stop, true
	},
	laneQuestion: func(agent WrappedAgent) (<-chan session.Event, func(), bool) {
		door, ok := agent.(interface {
			WatchQuestions() (<-chan session.Event, func())
		})
		if !ok {
			return nil, nil, false
		}
		lane, stop := door.WatchQuestions()
		return lane, stop, true
	},
	laneTitle: func(agent WrappedAgent) (<-chan session.Event, func(), bool) {
		door, ok := agent.(interface {
			WatchTitle() (<-chan session.Event, func())
		})
		if !ok {
			return nil, nil, false
		}
		lane, stop := door.WatchTitle()
		return lane, stop, true
	},
}

// laneFeed is one surface's subscription to one lane and the way out of it. It
// is [taskFeed] by another name and for the same reason: the pointer is held in
// two places so the pump can tell "still mine" from "replaced under me".
type laneFeed struct {
	stop func()
	once sync.Once
}

func (f *laneFeed) leave() {
	if f == nil {
		return
	}
	f.once.Do(func() {
		if f.stop != nil {
			f.stop()
		}
	})
}

// watchLane opens one surface's subscription to one lane, replacing whatever it
// held there.
//
// THE OLD ONE IS LEFT BEFORE THE NEW ONE IS OPENED, exactly as the rail's is:
// two subscriptions pumping one screen is a page drawn twice, and after a swap
// the older one belongs to a conversation this surface has been told it is no
// longer in.
func (sess *Session) watchLane(s *server, name laneName) {
	sess.mu.Lock()
	agent, generation, closed := sess.agent, sess.generation, sess.closed
	_, watching := sess.surfaces[s]
	previous := sess.laneAt(name)[s]
	delete(sess.laneAt(name), s)
	sess.mu.Unlock()

	previous.leave()
	if closed || !watching || agent == nil {
		return
	}
	open, known := laneDoors[name]
	if !known {
		return
	}
	// The subscription is opened OUTSIDE the session lock: it may walk state of
	// its own, which has locks of its own.
	lane, stop, ok := open(agent)
	if !ok {
		return
	}
	feed := &laneFeed{stop: stop}

	sess.mu.Lock()
	_, here := sess.surfaces[s]
	if sess.closed || !here || sess.generation != generation {
		// The conversation was swapped or closed while the subscription was
		// being opened: this lane is already about the wrong agent.
		sess.mu.Unlock()
		feed.leave()
		return
	}
	sess.laneAt(name)[s] = feed
	sess.mu.Unlock()

	go sess.pumpLane(s, name, generation, lane, feed)
}

// laneAt is one lane's map of subscriptions, made on first use. It is called
// with sess.mu held.
func (sess *Session) laneAt(name laneName) map[*server]*laneFeed {
	if sess.lanes == nil {
		sess.lanes = map[laneName]map[*server]*laneFeed{}
	}
	if sess.lanes[name] == nil {
		sess.lanes[name] = map[*server]*laneFeed{}
	}
	return sess.lanes[name]
}

// pumpLane puts one surface's subscription on its wire, one frame per event. It
// keeps draining whatever happens, on [Session.pump]'s law: a lane left with a
// writer parked on it is a goroutine that never ends.
func (sess *Session) pumpLane(s *server, name laneName, generation uint64, lane <-chan session.Event, feed *laneFeed) {
	defer guard.Recover("remote/engine " + string(name) + " lane")
	quiet := false
	for event := range lane {
		if quiet {
			continue
		}
		sess.mu.Lock()
		stale := sess.generation != generation || sess.laneAt(name)[s] != feed
		// THE NAMING LANE'S FRAME IS BUILT UNDER THIS SAME HOLD, and it is the
		// only lane here that carries anything but the event.
		//
		// The reason is the revision. A name is a FACT as well as an event, and
		// the number that orders it against every other fact this session states
		// is minted under this lock ([Session.factsLocked] says why it may not be
		// minted anywhere else). Photographing the set out here would let a set
		// taken BEFORE the name reach the surface after it and blank the title;
		// worse, a swap between this unlock and the write below would land a name
		// belonging to the conversation the surface just left. Both are refused
		// on the surface by one rule, and this is where the number they are
		// refused by comes from ([FactsPush]).
		var named *FactsPush
		if !stale && name == laneTitle {
			named = sess.factsLocked()
		}
		sess.mu.Unlock()
		if stale {
			quiet = true
			continue
		}
		// Publish attention before waking a hidden reader. The title frame already
		// carries those facts; announcing a newer revision first would invalidate
		// its replay before a new subscriber can receive the name.
		if named == nil && factsMoved(event.Kind) {
			sess.announce()
		}
		var payload []byte
		var err error
		if named != nil {
			payload, err = json.Marshal(named)
		} else {
			payload, err = json.Marshal(WireEvent(event))
		}
		if err != nil {
			continue
		}
		if err := s.send(Frame{Kind: string(name), Payload: payload}); err != nil {
			quiet = true
		}
	}
}

// dropLanes ends every subscription one surface holds. It runs where a surface
// leaves the room, beside the rail's own.
func (sess *Session) dropLanes(s *server) {
	sess.mu.Lock()
	var leaving []*laneFeed
	for name := range sess.lanes {
		if feed := sess.lanes[name][s]; feed != nil {
			leaving = append(leaving, feed)
		}
		delete(sess.lanes[name], s)
	}
	sess.mu.Unlock()
	for _, feed := range leaving {
		feed.leave()
	}
}

// retakeLanes is what a session SWAP owes every lane in the room: the old
// subscriptions belong to the agent being closed, so each is left and reopened
// on the one that replaced it.
func (sess *Session) retakeLanes() {
	sess.mu.Lock()
	held := map[laneName][]*server{}
	for name, feeds := range sess.lanes {
		for surface := range feeds {
			held[name] = append(held[name], surface)
		}
	}
	sess.mu.Unlock()
	for name, surfaces := range held {
		for _, surface := range surfaces {
			sess.watchLane(surface, name)
		}
	}
}

// closeLanes ends every subscription in the room. It runs where the
// conversation does, before the agent is flushed and shut.
func (sess *Session) closeLanes() {
	sess.mu.Lock()
	var leaving []*laneFeed
	for name, feeds := range sess.lanes {
		for surface, feed := range feeds {
			leaving = append(leaving, feed)
			delete(sess.lanes[name], surface)
		}
	}
	sess.mu.Unlock()
	for _, feed := range leaving {
		feed.leave()
	}
}
