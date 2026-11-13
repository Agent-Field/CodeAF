package serve

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// ── A WORKSTREAM'S PARTS ───────────────────────────────────────────────────
//
// Every part of an organization workstream has its own route
// (api/workstream_parts.go has the wire), so two people changing two different
// parts never refuse each other: a page carries its own rev, as do the plan,
// each wake and each suggestion. The rules are the workstream's: you read a
// part of one you can see, and you change a part of one you can write and that
// is not archived. Every author and time is the server's.
//
// A PAGE'S BODY IS NOT A PART HERE. It is a live document others type into at
// the same moment (/v1/docs/ws/<id>/page/<pid>); these routes keep where a
// page sits, its title and kind, and the versions clients save of it. A
// restore is answered with the words and state of the new version, and the
// restorer's editor applies them to the live document so everyone sees it.

// The most a part may hold, so one save cannot fill what every read decodes.
const (
	partDocMost     = 256 << 10
	partListMost    = 500
	pageStateMost   = 512 << 10
	spendOneMostUSD = 10_000
)

var (
	errPageStale    = errors.New("this page changed since you read it; read it again, then make the change")
	errPageGone     = errors.New("page not found")
	errPartNotJSON  = workstreamInvalid("a part is one JSON object")
	errPartTooLarge = workstreamInvalid("a part may hold at most 256 KiB")
)

func (h *Handler) workstreamPartRoutes() {
	h.route("/v1/workstreams/{id}/pages", map[string]http.HandlerFunc{http.MethodPost: h.createWorkstreamPage})
	h.route("/v1/workstreams/{id}/pages/{pid}", map[string]http.HandlerFunc{http.MethodPatch: h.patchWorkstreamPage, http.MethodDelete: h.deleteWorkstreamPage})
	h.route("/v1/workstreams/{id}/pages/{pid}/versions", map[string]http.HandlerFunc{http.MethodGet: h.listPageVersions, http.MethodPost: h.savePageVersion})
	h.route("/v1/workstreams/{id}/pages/{pid}/versions/{version}", map[string]http.HandlerFunc{http.MethodGet: h.getPageVersion})
	h.route("/v1/workstreams/{id}/pages/{pid}/restore", map[string]http.HandlerFunc{http.MethodPost: h.restorePageVersion})
	h.route("/v1/workstreams/{id}/plan", map[string]http.HandlerFunc{http.MethodGet: h.getWorkstreamPlan, http.MethodPut: h.putWorkstreamPlan})
	for _, k := range []partKind{wakesPart, suggestionsPart} {
		h.route("/v1/workstreams/{id}/"+k.part, map[string]http.HandlerFunc{http.MethodGet: h.listParts(k), http.MethodPost: h.addPart(k)})
		h.route("/v1/workstreams/{id}/"+k.part+"/{pid}", map[string]http.HandlerFunc{http.MethodPatch: h.patchPart(k)})
	}
	h.route("/v1/workstreams/{id}/jobs/{job}", map[string]http.HandlerFunc{http.MethodPut: h.addWorkstreamJob, http.MethodDelete: h.removeWorkstreamJob})
	h.route("/v1/workstreams/{id}/unarchive", map[string]http.HandlerFunc{http.MethodPost: h.unarchiveWorkstream})
	h.route("/v1/workstreams/{id}/spend", map[string]http.HandlerFunc{http.MethodPost: h.postWorkstreamSpend})
}

// ── the shared steps ──

// writablePart is the workstream the path names when the caller may change
// its parts; otherwise the refusal is written and ok is false.
func (h *Handler) writablePart(w http.ResponseWriter, r *http.Request) (factory.OrgWorkstream, wsViewer, bool) {
	ws, v, ok := h.findWorkstream(w, r)
	if !ok {
		return ws, v, false
	}
	if !v.canWrite(ws) {
		h.fail(w, http.StatusForbidden, errWorkstreamNoWrite.Error())
		return ws, v, false
	}
	if ws.ArchivedAt != nil {
		h.fail(w, http.StatusConflict, errWorkstreamArchived.Error())
		return ws, v, false
	}
	return ws, v, true
}

// editPart runs change on the one workstream id under the store's lock, after
// checking again that the caller may write it and that it is live. change says
// whether it changed anything; the workstream's rev counts up only then.
func (h *Handler) editPart(id string, v wsViewer, change func(cur *factory.OrgWorkstream, all factory.OrgWorkstreams) (bool, error)) (before, after factory.OrgWorkstream, err error) {
	saved, err := h.config.Store.EditWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
		cur, ok := all[id]
		if !ok {
			return nil, errWorkstreamGone
		}
		if !v.canWrite(cur) {
			return nil, errWorkstreamNoWrite
		}
		if cur.ArchivedAt != nil {
			return nil, errWorkstreamArchived
		}
		before = cur
		cur.Pages = slices.Clone(cur.Pages)
		cur.Jobs = slices.Clone(cur.Jobs)
		changed, err := change(&cur, all)
		if err != nil || !changed {
			return nil, err
		}
		all[id] = cur
		return []string{id}, nil
	})
	if err != nil {
		return before, before, err
	}
	return before, saved[id], nil
}

// partRefused writes the refusal a part change ended with.
func (h *Handler) partRefused(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, errPageStale), errors.Is(err, store.ErrPartStale):
		h.fail(w, http.StatusPreconditionFailed, err.Error())
	case errors.Is(err, errPageGone), errors.Is(err, store.ErrPartGone):
		h.fail(w, http.StatusNotFound, err.Error())
	default:
		return h.workstreamRefused(w, err)
	}
	return true
}

// ifMatch is the rev a guarded write was read at; ok is false, with 428
// written, when it carries none.
func (h *Handler) ifMatch(w http.ResponseWriter, r *http.Request) (int, bool) {
	rev, err := strconv.Atoi(strings.Trim(strings.TrimSpace(r.Header.Get("If-Match")), `"`))
	if err != nil || rev < 0 {
		h.fail(w, http.StatusPreconditionRequired, "If-Match must carry the rev you read")
		return 0, false
	}
	return rev, true
}

// publishWorkstreamPart records one change to one part of ws, heard by
// everyone who can see the workstream.
func (h *Handler) publishWorkstreamPart(ws factory.OrgWorkstream, part, partID, by string, rev int, deleted bool) {
	e := workstreamLive{WorkstreamEvent: api.WorkstreamEvent{
		Kind: api.EventWorkstreamChanged, ID: ws.ID, Part: part, PartID: partID, By: by, Rev: rev, Deleted: deleted,
	}, itemAudience: audienceOf(workstreamItem(ws))}
	_ = live.Append(h.eventRoot(), live.New(api.EventWorkstreamChanged, 0, e))
}

// ── pages: place, title, kind ──

// createWorkstreamPage adds an empty page; its words are typed into its live
// document.
func (h *Handler) createWorkstreamPage(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.writablePart(w, r)
	if !ok {
		return
	}
	var body api.PageNew
	if !h.decode(w, r, &body, false) {
		return
	}
	now := h.clock().UTC()
	id, err := cell.NewID(now)
	if h.failed(w, err) {
		return
	}
	page := factory.WorkstreamPage{
		ID: "pg_" + id, Title: strings.TrimSpace(body.Title), Kind: body.Kind, Parent: body.Parent,
		UpdatedAt: now.Format(time.RFC3339Nano), UpdatedBy: v.Handle, Rev: 1,
	}
	_, saved, err := h.editPart(ws.ID, v, func(cur *factory.OrgWorkstream, _ factory.OrgWorkstreams) (bool, error) {
		at := len(cur.Pages)
		if body.Index != nil {
			at = max(0, min(*body.Index, at))
		}
		cur.Pages = slices.Insert(cur.Pages, at, page)
		cur.UpdatedAt = now
		return true, cleanWorkstream(cur, v.Handle, now)
	})
	if h.partRefused(w, err) {
		return
	}
	h.recordWorkstream(r, ws.ID, "added page", page.ID)
	h.publishWorkstreamPart(saved, api.PartPage, page.ID, h.orgActor(r), 1, false)
	h.writeWorkstream(w, http.StatusCreated, saved)
}

// patchWorkstreamPage renames, re-kinds or moves one page. IF-MATCH IS THE
// PAGE'S REV, not the workstream's: someone renaming another page meanwhile
// does not refuse this one.
func (h *Handler) patchWorkstreamPage(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.writablePart(w, r)
	if !ok {
		return
	}
	rev, ok := h.ifMatch(w, r)
	if !ok {
		return
	}
	var body api.PagePatch
	if !h.decode(w, r, &body, false) {
		return
	}
	pid := r.PathValue("pid")
	now := h.clock().UTC()
	var pageRev int
	_, saved, err := h.editPart(ws.ID, v, func(cur *factory.OrgWorkstream, _ factory.OrgWorkstreams) (bool, error) {
		at := slices.IndexFunc(cur.Pages, func(p factory.WorkstreamPage) bool { return p.ID == pid })
		if at < 0 {
			return false, errPageGone
		}
		p := cur.Pages[at]
		if p.Rev != rev {
			return false, errPageStale
		}
		if body.Title != nil {
			p.Title = strings.TrimSpace(*body.Title)
		}
		if body.Kind != nil {
			p.Kind = *body.Kind
		}
		if body.Parent != nil {
			if *body.Parent != "" && descends(cur.Pages, *body.Parent, pid) {
				return false, invalidf("a page cannot sit under itself or one of its own pages")
			}
			p.Parent = *body.Parent
		}
		p.Rev++
		p.UpdatedAt, p.UpdatedBy = now.Format(time.RFC3339Nano), v.Handle
		pageRev = p.Rev
		cur.Pages[at] = p
		if body.Index != nil {
			cur.Pages = slices.Delete(cur.Pages, at, at+1)
			to := max(0, min(*body.Index, len(cur.Pages)))
			cur.Pages = slices.Insert(cur.Pages, to, p)
		}
		cur.UpdatedAt = now
		return true, cleanWorkstream(cur, v.Handle, now)
	})
	if h.partRefused(w, err) {
		return
	}
	verb := "changed page"
	switch {
	case body.Title != nil:
		verb = "renamed page"
	case body.Parent != nil || body.Index != nil:
		verb = "moved page"
	}
	h.recordWorkstream(r, ws.ID, verb, pid)
	h.publishWorkstreamPart(saved, api.PartPage, pid, h.orgActor(r), pageRev, false)
	h.writeWorkstream(w, http.StatusOK, saved)
}

// descends is whether page is pid or sits somewhere under it.
func descends(pages []factory.WorkstreamPage, page, pid string) bool {
	parent := map[string]string{}
	for _, p := range pages {
		parent[p.ID] = p.Parent
	}
	for seen := 0; page != "" && seen <= len(pages); seen++ {
		if page == pid {
			return true
		}
		page = parent[page]
	}
	return false
}

// liveDocs is the live-document relay's half that a page deletion reaches:
// the document's log goes with the page. A server built without the relay
// has no documents to drop.
type liveDocs interface{ dropDoc(doc string) error }

// deleteWorkstreamPage removes one page. The pages under it move up to its
// parent rather than going with it, its versions are let go, and its live
// document is dropped.
func (h *Handler) deleteWorkstreamPage(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.writablePart(w, r)
	if !ok {
		return
	}
	pid := r.PathValue("pid")
	now := h.clock().UTC()
	_, saved, err := h.editPart(ws.ID, v, func(cur *factory.OrgWorkstream, _ factory.OrgWorkstreams) (bool, error) {
		at := slices.IndexFunc(cur.Pages, func(p factory.WorkstreamPage) bool { return p.ID == pid })
		if at < 0 {
			return false, errPageGone
		}
		gone := cur.Pages[at]
		cur.Pages = slices.Delete(cur.Pages, at, at+1)
		for i := range cur.Pages {
			if cur.Pages[i].Parent == pid {
				cur.Pages[i].Parent = gone.Parent
				cur.Pages[i].Rev++
			}
		}
		cur.UpdatedAt = now
		return true, nil
	})
	if h.partRefused(w, err) {
		return
	}
	// The page is gone whatever happens next; versions or a document left
	// behind are unreachable, never wrong.
	_ = h.config.Store.DropPageVersions(ws.ID, pid)
	if docs, ok := any(h).(liveDocs); ok {
		_ = docs.dropDoc("ws/" + ws.ID + "/page/" + pid)
	}
	h.recordWorkstream(r, ws.ID, "removed page", pid)
	h.publishWorkstreamPart(saved, api.PartPage, pid, h.orgActor(r), 0, true)
	h.writeWorkstream(w, http.StatusOK, saved)
}

// ── page versions ──

// pageOf is the page pid of the workstream the path names, readable by the
// caller; otherwise the refusal is written.
func (h *Handler) pageOf(w http.ResponseWriter, r *http.Request, write bool) (factory.OrgWorkstream, factory.WorkstreamPage, wsViewer, bool) {
	var ws factory.OrgWorkstream
	var v wsViewer
	var ok bool
	if write {
		ws, v, ok = h.writablePart(w, r)
	} else {
		ws, v, ok = h.findWorkstream(w, r)
	}
	if !ok {
		return ws, factory.WorkstreamPage{}, v, false
	}
	pid := r.PathValue("pid")
	at := slices.IndexFunc(ws.Pages, func(p factory.WorkstreamPage) bool { return p.ID == pid })
	if at < 0 {
		h.fail(w, http.StatusNotFound, errPageGone.Error())
		return ws, factory.WorkstreamPage{}, v, false
	}
	return ws, ws.Pages[at], v, true
}

func versionWire(v store.PageVersion) api.PageVersion {
	return api.PageVersion{
		Version: v.Version, At: v.At, By: v.By, Title: v.Title, Reason: v.Reason,
		RestoredFrom: v.RestoredFrom, Bytes: v.Bytes, BodyMD: v.BodyMD, State: v.State,
	}
}

func (h *Handler) listPageVersions(w http.ResponseWriter, r *http.Request) {
	ws, page, _, ok := h.pageOf(w, r, false)
	if !ok {
		return
	}
	list, err := h.config.Store.PageVersions(ws.ID, page.ID)
	if h.failed(w, err) {
		return
	}
	out := api.PageVersions{Versions: make([]api.PageVersion, 0, len(list))}
	for _, v := range list {
		out.Versions = append(out.Versions, versionWire(v))
	}
	h.write(w, http.StatusOK, out)
}

func (h *Handler) getPageVersion(w http.ResponseWriter, r *http.Request) {
	ws, page, _, ok := h.pageOf(w, r, false)
	if !ok {
		return
	}
	n, err := strconv.Atoi(r.PathValue("version"))
	if err != nil {
		h.fail(w, http.StatusNotFound, "version not found")
		return
	}
	v, found, err := h.config.Store.PageVersionAt(ws.ID, page.ID, n)
	if h.failed(w, err) {
		return
	}
	if !found {
		h.fail(w, http.StatusNotFound, "version not found")
		return
	}
	h.write(w, http.StatusOK, versionWire(v))
}

// savePageVersion keeps a version a client took of the live document: after
// thirty seconds without a change ("idle") or when a person asked
// ("manual"). The server never parses the state. The same words twice are
// kept once, answered 200 with the version already kept.
func (h *Handler) savePageVersion(w http.ResponseWriter, r *http.Request) {
	ws, page, v, ok := h.pageOf(w, r, true)
	if !ok {
		return
	}
	var body api.PageVersionNew
	if !h.decode(w, r, &body, false) {
		return
	}
	if body.Reason != "idle" && body.Reason != "manual" {
		h.fail(w, http.StatusUnprocessableEntity, `reason must be "idle" or "manual"`)
		return
	}
	if len(body.BodyMD) > workstreamBodyMost || len(body.State) > pageStateMost {
		h.fail(w, http.StatusUnprocessableEntity, "a version may hold at most 256 KiB of words and 512 KiB of state")
		return
	}
	h.keepVersion(w, r, ws, page, v, store.PageVersion{BodyMD: body.BodyMD, State: body.State, Reason: body.Reason})
}

// restorePageVersion brings an older version back as the newest one, by the
// caller. The answer carries its words and state, which the caller's editor
// applies to the live document so everyone in it sees the restore.
func (h *Handler) restorePageVersion(w http.ResponseWriter, r *http.Request) {
	ws, page, v, ok := h.pageOf(w, r, true)
	if !ok {
		return
	}
	var body api.PageRestore
	if !h.decode(w, r, &body, false) {
		return
	}
	old, found, err := h.config.Store.PageVersionAt(ws.ID, page.ID, body.Version)
	if h.failed(w, err) {
		return
	}
	if !found {
		h.fail(w, http.StatusNotFound, "version not found")
		return
	}
	h.keepVersion(w, r, ws, page, v, store.PageVersion{BodyMD: old.BodyMD, State: old.State, Reason: "restore", RestoredFrom: old.Version})
}

// keepVersion saves one version by the caller, refreshes the page's
// plain-text mirror (what search, export and agents read) without moving any
// rev, and answers the version whole. A version saved on idle is not recorded
// (one arrives every thirty seconds anyone types); a restore is.
func (h *Handler) keepVersion(w http.ResponseWriter, r *http.Request, ws factory.OrgWorkstream, page factory.WorkstreamPage, v wsViewer, ver store.PageVersion) {
	now := h.clock().UTC()
	ver.At, ver.By, ver.Title = now.Format(time.RFC3339Nano), v.Handle, page.Title
	saved, added, err := h.config.Store.AddPageVersion(ws.ID, page.ID, ver)
	if h.failed(w, err) {
		return
	}
	if !added {
		h.write(w, http.StatusOK, versionWire(saved))
		return
	}
	after, err := h.config.Store.MirrorWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
		cur, ok := all[ws.ID]
		if !ok {
			return nil, nil
		}
		at := slices.IndexFunc(cur.Pages, func(p factory.WorkstreamPage) bool { return p.ID == page.ID })
		if at < 0 || cur.Pages[at].BodyMD == ver.BodyMD {
			return nil, nil
		}
		cur.Pages = slices.Clone(cur.Pages)
		cur.Pages[at].BodyMD = ver.BodyMD
		cur.Pages[at].UpdatedAt, cur.Pages[at].UpdatedBy = ver.At, v.Handle
		all[ws.ID] = cur
		return []string{ws.ID}, nil
	})
	if h.failed(w, err) {
		return
	}
	if cur, ok := after[ws.ID]; ok {
		ws = cur
	}
	if ver.RestoredFrom != 0 {
		h.recordWorkstream(r, ws.ID, "restored page", page.ID+" version "+strconv.Itoa(ver.RestoredFrom))
	}
	h.publishWorkstreamPart(ws, api.PartVersions, page.ID, h.orgActor(r), saved.Version, false)
	h.write(w, http.StatusCreated, versionWire(saved))
}

// ── plan ──

func planWire(p store.Part, ok bool) api.WorkstreamPlan {
	if !ok {
		return api.WorkstreamPlan{Plan: json.RawMessage("null")}
	}
	return api.WorkstreamPlan{Plan: p.Doc, Rev: p.Rev, UpdatedAt: p.UpdatedAt, UpdatedBy: p.UpdatedBy}
}

func (h *Handler) getWorkstreamPlan(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := h.findWorkstream(w, r)
	if !ok {
		return
	}
	p, found, err := h.config.Store.PartAt(ws.ID, store.PartPlan, store.PartPlan)
	if h.failed(w, err) {
		return
	}
	w.Header().Set("ETag", strconv.Itoa(p.Rev))
	h.write(w, http.StatusOK, planWire(p, found))
}

// putWorkstreamPlan replaces the plan. If-Match is the plan's rev, 0 for the
// first one.
func (h *Handler) putWorkstreamPlan(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.writablePart(w, r)
	if !ok {
		return
	}
	rev, ok := h.ifMatch(w, r)
	if !ok {
		return
	}
	var body api.WorkstreamPlanPut
	if !h.decode(w, r, &body, false) {
		return
	}
	doc, err := partObject(body.Plan)
	if h.partRefused(w, err) {
		return
	}
	p, err := h.config.Store.PutPart(ws.ID, store.PartPlan, store.PartPlan, rev, v.Handle, h.clock(), doc)
	if h.partRefused(w, err) {
		return
	}
	h.recordWorkstream(r, ws.ID, "changed plan", "")
	h.publishWorkstreamPart(ws, api.PartPlan, store.PartPlan, h.orgActor(r), p.Rev, false)
	w.Header().Set("ETag", strconv.Itoa(p.Rev))
	h.write(w, http.StatusOK, planWire(p, true))
}

// ── wakes and suggestions ──

// partKind is one list of desk-shaped parts kept beside a workstream.
type partKind struct {
	part, prefix, list, one string
}

var (
	wakesPart       = partKind{part: store.PartWakes, prefix: "wk_", list: "wakes", one: "wake"}
	suggestionsPart = partKind{part: store.PartSuggestions, prefix: "sg_", list: "suggestions", one: "suggestion"}
)

// partStampKeys are the fields the server writes over a part's body.
var partStampKeys = []string{"id", "workstream_id", "rev", "by", "at", "updated_by", "updated_at"}

// partObject is raw checked as one JSON object of bounded size, less the
// fields the server stamps.
func partObject(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) > partDocMost {
		return nil, errPartTooLarge
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return nil, errPartNotJSON
	}
	for _, k := range partStampKeys {
		delete(obj, k)
	}
	return json.Marshal(obj)
}

// partWire is a part as the desk reads it: its own object with the server's
// stamp on top.
func partWire(ws string, p store.Part) json.RawMessage {
	var obj map[string]any
	if json.Unmarshal(p.Doc, &obj) != nil || obj == nil {
		obj = map[string]any{}
	}
	obj["id"], obj["workstream_id"], obj["rev"] = p.ID, ws, p.Rev
	obj["by"], obj["at"], obj["updated_by"], obj["updated_at"] = p.By, p.At, p.UpdatedBy, p.UpdatedAt
	out, _ := json.Marshal(obj)
	return out
}

func (h *Handler) listParts(k partKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ws, _, ok := h.findWorkstream(w, r)
		if !ok {
			return
		}
		parts, err := h.config.Store.Parts(ws.ID, k.part)
		if h.failed(w, err) {
			return
		}
		page := r.URL.Query().Get("page")
		out := []json.RawMessage{}
		for _, p := range parts {
			if page != "" {
				var on struct {
					PageID string `json:"page_id"`
				}
				if json.Unmarshal(p.Doc, &on) != nil || on.PageID != page {
					continue
				}
			}
			out = append(out, partWire(ws.ID, p))
		}
		h.write(w, http.StatusOK, map[string][]json.RawMessage{k.list: out})
	}
}

func (h *Handler) addPart(k partKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ws, v, ok := h.writablePart(w, r)
		if !ok {
			return
		}
		var raw json.RawMessage
		if !h.decode(w, r, &raw, false) {
			return
		}
		doc, err := partObject(raw)
		if h.partRefused(w, err) {
			return
		}
		kept, err := h.config.Store.Parts(ws.ID, k.part)
		if h.failed(w, err) {
			return
		}
		if len(kept) >= partListMost {
			h.fail(w, http.StatusUnprocessableEntity, "a workstream keeps at most 500 of these")
			return
		}
		now := h.clock()
		id, err := cell.NewID(now)
		if h.failed(w, err) {
			return
		}
		p, err := h.config.Store.PutPart(ws.ID, k.part, k.prefix+id, 0, v.Handle, now, doc)
		if h.partRefused(w, err) {
			return
		}
		h.recordWorkstream(r, ws.ID, "added "+k.one, p.ID)
		h.publishWorkstreamPart(ws, k.part, p.ID, h.orgActor(r), p.Rev, false)
		h.write(w, http.StatusCreated, partWire(ws.ID, p))
	}
}

// patchPart merges the body into one part, one level deep: a key sent
// replaces the one kept, and null removes it. If-Match is the part's rev.
func (h *Handler) patchPart(k partKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ws, v, ok := h.writablePart(w, r)
		if !ok {
			return
		}
		rev, ok := h.ifMatch(w, r)
		if !ok {
			return
		}
		var patch map[string]json.RawMessage
		if !h.decode(w, r, &patch, false) {
			return
		}
		cur, found, err := h.config.Store.PartAt(ws.ID, k.part, r.PathValue("pid"))
		if h.failed(w, err) {
			return
		}
		if !found {
			h.fail(w, http.StatusNotFound, store.ErrPartGone.Error())
			return
		}
		var obj map[string]json.RawMessage
		if json.Unmarshal(cur.Doc, &obj) != nil || obj == nil {
			obj = map[string]json.RawMessage{}
		}
		for key, val := range patch {
			if string(val) == "null" {
				delete(obj, key)
			} else {
				obj[key] = val
			}
		}
		merged, _ := json.Marshal(obj)
		doc, err := partObject(merged)
		if h.partRefused(w, err) {
			return
		}
		p, err := h.config.Store.PutPart(ws.ID, k.part, cur.ID, rev, v.Handle, h.clock(), doc)
		if h.partRefused(w, err) {
			return
		}
		h.recordWorkstream(r, ws.ID, "changed "+k.one, p.ID)
		h.publishWorkstreamPart(ws, k.part, p.ID, h.orgActor(r), p.Rev, false)
		h.write(w, http.StatusOK, partWire(ws.ID, p))
	}
}

// ── jobs ──

// jobOf is the factory job the path names when the caller can see it.
func (h *Handler) jobOf(w http.ResponseWriter, r *http.Request) (int, bool) {
	n, err := strconv.Atoi(r.PathValue("job"))
	if err != nil || n < 1 {
		h.fail(w, http.StatusNotFound, "job not found")
		return 0, false
	}
	it, err := h.config.Store.Get(n)
	if err != nil || !h.repoVisible(r, itemRepo(it)) || !h.jobVisible(r, it) {
		h.fail(w, http.StatusNotFound, "job not found")
		return 0, false
	}
	return n, true
}

// addWorkstreamJob puts a job the caller can see in the workstream. A job is
// in at most one workstream, so it leaves any other, which the caller must be
// able to write too; that one changes as well and says so.
func (h *Handler) addWorkstreamJob(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.writablePart(w, r)
	if !ok {
		return
	}
	job, ok := h.jobOf(w, r)
	if !ok {
		return
	}
	now := h.clock().UTC()
	var left []string
	saved, err := h.config.Store.EditWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
		cur, ok := all[ws.ID]
		if !ok {
			return nil, errWorkstreamGone
		}
		if !v.canWrite(cur) {
			return nil, errWorkstreamNoWrite
		}
		if cur.ArchivedAt != nil {
			return nil, errWorkstreamArchived
		}
		if slices.Contains(cur.Jobs, job) {
			return nil, nil
		}
		for id, other := range all {
			if id != ws.ID && slices.Contains(other.Jobs, job) {
				// Taking a job out of a workstream is a change to it, so
				// one you can only read keeps its job.
				if !v.canWrite(other) {
					return nil, errWorkstreamNoWrite
				}
				other.Jobs = slices.DeleteFunc(slices.Clone(other.Jobs), func(j int) bool { return j == job })
				other.UpdatedAt = now
				all[id] = other
				left = append(left, id)
			}
		}
		if len(cur.Jobs) >= workstreamListMost {
			return nil, invalidf("a workstream keeps at most %d jobs", workstreamListMost)
		}
		cur.Jobs = append(slices.Clone(cur.Jobs), job)
		cur.UpdatedAt = now
		all[ws.ID] = cur
		return append(left, ws.ID), nil
	})
	if h.partRefused(w, err) {
		return
	}
	jobID, by := strconv.Itoa(job), h.orgActor(r)
	for _, id := range left {
		h.recordWorkstream(r, id, "removed job", "job "+jobID)
		h.publishWorkstreamPart(saved[id], api.PartJobs, jobID, by, saved[id].Rev, true)
	}
	if len(left) > 0 || saved[ws.ID].Rev != ws.Rev {
		h.recordWorkstream(r, ws.ID, "added job", "job "+jobID)
		h.publishWorkstreamPart(saved[ws.ID], api.PartJobs, jobID, by, saved[ws.ID].Rev, false)
	}
	h.writeWorkstream(w, http.StatusOK, saved[ws.ID])
}

func (h *Handler) removeWorkstreamJob(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.writablePart(w, r)
	if !ok {
		return
	}
	job, err := strconv.Atoi(r.PathValue("job"))
	if err != nil {
		h.fail(w, http.StatusNotFound, "job not found")
		return
	}
	now := h.clock().UTC()
	before, saved, err := h.editPart(ws.ID, v, func(cur *factory.OrgWorkstream, _ factory.OrgWorkstreams) (bool, error) {
		if !slices.Contains(cur.Jobs, job) {
			return false, nil
		}
		cur.Jobs = slices.DeleteFunc(cur.Jobs, func(j int) bool { return j == job })
		cur.UpdatedAt = now
		return true, nil
	})
	if h.partRefused(w, err) {
		return
	}
	if saved.Rev != before.Rev {
		h.recordWorkstream(r, ws.ID, "removed job", "job "+strconv.Itoa(job))
		h.publishWorkstreamPart(saved, api.PartJobs, strconv.Itoa(job), h.orgActor(r), saved.Rev, true)
	}
	h.writeWorkstream(w, http.StatusOK, saved)
}

// ── unarchive ──

// unarchiveWorkstream brings an archived workstream back to every list. One
// that is not archived is answered as it is.
func (h *Handler) unarchiveWorkstream(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.findWorkstream(w, r)
	if !ok {
		return
	}
	if !v.canWrite(ws) {
		h.fail(w, http.StatusForbidden, errWorkstreamNoWrite.Error())
		return
	}
	now := h.clock().UTC()
	var before factory.OrgWorkstream
	saved, err := h.config.Store.EditWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
		cur, ok := all[ws.ID]
		if !ok {
			return nil, errWorkstreamGone
		}
		if !v.canWrite(cur) {
			return nil, errWorkstreamNoWrite
		}
		before = cur
		if cur.ArchivedAt == nil {
			return nil, nil
		}
		cur.ArchivedAt, cur.UpdatedAt = nil, now
		all[cur.ID] = cur
		return []string{cur.ID}, nil
	})
	if h.workstreamRefused(w, err) {
		return
	}
	if saved[ws.ID].Rev != before.Rev {
		h.recordWorkstream(r, ws.ID, "unarchived workstream", "")
		h.publishWorkstreamPart(saved[ws.ID], api.PartWorkstream, ws.ID, h.orgActor(r), saved[ws.ID].Rev, false)
	}
	h.writeWorkstream(w, http.StatusOK, saved[ws.ID])
}

// ── budget and spend ──

// budgetOf is a budget as kept: a negative one clears it.
func budgetOf(usd float64) *float64 {
	if usd < 0 || math.IsNaN(usd) {
		return nil
	}
	return &usd
}

// postWorkstreamSpend keeps what someone's run of the workstream's work cost,
// posted by the desk or engine that ran it. The poster is the caller, never
// the body, and one Idempotency-Key is counted once. A workstream archived
// after the work ran still takes its late spend.
func (h *Handler) postWorkstreamSpend(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.findWorkstream(w, r)
	if !ok {
		return
	}
	if !v.canWrite(ws) {
		h.fail(w, http.StatusForbidden, errWorkstreamNoWrite.Error())
		return
	}
	var body api.WorkstreamSpendNew
	if !h.decode(w, r, &body, false) {
		return
	}
	if !(body.USD > 0) || body.USD > spendOneMostUSD || math.IsInf(body.USD, 0) {
		h.fail(w, http.StatusUnprocessableEntity, "usd must be more than 0 and at most 10000")
		return
	}
	if err := plainWords("chat", body.Chat, 0, 200); err != nil {
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(key) > 200 {
		h.fail(w, http.StatusUnprocessableEntity, "Idempotency-Key may have at most 200 characters")
		return
	}
	s := store.Spend{At: h.clock().UTC().Format(time.RFC3339Nano), By: v.Handle, USD: body.USD, Chat: body.Chat, Key: key}
	if body.Job != nil {
		s.Job = *body.Job
	}
	total, err := h.config.Store.AddSpend(ws.ID, s)
	if h.failed(w, err) {
		return
	}
	h.recordWorkstream(r, ws.ID, "posted spend", strconv.FormatFloat(body.USD, 'f', -1, 64)+" USD")
	h.publishWorkstreamPart(ws, api.PartSpend, "", h.orgActor(r), 0, false)
	h.write(w, http.StatusOK, api.WorkstreamSpend{SpendUSD: total, BudgetUSD: ws.BudgetUSD})
}

// ── decisions ──

// supersedeDropped is next with every decision before had that next leaves
// out kept as superseded: a decision is never deleted, so what a team decided
// stays readable after someone changes their mind.
func supersedeDropped(next, before []factory.WorkstreamDecision) []factory.WorkstreamDecision {
	out := slices.Clone(next)
	for _, d := range before {
		if slices.ContainsFunc(next, func(n factory.WorkstreamDecision) bool { return n.ID == d.ID }) {
			continue
		}
		d.State = "superseded"
		out = append(out, d)
	}
	return out
}
