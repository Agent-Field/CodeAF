package main

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// CompletePath answers files.complete: the directory a typed path names, from
// exactly where it says, narrowed by the half-typed name after its last
// separator. It is the terminal's own path walk ([tui3.CompletePath]) and not
// a second one, so `@~/code/` lists the same folder in the app that the
// terminal's folder browser opens on, with the same names hidden.
//
// A relative path hangs off the agent's workspace — the folder its sentences
// are about — and off home for an agent with none or for no agent at all,
// which is where a new agent starts. One directory is read per call; no file
// is opened and nothing is written.
func (b *appBackend) CompletePath(agent, path string, limit int) (appwire.PathCompletion, error) {
	var root string
	if strings.TrimSpace(agent) != "" {
		a, err := b.find(agent)
		if err != nil {
			return appwire.PathCompletion{}, err
		}
		b.locked(func() { root = a.view.Workspace })
	}
	listed := tui3.CompletePath(path, root, limit)
	out := appwire.PathCompletion{Dir: listed.Dir, More: listed.More, Entries: make([]appwire.PathEntry, 0, len(listed.Entries))}
	switch {
	case listed.Missing:
		out.State = appwire.PathMissing
	case listed.Unreadable:
		out.State = appwire.PathUnreadable
	}
	for _, entry := range listed.Entries {
		out.Entries = append(out.Entries, appwire.PathEntry{Name: entry.Name, Path: entry.Path, Dir: entry.Dir})
	}
	return out, nil
}

var _ appwire.PathCompleter = (*appBackend)(nil)
