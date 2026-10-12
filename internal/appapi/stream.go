package appapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// EventRing bounds replay memory independently of how many windows listen.
const EventRing = 4096

const eventKeepalive = 20 * time.Second

type eventRecord struct {
	seq   uint64
	kind  EventType
	agent string
	data  []byte
}

// EventHub ports the bridge's pull-based replay: publishers never wait on a
// reader and slow readers get a reset instead of an unbounded private queue.
// The circular ring replaces the bridge's copying of 4096 records per append.
type EventHub struct {
	mu      sync.Mutex
	epoch   string
	seq     uint64
	ring    [EventRing]eventRecord
	changed chan struct{}
	closed  bool
}

func newEventHub(epoch string) *EventHub {
	return &EventHub{epoch: epoch, changed: make(chan struct{})}
}

// Publish freezes the payload before returning, so callers may reuse their
// objects without changing replay. Filtering reads agent from these same bytes.
// Resource lanes own payload fields; the hub enforces the transport envelope.
func (h *EventHub) Publish(kind EventType, payload any) (string, error) {
	if !kind.publishable() {
		return "", errors.New("event type is not published on the app stream")
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	var envelope struct {
		Agent string `json:"agent"`
	}
	if len(data) == 0 || data[0] != '{' || json.Unmarshal(data, &envelope) != nil {
		return "", errors.New("event data must be a JSON object with an optional string agent")
	}
	if kind == EventTurnDelta {
		var delta TurnDeltaEvent
		if json.Unmarshal(data, &delta) != nil || delta.Agent == "" || delta.Turn == "" || (delta.Kind != "text" && delta.Kind != "thinking") {
			return "", errors.New("turn.delta needs agent, turn and text or thinking kind")
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return "", errors.New("app event stream is closed")
	}
	h.seq++
	h.ring[(h.seq-1)%EventRing] = eventRecord{h.seq, kind, envelope.Agent, data}
	close(h.changed)
	h.changed = make(chan struct{})
	return h.id(h.seq), nil
}

// Close wakes idle streams as well as refusing further publication.
func (h *EventHub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.closed {
		h.closed = true
		close(h.changed)
	}
}

func (h *EventHub) id(seq uint64) string {
	return "e_" + h.epoch + "_" + strconv.FormatUint(seq, 10)
}

// read captures replay and its wake-up channel under one lock. A publication
// while the batch is sent closes that channel, eliminating the replay/live gap.
func (h *EventHub) read(after uint64) ([]eventRecord, uint64, bool, <-chan struct{}, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	first := uint64(1)
	if h.seq >= EventRing {
		first = h.seq - EventRing + 1
	}
	gap := after > h.seq || (after < first && !(after == 0 && first == 1))
	var batch []eventRecord
	if !gap {
		for seq := after + 1; seq <= h.seq; seq++ {
			batch = append(batch, h.ring[(seq-1)%EventRing])
		}
	}
	return batch, h.seq, gap, h.changed, h.closed
}

func (h *EventHub) cursor(headers http.Header) (uint64, string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	values, present := headers["Last-Event-Id"]
	if !present {
		return h.seq, ""
	}
	if len(values) != 1 || values[0] == "" {
		return h.seq, "unknown"
	}
	raw := values[0]
	split := strings.LastIndexByte(raw, '_')
	if !strings.HasPrefix(raw, "e_") || split <= 2 {
		return h.seq, "unknown"
	}
	seq, err := strconv.ParseUint(raw[split+1:], 10, 64)
	if err != nil || strconv.FormatUint(seq, 10) != raw[split+1:] {
		return h.seq, "unknown"
	}
	if raw[2:split] != h.epoch {
		return h.seq, "epoch_changed"
	}
	if seq > h.seq {
		return h.seq, "unknown"
	}
	if h.seq >= EventRing && seq < h.seq-EventRing+1 && !(seq == 0 && h.seq == EventRing) {
		return h.seq, "expired"
	}
	return seq, ""
}

func (record eventRecord) visible(agents map[string]bool) bool {
	if record.kind == EventTurnDelta {
		return agents[record.agent]
	}
	return len(agents) == 0 || record.agent == "" || agents[record.agent]
}

func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	s.streamWithClock(w, r, func() (<-chan time.Time, func()) {
		ticker := time.NewTicker(eventKeepalive)
		return ticker.C, ticker.Stop
	})
}

// streamWithClock lets tests advance an idle connection without real sleeps.
func (s *Server) streamWithClock(w http.ResponseWriter, r *http.Request, clock func() (<-chan time.Time, func())) {
	if _, ok := w.(http.Flusher); !ok {
		fail(w, http.StatusInternalServerError, "not_allowed", "this connection cannot stream events")
		return
	}
	agents := map[string]bool{}
	for _, agent := range r.URL.Query()["agent"] {
		if agent == "" {
			fail(w, http.StatusUnprocessableEntity, "bad_input", "agent filter needs an agent id")
			return
		}
		agents[agent] = true
	}
	after, reason := s.events.cursor(r.Header)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	// A reconnect's hello has no id: assigning the current head before replay
	// would lose the missed events if the connection broke during that hello.
	hello, _ := json.Marshal(streamHello{s.options.Epoch, s.options.Version, Contract})
	helloID := ""
	if _, present := r.Header["Last-Event-Id"]; !present {
		helloID = s.events.id(after)
	}
	if !sendEvent(w, helloID, "hello", hello) {
		return
	}
	beat, stop := clock()
	defer stop()
	for {
		batch, head, gap, changed, closed := s.events.read(after)
		if closed {
			return
		}
		if gap && reason == "" {
			reason = "expired"
		}
		if reason != "" {
			data, _ := json.Marshal(streamReset{s.options.Epoch, reason})
			if !sendEvent(w, s.events.id(head), "reset", data) {
				return
			}
			after, reason = head, ""
		} else {
			for _, record := range batch {
				if r.Context().Err() != nil {
					return
				}
				if record.visible(agents) && !sendEvent(w, s.events.id(record.seq), string(record.kind), record.data) {
					return
				}
			}
			// Hidden events still advance this reader, so a busy unrelated agent
			// cannot make the connection spin on the same invisible records.
			after = head
		}
		select {
		case <-r.Context().Done():
			return
		case <-changed:
		case <-beat:
			if !sendStream(w, ": keepalive\n\n") {
				return
			}
		}
	}
}

func sendEvent(w http.ResponseWriter, id, kind string, data []byte) bool {
	frame := "event: " + kind + "\ndata: " + string(data) + "\n\n"
	if id != "" {
		frame = "id: " + id + "\n" + frame
	}
	return sendStream(w, frame)
}

func sendStream(w http.ResponseWriter, frame string) bool {
	control := http.NewResponseController(w)
	// An unread window cannot hold a handler forever. The deadline is cleared
	// while idle so the keepalive clock, rather than a stale write timer, rules.
	_ = control.SetWriteDeadline(time.Now().Add(10 * time.Second))
	defer control.SetWriteDeadline(time.Time{})
	if _, err := fmt.Fprint(w, frame); err != nil {
		return false
	}
	return control.Flush() == nil
}
