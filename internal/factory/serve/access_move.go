package serve

import (
	"net/http"
	"os"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// moveAccessResource moves an item into another space: POST
// /v1/resources/{kind}/{id}/move {"to": "<space id>"}. MOVE IS NOT SHARE. A share
// adds or removes who else may reach an item and never changes where it lives;
// a move changes where it lives and so who reaches it, and every person who
// could reach it only at the old place or only at the new one hears about it,
// through one event to the union of the two audiences.
//
// The actor needs manage on the item and edit on the destination, which a
// person who can only see the destination does not have. A destination the
// actor cannot see, another person's space included, is 404, never 403.
//
// NOT GENERIC YET: A page or file keeps the workstream file move route
// because that route offers Undo. Spaces, workstreams and sessions stay put.
func (h *Handler) moveAccessResource(w http.ResponseWriter, r *http.Request) {
	res, v, ok := h.findAccessResource(w, r)
	if !ok {
		return
	}
	var body api.ResourceMove
	if !h.decode(w, r, &body, false) {
		return
	}
	to := strings.TrimSpace(body.To)
	if to == "" {
		h.fail(w, http.StatusUnprocessableEntity, "name the space to move it to")
		return
	}
	switch res.item.Kind {
	case ItemCanvas:
	case chatKind:
		h.moveAccessChat(w, r, res.ref.ID, to)
		return
	case store.SessionKind:
		h.fail(w, http.StatusConflict, "a "+res.item.Kind+" cannot be moved yet; start a new one where you want it")
		return
	default:
		h.fail(w, http.StatusConflict, "a "+res.item.Kind+" does not move here; pages and files move with POST /v1/workstreams/{id}/files/move")
		return
	}
	if !can(v, res.item, acl.VerbManage) {
		h.fail(w, http.StatusForbidden, "you need permission to manage this canvas to move it")
		return
	}
	c, found, err := h.config.Store.Canvas(res.item.ID)
	if h.failed(w, err) {
		return
	}
	if !found {
		h.fail(w, http.StatusNotFound, "resource not found")
		return
	}
	if c.Workstream == to {
		h.fail(w, http.StatusConflict, "it is already there")
		return
	}
	dest, ok := h.canvasWorkstream(w, r, v, to)
	if !ok {
		return
	}
	privateDest := dest.Private || dest.Space
	before := v.acc.audienceOf(res.item)
	var dropped []acl.Grant
	if privateDest {
		if dropped, err = h.config.Store.DropGrantsOn(ItemCanvas, c.ID); h.failed(w, err) {
			return
		}
	}
	saved, err := h.config.Store.EditCanvases(func(all factory.OrgCanvases) (bool, error) {
		cur, ok := all[c.ID]
		if !ok {
			return false, os.ErrNotExist
		}
		cur.Workstream, cur.Owner, cur.UpdatedBy, cur.Private = dest.ID, dest.Owner, v.Handle, false
		if privateDest {
			cur.Shares = nil
		}
		all[c.ID] = cur
		return true, nil
	})
	if h.failed(w, err) {
		return
	}
	moved := saved[c.ID]
	v.acc = h.access()
	after := v.acc.audienceOf(h.canvasItem(moved))
	h.publishCanvasAudience(r.Context(), api.EventCanvasChanged, moved.ID, v.Handle, acl.Union(before, after))
	h.recordOrg(r, record.ResourceAccessChanged, "moved", orgObjectResource+ItemCanvas+"/"+moved.ID, "")
	out := api.ResourceMoved{Dropped: []api.GrantTuple{}}
	for _, g := range dropped {
		out.Dropped = append(out.Dropped, api.GrantTuple{AccessGrant: g, Source: "grant", Live: g.Live(v.acc.time())})
	}
	if now, found, err := h.lookupAccessResource(ItemCanvas, moved.ID); err == nil && found {
		out.Resource = accessResourceWire(now, v)
	}
	h.write(w, http.StatusOK, out)
}
