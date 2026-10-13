package realtime

import (
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

const subscriberBuffer = 256

// Hub is the in-process pub/sub. A slow subscriber loses events rather than
// blocking publishers; clients re-query on reconnect.
type Hub struct {
	mu     sync.Mutex
	seq    map[string]int64
	nextID int
	subs   map[int]*subscriber
}

type subscriber struct {
	topic string
	ch    chan svc.Event
}

var _ svc.Hub = (*Hub)(nil)

// New returns an empty hub.
func New(svc.Config, *svc.Services) *Hub {
	return &Hub{seq: map[string]int64{}, subs: map[int]*subscriber{}}
}

// Publish sends data to every subscriber of topic.
func (h *Hub) Publish(topic string, data any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq[topic]++
	event := svc.Event{Topic: topic, Seq: h.seq[topic], Data: data}
	for _, sub := range h.subs {
		if matches(sub.topic, topic) {
			select {
			case sub.ch <- event:
			default:
			}
		}
	}
}

// Subscribe returns a channel of events on topic (or prefix "x/*").
func (h *Hub) Subscribe(topic string) (<-chan svc.Event, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextID++
	id := h.nextID
	sub := &subscriber{topic: topic, ch: make(chan svc.Event, subscriberBuffer)}
	h.subs[id] = sub
	var once sync.Once
	return sub.ch, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, id)
			h.mu.Unlock()
			close(sub.ch)
		})
	}
}

func matches(pattern, topic string) bool {
	if prefix, ok := strings.CutSuffix(pattern, "/*"); ok {
		return strings.HasPrefix(topic, prefix+"/")
	}
	return pattern == topic
}
