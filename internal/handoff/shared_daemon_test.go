package handoff

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/home"
)

// W1/W2 exercise a composed app chat: the daemon stays alive while its private
// staging folder moves, and the installed tree must still support dirty/seal.
func TestTakeSharedDaemonW1W2(t *testing.T) {
	takeSharedDaemon(t, false, false)
}

// W5 also covers an empty workspace absent after installation. The sender's
// recorded path is not a working directory on the receiver.
func TestTakeEmptyWorkspaceW5(t *testing.T) {
	takeSharedDaemon(t, true, false)
}

// W6 restores B's work and receipt into A's original composed layout. The
// workspace is short, but receipt names under the separate .cell exceed MAX_PATH.
func TestTakeBackComposedW6(t *testing.T) {
	takeSharedDaemon(t, false, true)
}

func takeSharedDaemon(t *testing.T, empty, takeBack bool) {
	t.Helper()
	celltest.RequireEngine(t)
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv("CODEAF_ENGINE_DAEMON", "1")
	defer cellstore.StopSpawned()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	parent := t.TempDir()
	if takeBack {
		// Keep the composed root short enough for a process and its temporary
		// files while making the final 64-character receipt name exceed 260.
		parent = filepath.Join(parent, strings.Repeat("c", max(1, 172-len(parent)-1)))
	}
	source, err := cell.CreateIn(parent, cell.Options{Class: cell.FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	if !empty {
		if err := os.WriteFile(filepath.Join(work, "hello.txt"), []byte("hello from A"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(source.Root, cell.TranscriptPath), []byte("{\"type\":\"session\",\"cwd\":\"/Users/sender/unavailable-project\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a := cellstore.Engine{DataRoot: t.TempDir(), Workspace: work}
	sealed, err := a.Seal(ctx, source, cellstore.TurnInfo{})
	if err != nil {
		t.Fatal(err)
	}
	keys := cellstore.SyncKeys{CellKey: make([]byte, 32), Dedup: make([]byte, 32)}
	objects := blobstore.NewMemory()
	pub := cellsync.Publisher{Engine: a.Sync(keys, "00000000000000e6"), Store: objects}
	if _, err := pub.Upload(ctx, source, sealed.Turn.ID); err != nil {
		t.Fatal(err)
	}
	b := cellstore.Engine{DataRoot: t.TempDir(), WorkspaceOf: func(c cell.Cell) string {
		return filepath.Join(c.Root, "work")
	}}
	root := filepath.Join(t.TempDir(), source.ID)
	stage := cell.Cell{ID: source.ID, Root: stagingOf(root)}
	if err := os.MkdirAll(filepath.Join(stage.Root, "work"), 0700); err != nil {
		t.Fatal(err)
	}
	receiver := b.Sync(keys, "00000000000000e6")
	fetch := cellsync.Fetcher{Engine: receiver, Store: objects, Inbox: receiver.Inbox}
	if err := fetch.Fetch(ctx, stage, sealed.Turn.ID); err != nil {
		t.Fatal(err)
	}
	assertTakeStageReleased(t, stage.Root)
	if empty {
		// Directory entries are not chat content. Exercise installation without
		// relying on the materializer preserving an empty working directory.
		if err := os.Remove(filepath.Join(stage.Root, "work")); err != nil {
			t.Fatal(err)
		}
		// The app files an arriving copy under its origin after fetching. Its
		// installed path can exceed Windows's process-directory limit.
		root = filepath.Join(t.TempDir(), strings.Repeat("a", 100), strings.Repeat("b", 100), source.ID)
		if err := os.MkdirAll(filepath.Dir(root), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := swap(stage.Root, root); err != nil {
		t.Fatalf("W1 rename after shared-daemon fetch: %v", err)
	}
	installed := cell.Cell{ID: source.ID, Root: root}
	if err := b.Follow(ctx, stage, installed); err != nil {
		t.Fatalf("W2 final binding: %v", err)
	}
	if empty {
		tree := filepath.Join(root, "work")
		info, err := os.Stat(tree)
		if err != nil || !info.IsDir() {
			t.Fatalf("W5 followed workspace %q: %v", tree, err)
		}
		bound, err := os.ReadFile(filepath.Join(b.LocalDir(installed), "engine-tree"))
		resolved, resolveErr := filepath.EvalSymlinks(tree)
		if err != nil || resolveErr != nil || string(bound) != resolved {
			t.Fatalf("W5 binding %q, want local tree %q: %v", bound, tree, err)
		}
		if dirty, err := b.Dirty(ctx, installed); err != nil || dirty {
			t.Fatalf("W5 installed head: dirty=%v err=%v", dirty, err)
		}
		return
	}
	path := filepath.Join(root, "work", "hello.txt")
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "hello from A" {
		t.Fatalf("W1 restored content %q: %v", raw, err)
	}
	if dirty, err := b.Dirty(ctx, installed); err != nil || dirty {
		t.Fatalf("W2 installed head: dirty=%v err=%v", dirty, err)
	}
	if err := os.WriteFile(path, []byte("hello from B"), 0600); err != nil {
		t.Fatal(err)
	}
	if dirty, err := b.Dirty(ctx, installed); err != nil || !dirty {
		t.Fatalf("W2 edited installed tree: dirty=%v err=%v", dirty, err)
	}
	installed, err = cell.OpenAt(root, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	returned, err := b.Seal(ctx, installed, cellstore.TurnInfo{})
	if err != nil {
		t.Fatalf("W2 seal at final root: %v", err)
	}
	if err := cellstore.Await(ctx, installed); err != nil {
		t.Fatalf("W2 save at final root: %v", err)
	}
	if !takeBack {
		return
	}
	sender := b.Sync(keys, "00000000000000e6")
	if _, err := (&cellsync.Publisher{Engine: sender, Store: objects}).Upload(ctx, installed, returned.Turn.ID); err != nil {
		t.Fatal(err)
	}
	receipt := filepath.Join(cell.StateDir, "receipts", returned.Turn.Receipt+".json")
	receiptAtA := filepath.Join(source.Root, receipt)
	if len(receiptAtA) <= 260 {
		t.Fatalf("W6 needs a long receipt path, got %d characters", len(receiptAtA))
	}
	back := a.Sync(keys, "00000000000000e6")
	if err := (&cellsync.Fetcher{Engine: back, Store: objects, Inbox: back.Inbox}).Fetch(ctx, source, returned.Turn.ID); err != nil {
		t.Fatalf("W6 take-back into original composed directory: %v", err)
	}
	if raw, err := os.ReadFile(filepath.Join(work, "hello.txt")); err != nil || string(raw) != "hello from B" {
		t.Fatalf("W6 restored work %q: %v", raw, err)
	}
	want, err := os.ReadFile(filepath.Join(installed.Root, receipt))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(receiptAtA); err != nil || string(got) != string(want) {
		t.Fatalf("W6 restored receipt %q: %v", receiptAtA, err)
	}
}
