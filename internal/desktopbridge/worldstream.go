package desktopbridge

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The wire is the same shape as the per-conversation stream — `id: <seq>` then
// `data: <json>` — so one reader serves both. A record is a small typed delta,
// never a transcript.
const (
	// WorldRing is how many records a late reader can still be replayed.
	WorldRing = 4096
	// worldInterval is the persisted-world poll while somebody listens. It stands
	// in for a native notification and is the ONLY clock this feed runs.
	worldInterval = 2 * time.Second
	// worldHeartbeat keeps an idle stream from looking dead to a proxy.
	worldHeartbeat = 15 * time.Second
	// worldMaxAge is how stale GET /world may serve from the last read.
	worldMaxAge = time.Second
)

// WorldRecord is one record on the engine-wide stream. Type is "world" (changed
// rows and removed ids), "attention" (the whole open set), "jobs" (one attached
// chat's background jobs, jobs.go) or "reset" (the whole state, sent instead of
// a replay the ring can no longer give).
type WorldRecord struct {
	Epoch   string          `json:"epoch"`
	Seq     uint64          `json:"seq"`
	Type    string          `json:"type"`
	At      string          `json:"at"`
	Payload json.RawMessage `json:"payload"`
}

// WorldFeed holds the ring, the last projection and the single poller. All
// methods are safe from any goroutine; Publish-side work never waits on a
// reader, because a reader pulls from the ring at its own pace.
type WorldFeed struct {
	read      func() session.World
	graph     func() *placegraph.Snapshot
	ledger    func() string
	interval  time.Duration
	heartbeat time.Duration
	maxAge    time.Duration
	ringSize  int
	// newTicker is the clock for the heartbeat and the poll; tests replace it.
	newTicker func(time.Duration) (<-chan time.Time, func())

	// refreshMu serialises reads of the disk so concurrent readers share one scan.
	refreshMu sync.Mutex

	mu        sync.Mutex
	epoch     string
	seq       uint64
	ring      []WorldRecord
	changed   chan struct{}
	observers int
	rows      map[string]WorldRow
	items     []AttentionItem
	lastRead  time.Time
	stopPoll  chan struct{}
	closed    bool
}

// NewWorldFeed builds a feed over a reader of the persisted world. A nil reader
// is session.ReadHome.
func NewWorldFeed(read func() session.World) *WorldFeed {
	if read == nil {
		read = session.ReadHome
	}
	epoch, err := Token()
	if err != nil {
		panic("cannot create world feed identity")
	}
	return &WorldFeed{
		epoch: epoch,
		read:  read, interval: worldInterval, heartbeat: worldHeartbeat, maxAge: worldMaxAge, ringSize: WorldRing,
		newTicker: func(d time.Duration) (<-chan time.Time, func()) {
			t := time.NewTicker(d)
			return t.C, t.Stop
		},
		changed: make(chan struct{}), rows: map[string]WorldRow{}, items: []AttentionItem{},
	}
}

// Observers is how many streams are attached right now.
func (f *WorldFeed) Observers() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.observers
}

// Close ends the poller. Open streams end with their requests.
func (f *WorldFeed) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	if f.stopPoll != nil {
		close(f.stopPoll)
		f.stopPoll = nil
	}
}

// attach counts a reader and starts the poller for the first one. The returned
// function detaches it, and stops the poller with the last.
func (f *WorldFeed) attach() func() {
	f.mu.Lock()
	f.observers++
	if f.observers == 1 && !f.closed {
		stop := make(chan struct{})
		f.stopPoll = stop
		go f.poll(stop)
	}
	f.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.observers--
			if f.observers == 0 && f.stopPoll != nil {
				close(f.stopPoll)
				f.stopPoll = nil
			}
		})
	}
}

func (f *WorldFeed) poll(stop <-chan struct{}) {
	tick, release := f.newTicker(f.interval)
	defer release()
	for {
		select {
		case <-stop:
			return
		case <-tick:
			f.refresh()
		}
	}
}

// refresh reads the persisted world once and publishes only what moved.
func (f *WorldFeed) refresh() {
	f.refreshMu.Lock()
	defer f.refreshMu.Unlock()
	var graph *placegraph.Snapshot
	if f.graph != nil {
		graph = f.graph()
	}
	var ledger string
	if f.ledger != nil {
		ledger = f.ledger()
	}
	rows, items := projectWorld(f.read(), ledger, graph)
	now := time.Now()

	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastRead = now
	next := make(map[string]WorldRow, len(rows))
	delta := worldDelta{Rows: []WorldRow{}, Removed: []string{}}
	for _, row := range rows {
		next[row.Session] = row
		if old, ok := f.rows[row.Session]; !ok || !jsonEqual(old, row) {
			delta.Rows = append(delta.Rows, row)
		}
	}
	for id := range f.rows {
		if _, ok := next[id]; !ok {
			delta.Removed = append(delta.Removed, id)
		}
	}
	f.rows = next
	if len(delta.Rows) > 0 || len(delta.Removed) > 0 {
		f.appendLocked("world", delta)
	}
	if !jsonEqual(f.items, items) {
		f.items = items
		f.appendLocked("attention", attentionDelta{Items: items})
	}
}

func (f *WorldFeed) appendLocked(kind string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	f.seq++
	f.ring = append(f.ring, WorldRecord{Epoch: f.epoch, Seq: f.seq, Type: kind, At: time.Now().UTC().Format(time.RFC3339Nano), Payload: data})
	if len(f.ring) > f.ringSize {
		f.ring = append([]WorldRecord(nil), f.ring[len(f.ring)-f.ringSize:]...)
	}
	close(f.changed)
	f.changed = make(chan struct{})
}

// fullLocked is the whole state, sorted, for a reset or GET /world.
func (f *WorldFeed) fullLocked() worldFull {
	rows := make([]WorldRow, 0, len(f.rows))
	for _, row := range f.rows {
		rows = append(rows, row)
	}
	sortRows(rows)
	return worldFull{Rows: rows, Items: append([]AttentionItem{}, f.items...)}
}

func sortRows(rows []WorldRow) {
	sort.Slice(rows, func(i, j int) bool { return rows[i].Session < rows[j].Session })
}

// Current is the whole state and the sequence it is consistent with. It reads
// the disk again only when the last read is older than maxAge.
func (f *WorldFeed) Current() (uint64, worldFull) {
	f.mu.Lock()
	fresh := !f.lastRead.IsZero() && time.Since(f.lastRead) < f.maxAge
	f.mu.Unlock()
	if !fresh {
		f.refresh()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.seq, f.fullLocked()
}

// worldRoutes serves GET /world and GET /events. It runs after the token check.
func (b *Bridge) worldRoutes(w http.ResponseWriter, r *http.Request, path string) bool {
	switch path {
	case "/world":
		if needGet(w, r) {
			seq, full := b.worldFeed().Current()
			write(w, struct {
				Seq uint64 `json:"seq"`
				worldFull
			}{seq, full})
		}
		return true
	case "/events":
		if needGet(w, r) {
			b.worldFeed().serve(w, r)
		}
		return true
	case "/world/failures/seen":
		if needPost(w, r) {
			b.markFailureSeen(w, r)
		}
		return true
	}
	return false
}

// UseWorld replaces the feed's reader of the persisted world; tests and doors
// that own another state root call it before the first request.
func (b *Bridge) UseWorld(f *WorldFeed) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.world != nil {
		b.world.Close()
	}
	f.graph = b.attentionGraph
	f.ledger = b.decideLedger
	b.world = f
}

func (b *Bridge) worldFeed() *WorldFeed {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.world == nil {
		b.world = NewWorldFeed(nil)
		b.world.graph = b.attentionGraph
	}
	if b.world.ledger == nil {
		b.world.ledger = b.decideLedger
	}
	return b.world
}

// serve is the SSE handler. A reader is replayed from `after` while the ring
// still holds it; otherwise it gets ONE reset carrying the whole state.
func (f *WorldFeed) serve(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		fail(w, 500, "streaming unsupported")
		return
	}
	var after uint64
	if raw := r.URL.Query().Get("after"); raw != "" {
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			fail(w, 400, "after must be a sequence number")
			return
		}
		after = n
	}
	detach := f.attach()
	defer detach()
	// Catch up on whatever moved while nobody listened, before deciding what this
	// reader missed.
	f.refresh()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	flusher.Flush()
	beat, release := f.newTicker(f.heartbeat)
	defer release()
	first := true
	for {
		f.mu.Lock()
		var batch []WorldRecord
		gap := first && (after == 0 || after > f.seq || (r.URL.Query().Get("epoch") != "" && r.URL.Query().Get("epoch") != f.epoch))
		if len(f.ring) > 0 && after+1 < f.ring[0].Seq {
			gap = true
		}
		if !gap && first && len(f.ring) == 0 && after < f.seq {
			gap = true
		}
		if gap {
			data, _ := json.Marshal(f.fullLocked())
			batch = []WorldRecord{{Epoch: f.epoch, Seq: f.seq, Type: "reset", At: time.Now().UTC().Format(time.RFC3339Nano), Payload: data}}
		} else {
			for _, rec := range f.ring {
				if rec.Seq > after {
					batch = append(batch, rec)
				}
			}
		}
		changed := f.changed
		f.mu.Unlock()
		first = false
		for _, rec := range batch {
			data, _ := json.Marshal(rec)
			if _, err := fmt.Fprintf(w, "id: %d\ndata: %s\n\n", rec.Seq, data); err != nil {
				return
			}
			after = rec.Seq
		}
		if len(batch) > 0 {
			flusher.Flush()
		}
		select {
		case <-r.Context().Done():
			return
		case <-changed:
		case <-beat:
			if _, err := io.WriteString(w, ": alive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
