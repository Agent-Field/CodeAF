package desktopbridge

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
)

// placesRoutesTable lets sibling files add /places handlers from their own
// init() without editing places.go. The key is "METHOD /places/<tail>", where
// <tail> uses "{id}" for the place-id segment (e.g. "POST /places/{id}/sources").
// It is written only at init time, so it is read without a lock.
var placesRoutesTable = map[string]func(p *Places, w http.ResponseWriter, r *http.Request, id string){}

// registerPlacesRoute adds one handler to the table. It panics on a duplicate,
// because two files claiming one route is a bug that must fail at start-up.
func registerPlacesRoute(key string, h func(p *Places, w http.ResponseWriter, r *http.Request, id string)) {
	if _, dup := placesRoutesTable[key]; dup {
		panic("desktopbridge: places route registered twice: " + key)
	}
	placesRoutesTable[key] = h
}

// tableRoute serves a registered route, reporting whether one matched. The
// bridge's CORS list has no DELETE, so only GET, POST and PUT are ever keyed.
func (p *Places) tableRoute(w http.ResponseWriter, r *http.Request, parts []string) bool {
	if len(placesRoutesTable) == 0 {
		return false
	}
	shape := append([]string{}, parts...)
	id := ""
	if len(shape) > 0 {
		id, shape[0] = shape[0], "{id}"
	}
	for _, tail := range []string{strings.Join(parts, "/"), strings.Join(shape, "/")} {
		if h, ok := placesRoutesTable[r.Method+" /places/"+tail]; ok {
			h(p, w, r, id)
			return true
		}
	}
	return false
}

// acceptGeneration lets a client spell the optimistic-concurrency guard
// "ifGeneration"; the store calls the same number its revision, so the body is
// rewritten to the one name every ask struct already reads. The body is only
// touched when the key is really a top-level member.
func acceptGeneration(w http.ResponseWriter, r *http.Request) {
	if r.Body == nil || r.ContentLength == 0 {
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		// Leave the failure for readBody to name (413 or 400).
		r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(raw), errReader{err}))
		return
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) == nil {
		if v, ok := m["ifGeneration"]; ok {
			delete(m, "ifGeneration")
			if _, has := m["ifRevision"]; !has {
				m["ifRevision"] = v
			}
			if out, err := json.Marshal(m); err == nil {
				raw = out
			}
		}
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	r.ContentLength = int64(len(raw))
}

// ---- the rail's own verb ---------------------------------------------------

type railAsk struct {
	IfRevision *uint64  `json:"ifRevision"`
	Op         string   `json:"op"`
	Place      string   `json:"place"`
	Index      *int     `json:"index"`
	Order      []string `json:"order"`
}

// railOp is POST /places/rail: one verb for the five things a person does to
// the rail. Visiting and closing are soft (no receipt, no undo); pin, unpin and
// reorder are structural and answer like every other write.
func (p *Places) railOp(w http.ResponseWriter, r *http.Request) {
	var ask railAsk
	if !readBody(w, r, &ask) {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.staleRevision(w, ask.IfRevision) {
		return
	}
	var b batch
	var err error
	switch ask.Op {
	case "visit":
		if err = p.Store.TouchOpened(ask.Place, p.now()); err == nil {
			err = p.Store.Visit(ask.Place)
		}
	case "close":
		err = p.Store.Close(ask.Place, p.busyFn())
	case "pin":
		index := -1
		if ask.Index != nil {
			index = *ask.Index
		}
		var rc placegraph.Receipt
		rc, err = p.Store.Pin(ask.Place, index)
		b.add(rc)
	case "unpin":
		var rc placegraph.Receipt
		rc, err = p.Store.Unpin(ask.Place)
		b.add(rc)
	case "reorder":
		var rc placegraph.Receipt
		rc, err = p.Store.Reorder(ask.Order)
		b.add(rc)
	default:
		failPlaces(w, 400, "invalid", "The rail can visit, close, pin, unpin or reorder a place.")
		return
	}
	if err != nil {
		p.failStore(w, err, "", nil)
		return
	}
	p.finish(w, &b, "", func(m *Mutation) {
		if x, ok := p.open(w, false); ok {
			rv := x.rail()
			m.Rail = &rv
		}
	})
}

// ---- sweeping --------------------------------------------------------------

// sweepEvery is how often idle rail rows are let go.
const sweepEvery = 30 * time.Second

// busyFn says whether a place has work the rail must not drop: something is
// running in it or something waits on the person.
func (p *Places) busyFn() placegraph.Busy {
	x, ok := p.indexQuiet()
	if !ok {
		return nil
	}
	return func(id string) bool {
		s := x.rollup(x.incl[id])
		return s.Running > 0 || s.NeedsYou > 0
	}
}

// indexQuiet reads the index without writing an error answer.
func (p *Places) indexQuiet() (*placeIndex, bool) {
	snap, err := p.Store.Snapshot()
	if err != nil {
		return nil, false
	}
	return newPlaceIndex(snap, p.world(false), p.now()), true
}

// sweepOnce lets go of the rail rows that have settled or sat idle, and tells
// the stream when the rail changed.
func (p *Places) sweepOnce() {
	before, ok := p.indexQuiet()
	if !ok {
		return
	}
	p.mu.Lock()
	err := p.Store.Sweep(p.now(), p.busyFn())
	p.mu.Unlock()
	if err != nil {
		return
	}
	if after, ok := p.indexQuiet(); ok && !jsonEqual(before.rail(), after.rail()) {
		p.publishPlaces(nil)
	}
}

// startSweep runs sweepOnce on a ticker until stop closes.
func (p *Places) startSweep(stop <-chan struct{}, every time.Duration) {
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				p.sweepOnce()
			}
		}
	}()
}

// ---- the stream record -----------------------------------------------------

type placesRecord struct {
	Generation uint64                  `json:"generation"`
	Nodes      []PlaceView             `json:"nodes"`
	Rail       RailView                `json:"rail"`
	Members    []placegraph.Membership `json:"members,omitempty"`
}

// publishPlaces puts ONE `places` record on the world stream after a write, so
// every window redraws from the same generation. A bridge with no stream
// attached publishes nothing.
func (p *Places) publishPlaces(members []placegraph.Membership) {
	if p.publish == nil {
		return
	}
	x, ok := p.indexQuiet()
	if !ok {
		return
	}
	nodes := x.views(x.snap.Places)
	p.publish("places", placesRecord{Generation: x.snap.Revision, Nodes: nodes, Rail: x.rail(), Members: members})
}

// Publish appends a record of a kind other than the poller's own to the ring.
func (f *WorldFeed) Publish(kind string, payload any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appendLocked(kind, payload)
}

// errReader replays a read failure that acceptGeneration already met.
type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }
