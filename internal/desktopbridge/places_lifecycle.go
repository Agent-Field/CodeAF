package desktopbridge

import (
	"net/http"

	"github.com/Agent-Field/codeaf/internal/placegraph"
)

func init() {
	registerPlacesRoute("POST /places/{id}/archive", func(p *Places, w http.ResponseWriter, r *http.Request, id string) { p.archive(w, r, id, true) })
	registerPlacesRoute("POST /places/{id}/restore", func(p *Places, w http.ResponseWriter, r *http.Request, id string) { p.archive(w, r, id, false) })
	registerPlacesRoute("POST /places/{id}/delete", func(p *Places, w http.ResponseWriter, r *http.Request, id string) { p.deletePlace(w, r, id) })
	registerPlacesRoute("POST /places/{id}/merge", func(p *Places, w http.ResponseWriter, r *http.Request, id string) { p.merge(w, r, id) })
	registerPlacesRoute("GET /places/{id}/impact", func(p *Places, w http.ResponseWriter, r *http.Request, id string) { p.impact(w, id) })
}

// lifecycleImpact uses the same direct counts as deletion, including archived children.
func (p *Places) lifecycleImpact(w http.ResponseWriter, id string) (placegraph.DeleteImpact, bool) {
	snap, err := p.Store.Snapshot()
	if err != nil {
		p.failStore(w, err, "", nil)
		return placegraph.DeleteImpact{}, false
	}
	impact, err := snap.PreviewDelete(id)
	if err != nil {
		p.failStore(w, err, "", nil)
		return placegraph.DeleteImpact{}, false
	}
	return impact, true
}

func (p *Places) impact(w http.ResponseWriter, id string) {
	if impact, ok := p.lifecycleImpact(w, id); ok {
		write(w, map[string]int{"chats": impact.ChatsHere, "children": impact.Children})
	}
}

// lifecycleFields preserves the existing batch envelope while exposing the single write.
func lifecycleFields(m *Mutation, rc placegraph.Receipt, chats, children int) {
	if !rc.Noop() {
		m.Receipt = &rc
	}
	m.Chats, m.Children = &chats, &children
}

// undoReceipt uses the store's receipt check so both undo routes refuse stale changes.
func (p *Places) undoReceipt(w http.ResponseWriter, r *http.Request, token string) {
	var ask struct{}
	if !readBody(w, r, &ask) {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	revision, err := p.Store.Undo(token)
	if err != nil {
		p.failStore(w, err, "", nil)
		return
	}
	write(w, map[string]uint64{"revision": revision})
	p.publishPlaces(nil)
}

func (p *Places) archive(w http.ResponseWriter, r *http.Request, id string, archive bool) {
	var ask struct {
		IfRevision *uint64 `json:"ifRevision"`
		Archived   *bool   `json:"archived"`
	}
	if !readBody(w, r, &ask) {
		return
	}
	if ask.Archived != nil {
		archive = *ask.Archived
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.staleRevision(w, ask.IfRevision) {
		return
	}
	impact, ok := p.lifecycleImpact(w, id)
	if !ok {
		return
	}
	var rc placegraph.Receipt
	var err error
	if archive {
		rc, err = p.Store.Archive(id)
	} else {
		rc, err = p.Store.Restore(id)
	}
	if err != nil {
		p.failStore(w, err, "", nil)
		return
	}
	var b batch
	b.add(rc)
	p.finish(w, &b, id, func(m *Mutation) { lifecycleFields(m, rc, impact.ChatsHere, impact.Children) })
}

func (p *Places) deletePlace(w http.ResponseWriter, r *http.Request, id string) {
	var ask plainAsk
	if !readBody(w, r, &ask) {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.staleRevision(w, ask.IfRevision) {
		return
	}
	res, rc, err := p.Store.DeletePlace(id)
	if err != nil {
		p.failStore(w, err, "", nil)
		return
	}
	var b batch
	b.add(rc)
	p.finish(w, &b, "", func(m *Mutation) {
		m.Result = res
		lifecycleFields(m, rc, res.Unfiled, res.ChildrenMoved)
	})
}

func (p *Places) merge(w http.ResponseWriter, r *http.Request, id string) {
	var ask plainAsk
	if !readBody(w, r, &ask) {
		return
	}
	if ask.Into == "" {
		failPlaces(w, 400, "invalid", "Say which place to merge into.")
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.staleRevision(w, ask.IfRevision) {
		return
	}
	res, rc, err := p.Store.MergePlaces(id, ask.Into)
	if err != nil {
		p.failStore(w, err, "", nil)
		return
	}
	var b batch
	b.add(rc)
	p.finish(w, &b, ask.Into, func(m *Mutation) {
		m.Result = res
		lifecycleFields(m, rc, res.MembershipsMoved, res.ChildrenMoved)
	})
}

type undoAsk struct {
	Receipts []string `json:"receipts"`
	Token    string   `json:"token"`
}

// undo takes receipts back newest first, so a request that lists the receipts
// of a multi-step write in the order they were made unwinds them correctly. It
// stops at the first the store refuses and says how many were undone.
func (p *Places) undo(w http.ResponseWriter, r *http.Request) {
	var ask undoAsk
	if !readBody(w, r, &ask) {
		return
	}
	if ask.Token != "" {
		if len(ask.Receipts) != 0 {
			failPlaces(w, 400, "invalid", "Say one change to undo.")
			return
		}
		ask.Receipts = []string{ask.Token}
	}
	if len(ask.Receipts) == 0 {
		failPlaces(w, 400, "invalid", "There is nothing to undo.")
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	undone := 0
	var rev uint64
	for i := len(ask.Receipts) - 1; i >= 0; i-- {
		var err error
		if rev, err = p.Store.Undo(ask.Receipts[i]); err != nil {
			status, code, sentence := storeFailure(err, "")
			writeStatus(w, status, placesError{Error: sentence, Code: code, Undone: &undone})
			if undone > 0 {
				p.publishPlaces(nil)
			}
			return
		}
		undone++
	}
	write(w, map[string]any{"revision": rev, "undone": undone})
	p.publishPlaces(nil)
}
