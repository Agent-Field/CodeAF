package appwire

import "strings"

// FILES.COMPLETE: the composer's `@` completes a typed path the way the
// terminal's folder browser walks one — directory by directory, from exactly
// where the path says (absolute, `~/`, or relative to the agent's workspace).
// files.find stays the answer to a bare word; this is the answer to a path.
// It reads one directory per call, never a file and never a tree, and it
// never writes.

// The bounds of files.complete.
const (
	// PathCompleteLimit is how many entries an answer carries when the request
	// names no limit.
	PathCompleteLimit = 50
	// PathCompleteMax is the most entries one answer carries; a larger limit
	// is clamped to it.
	PathCompleteMax = 200
)

// What files.complete found when it could not list the directory.
const (
	PathMissing    = "missing"    // nothing there, or not a folder
	PathUnreadable = "unreadable" // there, but it could not be read
)

// PathEntry is one name in the directory a typed path reaches into. Path is
// what the composer writes after the `@` when the entry is chosen: the request's
// path up to its last separator, then Name, then a separator for a folder.
type PathEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Dir  bool   `json:"dir,omitempty"`
}

// PathCompletion is files.complete's answer. Dir is the directory listed,
// absolute; Entries are folders first, then files, the names that begin with
// the half-typed last segment ahead of the ones that only hold it. More says
// matches were left off. State is absent when the directory was listed, and
// says why it was not otherwise — an unreadable folder is never an empty one.
type PathCompletion struct {
	Dir     string      `json:"dir"`
	Entries []PathEntry `json:"entries"`
	More    bool        `json:"more,omitempty"`
	State   string      `json:"state,omitempty" enum:"missing,unreadable"`
}

// PathCompleter is the optional engine half of files.complete. One that is
// not a PathCompleter refuses files.complete as unknown, and the app falls
// back on files.find.
type PathCompleter interface {
	// CompletePath lists the directory path names — the path itself when it
	// ends in a separator, its parent otherwise — narrowed by the half-typed
	// name after the last separator, at most limit entries. A relative path
	// hangs off the agent's workspace, or off home when agent is empty.
	CompletePath(agent, path string, limit int) (PathCompletion, error)
}

func init() {
	methods["files.complete"] = typed(func(e Engine, p struct {
		Agent string `json:"agent"`
		Path  string `json:"path"`
		Limit int    `json:"limit"`
	}) (*PathCompletion, error) {
		c, ok := e.(PathCompleter)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve files.complete")
		}
		if p.Limit < 0 || strings.ContainsAny(p.Path, "\x00\n") {
			return nil, Errorf(CodeBadParams, "files.complete needs a path on one line and a limit of zero or more")
		}
		limit := p.Limit
		if limit == 0 {
			limit = PathCompleteLimit
		}
		got, err := c.CompletePath(strings.TrimSpace(p.Agent), p.Path, min(limit, PathCompleteMax))
		if err != nil {
			return nil, err
		}
		got.Entries = nonNil(got.Entries)
		return &got, nil
	})
}
