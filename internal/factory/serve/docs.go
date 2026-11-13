package serve

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/docs"
)

// ── AN ORGANIZATION'S LIVE DOCUMENTS ───────────────────────────────────────
//
// The server is the authorized relay and store of the Yjs documents two people
// type into at once (internal/factory/docs has the store and the hub,
// api/docs.go the wire). It never parses one. What it decides is WHO: a
// document belongs to a parent item, and the parent's access is the
// document's.
//
//   - ws/<id>/page/<pid> belongs to workstream <id>, and <pid> must be one of
//     its pages. Seeing the workstream reads and follows the page and sends
//     a caret; writing it sends updates and snapshots. An archived workstream's
//     pages are read-only.
//   - canvas/<id>/frame/<fid> belongs to canvas <id>, through [CanvasDocAccess]
//     once the server keeps canvases. Until it does, every canvas document is
//     absent (404), never a refusal.
//
// UPDATES DO NOT TRAVEL ON /v1/events. That stream is the factory's durable
// record, a window of the last two hundred events rewritten on every append;
// keystrokes would push every job's and workstream's events out of it. Each
// document has its own stream instead, opened while the document is open and
// replayed from the store by seq, so it needs no window of its own.

// docRecheck is how often an open document stream asks again whether its
// reader may still see the document, and lapses stale carets.
const docRecheck = 5 * time.Second

// CanvasDocAccess answers what the caller may do with canvas id's documents:
// found false is a canvas they cannot see or that does not exist. The canvas
// lane sets it once canvases live on the server.
type CanvasDocAccess func(r *http.Request, canvas, frame string) (found, view, edit bool, err error)

// SetCanvasDocAccess installs the canvas half of the documents' access.
func (h *Handler) SetCanvasDocAccess(f CanvasDocAccess) {
	h.docs.mu.Lock()
	defer h.docs.mu.Unlock()
	h.docs.canvas = f
}

// docService is the handler's documents: the store, opened on first use, and
// the hub.
type docService struct {
	mu     sync.Mutex
	store  *docs.Store
	err    error
	closed bool
	hub    *docs.Hub
	canvas CanvasDocAccess
}

func (h *Handler) docStore() (*docs.Store, error) {
	h.docs.mu.Lock()
	defer h.docs.mu.Unlock()
	if h.docs.closed {
		return nil, errors.New("this factory is closing")
	}
	if h.docs.store == nil && h.docs.err == nil {
		h.docs.store, h.docs.err = docs.Open(filepath.Join(h.config.Store.Root(), docs.File))
	}
	return h.docs.store, h.docs.err
}

func (h *Handler) closeDocs() {
	h.docs.mu.Lock()
	defer h.docs.mu.Unlock()
	h.docs.closed = true
	if h.docs.store != nil {
		_ = h.docs.store.Close()
		h.docs.store = nil
	}
}

func (h *Handler) docRoutes() {
	h.docs.hub = docs.NewHub(h.clock)
	h.route("/v1/docs/{rest...}", map[string]http.HandlerFunc{
		http.MethodGet:  h.docGet,
		http.MethodPost: h.docPost,
		http.MethodPut:  h.docPut,
	})
}

// docSegment is one id inside a document id: no slash, no dot-dot, short.
var docSegment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

// docRef is a parsed document id.
type docRef struct {
	id     string // the whole id, as the wire spells it
	kind   string // "ws" or "canvas"
	parent string
	child  string
}

// parseDocPath splits /v1/docs/{rest...} into the document and what follows
// it ("", "updates", "snapshot", "awareness" or "events").
func parseDocPath(rest string) (docRef, string, bool) {
	parts := strings.Split(rest, "/")
	if len(parts) < 4 || len(parts) > 5 {
		return docRef{}, "", false
	}
	d := docRef{kind: parts[0], parent: parts[1], child: parts[3]}
	switch {
	case d.kind == "ws" && parts[2] == "page":
	case d.kind == "canvas" && parts[2] == "frame":
	default:
		return docRef{}, "", false
	}
	if !docSegment.MatchString(d.parent) || !docSegment.MatchString(d.child) {
		return docRef{}, "", false
	}
	d.id = strings.Join(parts[:4], "/")
	verb := ""
	if len(parts) == 5 {
		verb = parts[4]
	}
	return d, verb, true
}

// docAccess is what the caller may do with d: found false answers 404.
func (h *Handler) docAccess(r *http.Request, d docRef) (found, view, edit bool, err error) {
	switch d.kind {
	case "ws":
		return h.pageDocAccess(r, d)
	case "canvas":
		h.docs.mu.Lock()
		canvas := h.docs.canvas
		h.docs.mu.Unlock()
		if canvas == nil {
			return false, false, false, nil
		}
		return canvas(r, d.parent, d.child)
	}
	return false, false, false, nil
}

// pageDocAccess maps a page document onto its workstream. THIS IS THE ONE
// PLACE the workstream rules are read for documents, so the access core's
// can(view|edit) replaces these two lines when it lands.
func (h *Handler) pageDocAccess(r *http.Request, d docRef) (found, view, edit bool, err error) {
	if h.config.Store == nil || !strings.HasPrefix(d.parent, workstreamIDPrefix) {
		return false, false, false, nil
	}
	ws, ok, err := h.config.Store.Workstream(d.parent)
	if err != nil || !ok {
		return false, false, false, err
	}
	v, err := h.wsViewer(r)
	if err != nil {
		return false, false, false, err
	}
	if !v.canSee(ws) || !slices.ContainsFunc(ws.Pages, func(p api.WorkstreamPage) bool { return p.ID == d.child }) {
		return false, false, false, nil
	}
	return true, true, v.canWrite(ws) && ws.ArchivedAt == nil, nil
}

// openDoc parses the path and checks the caller may at least see it; the
// refusal is written when ok is false.
func (h *Handler) openDoc(w http.ResponseWriter, r *http.Request, wantVerb string) (docRef, bool, bool) {
	if !h.requireVerb(w, h.config.Store != nil) {
		return docRef{}, false, false
	}
	d, verb, ok := parseDocPath(r.PathValue("rest"))
	if !ok || verb != wantVerb {
		h.fail(w, http.StatusNotFound, api.EndpointNotFound)
		return docRef{}, false, false
	}
	found, view, edit, err := h.docAccess(r, d)
	if h.failed(w, err) {
		return docRef{}, false, false
	}
	if !found || !view {
		h.fail(w, http.StatusNotFound, "document not found")
		return docRef{}, false, false
	}
	return d, edit, true
}

func (h *Handler) docGet(w http.ResponseWriter, r *http.Request) {
	if _, verb, _ := parseDocPath(r.PathValue("rest")); verb == "events" {
		h.docEvents(w, r)
		return
	}
	d, edit, ok := h.openDoc(w, r, "")
	if !ok {
		return
	}
	since, ok := h.docSince(w, r)
	if !ok {
		return
	}
	store, err := h.docStore()
	if h.failed(w, err) {
		return
	}
	st, err := store.Since(d.id, since)
	if h.failed(w, err) {
		return
	}
	out := api.DocState{Doc: d.id, Seq: st.Seq, Updates: make([]api.DocUpdate, 0, len(st.Updates)), CanEdit: edit, Compact: edit && st.Compact()}
	if st.Snapshot != nil {
		s := docSnapshotWire(*st.Snapshot)
		out.Snapshot = &s
	}
	for _, u := range st.Updates {
		out.Updates = append(out.Updates, docUpdateWire(u))
	}
	aware := h.docs.hub.Awareness(d.id)
	out.Awareness = make([]api.DocAware, 0, len(aware))
	for _, a := range aware {
		out.Awareness = append(out.Awareness, docAwareWire(a))
	}
	h.write(w, http.StatusOK, out)
}

func (h *Handler) docSince(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := r.URL.Query().Get("since")
	if raw == "" {
		raw = r.Header.Get("Last-Event-ID")
	}
	if raw == "" {
		return 0, true
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		h.fail(w, http.StatusUnprocessableEntity, "since is the newest seq you have applied, a whole number from 0")
		return 0, false
	}
	return n, true
}

func (h *Handler) docPost(w http.ResponseWriter, r *http.Request) {
	switch _, verb, _ := parseDocPath(r.PathValue("rest")); verb {
	case "updates":
		h.docSendUpdate(w, r)
	case "awareness":
		h.docSendAwareness(w, r)
	default:
		h.fail(w, http.StatusNotFound, api.EndpointNotFound)
	}
}

func (h *Handler) docPut(w http.ResponseWriter, r *http.Request) {
	if _, verb, _ := parseDocPath(r.PathValue("rest")); verb != "snapshot" {
		h.fail(w, http.StatusNotFound, api.EndpointNotFound)
		return
	}
	h.docPutSnapshot(w, r)
}

// docEditor is the caller who may change d, or the refusal.
func (h *Handler) docEditor(w http.ResponseWriter, r *http.Request, edit bool) (string, bool) {
	if !edit {
		h.fail(w, http.StatusForbidden, "you can read this document but not change it")
		return "", false
	}
	by, err := h.callerHandle(r)
	if h.failed(w, err) {
		return "", false
	}
	return by, true
}

// decodeDoc reads a JSON body of at most most bytes.
func (h *Handler) decodeDoc(w http.ResponseWriter, r *http.Request, most int, out any) bool {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, int64(most)))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.refuse(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("this is larger than the %d bytes one request may carry", most), api.DocReasonTooLarge, "", nil, "")
			return false
		}
		h.fail(w, http.StatusBadRequest, "the request body could not be read")
		return false
	}
	if err := json.Unmarshal(data, out); err != nil {
		h.fail(w, http.StatusUnprocessableEntity, "the request body is not the JSON this route takes")
		return false
	}
	return true
}

// decodeBytes reads one base64 field of at most most bytes.
func (h *Handler) decodeBytes(w http.ResponseWriter, field, value string, most int) ([]byte, bool) {
	data, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(data) == 0 {
		h.fail(w, http.StatusUnprocessableEntity, field+" is required, as base64 of the bytes Yjs made")
		return nil, false
	}
	if len(data) > most {
		h.refuse(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("%s is larger than the %d bytes one may be", field, most), api.DocReasonTooLarge, "", nil, "")
		return nil, false
	}
	return data, true
}

// slowDown refuses a sender whose bucket is empty.
func (h *Handler) slowDown(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "1")
	h.refuse(w, http.StatusTooManyRequests, "too many changes at once; send them together, then again in a moment", api.DocReasonSlowDown, "", nil, "")
}

func (h *Handler) docSendUpdate(w http.ResponseWriter, r *http.Request) {
	d, edit, ok := h.openDoc(w, r, "updates")
	if !ok {
		return
	}
	by, ok := h.docEditor(w, r, edit)
	if !ok {
		return
	}
	var body api.DocUpdatePost
	if !h.decodeDoc(w, r, base64.StdEncoding.EncodedLen(docs.UpdateMost)+4096, &body) {
		return
	}
	data, ok := h.decodeBytes(w, "update", body.Update, docs.UpdateMost)
	if !ok {
		return
	}
	if body.BaseSeq < 0 {
		h.fail(w, http.StatusUnprocessableEntity, "base_seq is the newest seq you have applied, a whole number from 0")
		return
	}
	if !h.docs.hub.Allow(d.id, by, "update", docs.UpdateRate, docs.UpdateBurst) {
		h.slowDown(w)
		return
	}
	store, err := h.docStore()
	if h.failed(w, err) {
		return
	}
	hub := h.docs.hub
	u, compact, err := store.Append(d.id, by, h.clock(), data, body.BaseSeq, body.Seed, func(u docs.Update) {
		hub.Publish(docs.Event{Kind: docs.KindUpdate, Doc: d.id, Update: &u})
	})
	if !h.docRefused(w, err) {
		return
	}
	h.write(w, http.StatusOK, api.DocUpdated{Doc: d.id, Seq: u.Seq, By: u.By, At: u.At, Compact: compact})
}

// docRefused writes the refusal err is and is false; nil is true.
func (h *Handler) docRefused(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, docs.ErrSnapshotNeeded):
		h.refuse(w, http.StatusConflict, err.Error(), api.DocReasonSnapshotNeeded, "", nil, "")
	case errors.Is(err, docs.ErrSeeded):
		h.refuse(w, http.StatusConflict, err.Error(), api.DocReasonSeeded, "", nil, "")
	case errors.Is(err, docs.ErrAhead):
		h.refuse(w, http.StatusConflict, err.Error(), api.DocReasonReset, "", nil, "")
	default:
		h.failed(w, err)
	}
	return false
}

func (h *Handler) docPutSnapshot(w http.ResponseWriter, r *http.Request) {
	d, edit, ok := h.openDoc(w, r, "snapshot")
	if !ok {
		return
	}
	by, ok := h.docEditor(w, r, edit)
	if !ok {
		return
	}
	var body api.DocSnapshotPut
	if !h.decodeDoc(w, r, base64.StdEncoding.EncodedLen(docs.SnapshotMost)+4096, &body) {
		return
	}
	if body.Seq < 1 {
		h.fail(w, http.StatusUnprocessableEntity, "seq is the newest update this snapshot covers")
		return
	}
	state, ok := h.decodeBytes(w, "state", body.State, docs.SnapshotMost)
	if !ok {
		return
	}
	store, err := h.docStore()
	if h.failed(w, err) {
		return
	}
	kept, err := store.PutSnapshot(d.id, by, h.clock(), body.Seq, state)
	if !h.docRefused(w, err) {
		return
	}
	h.write(w, http.StatusOK, api.DocSnapshotKept{Doc: d.id, Seq: body.Seq, Kept: kept})
}

func (h *Handler) docSendAwareness(w http.ResponseWriter, r *http.Request) {
	d, _, ok := h.openDoc(w, r, "awareness")
	if !ok {
		return
	}
	by, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	var body api.DocAwarenessPost
	if !h.decodeDoc(w, r, base64.StdEncoding.EncodedLen(docs.AwarenessMost)+1024, &body) {
		return
	}
	var state []byte
	if body.State != nil {
		if state, ok = h.decodeBytes(w, "state", *body.State, docs.AwarenessMost); !ok {
			return
		}
	}
	// Leaving is never refused for speed, so a caret cannot be stranded.
	if state != nil && !h.docs.hub.Allow(d.id, by, "awareness", docs.AwarenessRate, docs.AwarenessBurst) {
		h.slowDown(w)
		return
	}
	h.docs.hub.SetAwareness(d.id, by, body.Client, state)
	w.WriteHeader(http.StatusNoContent)
}

// docEvents streams one document: the replay after since (the snapshot first
// when since is behind it), the live awareness, then every update and caret
// as it arrives. THE FEED IS JOINED BEFORE THE STORE IS READ, so an update
// committed between the two is both replayed and queued, and the queued copy
// is skipped by its seq: nothing falls between. The stream ends with
// doc.lagged when this reader falls behind and doc.removed when the document
// is gone; it ends silently when the reader may no longer see it.
func (h *Handler) docEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.fail(w, http.StatusInternalServerError, "this connection cannot stream events")
		return
	}
	d, _, ok := h.openDoc(w, r, "events")
	if !ok {
		return
	}
	since, ok := h.docSince(w, r)
	if !ok {
		return
	}
	store, err := h.docStore()
	if h.failed(w, err) {
		return
	}
	feed, leave := h.docs.hub.Subscribe(d.id)
	defer leave()
	st, err := store.Since(d.id, since)
	if h.failed(w, err) {
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	last := since
	if st.Snapshot != nil {
		last = st.Snapshot.Seq
		if !sendDocEvent(w, api.EventDocSnapshot, strconv.FormatInt(last, 10), api.DocSnapshotEvent{Doc: d.id, DocSnapshot: docSnapshotWire(*st.Snapshot)}) {
			return
		}
	} else if since > st.Seq {
		// A reader past the head holds a document that was reset; the
		// replay below is the whole document from nothing.
		last = 0
	}
	for _, u := range st.Updates {
		if !sendDocEvent(w, api.EventDocUpdate, strconv.FormatInt(u.Seq, 10), api.DocUpdateEvent{Doc: d.id, DocUpdate: docUpdateWire(u)}) {
			return
		}
		last = u.Seq
	}
	for _, a := range h.docs.hub.Awareness(d.id) {
		if !sendDocEvent(w, api.EventDocAwareness, "", api.DocAwarenessEvent{Doc: d.id, DocAware: docAwareWire(a)}) {
			return
		}
	}
	flusher.Flush()
	keepalive := time.NewTicker(eventKeepalive)
	defer keepalive.Stop()
	recheck := time.NewTicker(docRecheck)
	defer recheck.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
		case <-recheck.C:
			if found, view, _, err := h.docAccess(r, d); err != nil || !found || !view {
				return
			}
			if state, ok := r.Context().Value(authErrorKey{}).(*authErrorState); ok && state.err != nil {
				return
			}
			h.docs.hub.Sweep()
		case e, open := <-feed:
			if !open {
				sendDocEvent(w, api.EventDocLagged, "", api.DocEndEvent{Doc: d.id, Seq: last})
				flusher.Flush()
				return
			}
			switch e.Kind {
			case docs.KindRemoved:
				sendDocEvent(w, api.EventDocRemoved, "", api.DocEndEvent{Doc: d.id, Seq: last})
				flusher.Flush()
				return
			case docs.KindUpdate:
				if e.Update.Seq <= last {
					continue
				}
				if !sendDocEvent(w, api.EventDocUpdate, strconv.FormatInt(e.Update.Seq, 10), api.DocUpdateEvent{Doc: d.id, DocUpdate: docUpdateWire(*e.Update)}) {
					return
				}
				last = e.Update.Seq
			case docs.KindAwareness:
				if !sendDocEvent(w, api.EventDocAwareness, "", api.DocAwarenessEvent{Doc: d.id, DocAware: docAwareWire(*e.Aware)}) {
					return
				}
			}
		}
		flusher.Flush()
	}
}

func sendDocEvent(w io.Writer, kind, id string, body any) bool {
	data, err := json.Marshal(body)
	if err != nil {
		return false
	}
	if id != "" {
		_, err = fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", id, kind, data)
	} else {
		_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, data)
	}
	return err == nil
}

// dropDoc forgets a document whose parent part is gone (a deleted page):
// its log, its snapshot and its carets, and every open stream of it ends with
// doc.removed. The workstream lane calls it after a page delete commits.
func (h *Handler) dropDoc(doc string) error {
	if _, _, ok := parseDocPath(doc); !ok {
		return fmt.Errorf("%q is not a document id", doc)
	}
	store, err := h.docStore()
	if err != nil {
		return err
	}
	if err := store.Drop(doc); err != nil {
		return err
	}
	h.docs.hub.Remove(doc)
	return nil
}

func docUpdateWire(u docs.Update) api.DocUpdate {
	return api.DocUpdate{Seq: u.Seq, By: u.By, At: u.At, Update: base64.StdEncoding.EncodeToString(u.Data)}
}

func docSnapshotWire(s docs.Snapshot) api.DocSnapshot {
	return api.DocSnapshot{Seq: s.Seq, By: s.By, At: s.At, State: base64.StdEncoding.EncodeToString(s.State)}
}

func docAwareWire(a docs.Aware) api.DocAware {
	out := api.DocAware{By: a.By, Client: a.Client, At: a.At}
	if a.State != nil {
		s := base64.StdEncoding.EncodeToString(a.State)
		out.State = &s
	}
	return out
}
