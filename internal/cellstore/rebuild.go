package cellstore

import (
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/inventory"
	"github.com/Agent-Field/codeaf/internal/keys"
	"github.com/Agent-Field/codeaf/internal/rebuild"
	"github.com/Agent-Field/codeaf/internal/taskcopy"
)

// Left is an install folder a seal leaves out and the lockfile that rebuilds it.
type Left struct{ Path, Lock string }

// Screened is what a screening found besides secrets: the install folders left
// out of the seal, and the lockfiles seen in the tree.
type Screened struct {
	Folders []Left
	Locks   []string
}

func (s Screened) paths() []string {
	out := make([]string, len(s.Folders))
	for i, f := range s.Folders {
		out[i] = f.Path
	}
	return out
}

// leftOut is the install folders this seal leaves out because a lockfile that
// travels rebuilds them. It is the rule of docs/STAGE-1-CONTRACTS.md section
// 19.4, and each folder that fails any test of it travels with the seal exactly
// as before, so being wrong costs the bytes the seal would have carried anyway.
func (g Guard) leftOut(tree string, changed, locks []string, policy policyFile) Screened {
	found := rebuild.Scan(tree, changed, rebuild.Found{Folders: policy.rebuilt, Locks: locks}, policy.isLeftOut)
	out := Screened{Locks: found.Locks}
	for _, folder := range found.Folders {
		if lock, ok := g.licence(tree, folder, policy); ok {
			out.Folders = append(out.Folders, Left{Path: folder, Lock: lock})
		}
	}
	return out
}

// licence is the lockfile that lets folder stay out of the seal, when there is
// one: the person has not excluded the folder themselves, a lock that will be
// in the seal rebuilds it, and nothing in it is source the project tracks.
func (g Guard) licence(tree, folder string, policy policyFile) (string, bool) {
	if policy.isLeftOut(folder) {
		return "", false
	}
	lock, ok := rebuild.LockFor(tree, folder, func(lock string) bool { return g.inSeal(tree, lock, policy) })
	return lock, ok && !rebuild.Tracked(tree, folder)
}

// inSeal reports whether a lockfile is carried by the seal: the person has not
// excluded it and the secret guard would not hold it back. The other machine
// rebuilds from what it receives, so a lock that does not arrive licenses nothing.
func (g Guard) inSeal(tree, lock string, policy policyFile) bool {
	if policy.isLeftOut(lock) {
		return false
	}
	return len(keys.Scanner{Ledger: g.Ledger}.Paths(tree, []string{lock})) == 0
}

// ownedBySeal is the record entries the seal's own step writes: the task-copy
// step writes those of the copies, and neither erases the other's.
func ownedBySeal(path string) bool { return !taskcopy.OwnsWithheld(path) }

// knownLocks is the lockfiles the record already names, so a seal that looks at
// only the changed paths does not forget the rest.
func knownLocks(c cell.Cell) []string {
	s, err := inventory.Open(c.Root)
	if err != nil {
		return nil
	}
	return s.Snapshot().Lockfiles
}

// noteLeftOut writes into the record, in the same seal that leaves a folder out,
// which folders those are and what rebuilds each. It is what makes the record
// and the exclusion one fact: a folder is never left out without the next
// machine being able to read that it was.
func noteLeftOut(c cell.Cell, tree string, s Screened, calls []Executed) error {
	return inventory.Record(c.Root, func(inv *inventory.Inventory) {
		inv.Lockfiles = s.Locks
		inv.SetWithheld(ownedBySeal, withheldEntries(inv.Withheld, s.Folders, calls, modifiedAt(tree)))
	})
}

// withheldEntries is one record entry per folder left out. A folder keeps the
// command that made it until a later call is seen to change it; one that turns
// up in this seal is credited to the newest successful command of the batch,
// since a command line does not say what it wrote, unless the folder is older
// than that command: then it was there before and nobody is named.
func withheldEntries(before []inventory.Withheld, folders []Left, calls []Executed, modified modTime) []inventory.Withheld {
	known := map[string]inventory.Withheld{}
	for _, w := range before {
		known[w.Path] = w
	}
	out := make([]inventory.Withheld, 0, len(folders))
	for _, f := range folders {
		prev, seen := known[f.Path]
		w := inventory.Withheld{Path: f.Path, Lock: f.Lock, MadeBy: prev.MadeBy, Cwd: prev.Cwd}
		if call, ok := newestMaker(f.Path, calls, !seen, modified); ok {
			w.MadeBy, w.Cwd = inventory.Cleaned(call.Command), inventory.Cwd(call.Dir)
		}
		out = append(out, w)
	}
	return out
}

// newestMaker is the latest successful command that could have written the
// folder: one whose known changes reach into it, or, when the folder is new to
// the record, one that says nothing about what it changed but could have.
func newestMaker(folder string, calls []Executed, fresh bool, modified modTime) (Executed, bool) {
	for i := len(calls) - 1; i >= 0; i-- {
		call := calls[i]
		if call.Command != "" && call.Call.Exit == 0 && wrote(call, folder, fresh && !predates(folder, call, modified)) {
			return call, true
		}
	}
	return Executed{}, false
}

func wrote(call Executed, folder string, fresh bool) bool {
	if call.Changed == nil {
		return fresh
	}
	for _, path := range call.Changed {
		if inRules([]string{folder}, path) {
			return true
		}
	}
	return false
}

// modTime says when a folder of the tree was last modified, in Unix
// milliseconds; false when it cannot be read.
type modTime func(folder string) (int64, bool)

// modifiedAt reads folder modification times under the tree.
func modifiedAt(tree string) modTime {
	return func(folder string) (int64, bool) {
		info, err := os.Stat(filepath.Join(tree, folder))
		if err != nil {
			return 0, false
		}
		return info.ModTime().UnixMilli(), true
	}
}

// predates reports whether the folder was last touched before the call began.
// A command that says nothing about what it changed is credited with a folder
// new to the record, but a folder whose newest change is older than the call
// cannot be its work: it was in the tree before the chat reached it. A folder
// whose time cannot be read is not excused, so the old credit stands.
func predates(folder string, call Executed, modified modTime) bool {
	at, ok := modified(folder)
	return ok && at < call.Call.Started
}
