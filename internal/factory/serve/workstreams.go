package serve

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// ── AN ORGANIZATION'S WORKSTREAMS ──────────────────────────────────────────
//
// The routes below keep an organization's workstreams on its server
// (api/workstreams.go has the wire). Who may do what is the access core's
// (item_access.go), with these verbs:
//
//   - VIEW to read it. The operator views every one that is not private, and
//     of a private one sees only a name-only row in the list.
//   - EDIT to change its words (PATCH) or approve its go-aheads.
//   - FULL to archive it. Its owner holds full.
//   - FULL, OR THE OPERATOR on one that is not private, to change its owner,
//     shares or privacy. Seeing everything does not make the operator a
//     writer.
//
// A workstream you cannot see answers 404, the same as one that does not
// exist, so an id is never a way to learn what someone else is working on.
// On a solo factory there is one person, and they may do everything.

// workstreamIDPrefix and teamIDPrefix are the contract's id shapes.
const (
	workstreamIDPrefix = "ws_"
	teamIDPrefix       = "tm_"
)

// The longest a workstream's words may be, so one save cannot fill the
// document every read of the organization's workstreams has to parse.
const (
	workstreamNameMost  = 200
	workstreamTitleMost = 300
	workstreamBodyMost  = 256 << 10
	workstreamListMost  = 500
)

var (
	errWorkstreamStale    = errors.New("this workstream changed since you read it; read it again, then make the change")
	errWorkstreamArchived = errors.New("this workstream is archived")
	errWorkstreamGone     = errors.New("workstream not found")
	errWorkstreamNoWrite  = errors.New("only its owner and those it is shared with for edit can change this workstream")
	errWorkstreamNoFull   = errors.New("only its owner and those it is shared with for full access can archive this workstream")
	errWorkstreamNoGovern = errors.New("only its owner, its owning team or the operator can change who owns or sees this workstream")
)

func (h *Handler) workstreamRoutes() {
	h.route("/v1/workstreams", map[string]http.HandlerFunc{http.MethodGet: h.listWorkstreams, http.MethodPost: h.createWorkstream})
	h.route("/v1/workstreams/import", map[string]http.HandlerFunc{http.MethodPost: h.importWorkstream})
	h.route("/v1/workstreams/{id}", map[string]http.HandlerFunc{http.MethodGet: h.getWorkstream, http.MethodPatch: h.patchWorkstream, http.MethodDelete: h.deleteWorkstream})
	h.route("/v1/workstreams/{id}/owner", map[string]http.HandlerFunc{http.MethodPut: h.setWorkstreamOwner})
	h.route("/v1/workstreams/{id}/shares", map[string]http.HandlerFunc{http.MethodPut: h.setWorkstreamShares})
	h.route("/v1/workstreams/{id}/shares/preview", map[string]http.HandlerFunc{http.MethodGet: h.previewWorkstreamShares})
	h.route("/v1/workstreams/{id}/activity", map[string]http.HandlerFunc{http.MethodGet: h.workstreamActivity})
}

// ── who is asking ──

// wsViewer is the caller as the workstream routes see them: the access core's
// [Viewer] (item_access.go), kept under this name for the routes that use it.
type wsViewer = Viewer

func (h *Handler) wsViewer(r *http.Request) (wsViewer, error) { return h.itemViewer(r) }

// canSee and canWrite are the core's view and edit on a workstream. canGovern
// (change its owner or shares) is full, or the operator on one that is not
// private: the operator keeps the organization's workstreams owned and shared
// rightly without being their writer, and never governs what they cannot read.
func (v Viewer) canSee(w factory.OrgWorkstream) bool {
	return can(v, workstreamItem(w), factory.RoleView)
}
func (v Viewer) canWrite(w factory.OrgWorkstream) bool {
	return can(v, workstreamItem(w), factory.RoleEdit)
}
func (v Viewer) canGovern(w factory.OrgWorkstream) bool {
	return can(v, workstreamItem(w), factory.RoleFull) || (v.Operator && !w.Private)
}

// orgWorkstreamAccess is whether id names an organization workstream and
// whether the caller may view and edit it; known is false, with no error, when
// it names none: a go-ahead group that is not an organization workstream keeps
// the rules it always had.
func (h *Handler) orgWorkstreamAccess(r *http.Request, id string) (known, see, write bool, err error) {
	if h.config.Store == nil || !strings.HasPrefix(id, workstreamIDPrefix) {
		return false, false, false, nil
	}
	w, ok, err := h.config.Store.Workstream(id)
	if err != nil || !ok {
		return false, false, false, err
	}
	v, err := h.wsViewer(r)
	if err != nil {
		return true, false, false, err
	}
	return true, v.canSee(w), v.canWrite(w), nil
}

// orgWorkstreamWrite is orgWorkstreamAccess's write half.
func (h *Handler) orgWorkstreamWrite(r *http.Request, id string) (known, allowed bool, err error) {
	known, _, allowed, err = h.orgWorkstreamAccess(r, id)
	return known, allowed, err
}

// ── reads ──

// listWorkstreams answers the workstreams the caller can see, newest change
// first. Archived ones are left out unless ?archived=1 asks for them too.
func (h *Handler) listWorkstreams(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.config.Store != nil) {
		return
	}
	v, err := h.wsViewer(r)
	if h.failed(w, err) {
		return
	}
	all, err := h.config.Store.Workstreams()
	if h.failed(w, err) {
		return
	}
	archived := r.URL.Query().Get("archived") == "1"
	seen := make([]factory.OrgWorkstream, 0, len(all))
	for _, ws := range all {
		if (v.canSee(ws) || nameOnly(v, workstreamItem(ws))) && (archived || ws.ArchivedAt == nil) {
			seen = append(seen, ws)
		}
	}
	store.SortWorkstreams(seen)
	counts, err := h.workstreamCounts()
	if h.failed(w, err) {
		return
	}
	out := api.WorkstreamList{Workstreams: make([]api.WorkstreamSummary, 0, len(seen))}
	for _, ws := range seen {
		out.Workstreams = append(out.Workstreams, workstreamSummary(ws, counts[ws.ID], v))
	}
	h.write(w, http.StatusOK, out)
}

func (h *Handler) getWorkstream(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.findWorkstream(w, r)
	if !ok {
		return
	}
	h.writeWorkstream(w, http.StatusOK, ws, v)
}

// findWorkstream is the workstream the path names when the caller can see it;
// otherwise the refusal is written and ok is false.
func (h *Handler) findWorkstream(w http.ResponseWriter, r *http.Request) (factory.OrgWorkstream, wsViewer, bool) {
	if !h.requireVerb(w, h.config.Store != nil) {
		return factory.OrgWorkstream{}, wsViewer{}, false
	}
	v, err := h.wsViewer(r)
	if h.failed(w, err) {
		return factory.OrgWorkstream{}, v, false
	}
	ws, ok, err := h.config.Store.Workstream(r.PathValue("id"))
	if h.failed(w, err) {
		return ws, v, false
	}
	if !ok || !v.canSee(ws) {
		h.fail(w, http.StatusNotFound, errWorkstreamGone.Error())
		return ws, v, false
	}
	return ws, v, true
}

// workstreamCount is what the jobs a workstream started add up to.
type workstreamCount struct{ needsYou, merged, total int }

// workstreamCounts reads the factory's jobs once and counts them by the
// go-ahead group that started them, which on the desk is the workstream.
func (h *Handler) workstreamCounts() (map[string]workstreamCount, error) {
	items, err := h.config.Store.List()
	if err != nil {
		return nil, err
	}
	out := map[string]workstreamCount{}
	for _, it := range items {
		if !strings.HasPrefix(it.PolicyGroup, workstreamIDPrefix) {
			continue
		}
		c := out[it.PolicyGroup]
		c.total++
		if factory.Merged(it) {
			c.merged++
		}
		if it.State == factory.StateNeedsYou {
			c.needsYou++
		}
		out[it.PolicyGroup] = c
	}
	return out, nil
}

// workstreamSummary is ws's row as v sees it. A row v holds no role on is the
// operator's name-only row for a private workstream: who owns it, what it is
// called and when it changed, and nothing of what it says or how it is going.
func workstreamSummary(ws factory.OrgWorkstream, c workstreamCount, v Viewer) api.WorkstreamSummary {
	out := api.WorkstreamSummary{
		ID: ws.ID, Name: ws.Name, Stage: ws.Stage, Owner: ws.Owner,
		SharedWith: factory.ShareTeams(ws.Shares), Shares: nonNil(ws.Shares),
		Private: ws.Private, MyRole: roleOf(v, workstreamItem(ws)),
		NeedsYou: c.needsYou, Merged: c.merged, Total: c.total,
		UpdatedAt: ws.UpdatedAt.UTC().Format(time.RFC3339Nano), Rev: ws.Rev,
	}
	if out.MyRole == "" {
		out.SharedWith, out.Shares = []string{}, []factory.Share{}
		out.NeedsYou, out.Merged, out.Total = 0, 0, 0
	}
	if ws.ArchivedAt != nil {
		out.ArchivedAt = ws.ArchivedAt.UTC().Format(time.RFC3339Nano)
	}
	return out
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// writeWorkstream answers one workstream whole, with its counts and the
// go-aheads kept for its id.
func (h *Handler) writeWorkstream(w http.ResponseWriter, status int, ws factory.OrgWorkstream, v Viewer) {
	out, err := h.workstreamBody(ws, v)
	if h.failed(w, err) {
		return
	}
	w.Header().Set("ETag", strconv.Itoa(ws.Rev))
	h.write(w, status, out)
}

func (h *Handler) workstreamBody(ws factory.OrgWorkstream, v Viewer) (api.Workstream, error) {
	items, err := h.config.Store.List()
	if err != nil {
		return api.Workstream{}, err
	}
	var c workstreamCount
	for _, it := range items {
		if it.PolicyGroup != ws.ID {
			continue
		}
		c.total++
		if factory.Merged(it) {
			c.merged++
		}
		if it.State == factory.StateNeedsYou {
			c.needsYou++
		}
	}
	goAheads := []api.PolicyAllowance{}
	if p, ok, err := h.config.Store.Policy(ws.ID); err != nil {
		return api.Workstream{}, err
	} else if ok {
		goAheads = api.PolicyOf(p, items, h.clock()).Allowances
	}
	return api.Workstream{
		WorkstreamSummary: workstreamSummary(ws, c, v),
		Purpose:           ws.Purpose,
		Pages:             nonNil(ws.Pages),
		Decisions:         nonNil(ws.Decisions),
		Sources:           nonNil(ws.Sources),
		Links:             nonNil(ws.Links),
		GoAheads:          goAheads,
		CreatedBy:         ws.CreatedBy,
	}, nil
}

// ── writes ──

// createWorkstream makes a workstream. Its owner is the caller unless the body
// names another: a team the caller is in, or (for the operator) anyone.
func (h *Handler) createWorkstream(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.config.Store != nil) {
		return
	}
	var body api.WorkstreamNew
	if !h.decode(w, r, &body, false) {
		return
	}
	v, err := h.wsViewer(r)
	if h.failed(w, err) {
		return
	}
	owner := api.WorkstreamOwner{Kind: api.OwnerPerson, ID: v.Handle}
	if body.Owner != nil {
		owner = *body.Owner
	}
	owner, err = h.cleanOwner(owner)
	if err != nil {
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if !v.Operator && !(owner.Kind == api.OwnerPerson && owner.ID == v.Handle) && !(owner.Kind == api.OwnerTeam && v.Teams[owner.ID]) {
		h.fail(w, http.StatusForbidden, "you can make a workstream owned by you or by a team you are in")
		return
	}
	if body.Private && owner.Kind != api.OwnerPerson {
		h.fail(w, http.StatusUnprocessableEntity, errPrivateShape.Error())
		return
	}
	now := h.clock().UTC()
	ws := factory.OrgWorkstream{
		Name: body.Name, Stage: body.Stage, Purpose: body.Purpose, Owner: owner, Private: body.Private,
		Pages: body.Pages, Decisions: stampDecisions(body.Decisions, nil, v.Handle, now, false),
		Sources: body.Sources, Links: body.Links,
		CreatedBy: v.Handle, CreatedAt: now, UpdatedAt: now,
	}
	if ws.Stage == "" {
		ws.Stage = api.WorkstreamShaping
	}
	h.saveNewWorkstream(w, r, ws, v, false)
}

// importWorkstream takes a Personal workstream moving in, once. The caller
// owns the copy; links to workstreams this server does not keep are dropped,
// because they named the other copies on the laptop. The same person moving
// the same Personal workstream in again gets the copy already made.
func (h *Handler) importWorkstream(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.config.Store != nil) {
		return
	}
	var body api.WorkstreamPackage
	if !h.decode(w, r, &body, false) {
		return
	}
	v, err := h.wsViewer(r)
	if h.failed(w, err) {
		return
	}
	if v.Handle == "" {
		h.fail(w, http.StatusUnprocessableEntity, "a workstream moves in as someone's; sign in first")
		return
	}
	body.FromLocalID = strings.TrimSpace(body.FromLocalID)
	if body.FromLocalID == "" {
		h.fail(w, http.StatusUnprocessableEntity, "from_local_id is required")
		return
	}
	// The copy is the caller's, whatever the package says made it: a name
	// the server did not see sign in is not one it may write down.
	now := h.clock().UTC()
	ws := factory.OrgWorkstream{
		Name: body.Name, Stage: body.Stage, Purpose: body.Purpose,
		Owner: api.WorkstreamOwner{Kind: api.OwnerPerson, ID: v.Handle},
		Pages: body.Pages, Decisions: stampDecisions(body.Decisions, nil, v.Handle, now, true),
		Sources: body.Sources, Links: body.Links, FromLocalLinks: slices.Clone(body.Links),
		CreatedBy: v.Handle, FromLocalID: body.FromLocalID, ImportedBy: v.Handle,
		CreatedAt: now, UpdatedAt: now,
	}
	if ws.Stage == "" {
		ws.Stage = api.WorkstreamShaping
	}
	h.saveNewWorkstream(w, r, ws, v, true)
}

// saveNewWorkstream checks ws, gives it an id and keeps it, answering 201.
func (h *Handler) saveNewWorkstream(w http.ResponseWriter, r *http.Request, ws factory.OrgWorkstream, v wsViewer, imported bool) {
	if err := cleanWorkstream(&ws, v.Handle, ws.CreatedAt); err != nil {
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	id, err := cell.NewID(ws.CreatedAt)
	if h.failed(w, err) {
		return
	}
	ws.ID = workstreamIDPrefix + id
	var again *factory.OrgWorkstream
	befores := map[string]factory.OrgWorkstream{}
	saved, err := h.config.Store.EditWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
		changed := []string{ws.ID}
		if imported {
			for _, cur := range all {
				if cur.ArchivedAt == nil && cur.FromLocalID == ws.FromLocalID && cur.ImportedBy == ws.ImportedBy {
					again = &cur
					return nil, nil
				}
			}
			ws.Links = importedLinks(all, ws.Links, ws.ID, ws.ImportedBy)
			// The same person's earlier imports that linked to this one on the
			// laptop link to it here too, now that it has an id.
			for id, cur := range all {
				if cur.ArchivedAt == nil && cur.ImportedBy == ws.ImportedBy && slices.Contains(cur.FromLocalLinks, ws.FromLocalID) && !slices.Contains(cur.Links, ws.ID) && len(cur.Links) < workstreamListMost {
					befores[id] = cur
					cur.Links, cur.UpdatedAt = append(slices.Clone(cur.Links), ws.ID), ws.CreatedAt
					all[id] = cur
					changed = append(changed, id)
				}
			}
		} else if err := checkLinks(all, ws.Links, ws.ID); err != nil {
			return nil, err
		}
		all[ws.ID] = ws
		return changed, nil
	})
	if h.workstreamRefused(w, err) {
		return
	}
	if again != nil {
		h.writeWorkstream(w, http.StatusOK, *again, v)
		return
	}
	verb := "created workstream"
	if imported {
		verb = "imported workstream"
	}
	h.recordWorkstream(r, ws.ID, verb, "")
	h.publishWorkstreams(api.EventWorkstreamChanged, h.orgActor(r), nil, saved[ws.ID])
	for id, before := range befores {
		h.publishWorkstreams(api.EventWorkstreamChanged, h.orgActor(r), &before, saved[id])
	}
	h.writeWorkstream(w, http.StatusCreated, saved[ws.ID], v)
}

// patchWorkstream changes any of a workstream's words. IF-MATCH IS REQUIRED
// and must be the rev the writer read: two people editing one page must not
// silently lose one of the edits, so the second is refused with 412 and reads
// the page again.
func (h *Handler) patchWorkstream(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.findWorkstream(w, r)
	if !ok {
		return
	}
	rev, err := strconv.Atoi(strings.Trim(strings.TrimSpace(r.Header.Get("If-Match")), `"`))
	if err != nil {
		h.fail(w, http.StatusPreconditionRequired, "If-Match must carry the rev you read")
		return
	}
	var body api.WorkstreamPatch
	if !h.decode(w, r, &body, false) {
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
		if cur.Rev != rev {
			return nil, errWorkstreamStale
		}
		if cur.ArchivedAt != nil {
			return nil, errWorkstreamArchived
		}
		before = cur
		if body.Name != nil {
			cur.Name = *body.Name
		}
		if body.Purpose != nil {
			cur.Purpose = *body.Purpose
		}
		if body.Stage != nil {
			cur.Stage = *body.Stage
		}
		if body.Pages != nil {
			cur.Pages = stampPages(*body.Pages, before.Pages, v.Handle, now)
		}
		if body.Decisions != nil {
			cur.Decisions = stampDecisions(*body.Decisions, before.Decisions, v.Handle, now, false)
		}
		if body.Sources != nil {
			cur.Sources = *body.Sources
		}
		if body.Links != nil {
			cur.Links = *body.Links
			if err := checkLinks(all, cur.Links, cur.ID); err != nil {
				return nil, err
			}
		}
		if err := cleanWorkstream(&cur, v.Handle, now); err != nil {
			return nil, err
		}
		cur.UpdatedAt = now
		all[cur.ID] = cur
		return []string{cur.ID}, nil
	})
	if h.workstreamRefused(w, err) {
		return
	}
	h.recordWorkstream(r, ws.ID, "changed workstream", patchedFields(body))
	h.publishWorkstreams(api.EventWorkstreamChanged, h.orgActor(r), &before, saved[ws.ID])
	h.writeWorkstream(w, http.StatusOK, saved[ws.ID], v)
}

// patchedFields names the fields a patch sent, never their words: the record
// is read by people who may not see the workstream's content tomorrow.
func patchedFields(body api.WorkstreamPatch) string {
	var out []string
	for _, f := range []struct {
		name string
		sent bool
	}{{"name", body.Name != nil}, {"purpose", body.Purpose != nil}, {"stage", body.Stage != nil},
		{"pages", body.Pages != nil}, {"decisions", body.Decisions != nil}, {"sources", body.Sources != nil},
		{"links", body.Links != nil}} {
		if f.sent {
			out = append(out, f.name)
		}
	}
	return changedFields(out...)
}

// deleteWorkstream archives a workstream: it leaves every list and keeps its
// pages, so nothing a team wrote is lost by one click. ?purge=1 removes it for
// good, and only the operator may.
func (h *Handler) deleteWorkstream(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.findWorkstream(w, r)
	if !ok {
		return
	}
	purge := r.URL.Query().Get("purge") == "1"
	if purge && !v.Operator {
		h.fail(w, http.StatusForbidden, "only the operator can remove a workstream for good")
		return
	}
	if !purge && !can(v, workstreamItem(ws), factory.RoleFull) {
		h.fail(w, http.StatusForbidden, errWorkstreamNoFull.Error())
		return
	}
	now := h.clock().UTC()
	var before factory.OrgWorkstream
	_, err := h.config.Store.EditWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
		cur, ok := all[ws.ID]
		if !ok {
			return nil, errWorkstreamGone
		}
		before = cur
		if purge {
			delete(all, cur.ID)
			return []string{cur.ID}, nil
		}
		if cur.ArchivedAt != nil {
			return nil, nil
		}
		cur.ArchivedAt, cur.UpdatedAt = &now, now
		all[cur.ID] = cur
		return []string{cur.ID}, nil
	})
	if h.workstreamRefused(w, err) {
		return
	}
	if purge || before.ArchivedAt == nil {
		verb := "archived workstream"
		if purge {
			verb = "deleted workstream"
		}
		h.recordWorkstream(r, ws.ID, verb, "")
		h.publishWorkstreams(api.EventWorkstreamRemoved, h.orgActor(r), &before, before)
	}
	w.WriteHeader(http.StatusNoContent)
}

// setWorkstreamOwner gives a workstream to one person or one team.
func (h *Handler) setWorkstreamOwner(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.findWorkstream(w, r)
	if !ok {
		return
	}
	var body api.WorkstreamOwner
	if !h.decode(w, r, &body, false) {
		return
	}
	owner, err := h.cleanOwner(body)
	if err != nil {
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	now := h.clock().UTC()
	var before factory.OrgWorkstream
	saved, err := h.config.Store.EditWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
		cur, ok := all[ws.ID]
		if !ok {
			return nil, errWorkstreamGone
		}
		if !v.canGovern(cur) {
			return nil, errWorkstreamNoGovern
		}
		before = cur
		if cur.Owner == owner {
			return nil, nil
		}
		cur.Owner, cur.UpdatedAt = owner, now
		// The owner holds full already; a share naming them as well would only
		// say it twice. A team's workstream is never private.
		cur.Shares = withoutOwnerShare(cur.Shares, owner)
		cur.Private = cur.Private && owner.Kind == api.OwnerPerson
		all[cur.ID] = cur
		return []string{cur.ID}, nil
	})
	if h.workstreamRefused(w, err) {
		return
	}
	if saved[ws.ID].Rev != before.Rev {
		h.recordWorkstream(r, ws.ID, "changed owner", owner.Kind+" "+owner.ID)
		h.publishWorkstreams(api.EventWorkstreamChanged, h.orgActor(r), &before, saved[ws.ID])
	}
	h.writeWorkstream(w, http.StatusOK, saved[ws.ID], v)
}

// setWorkstreamShares sets who a workstream is shared with, and with which
// role, and may give it a new owner or change whether it is private, all in
// ONE save so a share sheet never lands half ([api.WorkstreamSharesPut]).
// With include_linked, every workstream it links to that the caller may
// govern and that is not private is shared with the same people and teams
// too: added to, never taken from, because the linked one may be shared with
// others for its own reasons. A linked one the caller cannot govern is left
// as it is and not named in also_shared.
func (h *Handler) setWorkstreamShares(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.findWorkstream(w, r)
	if !ok {
		return
	}
	var body api.WorkstreamSharesPut
	if !h.decode(w, r, &body, false) {
		return
	}
	owner := ws.Owner
	if body.Owner != nil {
		o, err := h.cleanOwner(*body.Owner)
		if err != nil {
			h.fail(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		owner = o
	}
	// Shares are checked before the save, so the team list is not read under
	// the workstreams' lock.
	var shares []factory.Share
	var teams []string
	var err error
	switch {
	case body.Shares != nil:
		shares, err = h.cleanShares(*body.Shares, owner)
	case body.Teams != nil:
		teams, err = h.cleanTeams(body.Teams)
	}
	if err != nil {
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	now := h.clock().UTC()
	befores := map[string]factory.OrgWorkstream{}
	also := []string{}
	saved, err := h.config.Store.EditWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
		cur, ok := all[ws.ID]
		if !ok {
			return nil, errWorkstreamGone
		}
		if !v.canGovern(cur) {
			return nil, errWorkstreamNoGovern
		}
		if body.Rev != nil && cur.Rev != *body.Rev {
			return nil, errWorkstreamStale
		}
		next := cur
		if body.Owner != nil {
			next.Owner = owner
		}
		switch {
		case body.Shares != nil:
			next.Shares = withoutOwnerShare(shares, next.Owner)
		case body.Teams != nil:
			next.Shares = withTeamShares(cur.Shares, teams, next.Owner)
		default:
			next.Shares = withoutOwnerShare(cur.Shares, next.Owner)
		}
		switch {
		case body.Private != nil:
			next.Private = *body.Private
		case len(next.Shares) > 0 || next.Owner.Kind != api.OwnerPerson:
			next.Private = false
		}
		if next.Private && (next.Owner.Kind != api.OwnerPerson || len(next.Shares) > 0) {
			return nil, errPrivateShape
		}
		var changed []string
		if next.Owner != cur.Owner || next.Private != cur.Private || !slices.Equal(next.Shares, cur.Shares) {
			befores[cur.ID] = cur
			next.UpdatedAt = now
			all[cur.ID] = next
			changed = append(changed, cur.ID)
		}
		if body.IncludeLinked {
			for _, linked := range linkedToShare(all, next, next.Shares, v) {
				befores[linked.ID] = linked
				linked.Shares, linked.UpdatedAt = addShares(linked.Shares, next.Shares, linked.Owner), now
				all[linked.ID] = linked
				changed = append(changed, linked.ID)
				also = append(also, linked.ID)
			}
		}
		return changed, nil
	})
	if h.workstreamRefused(w, err) {
		return
	}
	for id, before := range befores {
		h.recordWorkstream(r, id, "changed sharing", sharesWords(saved[id]))
		h.publishWorkstreams(api.EventWorkstreamChanged, h.orgActor(r), &before, saved[id])
	}
	body2, err := h.workstreamBody(saved[ws.ID], v)
	if h.failed(w, err) {
		return
	}
	h.write(w, http.StatusOK, api.WorkstreamShared{Workstream: body2, AlsoShared: also})
}

// previewWorkstreamShares answers what include_linked would add for ?teams=:
// the linked workstreams the caller may govern that one of those teams does
// not see yet. It writes nothing.
func (h *Handler) previewWorkstreamShares(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.findWorkstream(w, r)
	if !ok {
		return
	}
	var asked []string
	for _, t := range strings.Split(r.URL.Query().Get("teams"), ",") {
		if t = strings.TrimSpace(t); t != "" {
			asked = append(asked, t)
		}
	}
	teams, err := h.cleanTeams(asked)
	if err != nil {
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	all, err := h.config.Store.Workstreams()
	if h.failed(w, err) {
		return
	}
	counts, err := h.workstreamCounts()
	if h.failed(w, err) {
		return
	}
	out := api.WorkstreamSharePreview{Linked: []api.WorkstreamSummary{}}
	for _, linked := range linkedToShare(all, ws, factory.TeamShares(teams), v) {
		out.Linked = append(out.Linked, workstreamSummary(linked, counts[linked.ID], v))
	}
	h.write(w, http.StatusOK, out)
}

// linkedToShare is the workstreams ws links to, in its link order, that are
// live, not private, the viewer may govern, and that some share of shares
// would give more than they hold now.
func linkedToShare(all factory.OrgWorkstreams, ws factory.OrgWorkstream, shares []factory.Share, v wsViewer) []factory.OrgWorkstream {
	var out []factory.OrgWorkstream
	for _, id := range ws.Links {
		linked, ok := all[id]
		if !ok || linked.ArchivedAt != nil || linked.Private || !v.canGovern(linked) {
			continue
		}
		if !slices.Equal(addShares(linked.Shares, shares, linked.Owner), withoutOwnerShare(linked.Shares, linked.Owner)) {
			out = append(out, linked)
		}
	}
	return out
}

// workstreamRefused writes the refusal a store edit ended with.
func (h *Handler) workstreamRefused(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, errWorkstreamStale):
		h.fail(w, http.StatusPreconditionFailed, err.Error())
	case errors.Is(err, errWorkstreamArchived):
		h.fail(w, http.StatusConflict, err.Error())
	case errors.Is(err, errWorkstreamGone):
		h.fail(w, http.StatusNotFound, err.Error())
	case errors.Is(err, errWorkstreamNoWrite), errors.Is(err, errWorkstreamNoGovern), errors.Is(err, errWorkstreamNoFull):
		h.fail(w, http.StatusForbidden, err.Error())
	case errors.As(err, new(workstreamInvalid)), errors.Is(err, errLinkUnknown):
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
	default:
		return h.failed(w, err)
	}
	return true
}

// ── checks ──

// workstreamInvalid is a body the server will not keep, answered 422.
type workstreamInvalid string

func (e workstreamInvalid) Error() string { return string(e) }

var errLinkUnknown = workstreamInvalid("links may only name this organization's other workstreams")

var errPrivateShape = workstreamInvalid("a private workstream is one person's and shared with no one")

func invalidf(format string, args ...any) error {
	return workstreamInvalid(fmt.Sprintf(format, args...))
}

// cleanOwner checks an owner: a person by handle, or a team this organization
// keeps.
func (h *Handler) cleanOwner(o api.WorkstreamOwner) (api.WorkstreamOwner, error) {
	switch o.Kind {
	case api.OwnerPerson:
		o.ID = factory.NormalizeHandle(o.ID)
		if o.ID == "" {
			return o, invalidf("a person owner needs their handle")
		}
	case api.OwnerTeam:
		o.ID = strings.TrimSpace(o.ID)
		if !h.teamKnown(o.ID) {
			return o, invalidf("team %q is not in this organization", o.ID)
		}
	default:
		return o, invalidf(`owner kind must be "person" or "team"`)
	}
	return o, nil
}

// cleanTeams is teams checked, without repeats, in one order.
func (h *Handler) cleanTeams(teams []string) ([]string, error) {
	out := make([]string, 0, len(teams))
	for _, t := range teams {
		t = strings.TrimSpace(t)
		if !h.teamKnown(t) {
			return nil, invalidf("team %q is not in this organization", t)
		}
		out = append(out, t)
	}
	return sortedTeams(out), nil
}

func sortedTeams(teams []string) []string {
	out := slices.Clone(teams)
	slices.Sort(out)
	return nonNil(slices.Compact(out))
}

// withoutOwner is teams sorted, once each, less the owning team.
func withoutOwner(teams []string, owner api.WorkstreamOwner) []string {
	return slices.DeleteFunc(sortedTeams(teams), func(t string) bool { return owner.Kind == api.OwnerTeam && t == owner.ID })
}

// withoutOwnerShare is shares less any naming owner, who holds full already.
func withoutOwnerShare(shares []factory.Share, owner api.WorkstreamOwner) []factory.Share {
	return nonNil(slices.DeleteFunc(slices.Clone(shares), func(s factory.Share) bool { return s.Kind == owner.Kind && s.ID == owner.ID }))
}

// hasShare is whether shares name kind:id.
func hasShare(shares []factory.Share, kind, id string) bool {
	return slices.ContainsFunc(shares, func(s factory.Share) bool { return s.Kind == kind && s.ID == id })
}

// withTeamShares answers the old teams-only share list: the teams in it, as
// edit unless a team already holds another role, replace the team shares, and
// the person shares are kept as they are.
func withTeamShares(shares []factory.Share, teams []string, owner api.WorkstreamOwner) []factory.Share {
	out := []factory.Share{}
	for _, t := range sortedTeams(teams) {
		role := factory.RoleEdit
		if at := slices.IndexFunc(shares, func(s factory.Share) bool { return s.Kind == factory.ShareToTeam && s.ID == t }); at >= 0 {
			role = shares[at].Role
		}
		out = append(out, factory.Share{Kind: factory.ShareToTeam, ID: t, Role: role})
	}
	for _, s := range shares {
		if s.Kind != factory.ShareToTeam {
			out = append(out, s)
		}
	}
	return withoutOwnerShare(out, owner)
}

// addTeamShares is shares with every team of teams that it does not name yet
// added as edit; a team it already names keeps its role.
func addTeamShares(shares []factory.Share, teams []string, owner api.WorkstreamOwner) []factory.Share {
	out := slices.Clone(shares)
	for _, t := range sortedTeams(teams) {
		if !hasShare(out, factory.ShareToTeam, t) {
			out = append(out, factory.Share{Kind: factory.ShareToTeam, ID: t, Role: factory.RoleEdit})
		}
	}
	return withoutOwnerShare(out, owner)
}

// sharesWords is who a workstream is shared with, for the record: each share
// as kind, id and role, and "private" for a private one.
func sharesWords(ws factory.OrgWorkstream) string {
	if ws.Private {
		return "private"
	}
	words := make([]string, 0, len(ws.Shares))
	for _, s := range ws.Shares {
		words = append(words, s.Kind+" "+s.ID+" "+s.Role)
	}
	return strings.Join(words, ", ")
}

// addShares is shares with every share of more added: one naming someone new
// is added, and one naming someone already there raises their role if it is
// higher, never lowers it.
func addShares(shares, more []factory.Share, owner api.WorkstreamOwner) []factory.Share {
	out := slices.Clone(shares)
	for _, s := range more {
		at := slices.IndexFunc(out, func(cur factory.Share) bool { return cur.Kind == s.Kind && cur.ID == s.ID })
		if at < 0 {
			out = append(out, s)
			continue
		}
		out[at].Role = factory.HigherRole(out[at].Role, s.Role)
	}
	return withoutOwnerShare(out, owner)
}

// teamKnown is whether this organization keeps team, members or not. A team
// list that cannot be read knows no team, which refuses the change rather
// than sharing a workstream with a team nobody can confirm.
func (h *Handler) teamKnown(team string) bool {
	if h.config.Store == nil || !strings.HasPrefix(team, teamIDPrefix) {
		return false
	}
	teams, err := h.config.Store.Teams()
	if err != nil {
		return false
	}
	return slices.ContainsFunc(teams, func(t store.Team) bool { return t.ID == team })
}

// checkLinks refuses a link to anything but another workstream kept here.
func checkLinks(all factory.OrgWorkstreams, links []string, self string) error {
	for _, id := range links {
		if _, ok := all[id]; !ok || id == self {
			return errLinkUnknown
		}
	}
	return nil
}

// importedLinks is an import's links as this server knows them. A link to a
// workstream kept here stays; a link naming a Personal workstream by its
// laptop id becomes the id of the copy the same person already moved in; any
// other link named a workstream still on the laptop and is dropped.
func importedLinks(all factory.OrgWorkstreams, links []string, self, importer string) []string {
	out := []string{}
	add := func(id string) {
		if id != self && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	for _, id := range links {
		if _, ok := all[id]; ok {
			add(id)
			continue
		}
		for _, cur := range all {
			if cur.ArchivedAt == nil && cur.ImportedBy == importer && cur.FromLocalID == id {
				add(cur.ID)
				break
			}
		}
	}
	return out
}

// stampDecisions makes every decision's byline the server's: one that is new,
// or whose words or state changed from before, is by who, now. One that did
// not change keeps the byline it had. keepAt keeps a new decision's own time
// when it has one, for an import whose decisions were made before it moved
// in; who made them is still the person moving them in.
func stampDecisions(next, before []factory.WorkstreamDecision, who string, now time.Time, keepAt bool) []factory.WorkstreamDecision {
	old := map[string]factory.WorkstreamDecision{}
	for _, d := range before {
		old[d.ID] = d
	}
	stamp := now.Format(time.RFC3339Nano)
	out := slices.Clone(next)
	for i, d := range out {
		if was, ok := old[d.ID]; ok && d.ID != "" && was.Text == d.Text && was.State == d.State {
			out[i].By, out[i].At = was.By, was.At
			continue
		}
		out[i].By = who
		if !keepAt || d.At == "" {
			out[i].At = stamp
		}
	}
	return out
}

// stampPages marks every page whose words changed (or that is new) as written
// now by who, so a page's byline is the server's and not the writer's say.
func stampPages(next, before []factory.WorkstreamPage, who string, now time.Time) []factory.WorkstreamPage {
	old := map[string]factory.WorkstreamPage{}
	for _, p := range before {
		old[p.ID] = p
	}
	out := slices.Clone(next)
	for i, p := range out {
		was, ok := old[p.ID]
		if ok && was.Title == p.Title && was.BodyMD == p.BodyMD && was.Parent == p.Parent && was.Kind == p.Kind {
			out[i].UpdatedAt, out[i].UpdatedBy = was.UpdatedAt, was.UpdatedBy
			continue
		}
		out[i].UpdatedAt, out[i].UpdatedBy = now.Format(time.RFC3339Nano), who
	}
	return out
}

// cleanWorkstream checks every word of ws the server keeps and fills what a
// writer may leave out: ids for new pages, decisions and sources, and empty
// lists rather than null.
func cleanWorkstream(ws *factory.OrgWorkstream, who string, now time.Time) error {
	ws.Name = strings.TrimSpace(ws.Name)
	if err := plainWords("name", ws.Name, 1, workstreamNameMost); err != nil {
		return err
	}
	if !slices.Contains(factory.WorkstreamStages, ws.Stage) {
		return invalidf("stage must be one of %s", strings.Join(factory.WorkstreamStages, ", "))
	}
	if len(ws.Purpose) > workstreamBodyMost {
		return invalidf("purpose is longer than %d bytes", workstreamBodyMost)
	}
	if len(ws.Pages) > workstreamListMost || len(ws.Decisions) > workstreamListMost || len(ws.Sources) > workstreamListMost || len(ws.Links) > workstreamListMost {
		return invalidf("a workstream keeps at most %d pages, decisions, sources or links", workstreamListMost)
	}
	stamp := now.Format(time.RFC3339Nano)
	ids := map[string]bool{}
	for i := range ws.Pages {
		p := &ws.Pages[i]
		if p.ID == "" {
			id, err := cell.NewID(now)
			if err != nil {
				return err
			}
			p.ID = "pg_" + id
		}
		if ids[p.ID] {
			return invalidf("page id %q is used twice", p.ID)
		}
		ids[p.ID] = true
		if err := plainWords("a page's title", p.Title, 0, workstreamTitleMost); err != nil {
			return err
		}
		if len(p.BodyMD) > workstreamBodyMost {
			return invalidf("page %q is longer than %d bytes", p.ID, workstreamBodyMost)
		}
		if p.Kind == "" {
			p.Kind = "page"
		}
		if !slices.Contains(factory.WorkstreamPageKinds, p.Kind) {
			return invalidf("a page's kind must be one of %s", strings.Join(factory.WorkstreamPageKinds, ", "))
		}
		if p.UpdatedAt == "" {
			p.UpdatedAt, p.UpdatedBy = stamp, who
		}
	}
	for _, p := range ws.Pages {
		if p.Parent != "" && (!ids[p.Parent] || p.Parent == p.ID) {
			return invalidf("page %q has a parent that is not another of its pages", p.ID)
		}
	}
	seen := map[string]bool{}
	for i := range ws.Decisions {
		d := &ws.Decisions[i]
		if d.ID == "" {
			id, err := cell.NewID(now)
			if err != nil {
				return err
			}
			d.ID = "dc_" + id
		}
		if seen[d.ID] {
			return invalidf("decision id %q is used twice", d.ID)
		}
		seen[d.ID] = true
		if len(d.Text) > workstreamBodyMost {
			return invalidf("decision %q is longer than %d bytes", d.ID, workstreamBodyMost)
		}
		if d.State == "" {
			d.State = "proposed"
		}
		if !slices.Contains(factory.WorkstreamDecisionState, d.State) {
			return invalidf("a decision's state must be one of %s", strings.Join(factory.WorkstreamDecisionState, ", "))
		}
		if d.At == "" {
			d.At = stamp
		}
		if d.By == "" {
			d.By = who
		}
	}
	seen = map[string]bool{}
	for i := range ws.Sources {
		s := &ws.Sources[i]
		if s.ID == "" {
			id, err := cell.NewID(now)
			if err != nil {
				return err
			}
			s.ID = "src_" + id
		}
		if seen[s.ID] {
			return invalidf("source id %q is used twice", s.ID)
		}
		seen[s.ID] = true
		if !slices.Contains(factory.WorkstreamSourceKinds, s.Kind) {
			return invalidf("a source's kind must be one of %s", strings.Join(factory.WorkstreamSourceKinds, ", "))
		}
		if err := plainWords("a source's ref", s.Ref, 1, workstreamBodyMost); err != nil {
			return err
		}
		if err := plainWords("a source's title", s.Title, 0, workstreamTitleMost); err != nil {
			return err
		}
	}
	ws.Pages, ws.Decisions, ws.Sources = nonNil(ws.Pages), nonNil(ws.Decisions), nonNil(ws.Sources)
	ws.Links, ws.Shares, ws.SharedWith = nonNil(slices.Compact(ws.Links)), nonNil(ws.Shares), nil
	return nil
}

// plainWords refuses a one-line field that is too short, too long or carries
// a control character.
func plainWords(field, s string, least, most int) error {
	n := utf8.RuneCountInString(s)
	if n < least || n > most || strings.IndexFunc(s, unicode.IsControl) >= 0 {
		if least > 0 {
			return invalidf("%s needs %d–%d plain characters", field, least, most)
		}
		return invalidf("%s may have at most %d plain characters", field, most)
	}
	return nil
}

// ── events ──

// workstreamLive is a workstream event as the live record keeps it. Audience
// is who could see it before or after the change, so a team the workstream
// was just taken from still hears that it changed and drops its row; the
// stream sends only kind and id ([api.WorkstreamEvent]).
type workstreamLive struct {
	api.WorkstreamEvent
	itemAudience
}

// publishWorkstreams records one workstream event. A record that cannot be
// written loses only the push: the change is kept, and the next read finds it.
func (h *Handler) publishWorkstreams(kind, by string, before *factory.OrgWorkstream, after factory.OrgWorkstream) {
	items := []Item{workstreamItem(after)}
	if before != nil {
		items = append(items, workstreamItem(*before))
	}
	e := workstreamLive{WorkstreamEvent: api.WorkstreamEvent{Kind: kind, ID: after.ID, By: factory.NormalizeHandle(by)}, itemAudience: audienceOf(items...)}
	_ = live.Append(h.eventRoot(), live.New(kind, 0, e))
}

// workstreamEventForViewer is a workstream event when this caller could see
// the workstream before or after the change, sent as kind and id only.
func (h *Handler) workstreamEventForViewer(r *http.Request, e live.Event) (live.Event, bool) {
	var stored workstreamLive
	if json.Unmarshal(e.Data, &stored) != nil || stored.ID == "" {
		return e, false
	}
	v, err := h.wsViewer(r)
	if err != nil || !v.hears(stored.itemAudience) {
		return e, false
	}
	data, err := json.Marshal(stored.WorkstreamEvent)
	if err != nil {
		return e, false
	}
	e.Data = data
	return e, true
}
