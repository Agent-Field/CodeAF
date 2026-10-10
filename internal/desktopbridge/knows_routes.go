package desktopbridge

import (
	"net/http"

	"github.com/Agent-Field/codeaf/internal/placegraph"
)

// KnowsLine carries the stored evidence and the source words the design shows.
// Unknown dates, chat titles and answer counts stay absent rather than invented.
type KnowsLine struct {
	placegraph.Line
	SourceWords string `json:"sourceWords,omitempty"`
}

type knowsList struct {
	Revision  uint64      `json:"revision"`
	Lines     []KnowsLine `json:"lines"`
	StillTrue []string    `json:"stillTrue"`
}

type knowsMutation struct {
	Mutation
	Line *KnowsLine          `json:"line,omitempty"`
	Ask  *placegraph.AskOnce `json:"ask,omitempty"`
}

type knowsAsk struct {
	IfRevision *uint64 `json:"ifRevision"`
	Text       string  `json:"text"`
	Supersedes string  `json:"supersedes,omitempty"`
	Yes        *bool   `json:"yes,omitempty"`
}

func init() {
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/places/{id}/knows"},
		{http.MethodPost, "/places/{id}/knows"},
		{http.MethodPatch, "/places/{id}/knows/{line}"},
		{http.MethodDelete, "/places/{id}/knows/{line}"},
		{http.MethodPost, "/places/{id}/knows/{line}/still-true"},
	} {
		registerSeamRoute(route.method, route.path, knowsRoute)
	}
}

func knowsRoute(b *Bridge, w http.ResponseWriter, r *http.Request, ids map[string]string) {
	b.mu.Lock()
	p := b.places
	b.mu.Unlock()
	if p == nil {
		fail(w, 501, seamNotImplemented)
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	snap, err := p.Store.Snapshot()
	if err != nil {
		p.failStore(w, err, "", nil)
		return
	}
	place, found := snap.Place(ids["id"])
	if !found {
		p.failStore(w, placegraph.ErrNotFound, "", nil)
		return
	}
	var old *placegraph.Line
	for _, line := range snap.Knowledge(place.ID) {
		if line.ID == ids["line"] {
			copy := line
			old = &copy
			break
		}
	}
	if ids["line"] != "" && old == nil {
		failPlaces(w, 404, "not_found", "That line doesn't exist any more.")
		return
	}
	if r.Method == http.MethodGet {
		out := knowsList{Revision: snap.Revision, Lines: []KnowsLine{}, StillTrue: []string{}}
		for _, line := range snap.Knowledge(place.ID) {
			out.Lines = append(out.Lines, p.knowsLine(line))
		}
		for _, line := range placegraph.StillTrueDue(snap.Knowledge(place.ID), p.now()) {
			out.StillTrue = append(out.StillTrue, line.ID)
		}
		write(w, out)
		return
	}
	if place.Archived {
		p.failStore(w, placegraph.ErrArchived, "", nil)
		return
	}
	var ask knowsAsk
	if !readBody(w, r, &ask) || !p.staleRevision(w, ask.IfRevision) {
		return
	}
	var line placegraph.Line
	var conflict *placegraph.AskOnce
	var rc placegraph.Receipt
	switch {
	case r.Method == http.MethodDelete:
		rc, err = p.Store.DeleteLine(old.ID)
	case ids["line"] != "" && r.Method == http.MethodPost:
		if ask.Yes == nil || !*ask.Yes {
			failPlaces(w, 400, "invalid", "Say yes to confirm that this is still true.")
			return
		}
		line, rc, err = p.Store.ConfirmKnowledge(place.ID, old.ID)
	default:
		line, conflict, rc, err = p.Store.WriteKnowledge(place.ID, ids["line"], ask.Text, ask.Supersedes)
	}
	if err != nil {
		p.failStore(w, err, "", nil)
		return
	}
	revision, err := p.Store.Revision()
	if err != nil {
		p.failStore(w, err, "", nil)
		return
	}
	out := knowsMutation{Mutation: Mutation{Revision: revision, Generation: revision, Receipts: []placegraph.Receipt{}, Undo: []string{}, Noop: rc.Noop()}, Ask: conflict}
	if !rc.Noop() {
		out.Receipts = append(out.Receipts, rc)
		out.Undo = append(out.Undo, rc.ID)
		out.Receipt = &rc
	}
	if line.ID != "" {
		value := p.knowsLine(line)
		out.Line = &value
	}
	write(w, out)
	if !rc.Noop() {
		p.publishPlaces(nil)
	}
}

func (p *Places) knowsLine(line placegraph.Line) KnowsLine {
	words := ""
	switch line.Source.Kind {
	case placegraph.LineYouWrote:
		words = "You wrote"
	case placegraph.LineSaidInChat:
		words = "You said in chat"
		for _, project := range p.world(false).Projects {
			for _, chat := range project.Sessions {
				if chat.ID == line.Source.ChatID && chat.Title != "" {
					words = "You said in " + chat.Title
				}
			}
		}
	case placegraph.LineLearned:
		words = placegraph.LearnedFromAnswers(line.Source.Answers)
	case placegraph.LineFile:
		if line.Source.Path != "" {
			words = "From " + line.Source.Path
		}
	}
	return KnowsLine{Line: line, SourceWords: words}
}
