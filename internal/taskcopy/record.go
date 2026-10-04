package taskcopy

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/cell"
)

const (
	// recordName is the file that says where a copy stood, and filesDir holds
	// its changed files beside it. They are separate names so a task file called
	// tree.json cannot collide with the record.
	recordName = "tree.json"
	filesDir   = "files"
	// bundleName holds the commits the copy made that no other ref of its
	// repository holds, so a copy with a repository of its own arrives with its
	// branch's tip.
	bundleName = "commits.bundle"
)

// record is what a restore needs to cut a copy again: the branch it was on (empty
// for a detached head), the commit it was at, the files it had deleted, and
// whether it was a linked worktree rather than a fork with a repository of its
// own, because the branch of a worktree lives in the project and a fork's does
// not, so a copy must come back as the kind it was.
// The changed files themselves sit beside it under [filesDir], and the record
// holds the permission bits each of them had, because the folder they travel
// through is written by whatever umask the machine has and a copy must come back
// with the modes it left with.
type record struct {
	Branch  string                 `json:"branch,omitempty"`
	Head    string                 `json:"head"`
	Deleted []string               `json:"deleted,omitempty"`
	Linked  bool                   `json:"linked,omitempty"`
	Modes   map[string]fs.FileMode `json:"modes,omitempty"`
}

// modeOf is the mode a carried file is put back with: the one the record kept,
// or the one the carried file itself has for a record from before modes were kept.
func (r record) modeOf(rel string, carried fs.FileInfo) fs.FileMode {
	if mode, ok := r.Modes[rel]; ok {
		return mode
	}
	return carried.Mode().Perm()
}

// carriedRoot is the folder of a cell where the seal keeps its task copies.
func carriedRoot(c cell.Cell) string { return filepath.Join(c.Root, cell.CarriedTreesPath) }

// liveRoot is the folder of a cell where its task copies work.
func liveRoot(c cell.Cell) string { return filepath.Join(c.Root, cell.TreesDir) }

func writeRecord(dir string, r record) error {
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, recordName), raw, 0o600)
}

func readRecord(dir string) (record, error) {
	var r record
	raw, err := os.ReadFile(filepath.Join(dir, recordName))
	if err != nil {
		return r, err
	}
	return r, json.Unmarshal(raw, &r)
}

// Carried is the names of the task copies the cell carries, in order.
func Carried(c cell.Cell) []string {
	entries, _ := os.ReadDir(carriedRoot(c))
	var names []string
	for _, e := range entries {
		if _, err := os.Stat(filepath.Join(carriedRoot(c), e.Name(), recordName)); err == nil {
			names = append(names, e.Name())
		}
	}
	return names
}
