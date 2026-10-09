package desktopbridge

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/session"
)

// The messages a person queued behind a running turn, in the order they will
// run. The engine holds the real queue (session's follow-ups); this list is the
// window's names for them: each item keeps the stream the engine handed back at
// queue time, which is the receipt every later edit, move and take-back is
// matched on.

// QueuedWire is one waiting message as the window reads it.
type QueuedWire struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type queuedItem struct {
	QueuedWire
	events <-chan session.Event
}

// queueDoor is what the engine must offer for a queue to be editable. An engine
// without it keeps the queue it always had and answers 409, not a lie.
type queueDoor interface {
	EditFollowUp(<-chan session.Event, string) error
	MoveFollowUp(ch, before <-chan session.Event) bool
	UnqueueFollowUp(<-chan session.Event) bool
}

const queueGone = "that message has already been sent"

// remember lists a message the engine just queued.
func (s *conversation) remember(text string, events <-chan session.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queueSeq++
	id := "q" + strconv.FormatUint(s.queueSeq, 10)
	s.queue = append(s.queue, &queuedItem{QueuedWire: QueuedWire{ID: id, Text: strings.TrimSpace(text)}, events: events})
}

// forget drops the item that owns events, whichever way it left the queue: its
// turn started, the engine took it back, or a stop dropped the whole queue.
func (s *conversation) forget(events <-chan session.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, item := range s.queue {
		if item.events == events {
			s.dropAt(i)
			return
		}
	}
}

func (s *conversation) dropAt(i int) { s.queue = append(s.queue[:i], s.queue[i+1:]...) }

// queueWire is the snapshot's copy of the queue, never nil.
func (s *conversation) queueWire() []QueuedWire {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]QueuedWire, len(s.queue))
	for i, item := range s.queue {
		out[i] = item.QueuedWire
	}
	return out
}

type queueAsk struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	To   *int   `json:"to"`
}

// queueAction serves queue-edit, queue-move and queue-remove. EVERY ONE OF THEM
// IS ONLY TRUE WHILE THE MESSAGE IS STILL QUEUED: the engine decides under the
// same lock its drain takes, and a message it no longer holds is a 409 with
// [queueGone], never a silent success. The window then refreshes and sees the
// turn that message started.
func (s *conversation) queueAction(w http.ResponseWriter, r *http.Request, kind string) {
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
	status, message := s.changeQueue(door, kind, ask)
	snapshot := s.snapshot()
	s.publish(Record{Type: "snapshot", Snapshot: &snapshot})
	if status != 0 {
		fail(w, status, message)
		return
	}
	write(w, map[string]bool{"accepted": true})
}

// changeQueue applies one change under the conversation lock, so two changes
// never read the same list. The engine never calls back into the bridge, so
// holding the lock across its calls cannot deadlock. It returns the HTTP status
// of a refusal, or 0.
func (s *conversation) changeQueue(door queueDoor, kind string, ask queueAsk) (int, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	from := -1
	for i, item := range s.queue {
		if item.ID == ask.ID {
			from = i
		}
	}
	if from < 0 {
		return 409, queueGone
	}
	item := s.queue[from]
	switch kind {
	case "queue-edit":
		if strings.TrimSpace(ask.Text) == "" {
			return 400, "a queued message cannot be empty"
		}
		err := door.EditFollowUp(item.events, ask.Text)
		if errors.Is(err, session.ErrFollowUpGone) {
			s.dropAt(from)
			return 409, queueGone
		}
		if err != nil {
			return 409, err.Error()
		}
		item.Text = strings.TrimSpace(ask.Text)
	case "queue-remove":
		if !door.UnqueueFollowUp(item.events) {
			s.dropAt(from)
			return 409, queueGone
		}
		s.dropAt(from)
	default:
		return s.moveQueued(door, from, ask.To)
	}
	return 0, ""
}

// moveQueued puts the item at index to of the list that remains once it is out,
// naming its new place to the engine by the neighbour it now sits before.
func (s *conversation) moveQueued(door queueDoor, from int, to *int) (int, string) {
	if to == nil || *to < 0 {
		return 400, "a position is required"
	}
	item := s.queue[from]
	rest := append(append([]*queuedItem{}, s.queue[:from]...), s.queue[from+1:]...)
	at := min(*to, len(rest))
	var before <-chan session.Event
	if at < len(rest) {
		before = rest[at].events
	}
	if !door.MoveFollowUp(item.events, before) {
		s.dropAt(from)
		return 409, queueGone
	}
	s.queue = append(rest[:at], append([]*queuedItem{item}, rest[at:]...)...)
	return 0, ""
}
