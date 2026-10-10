package desktopbridge

import (
	"errors"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/session"
)

// queueSend serves POST /sessions/{id}/queue-send {"id"}.
//
// SEND NOW TAKES THE QUEUED MESSAGE OUT AND SENDS IT BEFORE THE CALL RETURNS.
// The design names the verb. While work is running the words are steered into
// that turn, and while nothing is running they are submitted as the next turn. The take-out and that choice share the
// conversation lock, so two sends of one id produce one send. An id the queue
// no longer holds is 409 [queueGone], the same sentence edit, move and remove
// use. The session action "queue-send" is this handler.
func (s *conversation) queueSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		fail(w, 405, "POST required")
		return
	}
	var ask queueAsk
	if !decodeMax(w, r, &ask, maxTurnBody) {
		return
	}
	door, ok := s.conn.Agent.(queueDoor)
	if !ok {
		fail(w, 409, "this engine cannot change queued messages")
		return
	}
	if s.conn.Take != nil {
		if err := s.conn.Take(); err != nil {
			fail(w, 409, err.Error())
			return
		}
	}
	text, running, status, message := s.claimQueuedSend(door, ask.ID)
	if status != 0 {
		s.publishSnapshot()
		fail(w, status, message)
		return
	}
	events, steer, err := s.dispatchQueuedSend(text, running)
	if err != nil {
		s.publishSnapshot()
		fail(w, 409, err.Error())
		return
	}
	s.armQueuedSend(events, steer)
	s.publishSnapshot()
	write(w, map[string]bool{"accepted": true})
}

// claimQueuedSend removes id from the engine queue and from this conversation's
// list, and reports whether work was running at that moment. Both happen under
// one lock, so a second caller finds the id gone and does not send. A false
// from the engine means the turn already took the message: the row is dropped
// and the caller answers 409. status 0 means the words are this call's to send.
func (s *conversation) claimQueuedSend(door queueDoor, id string) (text string, running bool, status int, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	from := -1
	for i, item := range s.queue {
		if item.ID == id {
			from = i
			break
		}
	}
	if from < 0 {
		return "", false, 409, queueGone
	}
	item := s.queue[from]
	if !door.UnqueueFollowUp(item.events) {
		s.dropAt(from)
		return "", false, 409, queueGone
	}
	s.dropAt(from)
	running = s.running
	if !running {
		// An idle send is a new turn. Mark it running before releasing the
		// lock so another turn cannot start beside it.
		s.running = true
	}
	return item.Text, running, 0, ""
}

// dispatchQueuedSend steers when the claim saw a running turn, and submits
// otherwise. A steer that finds the turn already over is submitted instead:
// the words are already out of the queue, and [session.ErrNothingToSteer] is
// the engine saying the ordinary send is the one that still reaches them.
func (s *conversation) dispatchQueuedSend(text string, running bool) (<-chan session.Event, bool, error) {
	if running {
		events, err := s.conn.Agent.Steer(text)
		if err == nil {
			return events, true, nil
		}
		if !errors.Is(err, session.ErrNothingToSteer) {
			return nil, false, err
		}
		// The turn ended in the gap. From here this is an idle send.
		running = false
		s.mu.Lock()
		s.running = true
		s.mu.Unlock()
	}
	events, err := s.submit(text, attachments{})
	if err != nil {
		s.mu.Lock()
		// The claim marked an idle conversation running. Nothing started, so
		// the flag comes back down.
		if !running {
			s.running = false
		}
		s.mu.Unlock()
		return nil, false, err
	}
	return events, false, nil
}

// armQueuedSend drains the stream the send opened. A steer's stream belongs to
// the turn already on screen, so it is drained the way a steered turn is. A
// submit is that turn's stream and is pumped like any other.
func (s *conversation) armQueuedSend(events <-chan session.Event, steer bool) {
	if events == nil {
		return
	}
	if steer {
		s.mu.Lock()
		primary := s.primary
		s.mu.Unlock()
		if events != primary {
			go s.steered(events)
		}
		return
	}
	go s.pump(events, func() {})
}
