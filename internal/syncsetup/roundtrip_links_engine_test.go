//go:build engine

package syncsetup

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
)

// TestEngineTakeKeepsHardLinkedFilesLinked is the take of a folder in which two
// paths are one file: on the receiving machine they are one file again, not two
// files of equal bytes. A file that shares its inode only with something outside
// the folder (what a warm take's seeded staging folder holds) is not tied to
// anything, and the file outside is not touched.
func TestEngineTakeKeepsHardLinkedFilesLinked(t *testing.T) {
	r := newDriveRig(t)
	writeFile(t, filepath.Join(r.work, "shared.txt"), "one file, two names\n")
	if err := os.MkdirAll(filepath.Join(r.work, "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(r.work, "shared.txt"), filepath.Join(r.work, "deep", "alias.txt")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(r.work, "lone.txt"), "alone\n")
	outside := filepath.Join(t.TempDir(), "seed.txt")
	writeFile(t, outside, "seeded\n")
	if err := os.Link(outside, filepath.Join(r.work, "seeded.txt")); err != nil {
		t.Fatal(err)
	}

	var notices noticeLog
	c := r.startChat(&notices)
	c.mustSay()
	waitFor(t, "the turn to be durable", func() bool { return r.directoryHead(r.cell.ID) == r.head() })

	workB := t.TempDir()
	cellB, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	engB := cellstore.EngineFor(workB)
	engB.DataRoot, engB.Binary, engB.Transport = t.TempDir(), r.bin, cellstore.Spawn{Binary: r.bin}
	sync := engB.Sync(cellstore.SyncKeys{CellKey: r.b.Identity.CellKey(), Dedup: r.b.Identity.DedupSecret()}, r.b.Ledger)
	fetch := cellsync.Fetcher{Engine: sync, Store: r.b.Store, Inbox: sync.Inbox}
	if err := fetch.Fetch(context.Background(), cellB, r.head()); err != nil {
		t.Fatal(err)
	}

	shared, alias := statOf(t, filepath.Join(workB, "shared.txt")), statOf(t, filepath.Join(workB, "deep", "alias.txt"))
	if !os.SameFile(shared, alias) {
		t.Fatal("the two names of one file arrived as two files")
	}
	if os.SameFile(shared, statOf(t, filepath.Join(workB, "lone.txt"))) {
		t.Fatal("an unrelated file was linked to the pair")
	}
	if os.SameFile(statOf(t, filepath.Join(workB, "seeded.txt")), statOf(t, outside)) {
		t.Fatal("a file that only shared an inode with something outside the folder stayed tied to it")
	}
	if got, _ := os.ReadFile(outside); string(got) != "seeded\n" {
		t.Fatalf("the file outside the folder changed: %q", got)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func statOf(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}
