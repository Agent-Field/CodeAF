package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// placeRetiredForks is the sweep's memory of the forks it has already retired,
// in the session's own folder beside meta.json.
//
// THE CHECKPOINT NEVER FORGETS A LANDED FORK. A node's record keeps its
// universe and its merge for as long as the conversation exists, because a
// person reads them, so a sweep that reads only the checkpoint asks furrow
// about every landed fork of every kept conversation on every launch, and
// [Workspace.DropFork] answers a fork furrow no longer holds with success. The
// log said "retired" for the same pair on every launch for days (2026-10-26),
// each one a furrow detection and a fork listing in somebody's project.
//
// IT IS THE SWEEP'S FILE AND NOT THE CHECKPOINT'S. The task store owns
// tasks.json and rewrites it whole whenever the conversation is open again, so
// a mark the sweep put there could be lost to a save, or could lose the save.
// A missing, unreadable or unknown file is an empty memory, which costs one
// more furrow question per fork and never a removal: the retirement itself is
// what proves a removal safe, every time it runs.
const placeRetiredForks = "forks-retired.json"

// retiredForksSchema is the file's version, on told.json's rule: a reader
// refuses a number it does not know rather than guessing at the fields.
const retiredForksSchema = 1

type retiredForks struct {
	Schema int           `json:"schema"`
	Forks  []retiredFork `json:"forks"`
}

// retiredFork names one fork by everything the checkpoint names it by. furrow's
// name carries a random suffix and the directory is the node's own, so a later
// fork of the same node can never be mistaken for one already gone.
type retiredFork struct {
	Ground   string `json:"ground"`
	Universe string `json:"universe"`
	Dir      string `json:"dir"`
}

func retiredForkOf(tree taskTree) retiredFork {
	return retiredFork{
		Ground:   filepath.Clean(strings.TrimSpace(tree.ground)),
		Universe: strings.TrimSpace(tree.universe),
		Dir:      filepath.Clean(strings.TrimSpace(tree.dir)),
	}
}

func loadRetiredForks(dir string) retiredForks {
	raw, err := os.ReadFile(filepath.Join(dir, placeRetiredForks))
	if err != nil {
		return retiredForks{Schema: retiredForksSchema}
	}
	var memory retiredForks
	if json.Unmarshal(raw, &memory) != nil || memory.Schema != retiredForksSchema {
		return retiredForks{Schema: retiredForksSchema}
	}
	return memory
}

func (r retiredForks) holds(tree taskTree) bool {
	want := retiredForkOf(tree)
	for _, fork := range r.Forks {
		if fork == want {
			return true
		}
	}
	return false
}

// remember adds one retired fork and writes the whole file, replacing it in
// one rename so a sweep cut short leaves the previous memory whole.
func (r *retiredForks) remember(dir string, tree taskTree) error {
	if r.holds(tree) {
		return nil
	}
	r.Schema = retiredForksSchema
	r.Forks = append(r.Forks, retiredForkOf(tree))
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".forks-retired-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(append(raw, '\n')); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, placeRetiredForks))
}
