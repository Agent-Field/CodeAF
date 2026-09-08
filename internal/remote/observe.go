package remote

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// Observation is connection-local. It never announces a new turn, writes the
// journal, or shares a consumer with Submit. Reopening therefore reads the
// engine's atomic transcript/backlog boundary without executing anything again.
const (
	MethodObserve   = "Observe"
	MethodUnobserve = "Unobserve"
)

type observeArgs struct {
	ID     uint64
	Replay bool
}

type observed struct {
	Entries []session.DisplayEntry
	Running bool
}

type replayObserver interface {
	AttachReplay() ([]session.DisplayEntry, <-chan session.Event, func())
}

var _ replayObserver = (*session.Agent)(nil)

func (s *server) observe(agent WrappedAgent, call Frame) (json.RawMessage, error) {
	args, err := arg[observeArgs](call)
	if err != nil {
		return nil, err
	}
	var answer observed
	var events <-chan session.Event
	stop := func() {}
	if args.Replay {
		if door, ok := agent.(replayObserver); ok {
			answer.Entries, events, stop = door.AttachReplay()
		} else {
			answer.Entries = agent.Transcript()
		}
	} else if door, ok := agent.(interface {
		Attach() (<-chan session.Event, bool, func())
	}); ok {
		events, _, stop = door.Attach()
	}
	answer.Running = events != nil
	if events == nil {
		stop()
		return json.Marshal(answer)
	}
	ctx, cancel := context.WithCancel(context.Background())
	feed := &taskFeed{stop: func() { cancel(); stop() }}
	s.dropObserver(args.ID)
	s.observersMu.Lock()
	if s.observers == nil {
		s.observers = make(map[uint64]*taskFeed)
	}
	s.observers[args.ID] = feed
	s.observersMu.Unlock()
	// The result must arrive before its first event, just as for Submit.
	s.pending = &pending{start: func() { go s.pumpObserver(ctx, args.ID, feed, events) }}
	return json.Marshal(answer)
}

func (s *server) pumpObserver(ctx context.Context, id uint64, feed *taskFeed, events <-chan session.Event) {
	defer feed.leave()
	defer func() {
		s.observersMu.Lock()
		if s.observers[id] == feed {
			delete(s.observers, id)
		}
		s.observersMu.Unlock()
		_ = s.send(Frame{Kind: "observerClosed", ID: id})
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			if s.send(Frame{Kind: "observed", ID: id, Payload: mustJSON(WireEvent(ev))}) != nil {
				return
			}
		}
	}
}

func (s *server) dropObserver(id uint64) {
	s.observersMu.Lock()
	feed := s.observers[id]
	delete(s.observers, id)
	s.observersMu.Unlock()
	feed.leave()
}

func (s *server) stopObservers() {
	s.observersMu.Lock()
	feeds := s.observers
	s.observers = nil
	s.observersMu.Unlock()
	for _, feed := range feeds {
		feed.leave()
	}
}

// Attach gives the keeper its own reader. Leaving it stops observation only.
func (a *Agent) Attach() (<-chan session.Event, bool, func()) {
	_, events, stop := a.observe(false)
	return events, events != nil, stop
}

// AttachReplay leaves the turn's partial output in its stream, so the transcript
// and the replay cannot draw the same model output twice.
func (a *Agent) AttachReplay() ([]session.DisplayEntry, <-chan session.Event, func()) {
	return a.observe(true)
}

func (a *Agent) observe(replay bool) ([]session.DisplayEntry, <-chan session.Event, func()) {
	c := a.c
	id := c.seq.Add(1)
	tail := newStream()
	c.mu.Lock()
	if c.observers == nil {
		c.observers = make(map[uint64]*stream)
	}
	c.observers[id] = tail
	c.mu.Unlock()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			c.mu.Lock()
			delete(c.observers, id)
			c.mu.Unlock()
			tail.finish()
			// Release a pump already handing an event to the departing surface.
			go func() {
				for range tail.events() {
				}
			}()
			go func() { _, _ = c.call(nil, MethodUnobserve, observeArgs{ID: id}) }()
		})
	}
	payload, err := c.call(nil, MethodObserve, observeArgs{ID: id, Replay: replay})
	var answer observed
	if err != nil || json.Unmarshal(payload, &answer) != nil {
		stop()
		if replay {
			return a.Transcript(), nil, func() {}
		}
		return nil, nil, func() {}
	}
	if !answer.Running {
		stop()
		return answer.Entries, nil, func() {}
	}
	return answer.Entries, tail.events(), stop
}

func (c *Client) observerFrame(frame Frame) {
	c.mu.Lock()
	stream := c.observers[frame.ID]
	if frame.Kind == "observerClosed" {
		delete(c.observers, frame.ID)
	}
	c.mu.Unlock()
	if stream == nil {
		return
	}
	if frame.Kind == "observerClosed" {
		stream.finish()
	} else {
		stream.push(0, frame.Payload)
	}
}

// Observer tails end on link loss. The durable turn uses the existing resumable
// stream; reopening obtains a fresh atomic reading from the surviving engine.
func (c *Client) closeObservers() {
	c.mu.Lock()
	streams := c.observers
	c.observers = nil
	c.mu.Unlock()
	for _, stream := range streams {
		stream.finish()
	}
}
