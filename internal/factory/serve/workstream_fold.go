package serve

import (
	"errors"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"net/http"
)

// Fold is a move of everything, authorized on both sides, with a durable Undo
// receipt. A member who can edit a task cannot hand on everyone's workstream.
func (h *Handler) foldWorkstreamPatch(w http.ResponseWriter, r *http.Request, ws factory.OrgWorkstream, v wsViewer, rev int, body api.WorkstreamPatch) {
	from, to, undo := ws.ID, body.FoldInto, false
	if body.Unfold != "" {
		from, to, undo = body.Unfold, ws.ID, true
	}
	if body.FoldInto != "" && body.Unfold != "" {
		h.fail(w, 422, "choose Fold or Undo")
		return
	}
	if !undo {
		other, found, err := h.config.Store.Workstream(to)
		if h.failed(w, err) {
			return
		}
		if !found || !v.canSee(other) {
			h.fail(w, 404, errWorkstreamGone.Error())
			return
		}
		if _, err = h.foldFiles(ws); h.failed(w, err) {
			return
		}
		if _, err = h.foldFiles(other); h.failed(w, err) {
			return
		}
	}
	guard := func(a, b factory.OrgWorkstream) error {
		if !v.canGovern(a) || !v.canGovern(b) {
			return errWorkstreamNoWrite
		}
		current := a
		if undo {
			current = b
		}
		if current.Rev != rev {
			return errWorkstreamStale
		}
		if a.ArchivedAt != nil || b.ArchivedAt != nil {
			return errWorkstreamArchived
		}
		return nil
	}
	result, err := h.config.Store.FoldWorkstream(from, to, undo, h.clock().UTC(), guard)
	if err != nil {
		if errors.Is(err, store.ErrFoldInvalid) {
			h.fail(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		if errors.Is(err, store.ErrFoldChanged) {
			h.fail(w, http.StatusConflict, err.Error())
			return
		}
		if h.workstreamRefused(w, err) {
			return
		}
		h.fail(w, http.StatusConflict, err.Error())
		return
	}
	// Live document state follows the stable page identities. If it cannot
	// follow, compensate the database move before reporting a failure.
	ds, docErr := h.docStore()
	pages := ws.Pages
	if undo {
		pages = result.Pages
	}
	a, b := from, to
	if undo {
		a, b = to, from
	}
	var renamed []string
	if docErr == nil {
		for _, p := range pages {
			old := "ws/" + a + "/page/" + p.ID
			if docErr = ds.Rename(old, "ws/"+b+"/page/"+p.ID); docErr != nil {
				break
			}
			renamed = append(renamed, p.ID)
			h.docs.hub.Remove(old)
		}
	}
	if docErr != nil {
		for i := len(renamed) - 1; i >= 0; i-- {
			pid := renamed[i]
			_ = ds.Rename("ws/"+b+"/page/"+pid, "ws/"+a+"/page/"+pid)
		}
		_, rollbackErr := h.config.Store.FoldWorkstream(from, to, !undo, h.clock().UTC(), func(x, y factory.OrgWorkstream) error { return nil })
		if rollbackErr != nil {
			h.failed(w, rollbackErr)
			return
		}
		h.failed(w, docErr)
		return
	}
	actor := h.orgActor(r)
	if undo {
		h.publishWorkstreams(api.EventWorkstreamChanged, actor, nil, result)
	} else {
		h.publishWorkstreams(api.EventWorkstreamRemoved, actor, &ws, ws)
	}
	if other, ok, e := h.config.Store.Workstream(to); e == nil && ok {
		h.publishWorkstreams(api.EventWorkstreamChanged, actor, nil, other)
	}
	verb := "folded workstream"
	if undo {
		verb = "undid folding workstream"
	}
	h.recordWorkstream(r, to, verb, from)
	h.syncStartWithoutAsking(from, to)
	if r.Method == http.MethodPost {
		parent, err := h.workstreamBody(result, v)
		if h.failed(w, err) {
			return
		}
		h.write(w, http.StatusOK, api.WorkstreamFolded{Parent: parent, Heading: ws.Name})
		return
	}
	h.writeWorkstream(w, http.StatusOK, result, v)
}
