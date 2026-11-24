package docs

import (
	"sync"
	"time"
)

// ── THE LIVE HALF: who has a document open, and where their caret is ──────
//
// The hub is memory only. An update reaches the hub after the store has
// committed it, so a stream that drops loses nothing: it opens again with the
// last sequence number it saw and the store replays the rest. Awareness (the
// carets, the names, the selection) is never written down: it is true for
// [AwarenessTTL] after it was sent and then it is gone, which is the same
// thing a closed laptop looks like.

// AwarenessTTL is how long one awareness state stays true without being sent
// again.
const AwarenessTTL = 30 * time.Second

// AwarenessMost is the largest one awareness state may be, decoded.
const AwarenessMost = 4 << 10

// The rate limits, per person per document, as a token bucket: a sustained
// rate and the burst a person may spend at once. A desk sends one update per
// editor transaction; typing quickly is about fifteen a second.
const (
	UpdateRate      = 30
	UpdateBurst     = 120
	AwarenessRate   = 10
	AwarenessBurst  = 20
	subscriberQueue = 512
)

// The event kinds, exactly as the wire spells them.
const (
	KindUpdate    = "doc.update"
	KindAwareness = "doc.awareness"
	KindRemoved   = "doc.removed"
)

// Aware is one client's awareness state on one document. A nil State is a
// client that left, or whose state lapsed.
type Aware struct {
	By     string    `json:"by"`
	Client uint64    `json:"client"`
	State  []byte    `json:"state"`
	At     time.Time `json:"at"`
}

// Event is one thing a document's subscribers hear.
type Event struct {
	Kind   string
	Doc    string
	Update *Update
	Aware  *Aware
}

type awareKey struct {
	by     string
	client uint64
}

type bucketKey struct {
	doc, by, kind string
}

type bucket struct {
	tokens float64
	at     time.Time
}

// Hub fans a document's events out to whoever has it open.
type Hub struct {
	mu      sync.Mutex
	subs    map[string]map[chan Event]struct{}
	aware   map[string]map[awareKey]Aware
	buckets map[bucketKey]*bucket
	now     func() time.Time
}

// NewHub is an empty hub reading time from now (time.Now when nil).
func NewHub(now func() time.Time) *Hub {
	if now == nil {
		now = time.Now
	}
	return &Hub{
		subs:    map[string]map[chan Event]struct{}{},
		aware:   map[string]map[awareKey]Aware{},
		buckets: map[bucketKey]*bucket{},
		now:     now,
	}
}

// Subscribe opens doc's feed. The channel is CLOSED when the subscriber falls
// so far behind that its queue fills: an update is never silently skipped, so
// a lagging reader is ended and reads the gap back from the store. cancel
// leaves the feed and is safe to call twice.
func (h *Hub) Subscribe(doc string) (<-chan Event, func()) {
	ch := make(chan Event, subscriberQueue)
	h.mu.Lock()
	if h.subs[doc] == nil {
		h.subs[doc] = map[chan Event]struct{}{}
	}
	h.subs[doc][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := h.subs[doc][ch]; ok {
			delete(h.subs[doc], ch)
			if len(h.subs[doc]) == 0 {
				delete(h.subs, doc)
			}
			close(ch)
		}
	}
}

// Subscribers is how many feeds doc has open.
func (h *Hub) Subscribers(doc string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs[doc])
}

// Publish hands e to every subscriber of its document.
func (h *Hub) Publish(e Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.publishLocked(e)
}

func (h *Hub) publishLocked(e Event) {
	for ch := range h.subs[e.Doc] {
		select {
		case ch <- e:
		default:
			delete(h.subs[e.Doc], ch)
			close(ch)
		}
	}
	if len(h.subs[e.Doc]) == 0 {
		delete(h.subs, e.Doc)
	}
}

// Remove ends every feed of doc with a removed event and forgets its
// awareness.
func (h *Hub) Remove(doc string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.aware, doc)
	for ch := range h.subs[doc] {
		select {
		case ch <- Event{Kind: KindRemoved, Doc: doc}:
		default:
		}
		close(ch)
	}
	delete(h.subs, doc)
	for k := range h.buckets {
		if k.doc == doc {
			delete(h.buckets, k)
		}
	}
}

// SetAwareness keeps a client's state on doc (nil: the client left) and
// publishes it.
func (h *Hub) SetAwareness(doc, by string, client uint64, state []byte) Aware {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sweepLocked()
	a := Aware{By: by, Client: client, State: state, At: h.now().UTC()}
	k := awareKey{by, client}
	if state == nil {
		delete(h.aware[doc], k)
		if len(h.aware[doc]) == 0 {
			delete(h.aware, doc)
		}
	} else {
		if h.aware[doc] == nil {
			h.aware[doc] = map[awareKey]Aware{}
		}
		h.aware[doc][k] = a
	}
	h.publishLocked(Event{Kind: KindAwareness, Doc: doc, Aware: &a})
	return a
}

// Awareness is every live state on doc.
func (h *Hub) Awareness(doc string) []Aware {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sweepLocked()
	out := make([]Aware, 0, len(h.aware[doc]))
	for _, a := range h.aware[doc] {
		out = append(out, a)
	}
	return out
}

// Sweep lapses every awareness state older than [AwarenessTTL], publishing
// each as a client that left. Any reader may call it; it is idempotent.
func (h *Hub) Sweep() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sweepLocked()
}

func (h *Hub) sweepLocked() {
	now := h.now()
	for doc, states := range h.aware {
		for k, a := range states {
			if now.Sub(a.At) < AwarenessTTL {
				continue
			}
			delete(states, k)
			gone := Aware{By: a.By, Client: a.Client, At: now.UTC()}
			h.publishLocked(Event{Kind: KindAwareness, Doc: doc, Aware: &gone})
		}
		if len(states) == 0 {
			delete(h.aware, doc)
		}
	}
	for k, b := range h.buckets {
		if now.Sub(b.at) > time.Minute {
			delete(h.buckets, k)
		}
	}
}

// Allow spends one token of by's bucket of kind on doc, and is false when it
// is empty. rate is tokens a second, burst the most the bucket holds.
func (h *Hub) Allow(doc, by, kind string, rate, burst float64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	k := bucketKey{doc, by, kind}
	b := h.buckets[k]
	if b == nil {
		b = &bucket{tokens: burst, at: now}
		h.buckets[k] = b
	}
	if el := now.Sub(b.at).Seconds(); el > 0 {
		b.tokens = min(burst, b.tokens+el*rate)
	}
	b.at = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
