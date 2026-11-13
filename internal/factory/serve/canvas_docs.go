package serve

import (
	"context"
	"net/http"
	"sync"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── THE TEXT INSIDE A CANVAS FRAME ─────────────────────────────────────────
//
// The words typed into a frame are not canvas-core ops: they are a live
// document, canvas/<id>/frame/<fid>, kept by the documents relay (docs.go).
// What this file decides is the document's access, which is its canvas's:
//
//   - FOUND when the caller may view the canvas AND the frame is a node in
//     the canvas's current snapshot (any kind of node, so a note or a text
//     shape has its words too). A frame that was deleted is absent, so a
//     document left behind by a race below is unreachable rather than stale.
//   - VIEW reads and follows it; EDIT (and a signed-in handle) writes it.
//
// A node deleted by a batch or by a restore, and every node of a deleted
// canvas, has its document dropped after the change commits. A drop that
// fails costs only disk, since the document is already unreachable.

// canvasFrames caches each canvas's live node ids. Every write to a canvas
// goes through this server, and every write publishes, so publishing is
// where an entry is forgotten; between writes the set cannot change.
type canvasFrames struct {
	mu    sync.Mutex
	nodes map[string]map[string]bool
}

func (f *canvasFrames) forget(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.nodes, id)
}

// canvasNodes is canvas id's live node ids, from the cache when it has them.
func (h *Handler) canvasNodes(ctx context.Context, id string) (map[string]bool, error) {
	h.canvasFrames.mu.Lock()
	if got, ok := h.canvasFrames.nodes[id]; ok {
		h.canvasFrames.mu.Unlock()
		return got, nil
	}
	h.canvasFrames.mu.Unlock()
	var snap struct {
		Nodes []struct {
			ID string `json:"id"`
		} `json:"nodes"`
	}
	if err := h.rpc(ctx, id, map[string]any{"cmd": "snapshot"}, &snap); err != nil {
		return nil, err
	}
	nodes := make(map[string]bool, len(snap.Nodes))
	for _, n := range snap.Nodes {
		nodes[n.ID] = true
	}
	h.canvasFrames.mu.Lock()
	if h.canvasFrames.nodes == nil {
		h.canvasFrames.nodes = map[string]map[string]bool{}
	}
	h.canvasFrames.nodes[id] = nodes
	h.canvasFrames.mu.Unlock()
	return nodes, nil
}

// canvasDocAccess is the canvas half of the documents' access ([CanvasDocAccess]).
func (h *Handler) canvasDocAccess(r *http.Request, id, frame string) (found, view, edit bool, err error) {
	if !canvasIDShape.MatchString(id) {
		return false, false, false, nil
	}
	c, ok, err := h.config.Store.Canvas(id)
	if err != nil || !ok {
		return false, false, false, err
	}
	v, err := h.itemViewer(r)
	if err != nil {
		return false, false, false, err
	}
	if !can(v, canvasItem(c), factory.RoleView) {
		return false, false, false, nil
	}
	nodes, err := h.canvasNodes(r.Context(), id)
	if err != nil {
		return false, false, false, err
	}
	if !nodes[frame] {
		return false, false, false, nil
	}
	return true, true, v.Handle != "" && can(v, canvasItem(c), factory.RoleEdit), nil
}

// dropCanvasDocs drops the document of every node in before that the canvas
// no longer has (after nil: the canvas itself is gone).
func (h *Handler) dropCanvasDocs(id string, before, after map[string]bool) {
	for node := range before {
		if !after[node] {
			_ = h.dropDoc("canvas/" + id + "/frame/" + node)
		}
	}
}

// nodesBeforeRemoval is canvas id's nodes before a write that may delete
// some, or nil when they cannot be read: the write goes ahead regardless, and
// only the cleanup after it is skipped.
func (h *Handler) nodesBeforeRemoval(ctx context.Context, id string) map[string]bool {
	nodes, err := h.canvasNodes(ctx, id)
	if err != nil {
		return nil
	}
	return nodes
}

// dropRemovedNodes drops the documents of the nodes in before that a
// committed write deleted.
func (h *Handler) dropRemovedNodes(ctx context.Context, id string, before map[string]bool) {
	if before == nil {
		return
	}
	h.canvasFrames.forget(id)
	after, err := h.canvasNodes(ctx, id)
	if err != nil {
		return
	}
	h.dropCanvasDocs(id, before, after)
}
