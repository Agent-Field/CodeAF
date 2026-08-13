// Package enginestate names the paths the coding engine keeps its own
// bookkeeping in, once, for everybody who has to reason about them.
//
// It exists because there were three lists and they were three different
// lists. The engine declared one to tell git what to ignore
// (internal/swepro/internal/util/gitexclude.go). aforge kept a second to sweep
// a person's directory after a fallback run and a third to keep checkpoint
// files out of the artifact list, and the third had drifted: it named
// `.plandb.db` and every sqlite sidecar beside it, and had never named the
// `.plandb/` directory the scheduler cuts its worktrees into. A boundary
// between the harness's machinery and somebody's repository cannot be drawn by
// three lists that disagree, so there is one, here, at the only address both
// sides can reach — the engine's tree is `internal/`-scoped to itself, and
// aforge reaches the engine through doors like this one and `codeaf`.
//
// It is deliberately dependency-free. What imports it is the harness's git
// plumbing on one side and the engine's exclusion writer on the other, and
// neither should have to build the other to ask what the engine's own files
// are called.
package enginestate

import (
	"path/filepath"
	"strings"
)

// Path is one entry the engine writes in the directory it runs in.
//
// Directory is not decoration: git's exclude patterns distinguish a directory
// from a file by the trailing slash, and a filesystem sweep has to know
// whether it is removing a tree or a file. One declaration answers both
// because both questions are about the same five names.
type Path struct {
	Name string
	// Directory is whether the engine writes a tree here rather than a file.
	Directory bool
}

// Paths is the engine's own bookkeeping, in the directory it was pointed at.
//
//   - .codeaf/ is the pipeline's state: the plan, the outcomes journal, the
//     resume checkpoint, the contract a run is judged against.
//   - .plandb/ is where the session scheduler cuts its per-task worktrees
//     (wt-<task>), which it merges onto the branch as each one is judged.
//   - .plandb.db and its sqlite sidecars are the plan database's journal.
//
// The engine hardcodes `.codeaf` in some forty joins of its own, so this list
// is a boundary aforge draws around the engine rather than a setting the
// engine reads. Changing a name here does not move the engine's files; it
// changes what everybody agrees the engine's files are called.
var Paths = []Path{
	{Name: ".codeaf", Directory: true},
	{Name: ".plandb", Directory: true},
	{Name: ".plandb.db"},
	{Name: ".plandb.db-shm"},
	{Name: ".plandb.db-wal"},
}

// Names is the list as filesystem entries, for a sweep or a git pathspec.
func Names() []string {
	names := make([]string, 0, len(Paths))
	for _, path := range Paths {
		names = append(names, path.Name)
	}
	return names
}

// ExcludePatterns is the list as git exclusion patterns: a directory carries
// the trailing slash that keeps the pattern from matching a file of the same
// name in somebody's repository.
func ExcludePatterns() []string {
	patterns := make([]string, 0, len(Paths))
	for _, path := range Paths {
		if path.Directory {
			patterns = append(patterns, path.Name+"/")
			continue
		}
		patterns = append(patterns, path.Name)
	}
	return patterns
}

// Holds reports whether a repository-relative path is the engine's own
// bookkeeping rather than anybody's work.
//
// The test is on the first segment, because that is where the engine writes:
// it is pointed at one directory and keeps its state at the top of it. A file
// a person happens to keep at `docs/.codeaf-notes.md` is theirs, and a rule
// that matched anywhere in a path would take it.
//
// A non-directory entry also matches by prefix. Sqlite writes sidecars beside
// a database under names no list can enumerate ahead of time — `-shm`, `-wal`,
// `-journal`, and whatever a future build adds — and the two that are
// enumerated above are there so a reader sees the shape rather than having to
// infer it.
func Holds(path string) bool {
	head, _, _ := strings.Cut(filepath.ToSlash(strings.TrimSpace(path)), "/")
	if head == "" {
		return false
	}
	for _, entry := range Paths {
		if head == entry.Name {
			return true
		}
		if !entry.Directory && strings.HasPrefix(head, entry.Name) {
			return true
		}
	}
	return false
}
