// Package chatfiles carries the files a chat owns that sit beside its cell and
// that its own transcript names by path: the logs of background jobs, the full
// output a long tool result was cut to, and the journals of its tasks.
//
// The sealed tree holds the workspace and the cell's .cell/ folder, and none of
// these live in either, so a chat that moved arrived with messages saying "log
// at .../logs/jobs/2.log" and no such file. They belong to the chat (it made
// them and its messages cite them), so they travel with it, by the same road as
// a task's copy: mirrored into .cell/ at every seal ([Carry.Compose]) and put
// back beside the chat on the other machine ([Carry.Restore]).
//
// WHAT STAYS BEHIND, AND IS SAID SO. A file over [MaxFileBytes], a file past
// the [MaxTotalBytes] a chat carries in all (the oldest go first) and a file
// that looks like it holds a secret are not carried. Each is named in the
// inventory's withheld list with the reason, so the receiving agent is told it
// stayed on the other machine instead of finding a path that opens nothing.
//
// WHAT IS NOT HERE. Derived files (card.json, plandb.db, meta.json) are made
// again on open, and the task copies under trees/ have their own carrier
// (internal/taskcopy).
package chatfiles

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/inventory"
	"github.com/Agent-Field/codeaf/internal/keys"
	"github.com/Agent-Field/codeaf/internal/mirror"
)

const (
	// MaxFileBytes is the largest file a chat carries. A job log that grew past
	// it is named as left behind rather than sent, because one runaway log must
	// not make every seal and every take heavy.
	MaxFileBytes = 1 << 20
	// MaxTotalBytes is the most a chat carries in all, newest files first.
	MaxTotalBytes = 16 << 20

	// CarriedPath is where a seal keeps the files, under the same relative
	// paths they have beside the chat.
	CarriedPath = cell.StateDir + "/files"
)

// roots are the folders beside a chat that hold what its messages name.
var roots = []string{"logs/jobs", "logs/stubs", "tasks"}

// OwnsWithheld reports whether a record entry names one of these files, which
// are the entries this package writes and the seal's other steps leave alone.
func OwnsWithheld(recordPath string) bool {
	return slices.ContainsFunc(roots, func(r string) bool { return strings.HasPrefix(recordPath, r+"/") })
}

// Carry is the mechanism: [Carry.Compose] on the seal side, [Carry.Restore] on
// the take side. It needs nothing, so the zero value works.
type Carry struct{}

// file is one candidate: where it is, how big, and when it last changed.
type file struct {
	rel  string
	info fs.FileInfo
}

// Compose mirrors the chat's files into the cell, within the limits, and
// records in the inventory what it left out and where the files were.
func (Carry) Compose(c cell.Cell) error {
	carried, withheld := within(candidates(c.Root))
	rels := relsOf(carried)
	wrote, err := mirror.Sync(c.Root, filepath.Join(c.Root, CarriedPath), rels)
	if err != nil {
		return fmt.Errorf("carry chat files: %w", err)
	}
	kept, secret := screen(c, rels, wrote)
	withheld = append(withheld, secret...)
	return note(c, len(kept) > 0, withheld)
}

// candidates is every file under the roots that is evidence and not the
// machine's own bookkeeping, newest first.
func candidates(root string) []file {
	var out []file
	for _, r := range roots {
		_ = filepath.WalkDir(filepath.Join(root, filepath.FromSlash(r)), func(p string, d fs.DirEntry, err error) error {
			if err != nil || !d.Type().IsRegular() || bookkeeping(d.Name()) {
				return nil
			}
			if info, err := d.Info(); err == nil {
				rel, _ := filepath.Rel(root, p)
				out = append(out, file{filepath.ToSlash(rel), info})
			}
			return nil
		})
	}
	slices.SortFunc(out, func(a, b file) int { return b.info.ModTime().Compare(a.info.ModTime()) })
	return out
}

// bookkeeping is the files a job registry keeps beside the logs it prunes: they
// say nothing about the work and mean nothing on another machine.
func bookkeeping(name string) bool {
	return strings.HasPrefix(name, ".retention") || strings.HasSuffix(name, ".retention")
}

// within splits the candidates into the files that fit the limits and the
// withheld entries naming those that do not.
func within(all []file) (carried []file, left []inventory.Withheld) {
	total := int64(0)
	for _, f := range all {
		switch {
		case f.info.Size() > MaxFileBytes:
			left = append(left, stayed(f.rel, fmt.Sprintf("over the %d MiB carry limit for one file", MaxFileBytes>>20)))
		case total+f.info.Size() > MaxTotalBytes:
			left = append(left, stayed(f.rel, fmt.Sprintf("past the %d MiB a chat carries in all", MaxTotalBytes>>20)))
		default:
			total += f.info.Size()
			carried = append(carried, f)
		}
	}
	return carried, left
}

func stayed(rel, why string) inventory.Withheld {
	return inventory.Withheld{Path: rel, Why: why + "; it stayed on the machine that made it"}
}

func relsOf(files []file) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.rel
	}
	return out
}

// screen takes out of the carried copy every file the secret scanner holds
// back, by the same scanner the seal uses on the workspace. Only files just
// written are read: a file the mirror left alone was judged when it was
// written. It answers the files still carried and the entries for the others.
func screen(c cell.Cell, rels, wrote []string) (kept []string, left []inventory.Withheld) {
	held := map[string]bool{}
	for _, f := range (keys.Scanner{}).Paths(c.Root, wrote) {
		held[f.Path] = true
		_ = os.Remove(filepath.Join(c.Root, CarriedPath, filepath.FromSlash(f.Path)))
		left = append(left, stayed(f.Path, "it looks like it holds a secret"))
	}
	for _, rel := range rels {
		if !held[rel] {
			kept = append(kept, rel)
		}
	}
	return kept, left
}

// note replaces the inventory's entries for these files, and names the chat's
// folder when any file was carried so the other machine can say where they are.
func note(c cell.Cell, carried bool, withheld []inventory.Withheld) error {
	dir := ""
	if carried {
		dir = c.Root
	}
	return inventory.Record(c.Root, func(inv *inventory.Inventory) {
		inv.ChatDir = dir
		inv.SetWithheld(OwnsWithheld, withheld)
	})
}

// Restore puts the carried files back beside the chat, over what is there: the
// carried copy is the newest the chat has, and a file only this machine has is
// left alone. It answers how many it wrote.
func (Carry) Restore(c cell.Cell) (int, error) {
	from := filepath.Join(c.Root, CarriedPath)
	n := 0
	err := filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		if !filepath.IsLocal(rel) {
			return fmt.Errorf("carried path %q leaves the chat folder", rel)
		}
		changed, err := mirror.CopyIfChanged(p, filepath.Join(c.Root, rel))
		if changed {
			n++
		}
		return err
	})
	if os.IsNotExist(err) {
		return 0, nil
	}
	return n, err
}
