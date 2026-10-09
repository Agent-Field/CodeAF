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
// .senior-dev ([openDurableSessionsIn]). The directory is made here, so one
// that cannot be is refused before anything is spent, and so is one inside
// the workspace: the run's checkpoints and the change it hands in are taken
// from that tree, and would carry the store's files with them. Its own
// .senior-dev is the one place in the tree they already leave alone.
func stateDirectory(flagged, workspace string) (dir, refusal string) {
	dir, said := strings.TrimSpace(flagged), "--state-dir"
	if dir == "" {
		value, _ := configpkg.NewEnv(os.LookupEnv).Get(StateDirEnv)
		dir, said = strings.TrimSpace(value), StateDirEnv
	}
	if dir == "" {
		return "", ""
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", said + " " + dir + " cannot be made: " + err.Error()
	}
	store, folder := realDirectory(dir), realDirectory(workspace)
	if within(store, folder) && !within(store, filepath.Join(folder, seniorDevDataDirectory)) {
		return "", said + " " + dir + " is inside the folder senior-dev works in, whose checkpoints and " +
			"handed-in change would carry the store; name a directory outside it"
	}
	return store, ""
}

// within says whether path is dir or somewhere under it; both are clean and
// absolute.
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
