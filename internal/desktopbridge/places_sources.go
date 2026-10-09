package desktopbridge

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/placegraph"
)

// SourceCheck is what could be said about a source without reading it. It is
// stat-only: no file is opened, no folder listed, no address fetched. The
// resolution of a source into what a chat is actually told is a later layer's.
type SourceCheck struct {
	// State is ok, missing, unreadable or unknown. A link is always unknown:
	// reaching it would be a network call this door does not make.
	State string `json:"state"`
	Note  string `json:"note,omitempty"`
	// Title is a referenced conversation's own title, when the world has it.
	Title string `json:"title,omitempty"`
}

// SourceView is a source with its check.
type SourceView struct {
	placegraph.Source
	Check SourceCheck `json:"check"`
}

func (x *placeIndex) sourceView(src placegraph.Source) SourceView {
	return SourceView{Source: src, Check: x.checkSource(src)}
}

func (x *placeIndex) checkSource(src placegraph.Source) SourceCheck {
	switch src.Kind {
	case placegraph.SourceFolder, placegraph.SourceRepo, placegraph.SourceFile:
		info, err := os.Stat(src.Ref)
		switch {
		case os.IsNotExist(err):
			return SourceCheck{State: "missing", Note: "This path no longer exists."}
		case err != nil:
			return SourceCheck{State: "unreadable", Note: "This path can't be read."}
		case src.Kind == placegraph.SourceFile && info.IsDir():
			return SourceCheck{State: "unreadable", Note: "This was a file and is now a folder."}
		case src.Kind != placegraph.SourceFile && !info.IsDir():
			return SourceCheck{State: "unreadable", Note: "This was a folder and is now a file."}
		}
		return SourceCheck{State: "ok"}
	case placegraph.SourceChat:
		if row := x.rows[src.Ref]; row != nil {
			return SourceCheck{State: "ok", Title: row.Title}
		}
		return SourceCheck{State: "missing", Note: "This conversation isn't saved on this machine."}
	case placegraph.SourceURL:
		return SourceCheck{State: "unknown", Note: "Links aren't fetched until a chat uses them."}
	}
	return SourceCheck{State: "unknown"}
}

type sourceAsk struct {
	IfRevision *uint64               `json:"ifRevision"`
	Kind       placegraph.SourceKind `json:"kind"`
	Ref        string                `json:"ref"`
	Label      string                `json:"label"`
	SourceID   string                `json:"sourceId"`
}

// normalizeSource validates and canonicalises a new source's reference. A local
// path must be absolute and exist; symlinks are resolved so two spellings of one
// folder are one source. Links must be http or https and carry no credentials.
func (x *placeIndex) normalizeSource(p *Places, kind placegraph.SourceKind, ref string) (string, string, int, string) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", "", 400, "Say what to add."
	}
	switch kind {
	case placegraph.SourceFolder, placegraph.SourceRepo, placegraph.SourceFile:
		if !filepath.IsAbs(ref) {
			return "", "", 400, "A folder or file needs its full path."
		}
		real, err := filepath.EvalSymlinks(filepath.Clean(ref))
		if err != nil {
			return "", "", 404, "That path doesn't exist."
		}
		info, err := os.Stat(real)
		if err != nil {
			return "", "", 404, "That path doesn't exist."
		}
		switch {
		case kind == placegraph.SourceFile && info.IsDir():
			return "", "", 400, "That is a folder, not a file."
		case kind != placegraph.SourceFile && !info.IsDir():
			return "", "", 400, "That is a file, not a folder."
		}
		if kind == placegraph.SourceRepo {
			if _, err := os.Stat(filepath.Join(real, ".git")); err != nil {
				return "", "", 400, "That folder isn't a git repository."
			}
		}
		return real, filepath.Base(real), 0, ""
	case placegraph.SourceURL:
		u, err := url.Parse(ref)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return "", "", 400, "Only web addresses starting with http or https can be added."
		}
		if u.User != nil {
			return "", "", 400, "A link with a password in it can't be added."
		}
		return u.String(), u.Host, 0, ""
	case placegraph.SourceChat:
		if !x.knowsChat(p, ref) {
			return "", "", 404, "That conversation isn't saved on this machine."
		}
		label := ""
		if row := x.rows[ref]; row != nil {
			label = row.Title
		}
		return ref, label, 0, ""
	}
	return "", "", 400, "That kind of source isn't one of folder, repo, file, link or chat."
}

func (p *Places) addSource(w http.ResponseWriter, r *http.Request, id string) {
	var ask sourceAsk
	if !readBody(w, r, &ask) {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.staleRevision(w, ask.IfRevision) {
		return
	}
	x, ok := p.open(w, true)
	if !ok {
		return
	}
	pl, found := x.snap.Place(id)
	if !found {
		failPlaces(w, 404, "not_found", "That place doesn't exist any more.")
		return
	}
	ref, label, status, sentence := x.normalizeSource(p, ask.Kind, ask.Ref)
	if status != 0 {
		failPlaces(w, status, "invalid_source", sentence)
		return
	}
	for _, have := range pl.Context.Sources {
		if have.Kind == ask.Kind && have.Ref == ref {
			failPlaces(w, 409, "duplicate_source", "That is already in this place.")
			return
		}
	}
	if strings.TrimSpace(ask.Label) != "" {
		label = strings.TrimSpace(ask.Label)
	}
	c := pl.Context
	c.Sources = append(append([]placegraph.Source{}, c.Sources...), placegraph.Source{Kind: ask.Kind, Ref: ref, Label: label, AddedBy: placegraph.AddedByYou})
	rc, err := p.Store.SetContext(id, c)
	if err != nil {
		p.failStore(w, err, "", nil)
		return
	}
	var b batch
	b.add(rc)
	p.finish(w, &b, id, nil)
}

func (p *Places) removeSource(w http.ResponseWriter, r *http.Request, id string) {
	var ask sourceAsk
	if !readBody(w, r, &ask) {
		return
	}
	if ask.SourceID == "" {
		failPlaces(w, 400, "invalid", "Say which source to remove.")
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.staleRevision(w, ask.IfRevision) {
		return
	}
	snap, err := p.Store.Snapshot()
	if err != nil {
		p.failStore(w, err, "", nil)
		return
	}
	pl, found := snap.Place(id)
	if !found {
		failPlaces(w, 404, "not_found", "That place doesn't exist any more.")
		return
	}
	c := pl.Context
	c.Sources = nil
	removed := false
	for _, s := range pl.Context.Sources {
		if s.ID == ask.SourceID {
			removed = true
			continue
		}
		c.Sources = append(c.Sources, s)
	}
	if !removed {
		failPlaces(w, 404, "not_found", "That source isn't in this place any more.")
		return
	}
	rc, err := p.Store.SetContext(id, c)
	if err != nil {
		p.failStore(w, err, "", nil)
		return
	}
	var b batch
	b.add(rc)
	p.finish(w, &b, id, nil)
}
