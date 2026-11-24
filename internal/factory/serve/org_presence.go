package serve

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
)

// ── WHO IS HERE ────────────────────────────────────────────────────────────
//
// POST /v1/presence is a heartbeat: the desk sends one every
// [api.PresenceBeat] while a person has an item open, naming the item, the
// part they are on (a page, a frame) and whether they are editing it. A
// person not heard from for [api.PresenceTTL] has left. `"left": true` leaves
// at once, when the item is closed.
//
// PRESENCE IS KEPT IN MEMORY AND NEVER WRITTEN AS STATE. It is who is looking
// right now; a restarted server knows nobody until their next beat, ten
// seconds later. Only a CHANGE to who is here (someone arrives, moves to
// another part, starts or stops editing, leaves or goes silent) is published,
// as a `presence` event carrying the whole list for that item, so a beat that
// changes nothing costs the event stream nothing. Silence is noticed by a
// timer set to the next expiry, the way a machine going quiet is
// (machine_events.go).
//
// AN EVENT REACHES ONLY THOSE WHO MAY VIEW THE ITEM, checked as each one is
// sent, and a heartbeat for an item the caller may not view is 404, the same
// answer the item's own route gives. Which item kinds exist, and who may view
// one, is [Handler.presenceKinds]: a workstream here; a canvas registers its
// own.
//
// Carets inside a document are not presence: they travel with the document's
// own awareness (the docs relay). Presence says who is in the item and on
// which part.

// presenceKind answers whether the caller may view the item id of one kind.
type presenceKind func(r *http.Request, id string) (bool, error)

// presenceKey is one person in one window on one item.
type presenceKey struct {
	kind, id, handle, client string
}

type presenceRow struct {
	part    string
	editing bool
	seen    time.Time
}

type presenceDesk struct {
	mu     sync.Mutex
	rows   map[presenceKey]presenceRow
	timer  *time.Timer
	closed bool
}

// presenceBody is POST /v1/presence's request.
type presenceBody struct {
	Item    api.PresenceItem `json:"item"`
	Part    string           `json:"part,omitempty"`
	Editing bool             `json:"editing,omitempty"`
	Left    bool             `json:"left,omitempty"`
	// Client tells one window from another, so a person with the same item
	// open twice is two rows, not one row flipping between parts.
	Client string `json:"client,omitempty"`
}

// presenceWordMost bounds an id, a part or a client name.
const presenceWordMost = 200

func (h *Handler) presenceRoutes() {
	h.presenceKinds = map[string]presenceKind{
		api.PresenceWorkstream: func(r *http.Request, id string) (bool, error) {
			known, see, _, err := h.orgWorkstreamAccess(r, id)
			return known && see, err
		},
	}
	h.route("/v1/presence", map[string]http.HandlerFunc{http.MethodPost: h.presence})
}

func (h *Handler) presence(w http.ResponseWriter, r *http.Request) {
	var body presenceBody
	if !h.decode(w, r, &body, false) {
		return
	}
	body.Item.Kind, body.Item.ID = strings.TrimSpace(body.Item.Kind), strings.TrimSpace(body.Item.ID)
	body.Part, body.Client = strings.TrimSpace(body.Part), strings.TrimSpace(body.Client)
	viewable, ok := h.presenceKinds[body.Item.Kind]
	if !ok {
		h.fail(w, http.StatusUnprocessableEntity, "item.kind must be one of: "+strings.Join(h.presenceKindNames(), ", "))
		return
	}
	for _, word := range []string{body.Item.ID, body.Part, body.Client} {
		if len(word) > presenceWordMost || strings.ContainsAny(word, "\x00\n\r") {
			h.fail(w, http.StatusUnprocessableEntity, "item.id, part and client are each one line of at most 200 characters")
			return
		}
	}
	if body.Item.ID == "" {
		h.fail(w, http.StatusUnprocessableEntity, "item.id is required")
		return
	}
	who := h.orgActor(r)
	if who == "" {
		h.fail(w, http.StatusForbidden, "presence needs a signed-in person")
		return
	}
	see, err := viewable(r, body.Item.ID)
	if h.failed(w, err) {
		return
	}
	if !see {
		h.fail(w, http.StatusNotFound, "not found")
		return
	}
	key := presenceKey{kind: body.Item.Kind, id: body.Item.ID, handle: who, client: body.Client}
	here := h.beatPresence(key, presenceRow{part: body.Part, editing: body.Editing && !body.Left, seen: h.clock()}, body.Left)
	h.write(w, http.StatusOK, api.Presence{Item: body.Item, Here: here})
}

func (h *Handler) presenceKindNames() []string {
	out := make([]string, 0, len(h.presenceKinds))
	for k := range h.presenceKinds {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// beatPresence records one beat (or one leaving) and answers who is on the
// item now. A change is published.
func (h *Handler) beatPresence(key presenceKey, row presenceRow, left bool) []api.PresenceHere {
	d := &h.presenceDesk
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.rows == nil {
		d.rows = map[presenceKey]presenceRow{}
	}
	before := hereOn(d.rows, key.kind, key.id)
	if left {
		delete(d.rows, key)
	} else {
		d.rows[key] = row
	}
	changed := map[[2]string]bool{}
	h.expirePresenceLocked(row.seen, changed)
	here := hereOn(d.rows, key.kind, key.id)
	if !sameHere(before, here) {
		changed[[2]string{key.kind, key.id}] = true
	}
	h.publishPresenceLocked(changed)
	h.schedulePresenceLocked(row.seen)
	return here
}

// sweepPresence is the timer's door: it drops every row gone silent and
// publishes the items whose list changed.
func (h *Handler) sweepPresence() {
	d := &h.presenceDesk
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	now := h.clock()
	changed := map[[2]string]bool{}
	h.expirePresenceLocked(now, changed)
	h.publishPresenceLocked(changed)
	h.schedulePresenceLocked(now)
}

func (h *Handler) expirePresenceLocked(now time.Time, changed map[[2]string]bool) {
	d := &h.presenceDesk
	for k, row := range d.rows {
		if !now.Before(row.seen.Add(api.PresenceTTL)) {
			delete(d.rows, k)
			changed[[2]string{k.kind, k.id}] = true
		}
	}
}

func (h *Handler) schedulePresenceLocked(now time.Time) {
	d := &h.presenceDesk
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	if d.closed {
		return
	}
	var next time.Time
	for _, row := range d.rows {
		due := row.seen.Add(api.PresenceTTL)
		if next.IsZero() || due.Before(next) {
			next = due
		}
	}
	if !next.IsZero() {
		d.timer = time.AfterFunc(max(next.Sub(now), 0)+10*time.Millisecond, h.sweepPresence)
	}
}

// publishPresenceLocked writes one presence event per changed item. A live
// record that cannot be written costs a prompt update, never the beat.
func (h *Handler) publishPresenceLocked(changed map[[2]string]bool) {
	root := h.eventRoot()
	if root == "" || len(changed) == 0 {
		return
	}
	items := make([][2]string, 0, len(changed))
	for item := range changed {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i][0]+items[i][1] < items[j][0]+items[j][1] })
	events := make([]live.Event, 0, len(items))
	for _, item := range items {
		events = append(events, live.New(api.EventPresence, 0, api.Presence{
			Item: api.PresenceItem{Kind: item[0], ID: item[1]}, Here: hereOn(h.presenceDesk.rows, item[0], item[1])}))
	}
	_ = live.Append(root, events...)
}

// closePresence stops the timer; Close calls it.
func (h *Handler) closePresence() {
	d := &h.presenceDesk
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	if d.timer != nil {
		d.timer.Stop()
	}
}

// hereOn is who is on one item, by handle then part, with two windows of one
// person on the same part folded into one row (editing if either is).
func hereOn(rows map[presenceKey]presenceRow, kind, id string) []api.PresenceHere {
	folded := map[[2]string]bool{}
	for k, row := range rows {
		if k.kind != kind || k.id != id {
			continue
		}
		at := [2]string{k.handle, row.part}
		folded[at] = folded[at] || row.editing
	}
	out := make([]api.PresenceHere, 0, len(folded))
	for at, editing := range folded {
		out = append(out, api.PresenceHere{Handle: at[0], Part: at[1], Editing: editing})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Handle != out[j].Handle {
			return out[i].Handle < out[j].Handle
		}
		return out[i].Part < out[j].Part
	})
	return out
}

func sameHere(a, b []api.PresenceHere) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// presenceForViewer sends a presence event to those who may view its item.
func (h *Handler) presenceForViewer(r *http.Request, e live.Event) (live.Event, bool) {
	var p api.Presence
	if json.Unmarshal(e.Data, &p) != nil {
		return e, false
	}
	viewable, ok := h.presenceKinds[p.Item.Kind]
	if !ok {
		return e, false
	}
	see, err := viewable(r, p.Item.ID)
	return e, err == nil && see
}
