//go:build !windows

package app

import (
	"os"
	"path/filepath"
	"strings"

	configpkg "github.com/Agent-Field/codeaf/internal/seniordev/config"
)

// StateDirEnv names the directory a run keeps its session store in when
// `--state-dir` names none.
const StateDirEnv = "SENIOR_DEV_STATE_DIR"

// stateDirectory is where this run keeps its session store — its database,
// its conversation and the lock that orders them: the --state-dir it was
// given, else SENIOR_DEV_STATE_DIR, else "" for the workspace's own
// .senior-dev ([openDurableSessionsIn]). One inside the workspace is refused
// before anything is spent: the run's checkpoints and the change it hands in
// are taken from that tree, and would carry the store's files with them. Its
// own .senior-dev is the one place in the tree they already leave alone. The
// directory is made here once it is allowed, so one that cannot be is refused
// before anything is spent too.
//
// IT IS JUDGED BEFORE IT IS MADE. Where it would lead is read from the deepest
// part of it that already exists ([resolvedPath]); making it first would leave
// a refused <folder>/state, or every level of <folder>/a/b/c, in the tree the
// refusal was protecting, for the next checkpoint to carry.
func stateDirectory(flagged, workspace string) (dir, refusal string) {
	dir, said := strings.TrimSpace(flagged), "--state-dir"
	if dir == "" {
		value, _ := configpkg.NewEnv(os.LookupEnv).Get(StateDirEnv)
		dir, said = strings.TrimSpace(value), StateDirEnv
	}
	if dir == "" {
		return "", ""
	}
	store, folder := resolvedPath(dir), realDirectory(workspace)
	if within(store, folder) && !within(store, filepath.Join(folder, seniorDevDataDirectory)) {
		return "", said + " " + dir + " is inside the folder senior-dev works in, whose checkpoints and " +
			"handed-in change would carry the store; name a directory outside it"
	}
	if err := os.MkdirAll(store, 0o755); err != nil {
		return "", said + " " + dir + " cannot be made: " + err.Error()
	}
	return realDirectory(store), ""
}

// resolvedPath is path made absolute, with the deepest part of it that already
// exists resolved through every link and the rest joined back on. What does
// not exist yet cannot be a link, so this is where the path will lead once it
// is made.
func resolvedPath(path string) string {
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	rest := ""
	for at := path; ; {
		if real, err := filepath.EvalSymlinks(at); err == nil {
			return filepath.Join(real, rest)
		}
		parent := filepath.Dir(at)
		if parent == at {
			return path
		}
		rest = filepath.Join(filepath.Base(at), rest)
		at = parent
	}
}

// within says whether path is dir or somewhere under it; both are clean and
// absolute. Spelling is not identity where the file system folds case, as
// APFS does by default: when the spellings differ, the directories on the
// way up path that already exist are compared with dir itself.
func within(path, dir string) bool {
	if rel, err := filepath.Rel(dir, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return true
	}
	want, err := os.Stat(dir)
	if err != nil {
		return false
	}
	for at := path; ; at = filepath.Dir(at) {
		if info, err := os.Stat(at); err == nil && os.SameFile(info, want) {
			return true
		}
		if filepath.Dir(at) == at {
			return false
		}
	}
}
