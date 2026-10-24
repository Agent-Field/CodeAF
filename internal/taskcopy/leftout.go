package taskcopy

import (
	"path"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/inventory"
	"github.com/Agent-Field/codeaf/internal/rebuild"
)

// A task copy carries only what its repository tracks or has changed, so the
// install folders it has installed for itself never travel. Nothing is left out
// that was going to be carried; what the record needs is to say so, so the next
// machine can tell the agent that the task's dependencies are to be installed
// again. This is the same rule the seal applies to the workspace, run over each
// live copy, and its entries are filed under the copy's own path.

// OwnsWithheld reports whether a record entry names a folder of a task copy,
// which is the entries this package writes and the seal's own step leaves alone.
func OwnsWithheld(recordPath string) bool { return strings.HasPrefix(recordPath, cell.TreesDir+"/") }

// noteLeftOut replaces the record's entries for task copies with the install
// folders each live copy holds.
func noteLeftOut(c cell.Cell, live []string) error {
	var entries []inventory.Withheld
	for _, tree := range live {
		entries = append(entries, leftOutOf(tree)...)
	}
	return inventory.Record(c.Root, func(inv *inventory.Inventory) { inv.SetWithheld(OwnsWithheld, entries) })
}

// leftOutOf is the install folders of one copy that a lockfile it carries
// rebuilds, named under the copy's own folder.
func leftOutOf(tree string) []inventory.Withheld {
	task := filepath.Base(tree)
	found := rebuild.Scan(tree, nil, rebuild.Found{}, func(rel string) bool { return rel == ".git" })
	var out []inventory.Withheld
	for _, folder := range found.Folders {
		lock, ok := rebuild.LockFor(tree, folder, func(lock string) bool { return rebuild.Tracked(tree, lock) })
		if ok && !rebuild.Tracked(tree, folder) {
			out = append(out, inventory.Withheld{Path: path.Join(cell.TreesDir, task, folder), Lock: path.Join(cell.TreesDir, task, lock)})
		}
	}
	return out
}
