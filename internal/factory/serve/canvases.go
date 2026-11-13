package serve

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/canvasrpc"
	"github.com/Agent-Field/codeaf/internal/factory/live"
)

// ── AN ORGANIZATION'S CANVASES ─────────────────────────────────────────────
//
// An organization's canvases live on its server (api/canvases.go has the
// wire). The canvas itself — its frames and nodes, op log, versions, marks
// and assets — is a canvas-core directory under the store's canvas root,
// read and written ONLY through `canvas rpc` (factory/canvasrpc). This file
// decides who is asking and whether they may, and nothing else:
//
//   - WHO MAY is the access core (item_access.go): view reads; comment adds
//     marks and takes a hand on a frame; edit writes; full shares, hands it to
//     another owner, makes it private, trashes and deletes. One the caller
//     cannot view answers 404, exactly as one that does not exist.
//   - WHO WROTE IT is stamped here: every write goes in as person:<handle> of
//     the signed-in caller, never as anyone a request names.
//   - TWO PEOPLE NEVER WRITE ONE FRAME AT ONCE: canvas-core refuses a batch
//     that writes in, or takes a hand on, a frame another person's hand is on,
//     and the caller gets a 409 naming who and what they have not seen.
//
// A server with no `canvas` program keeps no canvases: the routes are left
// off and the engine does not say org_canvases, so the desk has no verb to
// fail with.

// canvasHandSecs is canvas-core's HAND_SECS: how long a person's hand on a
// frame lasts unless renewed. A lease op whose until is "hand" is given it.
const canvasHandSecs = 10

// canvasAssetMost bounds one asset's bytes, and canvasImportMost a whole
// import (its snapshot and every asset in base64).
const (
	canvasAssetMost  = 25 << 20
	canvasImportMost = 120 << 20
	canvasOpsMost    = 8 << 20
)

// canvasIDShape is canvas-core's id: cv_ and twelve letters or digits.
var canvasIDShape = regexp.MustCompile(`^cv_[a-z0-9]{12}$`)

// canvasSha is an asset's sha256, hex.
var canvasSha = regexp.MustCompile(`^[0-9a-f]{64}$`)

// The meta keys only canvas-core or this server write: a client's op that
// sets one is refused, because trashing is full's and the rest are a laptop's
// own bookkeeping (who leads it, which agent is invited).
var canvasReservedMeta = []string{"trashed", "lead", "schema", "created_at"}
var canvasReservedMetaPrefixes = []string{"invite:", "joined:", "workaf."}

func (h *Handler) canvasRoutes() {
	if h.config.Store == nil || h.config.Canvas == nil {
		return
	}
	h.route("/v1/canvases", map[string]http.HandlerFunc{http.MethodGet: h.listCanvases, http.MethodPost: h.createCanvas})
	h.route("/v1/canvases/import", map[string]http.HandlerFunc{http.MethodPost: h.importCanvas})
	h.route("/v1/canvases/{id}", map[string]http.HandlerFunc{http.MethodGet: h.getCanvas, http.MethodPatch: h.patchCanvas, http.MethodDelete: h.deleteCanvas})
	h.route("/v1/canvases/{id}/shares", map[string]http.HandlerFunc{http.MethodPut: h.setCanvasShares})
	h.route("/v1/canvases/{id}/snapshot", map[string]http.HandlerFunc{http.MethodGet: h.canvasSnapshot})
	h.route("/v1/canvases/{id}/ops", map[string]http.HandlerFunc{http.MethodGet: h.canvasOps, http.MethodPost: h.applyCanvasOps})
	h.route("/v1/canvases/{id}/versions", map[string]http.HandlerFunc{http.MethodGet: h.canvasVersions, http.MethodPost: h.createCanvasVersion})
	h.route("/v1/canvases/{id}/versions/{vid}/restore", map[string]http.HandlerFunc{http.MethodPost: h.restoreCanvas})
	h.route("/v1/canvases/{id}/marks", map[string]http.HandlerFunc{http.MethodGet: h.canvasMarks, http.MethodPost: h.createCanvasMark})
	h.route("/v1/canvases/{id}/assets/{sha}", map[string]http.HandlerFunc{http.MethodGet: h.canvasAsset, http.MethodPut: h.putCanvasAsset})
	if h.presenceKinds != nil {
		// Who is in a canvas, and on which frame (the presence part), reaches
		// exactly those who may view it.
		h.presenceKinds[api.PresenceCanvas] = h.canvasViewable
	}
}

// canvasViewable is whether the caller of r may view canvas id; an id that
// names no canvas is false, with no error.
func (h *Handler) canvasViewable(r *http.Request, id string) (bool, error) {
	if !canvasIDShape.MatchString(id) {
		return false, nil
	}
	c, ok, err := h.config.Store.Canvas(id)
	if err != nil || !ok {
		return false, err
	}
	v, err := h.itemViewer(r)
	if err != nil {
		return false, err
	}
	return can(v, canvasItem(c), factory.RoleView), nil
}

// CanvasesServed is whether this server keeps canvases.
func (h *Handler) CanvasesServed() bool {
	return h.config.Store != nil && h.config.Canvas != nil
}

func canvasItem(c factory.OrgCanvas) Item {
	return Item{Kind: ItemCanvas, ID: c.ID, Owner: c.Owner, Shares: c.Shares, Private: c.Private}
}

func canvasActor(v Viewer) string {
	return "person:" + v.Handle
}

// ── who is asking ──

// findCanvas is the canvas the path names when the caller may do verb to it.
// It has already answered when ok is false: 404 when they may not view it,
// 403 when they may view it but not do verb.
func (h *Handler) findCanvas(w http.ResponseWriter, r *http.Request, verb string) (factory.OrgCanvas, Viewer, bool) {
	id := r.PathValue("id")
	v, err := h.itemViewer(r)
	if h.failed(w, err) {
		return factory.OrgCanvas{}, v, false
	}
	if !canvasIDShape.MatchString(id) {
		h.fail(w, http.StatusNotFound, "canvas not found")
		return factory.OrgCanvas{}, v, false
	}
	c, ok, err := h.config.Store.Canvas(id)
	if h.failed(w, err) {
		return c, v, false
	}
	if !ok || !can(v, canvasItem(c), factory.RoleView) {
		h.fail(w, http.StatusNotFound, "canvas not found")
		return c, v, false
	}
	if !can(v, canvasItem(c), verb) {
		h.refuse(w, http.StatusForbidden, canvasRefusal(verb), "role", "", nil, "")
		return c, v, false
	}
	if verb != factory.RoleView && v.Handle == "" {
		// A write is stamped with the writer's handle; a caller with none
		// (a token on a team server with no signed-in person) cannot write.
		h.fail(w, http.StatusForbidden, "sign in to change a canvas")
		return c, v, false
	}
	return c, v, true
}

func canvasRefusal(verb string) string {
	switch verb {
	case factory.RoleComment:
		return "you can view this canvas but not comment on it"
	case factory.RoleEdit:
		return "you can view this canvas but not change it"
	}
	return "only its owner, or someone it is shared with in full, can do that"
}

// canvasFailed answers a sidecar error: canvas-core's refusals with their
// meaning kept, anything else as the server's own failure.
func (h *Handler) canvasFailed(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	var e *canvasrpc.Error
	if !errors.As(err, &e) {
		return h.failed(w, err)
	}
	switch e.Code {
	case "invalid", "usage":
		h.refuse(w, http.StatusBadRequest, e.Message, "invalid", "", nil, "")
	case "not_found":
		h.refuse(w, http.StatusNotFound, e.Message, "not_found", "", nil, "")
	case "stale", "exists", "leased":
		h.refuse(w, http.StatusConflict, e.Message, e.Code, "", nil, "")
	case "role", "not_member":
		h.refuse(w, http.StatusForbidden, e.Message, "role", "", nil, "")
	default:
		h.refuse(w, http.StatusInternalServerError, e.Message, e.Code, "", nil, "")
	}
	return true
}

func (h *Handler) rpc(ctx context.Context, id string, req map[string]any, out any) error {
	return h.config.Canvas.Call(ctx, id, req, out)
}

// ── rows ──

// canvasRow is c as v sees it in a list, read from canvas-core.
func (h *Handler) canvasRow(ctx context.Context, c factory.OrgCanvas, v Viewer) (api.CanvasRow, error) {
	it := canvasItem(c)
	row := api.CanvasRow{ID: c.ID, Owner: c.Owner, Shares: nonNil(c.Shares), Private: c.Private, MyRole: roleOf(v, it), UpdatedBy: c.UpdatedBy}
	raw, err := h.config.Canvas.Raw(ctx, c.ID, map[string]any{"cmd": "summary"})
	if err != nil {
		return row, err
	}
	var s struct {
		Title string `json:"title"`
		Seq   int64  `json:"seq"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return row, err
	}
	row.Title, row.Seq = s.Title, s.Seq
	if nameOnly(v, it) {
		// The operator learns that a private canvas exists, its name and
		// owner, never what it says or who it is shared with.
		row.NameOnly, row.Shares, row.MyRole = true, []factory.Share{}, ""
		return row, nil
	}
	row.Summary = raw
	return row, nil
}

func (h *Handler) writeCanvas(w http.ResponseWriter, r *http.Request, status int, c factory.OrgCanvas, v Viewer) {
	row, err := h.canvasRow(r.Context(), c, v)
	if h.canvasFailed(w, err) {
		return
	}
	h.write(w, status, row)
}

// ── reads ──

func (h *Handler) listCanvases(w http.ResponseWriter, r *http.Request) {
	v, err := h.itemViewer(r)
	if h.failed(w, err) {
		return
	}
	all, err := h.config.Store.Canvases()
	if h.failed(w, err) {
		return
	}
	out := api.CanvasList{Canvases: []api.CanvasRow{}}
	updated := map[string]string{}
	for _, c := range all {
		it := canvasItem(c)
		if !can(v, it, factory.RoleView) && !nameOnly(v, it) {
			continue
		}
		row, err := h.canvasRow(r.Context(), c, v)
		if err != nil {
			// One canvas that cannot be read does not hide the others.
			continue
		}
		var s struct {
			Updated string `json:"updated"`
		}
		_ = json.Unmarshal(row.Summary, &s)
		updated[c.ID] = s.Updated
		out.Canvases = append(out.Canvases, row)
	}
	sort.Slice(out.Canvases, func(i, j int) bool {
		a, b := out.Canvases[i], out.Canvases[j]
		if updated[a.ID] != updated[b.ID] {
			return updated[a.ID] > updated[b.ID]
		}
		return a.ID > b.ID
	})
	h.write(w, http.StatusOK, out)
}

func (h *Handler) getCanvas(w http.ResponseWriter, r *http.Request) {
	c, v, ok := h.findCanvas(w, r, factory.RoleView)
	if !ok {
		return
	}
	h.writeCanvas(w, r, http.StatusOK, c, v)
}

func (h *Handler) canvasSnapshot(w http.ResponseWriter, r *http.Request) {
	c, _, ok := h.findCanvas(w, r, factory.RoleView)
	if !ok {
		return
	}
	raw, err := h.config.Canvas.Raw(r.Context(), c.ID, map[string]any{"cmd": "snapshot"})
	if h.canvasFailed(w, err) {
		return
	}
	h.write(w, http.StatusOK, raw)
}

func (h *Handler) canvasOps(w http.ResponseWriter, r *http.Request) {
	c, _, ok := h.findCanvas(w, r, factory.RoleView)
	if !ok {
		return
	}
	since := int64(0)
	if s := r.URL.Query().Get("since"); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n < 0 {
			h.fail(w, http.StatusBadRequest, "since is the seq you have read up to, a whole number")
			return
		}
		since = n
	}
	var out api.CanvasOps
	if h.canvasFailed(w, h.rpc(r.Context(), c.ID, map[string]any{"cmd": "ops", "since": since}, &out)) {
		return
	}
	out.Ops = nonNil(out.Ops)
	h.write(w, http.StatusOK, out)
}

func (h *Handler) canvasVersions(w http.ResponseWriter, r *http.Request) {
	c, _, ok := h.findCanvas(w, r, factory.RoleView)
	if !ok {
		return
	}
	raw, err := h.config.Canvas.Raw(r.Context(), c.ID, map[string]any{"cmd": "versions"})
	if h.canvasFailed(w, err) {
		return
	}
	h.write(w, http.StatusOK, raw)
}

func (h *Handler) canvasMarks(w http.ResponseWriter, r *http.Request) {
	c, _, ok := h.findCanvas(w, r, factory.RoleView)
	if !ok {
		return
	}
	raw, err := h.config.Canvas.Raw(r.Context(), c.ID, map[string]any{"cmd": "marks"})
	if h.canvasFailed(w, err) {
		return
	}
	h.write(w, http.StatusOK, raw)
}

func (h *Handler) canvasAsset(w http.ResponseWriter, r *http.Request) {
	c, _, ok := h.findCanvas(w, r, factory.RoleView)
	if !ok {
		return
	}
	sha := r.PathValue("sha")
	if !canvasSha.MatchString(sha) {
		h.fail(w, http.StatusNotFound, "asset not found")
		return
	}
	var a struct {
		Mime string `json:"mime"`
		Name string `json:"name"`
		Path string `json:"path"`
	}
	if h.canvasFailed(w, h.rpc(r.Context(), c.ID, map[string]any{"cmd": "asset", "sha": sha}, &a)) {
		return
	}
	// The path canvas-core answers is inside this canvas's own folder; one
	// that is not is never served.
	dir := filepath.Join(h.config.Canvas.Root, c.ID) + string(filepath.Separator)
	if !strings.HasPrefix(filepath.Clean(a.Path), dir) {
		h.fail(w, http.StatusNotFound, "asset not found")
		return
	}
	f, err := os.Open(a.Path)
	if h.failed(w, err) {
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", a.Mime)
	w.Header().Set("X-Canvas-Asset-Name", a.Name)
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
}

// ── writes ──

// readCanvasBody decodes a JSON body of at most most bytes into out.
func (h *Handler) readCanvasBody(w http.ResponseWriter, r *http.Request, most int64, out any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, most))
	if err := dec.Decode(out); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			h.fail(w, http.StatusRequestEntityTooLarge, "that is more than one request may carry")
			return false
		}
		h.fail(w, http.StatusBadRequest, "the body is not the JSON this route takes")
		return false
	}
	return true
}

func (h *Handler) createCanvas(w http.ResponseWriter, r *http.Request) {
	v, err := h.itemViewer(r)
	if h.failed(w, err) {
		return
	}
	if v.Handle == "" {
		h.fail(w, http.StatusForbidden, "sign in to make a canvas")
		return
	}
	var body api.CanvasNew
	if !h.readCanvasBody(w, r, 64<<10, &body) {
		return
	}
	if err := plainWords("title", body.Title, 1, workstreamNameMost); h.workstreamRefused(w, err) {
		return
	}
	c, ok := h.newCanvasRecord(w, v, body.Owner, body.Shares, body.Private)
	if !ok {
		return
	}
	var created struct {
		ID string `json:"id"`
	}
	if h.canvasFailed(w, h.rpc(r.Context(), "", map[string]any{"cmd": "create", "title": strings.TrimSpace(body.Title), "actor": canvasActor(v)}, &created)) {
		return
	}
	c.ID = created.ID
	h.saveNewCanvas(w, r, c, v, http.StatusCreated)
}

// newCanvasRecord is the record of a canvas v is making, owned by owner (v
// when nil) and shared as asked. Making one for a team needs being in it.
func (h *Handler) newCanvasRecord(w http.ResponseWriter, v Viewer, owner *api.WorkstreamOwner, shares []factory.Share, private bool) (factory.OrgCanvas, bool) {
	o := api.WorkstreamOwner{Kind: api.OwnerPerson, ID: v.Handle}
	if owner != nil {
		var err error
		if o, err = h.cleanOwner(*owner); h.workstreamRefused(w, err) {
			return factory.OrgCanvas{}, false
		}
		if !v.Solo && !(o.Kind == api.OwnerPerson && o.ID == v.Handle) && !(o.Kind == api.OwnerTeam && v.Teams[o.ID]) {
			h.refuse(w, http.StatusForbidden, "you can make a canvas for yourself or for a team you are in", "role", "", nil, "")
			return factory.OrgCanvas{}, false
		}
	}
	clean, err := h.cleanShares(shares, o)
	if h.workstreamRefused(w, err) {
		return factory.OrgCanvas{}, false
	}
	return factory.OrgCanvas{Owner: o, Shares: clean, Private: private, CreatedBy: v.Handle, CreatedAt: h.now().UTC(), UpdatedBy: v.Handle}, true
}

func (h *Handler) saveNewCanvas(w http.ResponseWriter, r *http.Request, c factory.OrgCanvas, v Viewer, status int) {
	_, err := h.config.Store.EditCanvases(func(all factory.OrgCanvases) (bool, error) {
		all[c.ID] = c
		return true, nil
	})
	if h.failed(w, err) {
		return
	}
	h.publishCanvas(r.Context(), api.EventCanvasChanged, nil, c, v.Handle)
	h.writeCanvas(w, r, status, c, v)
}

func (h *Handler) patchCanvas(w http.ResponseWriter, r *http.Request) {
	var body api.CanvasPatch
	if !h.readCanvasBody(w, r, 64<<10, &body) {
		return
	}
	verb := factory.RoleEdit
	if body.Trashed != nil {
		verb = factory.RoleFull
	}
	c, v, ok := h.findCanvas(w, r, verb)
	if !ok {
		return
	}
	var ops []map[string]any
	if body.Title != nil {
		if err := plainWords("title", *body.Title, 1, workstreamNameMost); h.workstreamRefused(w, err) {
			return
		}
		ops = append(ops, map[string]any{"t": "meta", "key": "title", "value": strings.TrimSpace(*body.Title)})
	}
	if body.Trashed != nil {
		value := ""
		if *body.Trashed {
			value = h.now().UTC().Format("2006-01-02T15:04:05.000Z")
		}
		ops = append(ops, map[string]any{"t": "meta", "key": "trashed", "value": value})
	}
	if len(ops) > 0 {
		var out api.CanvasOps
		if h.canvasFailed(w, h.rpc(r.Context(), c.ID, map[string]any{"cmd": "apply", "actor": canvasActor(v), "ops": ops}, &out)) {
			return
		}
		c = h.touchCanvas(c, v)
		h.publishCanvasSeq(api.EventCanvasChanged, c, v.Handle, out.Seq)
	}
	h.writeCanvas(w, r, http.StatusOK, c, v)
}

func (h *Handler) deleteCanvas(w http.ResponseWriter, r *http.Request) {
	c, v, ok := h.findCanvas(w, r, factory.RoleFull)
	if !ok {
		return
	}
	_, err := h.config.Store.EditCanvases(func(all factory.OrgCanvases) (bool, error) {
		delete(all, c.ID)
		return true, nil
	})
	if h.failed(w, err) {
		return
	}
	// The record is gone first, so nobody can reach the folder while it is
	// being removed; a folder that cannot be removed is only disk.
	_ = os.RemoveAll(filepath.Join(h.config.Canvas.Root, c.ID))
	h.publishCanvas(r.Context(), api.EventCanvasRemoved, &c, c, v.Handle)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) setCanvasShares(w http.ResponseWriter, r *http.Request) {
	var body api.CanvasSharesPut
	if !h.readCanvasBody(w, r, 64<<10, &body) {
		return
	}
	c, v, ok := h.findCanvas(w, r, factory.RoleFull)
	if !ok {
		return
	}
	before := c
	owner := c.Owner
	if body.Owner != nil {
		var err error
		if owner, err = h.cleanOwner(*body.Owner); h.workstreamRefused(w, err) {
			return
		}
	}
	shares, err := h.cleanShares(body.Shares, owner)
	if h.workstreamRefused(w, err) {
		return
	}
	saved, err := h.config.Store.EditCanvases(func(all factory.OrgCanvases) (bool, error) {
		cur, ok := all[c.ID]
		if !ok {
			return false, os.ErrNotExist
		}
		cur.Owner, cur.Shares, cur.UpdatedBy = owner, shares, v.Handle
		if body.Private != nil {
			cur.Private = *body.Private
		}
		all[c.ID] = cur
		return true, nil
	})
	if h.failed(w, err) {
		return
	}
	c = saved[c.ID]
	h.publishCanvas(r.Context(), api.EventCanvasChanged, &before, c, v.Handle)
	if !can(v, canvasItem(c), factory.RoleView) {
		// They gave it away and kept nothing: it is gone for them.
		h.publishCanvas(r.Context(), api.EventCanvasRemoved, &before, before, v.Handle)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	h.writeCanvas(w, r, http.StatusOK, c, v)
}

// cleanCanvasOps checks a client's batch and stamps it as v: an op that only
// canvas-core or this server may write is refused, and a hand (a lease) is
// always the caller's own. handsOnly is whether every op takes or lets go of
// a hand, which a commenter may do.
func cleanCanvasOps(raw []json.RawMessage, v Viewer, now time.Time) (ops []map[string]any, handsOnly bool, err error) {
	if len(raw) == 0 {
		return nil, false, invalidf("send at least one op")
	}
	if len(raw) > 2000 {
		return nil, false, invalidf("send at most 2000 ops at a time")
	}
	handsOnly = true
	me := canvasActor(v)
	for i, one := range raw {
		var op map[string]any
		if err := json.Unmarshal(one, &op); err != nil || op == nil {
			return nil, false, invalidf("op %d is not a JSON object", i+1)
		}
		t, _ := op["t"].(string)
		switch t {
		case "put", "patch", "move", "del", "token", "version", "mark", "item":
			handsOnly = false
		case "meta":
			handsOnly = false
			key, _ := op["key"].(string)
			if slicesContains(canvasReservedMeta, key) || hasAnyPrefix(key, canvasReservedMetaPrefixes) {
				return nil, false, invalidf("op %d: %q is not a key a person sets on a canvas", i+1, key)
			}
		case "lease":
			if a, _ := op["actor"].(string); a != "" && a != me {
				return nil, false, invalidf("op %d: a hand is your own; leave actor empty", i+1)
			}
			op["actor"] = me
			if until, _ := op["until"].(string); until == "hand" {
				op["until"] = now.Add(canvasHandSecs * time.Second).UTC().Format("2006-01-02T15:04:05.000Z")
			}
		default:
			// member, wait, proposal and suggest are agents' and canvas-core's
			// own: who is on a canvas is its owner and shares, here.
			return nil, false, invalidf("op %d: %q is not an op a person sends", i+1, t)
		}
		ops = append(ops, op)
	}
	return ops, handsOnly, nil
}

func slicesContains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func (h *Handler) applyCanvasOps(w http.ResponseWriter, r *http.Request) {
	var body api.CanvasOpsPost
	if !h.readCanvasBody(w, r, canvasOpsMost, &body) {
		return
	}
	// The role a batch needs depends on what is in it; view is checked
	// first so an unseen canvas answers 404 whatever was sent.
	c, v, ok := h.findCanvas(w, r, factory.RoleView)
	if !ok {
		return
	}
	// A hand's until is read by canvas-core against the wall clock, so it is
	// stamped from the wall clock too, never the server's test clock.
	ops, handsOnly, err := cleanCanvasOps(body.Ops, v, time.Now())
	if h.workstreamRefused(w, err) {
		return
	}
	verb := factory.RoleEdit
	if handsOnly {
		verb = factory.RoleComment
	}
	if !can(v, canvasItem(c), verb) {
		h.refuse(w, http.StatusForbidden, canvasRefusal(verb), "role", "", nil, "")
		return
	}
	if v.Handle == "" {
		h.fail(w, http.StatusForbidden, "sign in to change a canvas")
		return
	}
	var out struct {
		api.CanvasOps
		Conflict *struct {
			Message string            `json:"message"`
			Frame   string            `json:"frame"`
			Holder  string            `json:"holder"`
			Seq     int64             `json:"seq"`
			Ops     []json.RawMessage `json:"ops"`
		} `json:"conflict"`
	}
	if h.canvasFailed(w, h.rpc(r.Context(), c.ID, map[string]any{"cmd": "apply", "actor": canvasActor(v), "base_seq": body.BaseSeq, "ops": ops}, &out)) {
		return
	}
	if k := out.Conflict; k != nil {
		h.write(w, http.StatusConflict, api.CanvasConflict{Error: k.Message, Reason: "conflict", Frame: k.Frame, Holder: k.Holder, Seq: k.Seq, Ops: nonNil(k.Ops), RequestID: requestID()})
		return
	}
	if !handsOnly {
		c = h.touchCanvas(c, v)
	}
	h.publishCanvasSeq(api.EventCanvasChanged, c, v.Handle, out.Seq)
	out.Ops = nonNil(out.Ops)
	h.write(w, http.StatusOK, out.CanvasOps)
}

// touchCanvas records v as the canvas's last writer. A record that cannot be
// saved loses only that name: the change itself is in the canvas.
func (h *Handler) touchCanvas(c factory.OrgCanvas, v Viewer) factory.OrgCanvas {
	if c.UpdatedBy == v.Handle {
		return c
	}
	saved, err := h.config.Store.EditCanvases(func(all factory.OrgCanvases) (bool, error) {
		cur, ok := all[c.ID]
		if !ok {
			return false, nil
		}
		cur.UpdatedBy = v.Handle
		all[c.ID] = cur
		return true, nil
	})
	if err == nil {
		if cur, ok := saved[c.ID]; ok {
			return cur
		}
	}
	return c
}

func (h *Handler) createCanvasVersion(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Label string `json:"label"`
	}
	if r.ContentLength != 0 && !h.readCanvasBody(w, r, 16<<10, &body) {
		return
	}
	c, v, ok := h.findCanvas(w, r, factory.RoleEdit)
	if !ok {
		return
	}
	if err := plainWords("label", body.Label, 0, workstreamNameMost); h.workstreamRefused(w, err) {
		return
	}
	raw, err := h.config.Canvas.Raw(r.Context(), c.ID, map[string]any{"cmd": "version", "actor": canvasActor(v), "label": strings.TrimSpace(body.Label)})
	if h.canvasFailed(w, err) {
		return
	}
	var ver struct {
		Seq int64 `json:"seq"`
	}
	_ = json.Unmarshal(raw, &ver)
	h.publishCanvasSeq(api.EventCanvasChanged, c, v.Handle, ver.Seq)
	h.write(w, http.StatusCreated, raw)
}

func (h *Handler) restoreCanvas(w http.ResponseWriter, r *http.Request) {
	c, v, ok := h.findCanvas(w, r, factory.RoleEdit)
	if !ok {
		return
	}
	var out api.CanvasOps
	if h.canvasFailed(w, h.rpc(r.Context(), c.ID, map[string]any{"cmd": "restore", "actor": canvasActor(v), "version": r.PathValue("vid")}, &out)) {
		return
	}
	c = h.touchCanvas(c, v)
	h.publishCanvasSeq(api.EventCanvasChanged, c, v.Handle, out.Seq)
	out.Ops = nonNil(out.Ops)
	h.write(w, http.StatusOK, out)
}

func (h *Handler) createCanvasMark(w http.ResponseWriter, r *http.Request) {
	var mark struct {
		Frame *string         `json:"frame,omitempty"`
		Node  *string         `json:"node,omitempty"`
		Shape json.RawMessage `json:"shape,omitempty"`
		Text  string          `json:"text"`
	}
	if !h.readCanvasBody(w, r, 256<<10, &mark) {
		return
	}
	c, v, ok := h.findCanvas(w, r, factory.RoleComment)
	if !ok {
		return
	}
	if strings.TrimSpace(mark.Text) == "" {
		h.fail(w, http.StatusBadRequest, "a comment needs words")
		return
	}
	raw, err := h.config.Canvas.Raw(r.Context(), c.ID, map[string]any{"cmd": "mark", "actor": canvasActor(v), "mark": mark})
	if h.canvasFailed(w, err) {
		return
	}
	h.publishCanvasSeq(api.EventCanvasChanged, c, v.Handle, 0)
	h.write(w, http.StatusCreated, raw)
}

func (h *Handler) putCanvasAsset(w http.ResponseWriter, r *http.Request) {
	c, v, ok := h.findCanvas(w, r, factory.RoleEdit)
	if !ok {
		return
	}
	sha := r.PathValue("sha")
	if !canvasSha.MatchString(sha) {
		h.fail(w, http.StatusBadRequest, "an asset is named by the sha256 of its bytes, hex")
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, canvasAssetMost))
	if err != nil {
		h.fail(w, http.StatusRequestEntityTooLarge, "an asset is at most 25 MB")
		return
	}
	mimeType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	raw, err := h.storeCanvasAsset(r.Context(), c.ID, api.CanvasAsset{Sha: sha, Data: data, Type: mimeType, Name: r.URL.Query().Get("name")})
	if h.canvasFailed(w, err) {
		return
	}
	h.publishCanvasSeq(api.EventCanvasChanged, c, v.Handle, 0)
	h.write(w, http.StatusOK, raw)
}

// storeCanvasAsset hands a's bytes to canvas-core through a file in the
// canvas root (rpc takes paths, never bytes), checked against its sha.
func (h *Handler) storeCanvasAsset(ctx context.Context, id string, a api.CanvasAsset) (json.RawMessage, error) {
	path, done, err := h.canvasTemp(a)
	if err != nil {
		return nil, err
	}
	defer done()
	return h.config.Canvas.Raw(ctx, id, map[string]any{"cmd": "asset_put", "path": path, "sha": a.Sha, "name": a.Name, "mime": a.Type})
}

// canvasTemp writes a's bytes to a file canvas-core can read, after checking
// they are what a.Sha says. done removes it.
func (h *Handler) canvasTemp(a api.CanvasAsset) (string, func(), error) {
	sum := sha256.Sum256(a.Data)
	if a.Sha != "" && hex.EncodeToString(sum[:]) != a.Sha {
		return "", nil, &canvasrpc.Error{Code: "invalid", Message: "the asset's bytes do not hash to its sha; send it again"}
	}
	dir := filepath.Join(h.config.Canvas.Root, ".incoming")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", nil, err
	}
	f, err := os.CreateTemp(dir, "asset-*")
	if err != nil {
		return "", nil, err
	}
	_, werr := f.Write(a.Data)
	cerr := f.Close()
	done := func() { _ = os.Remove(f.Name()) }
	if werr != nil || cerr != nil {
		done()
		return "", nil, errors.Join(werr, cerr)
	}
	return f.Name(), done, nil
}

// importCanvas moves a Personal canvas in: canvas-core makes a new canvas of
// its snapshot, written by the importer, with its assets stored first. The
// same laptop canvas moved twice by the same person answers the first.
func (h *Handler) importCanvas(w http.ResponseWriter, r *http.Request) {
	v, err := h.itemViewer(r)
	if h.failed(w, err) {
		return
	}
	if v.Handle == "" {
		h.fail(w, http.StatusForbidden, "sign in to move a canvas in")
		return
	}
	var body api.CanvasImport
	if !h.readCanvasBody(w, r, canvasImportMost, &body) {
		return
	}
	local := strings.TrimSpace(body.FromLocalID)
	if local == "" || len(local) > 128 {
		h.fail(w, http.StatusBadRequest, "from_local_id is the canvas's id on your computer")
		return
	}
	if len(body.Snapshot) == 0 || body.Snapshot[0] != '{' {
		h.fail(w, http.StatusBadRequest, "snapshot is the canvas as canvas-core shows it")
		return
	}
	if body.Title != "" {
		if err := plainWords("title", body.Title, 1, workstreamNameMost); h.workstreamRefused(w, err) {
			return
		}
	}
	from := v.Handle + "/" + local
	all, err := h.config.Store.Canvases()
	if h.failed(w, err) {
		return
	}
	for _, c := range all {
		if c.FromLocal == from && can(v, canvasItem(c), factory.RoleView) {
			h.writeCanvas(w, r, http.StatusOK, c, v)
			return
		}
	}
	c, ok := h.newCanvasRecord(w, v, body.Owner, body.Shares, body.Private)
	if !ok {
		return
	}
	c.FromLocal = from
	var assets []map[string]any
	for _, a := range body.Assets {
		if !canvasSha.MatchString(a.Sha) || len(a.Data) > canvasAssetMost {
			h.fail(w, http.StatusBadRequest, "each asset is its sha256 and at most 25 MB of bytes")
			return
		}
		path, done, err := h.canvasTemp(a)
		if h.canvasFailed(w, err) {
			return
		}
		defer done()
		assets = append(assets, map[string]any{"path": path, "name": a.Name, "mime": a.Type})
	}
	var created struct {
		ID string `json:"id"`
	}
	req := map[string]any{"cmd": "import", "actor": canvasActor(v), "title": strings.TrimSpace(body.Title), "snapshot": body.Snapshot, "assets": assets}
	if h.canvasFailed(w, h.rpc(r.Context(), "", req, &created)) {
		return
	}
	c.ID = created.ID
	h.saveNewCanvas(w, r, c, v, http.StatusCreated)
}

// ── events ──

// canvasLive is a canvas event as kept: what is sent, and who may hear it.
type canvasLive struct {
	api.CanvasEvent
	itemAudience
}

// publishCanvas records one canvas event at its current seq. before, when
// given, joins its audience, so someone a change took the canvas from still
// hears to drop it.
func (h *Handler) publishCanvas(ctx context.Context, kind string, before *factory.OrgCanvas, after factory.OrgCanvas, by string) {
	var seq int64
	if kind != api.EventCanvasRemoved {
		var s struct {
			Seq int64 `json:"seq"`
		}
		if h.config.Canvas.Call(ctx, after.ID, map[string]any{"cmd": "summary"}, &s) == nil {
			seq = s.Seq
		}
	}
	items := []Item{canvasItem(after)}
	if before != nil {
		items = append(items, canvasItem(*before))
	}
	e := canvasLive{CanvasEvent: api.CanvasEvent{Kind: kind, ID: after.ID, Seq: seq, By: by}, itemAudience: audienceOf(items...)}
	_ = live.Append(h.eventRoot(), live.New(kind, 0, e))
}

// publishCanvasSeq is publishCanvas for a write that already knows its seq
// (0 reads it).
func (h *Handler) publishCanvasSeq(kind string, c factory.OrgCanvas, by string, seq int64) {
	if seq == 0 {
		h.publishCanvas(context.Background(), kind, nil, c, by)
		return
	}
	e := canvasLive{CanvasEvent: api.CanvasEvent{Kind: kind, ID: c.ID, Seq: seq, By: by}, itemAudience: audienceOf(canvasItem(c))}
	_ = live.Append(h.eventRoot(), live.New(kind, 0, e))
}

// canvasEventForViewer is a canvas event when this caller could view the
// canvas before or after the change, sent without its audience.
func (h *Handler) canvasEventForViewer(r *http.Request, e live.Event) (live.Event, bool) {
	var stored canvasLive
	if json.Unmarshal(e.Data, &stored) != nil || stored.ID == "" {
		return e, false
	}
	v, err := h.itemViewer(r)
	if err != nil || !v.hears(stored.itemAudience) {
		return e, false
	}
	data, err := json.Marshal(stored.CanvasEvent)
	if err != nil {
		return e, false
	}
	e.Data = data
	return e, true
}
