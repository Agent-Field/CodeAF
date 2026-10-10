package desktopbridge

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Agent-Field/codeaf/internal/placegraph"
)

// FolderResult rides in Mutation.Result, like a delete's or a merge's outcome:
// whether this drop made the place, and the conversations the surface may offer
// to file there ("Move Now's matching tabs").
type FolderResult struct {
	// Created is false when the folder already had its place.
	Created bool `json:"created"`
	// MatchingChats are chats that ran inside the folder and are filed nowhere.
	// The renderer files them with POST /places/{id}/members; this door moves none.
	MatchingChats []string `json:"matchingChats"`
}

type fromFolderAsk struct {
	IfRevision *uint64 `json:"ifRevision"`
	Path       string  `json:"path"`
}

// fromFolder makes (or finds) the place a dropped folder stands for. Matching
// is read after the write so a just-created place is not offered chats it could
// already have, and a repeat drop re-offers whatever is still unfiled.
func (p *Places) fromFolder(w http.ResponseWriter, r *http.Request) {
	var ask fromFolderAsk
	if !readBody(w, r, &ask) {
		return
	}
	path := strings.TrimSpace(ask.Path)
	if path == "" {
		failPlaces(w, 400, "invalid", "Say which folder.")
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.staleRevision(w, ask.IfRevision) {
		return
	}
	got, err := p.Store.FromFolder(path, p.sourcePolicy())
	switch {
	case errors.Is(err, placegraph.ErrSourceRefused):
		failPlaces(w, 422, "refused_source", refusedSentence(err))
		return
	case err != nil:
		p.failStore(w, err, "", nil)
		return
	}
	x, ok := p.open(w, true)
	if !ok {
		return
	}
	workspaces := map[string]string{}
	for id, row := range x.rows {
		workspaces[id] = row.Workspace
	}
	matching := x.snap.MatchingUnplacedChats(got.Place.Context.Sources[0].Ref, workspaces)
	var b batch
	b.add(got.Receipt)
	p.finish(w, &b, got.Place.ID, func(m *Mutation) {
		m.Result = FolderResult{Created: got.Created, MatchingChats: matching}
	})
}
