package desktopbridge

// GET /roots is the list open_path and reveal_path are allowed to use.
//
// THE RENDERER DOES NOT NAME THE ROOT. A workspace string from the window
// would let the page open any path it can spell. The list is built here, from
// engines this bridge actually holds and from the place graph on this machine,
// and the native side confines to it.
//
// ServeHTTP routes here right after the token check:
//
//	if path == "/roots" {
//		b.roots(w, r)
//		return
//	}
//
// The token is checked here as well, so a direct call cannot skip it.

import (
	"errors"
	"io/fs"
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/session"
)

// rootsBody is GET /roots. Roots is always an array: a forwarded engine, and
// a local engine with nothing to name, both answer [] rather than null.
type rootsBody struct {
	Roots []string `json:"roots"`
}

func (b *Bridge) roots(w http.ResponseWriter, r *http.Request) {
	if !b.tokenOK(r) {
		fail(w, http.StatusUnauthorized, "engine connection required")
		return
	}
	if !needGet(w, r) {
		return
	}
	list, err := b.openRoots()
	if err != nil {
		fail(w, http.StatusInternalServerError, "the place list could not be read")
		return
	}
	write(w, rootsBody{Roots: list})
}

// openRoots is the union the native confine uses. A conversation whose engine
// is not on this machine contributes nothing: its workspace, its session
// folder and its referred places are paths on the other machine. When every
// attached engine is forwarded, the whole answer is empty, place sources
// included, so nothing on this disk is opened as though it were that engine's.
// With no conversation open yet the bridge itself is local, the same reading
// as engineStatus, and the place graph's folders are still roots.
func (b *Bridge) openRoots() ([]string, error) {
	b.mu.Lock()
	sessions := make([]*conversation, 0, len(b.sessions))
	for _, s := range b.sessions {
		if s != nil {
			sessions = append(sessions, s)
		}
	}
	places := b.places
	b.mu.Unlock()

	seen := map[string]struct{}{}
	roots := []string{}
	add := func(path string) {
		path = canonicalRoot(path)
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		roots = append(roots, path)
	}

	// Zero conversations means nothing has been opened elsewhere.
	local := len(sessions) == 0
	for _, s := range sessions {
		if !s.conn.Local {
			continue
		}
		local = true
		add(s.conn.Welcome.Workspace)
		add(sessionFolder(s.conn.Welcome.SessionFile))
		for _, path := range saidPlacePaths(s) {
			add(path)
		}
	}
	if !local {
		sort.Strings(roots)
		return roots, nil
	}
	if places != nil && places.Store != nil {
		snap, err := places.Store.Snapshot()
		if err != nil {
			return nil, err
		}
		for _, pl := range snap.Places {
			for _, src := range pl.Context.Sources {
				// A file, a link or another chat is not a directory the
				// confine can stand on. Widening a file to its parent would
				// hand over a folder nobody named.
				if src.Kind == placegraph.SourceFolder || src.Kind == placegraph.SourceRepo {
					add(src.Ref)
				}
			}
		}
	}
	sort.Strings(roots)
	return roots, nil
}

// sessionFolder is the directory that holds the conversation's own files.
// The transcript path itself is not a root.
func sessionFolder(sessionFile string) string {
	sessionFile = strings.TrimSpace(sessionFile)
	if sessionFile == "" || !filepath.IsAbs(sessionFile) {
		return ""
	}
	return filepath.Dir(sessionFile)
}

// saidPlacePaths are the folders this conversation's person named. A place
// the work merely kept is a cache of a ground, not a root, and the workspace
// is already on the list on its own. The live engine's set wins when the
// agent can say it; otherwise the session folder's meta is the record, and
// only because this conversation is local (the caller already checked).
func saidPlacePaths(s *conversation) []string {
	if s.conn.Agent != nil {
		if door, ok := s.conn.Agent.(interface{ Places() []session.PlaceRef }); ok {
			var out []string
			for _, place := range door.Places() {
				if place.Arrival == session.PlaceSaid {
					out = append(out, place.Path)
				}
			}
			return out
		}
	}
	dir := sessionFolder(s.conn.Welcome.SessionFile)
	if dir == "" {
		return nil
	}
	meta, err := session.LoadMeta(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, place := range meta.Places {
		if place.Arrival == session.PlaceSaid {
			out = append(out, place.Path)
		}
	}
	return out
}

// canonicalRoot is one spelling for one directory, so a symlink and its
// target are not two roots. An existing path is resolved. A path that is not
// there yet keeps the resolved spelling of the nearest existing parent with
// the missing suffix put back, which is the rule session uses so two writings
// of one folder do not drift. Anything that is not absolute is dropped: the
// engine did not name a place on this disk.
func canonicalRoot(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || !filepath.IsAbs(path) {
		return ""
	}
	path = filepath.Clean(path)
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return path
	}
	parent := filepath.Dir(path)
	if parent == path {
		return path
	}
	base := canonicalRoot(parent)
	if base == "" {
		return path
	}
	return filepath.Join(base, filepath.Base(path))
}
