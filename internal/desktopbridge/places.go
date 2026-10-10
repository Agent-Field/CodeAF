package desktopbridge

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Places is the desktop's door to the canonical place graph. The graph, the
// memberships, the pinned order and the undo ring are internal/placegraph's; the
// status a place shows is read from the canonical session world. This type owns
// no state of its own beyond a short world cache and a mutex around the one
// read-modify-write the store has no verb for (a place's sources).
//
// THE STORE PATH IS INJECTED. There is no default location and no package-level
// store: whoever builds a Places chose the file, so two bridges in one process
// (or one test) can never share a graph by accident.
type Places struct {
	// Store is the graph. Required.
	Store *placegraph.Store
	// World reads the canonical session world. Nil means session.ReadHome. A test
	// hands a directory it built through session.ReadWorld.
	World func() session.World
	// Now is the clock for the rail's idle window and the world cache. Nil is
	// time.Now.
	Now func() time.Time
	// Recaps reads the recap a conversation persisted about itself, for a Home's
	// "Since yesterday". Nil reads each conversation's own meta.json
	// (places_digest.go); a test injects a fixture.
	Recaps RecapReader
	// Stale remembers "Not now" for the untouched-place suggestion (places_stale.go).
	// Nil leaves both of its routes absent.
	Stale *placegraph.StaleBook

	// live lists the chat ids of conversations this bridge holds open. A
	// conversation the person just started may not be in the world yet; it is
	// still a real chat they can file. UsePlaces fills it.
	live func() []string
	// publish puts a record on the world stream. UsePlaces points it at the
	// bridge's feed; nil publishes nothing.
	publish func(kind string, payload any)
	// tell puts a placeChange event on one open conversation's own ring
	// (placechange.go). UsePlaces points it at the bridge; nil tells nobody.
	tell func(chat string, change PlaceChange)
	// sweepStop ends the rail's 30-second sweep. Bridge.Close closes it.
	sweepStop chan struct{}

	// door is the engine's own door onto this graph (session.PlaceGraphDoorFor):
	// the remembered-picks file and the source policy the Using list and the
	// add-a-source route share with the engine, and choices is the book opened
	// on that file. UseDoor sets both (using.go); without them the Using routes
	// answer that this bridge cannot.
	door    *session.PlaceGraphDoor
	choices *placegraph.ChoiceBook
	// allowsModel checks a place's default model against the same model list
	// the settings page offers. UsePlaces fills it from the bridge's models.
	allowsModel func(context.Context, string) bool

	// mu serialises this process's read-modify-write mutations. The store's file
	// lock already protects the document across processes; this protects the
	// gap between reading a place's context and writing it back.
	mu sync.Mutex

	cacheMu   sync.Mutex
	cached    session.World
	cachedAt  time.Time
	cacheDone bool

	recapOnce    sync.Once
	recapDefault *metaRecaps
}

// NewPlaces wraps an opened store. The caller chose its path.
func NewPlaces(store *placegraph.Store) *Places { return &Places{Store: store} }

// UsePlaces attaches the places door. Without it every /places route is absent
// (404 from the ordinary "unknown engine action"), never a route that fails.
func (b *Bridge) UsePlaces(p *Places) {
	if p != nil {
		p.live = b.liveChatIDs
		p.allowsModel = b.allowsModel
		p.tell = b.tellChat
		p.publish = func(kind string, payload any) { b.worldFeed().Publish(kind, payload) }
		if p.sweepStop == nil {
			p.sweepStop = make(chan struct{})
			p.startSweep(p.sweepStop, sweepEvery)
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.places = p
	// The feed reads the ledger when it projects, so this is the function,
	// not a path captured before the door exists.
	if b.world != nil && b.world.ledger == nil {
		b.world.ledger = b.decideLedger
	}
}

// ChatIDFromSessionFile is the canonical chat id of a conversation: the name of
// the folder that holds its transcript, which is session.SessionRow.ID.
func ChatIDFromSessionFile(sessionFile string) string {
	if sessionFile == "" {
		return ""
	}
	return filepath.Base(filepath.Dir(sessionFile))
}

// liveChatIDs are the chat ids of the conversations this bridge has open.
func (b *Bridge) liveChatIDs() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var ids []string
	for _, s := range b.sessions {
		if id := ChatIDFromSessionFile(s.conn.Welcome.SessionFile); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func (p *Places) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// worldLife is how long one reading of the world is reused. The rail polls the
// status route; a disk scan per poll would be the cost of a feature that is
// only ever slightly stale.
const worldLife = 1500 * time.Millisecond

func (p *Places) world(fresh bool) session.World {
	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()
	if !fresh && p.cacheDone && p.now().Sub(p.cachedAt) < worldLife {
		return p.cached
	}
	read := p.World
	if read == nil {
		read = session.ReadHome
	}
	p.cached, p.cachedAt, p.cacheDone = read(), p.now(), true
	return p.cached
}

// placesRoutes serves /places* and /chats/{id}/places. It reports whether the
// path was one of its own; with no Places attached it claims nothing.
func (b *Bridge) placesRoutes(w http.ResponseWriter, r *http.Request, path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if parts[0] != "places" && parts[0] != "chats" {
		return false
	}
	b.mu.Lock()
	p := b.places
	b.mu.Unlock()
	if p == nil {
		return false
	}
	if parts[0] == "chats" {
		if len(parts) == 3 && parts[2] == "places" {
			if needGet(w, r) {
				p.chatPlaces(w, parts[1])
			}
			return true
		}
		return false
	}
	p.serve(w, r, parts[1:])
	return true
}

func (p *Places) serve(w http.ResponseWriter, r *http.Request, parts []string) {
	if p.tableRoute(w, r, parts) {
		return
	}
	get, post := r.Method == http.MethodGet, r.Method == http.MethodPost
	switch len(parts) {
	case 0:
		switch {
		case get:
			p.graph(w, r)
		case post:
			p.create(w, r)
		default:
			fail(w, 405, "GET or POST required")
		}
	case 1:
		switch parts[0] {
		case "status":
			if needGet(w, r) {
				p.status(w)
			}
		case "rail":
			if r.Method == http.MethodPost {
				p.railOp(w, r)
			} else if needGet(w, r) {
				p.railRoute(w)
			}
		case "undo":
			if needPost(w, r) {
				p.undo(w, r)
			}
		case "from-folder":
			if needPost(w, r) {
				p.fromFolder(w, r)
			}
		case "stale":
			if p.Stale == nil {
				fail(w, 404, "unknown engine action")
			} else if needGet(w, r) {
				p.staleList(w)
			}
		default:
			switch {
			case get:
				p.digest(w, parts[0])
			case post:
				p.update(w, r, parts[0])
			default:
				fail(w, 405, "GET or POST required")
			}
		}
	case 2:
		// Checked before undo: "undo" is also a reserved place word, and its
		// effective-model question must reach the 400 "reserved" answer.
		if parts[1] == "effective-model" {
			if needGet(w, r) {
				p.effectiveModel(w, parts[0])
			}
			return
		}
		if parts[0] == "undo" {
			if needPost(w, r) {
				p.undoReceipt(w, r, parts[1])
			}
			return
		}
		if parts[1] == "delete-preview" {
			if needGet(w, r) {
				p.deletePreview(w, parts[0])
			}
			return
		}
		if needPost(w, r) {
			p.action(w, r, parts[0], parts[1])
		}
	case 3:
		switch {
		case parts[1] == "members" && parts[2] == "remove":
			if needPost(w, r) {
				p.removeMembers(w, r, parts[0])
			}
		case parts[1] == "sources" && parts[2] == "remove":
			if needPost(w, r) {
				p.removeSource(w, r, parts[0])
			}
		default:
			fail(w, 404, "unknown engine action")
		}
	default:
		fail(w, 404, "unknown engine action")
	}
}

// action dispatches the single-segment POST verbs under /places/{id}/.
func (p *Places) action(w http.ResponseWriter, r *http.Request, id, verb string) {
	switch verb {
	case "parents":
		p.parents(w, r, id)
	case "archive", "restore":
		p.archive(w, r, id, verb == "archive")
	case "delete":
		p.deletePlace(w, r, id)
	case "merge":
		p.merge(w, r, id)
	case "sources":
		p.addSource(w, r, id)
	case "members":
		p.addMembers(w, r, id)
	case "pin", "unpin":
		p.pin(w, r, id, verb == "pin")
	case "visit":
		p.visit(w, r, id)
	case "stale-snooze":
		if p.Stale == nil {
			fail(w, 404, "unknown engine action")
			return
		}
		p.staleSnooze(w, r, id)
	default:
		fail(w, 404, "unknown engine action")
	}
}

// ---- wire errors -----------------------------------------------------------

type placesError struct {
	Error   string               `json:"error"`
	Code    string               `json:"code,omitempty"`
	Applied []placegraph.Receipt `json:"applied,omitempty"`
	Undone  *int                 `json:"undone,omitempty"`
}

func failPlaces(w http.ResponseWriter, status int, code, sentence string) {
	writeStatus(w, status, placesError{Error: sentence, Code: code})
}

func writeStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// placeName is the name to quote in a sentence, and "that place" when the id is
// not one the snapshot knows.
func placeName(s *placegraph.Snapshot, id string) string {
	switch id {
	case placegraph.RootID:
		return "All places"
	case placegraph.NowID:
		return "Now"
	}
	if s != nil {
		if pl, ok := s.Place(id); ok {
			return "“" + pl.Name + "”"
		}
	}
	return "that place"
}

// storeFailure turns a store error into a status, a slug and a sentence a
// person can read. The store's own text carries a package prefix and is never
// shown. detail is what the caller knows that the error does not: the names
// involved.
func storeFailure(err error, detail string) (int, string, string) {
	switch {
	case errors.Is(err, placegraph.ErrNotFound):
		return 404, "not_found", "That place doesn't exist any more."
	case errors.Is(err, placegraph.ErrReservedID):
		return 400, "reserved", "All places and Now are built in; they can't be changed this way."
	case errors.Is(err, placegraph.ErrNameTaken):
		return 409, "name_taken", "Another place here already has that name."
	case errors.Is(err, placegraph.ErrCycle):
		if detail != "" {
			return 409, "cycle", detail
		}
		return 409, "cycle", "That would put a place inside itself."
	case errors.Is(err, placegraph.ErrArchived):
		return 409, "archived", "That place is archived. Restore it first."
	case errors.Is(err, placegraph.ErrNotArchived):
		return 409, "not_archived", "That place isn't archived."
	case errors.Is(err, placegraph.ErrRevisionConflict):
		return 409, "cannot_undo", "That can't be undone now because the places changed afterwards."
	case errors.Is(err, placegraph.ErrNoReceipt):
		return 410, "no_receipt", "That change can no longer be undone."
	case errors.Is(err, placegraph.ErrTooLarge):
		return 413, "too_large", "That is larger than a place can hold."
	case errors.Is(err, placegraph.ErrLocked):
		return 503, "busy", "Places are busy in another window. Try again in a moment."
	case errors.Is(err, placegraph.ErrUnsupportedVersion):
		return 409, "newer_file", "Your places were saved by a newer codeaf. Update this app to change them."
	case errors.Is(err, placegraph.ErrInvalid):
		return 400, "invalid", invalidSentence(err)
	}
	return 500, "store", "Places couldn't be saved. Nothing was changed."
}

// invalidSentence keeps the store's own reason for a refused value (it names
// the field) without the package prefix, as a capitalised sentence.
func invalidSentence(err error) string {
	text := strings.TrimPrefix(err.Error(), placegraph.ErrInvalid.Error())
	text = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), ":"))
	if text == "" {
		return "That value isn't allowed."
	}
	text = strings.ToUpper(text[:1]) + text[1:]
	if !strings.HasSuffix(text, ".") {
		text += "."
	}
	return text
}

func (p *Places) failStore(w http.ResponseWriter, err error, detail string, applied []placegraph.Receipt) {
	status, code, sentence := storeFailure(err, detail)
	writeStatus(w, status, placesError{Error: sentence, Code: code, Applied: applied})
}

// readBody decodes a request body under the bridge's ordinary limits. An empty
// body is an empty object, because several verbs carry nothing.
func readBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.Body == nil || r.ContentLength == 0 {
		return true
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		fail(w, 415, "JSON required")
		return false
	}
	acceptGeneration(w, r)
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		switch {
		case errors.Is(err, io.EOF):
			return true
		case errors.As(err, &tooBig):
			failPlaces(w, 413, "too_large", "That request is larger than the desktop can carry.")
		default:
			failPlaces(w, 400, "invalid", "That request wasn't understood.")
		}
		return false
	}
	return true
}

// open reads the snapshot and the world together. A store that cannot be read
// is an honest 503, not an empty graph.
func (p *Places) open(w http.ResponseWriter, fresh bool) (*placeIndex, bool) {
	snap, err := p.Store.Snapshot()
	if err != nil {
		p.failStore(w, err, "", nil)
		return nil, false
	}
	return newPlaceIndex(snap, p.world(fresh), p.now()), true
}

// staleRevision enforces the optional ifRevision guard. It reports whether the
// request may proceed.
func (p *Places) staleRevision(w http.ResponseWriter, ifRevision *uint64) bool {
	if ifRevision == nil {
		return true
	}
	rev, err := p.Store.Revision()
	if err != nil {
		p.failStore(w, err, "", nil)
		return false
	}
	if rev != *ifRevision {
		failPlaces(w, 409, "stale", "Your places changed in another window. Reload and try again.")
		return false
	}
	return true
}
