package handoff

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/home"
)

// L5 exercises the real fetch transport before the directory move, including
// the native engine's retained handles. No provider or external relay is used.
func TestTakeNativeFetchL5(t *testing.T) {
	celltest.RequireEngine(t)
	t.Setenv(home.EnvVar, t.TempDir())
	defer cellstore.StopSpawned()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	t.Setenv("CODEAF_ENGINE_DAEMON", "")
	source, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source.Root, "hello.txt"), []byte("hello from A"), 0600); err != nil {
		t.Fatal(err)
	}
	a := cellstore.Engine{DataRoot: t.TempDir()}
	sealed, err := a.Seal(ctx, source, cellstore.TurnInfo{})
	if err != nil {
		t.Fatal(err)
	}
	keys := cellstore.SyncKeys{CellKey: make([]byte, 32), Dedup: make([]byte, 32)}
	objects := blobstore.NewMemory()
	sender := a.Sync(keys, "00000000000000e3")
	pub := cellsync.Publisher{Engine: sender, Store: objects}
	if _, err := pub.Upload(ctx, source, sealed.Turn.ID); err != nil {
		t.Fatal(err)
	}
	b := cellstore.Engine{DataRoot: t.TempDir()}
	receiver := b.Sync(keys, "00000000000000e3")
	root := filepath.Join(t.TempDir(), source.ID)
	staged := cell.Cell{ID: source.ID, Root: stagingOf(root)}
	if err := os.MkdirAll(staged.Root, 0700); err != nil {
		t.Fatal(err)
	}
	fetch := cellsync.Fetcher{Engine: receiver, Store: objects, Inbox: receiver.Inbox}
	if err := fetch.Fetch(ctx, staged, sealed.Turn.ID); err != nil {
		t.Fatal(err)
	}
	if err := swap(staged.Root, root); err != nil {
		t.Fatalf("L5 rename after real fetch: %v", err)
	}
	if err := b.Follow(ctx, staged, cell.Cell{ID: source.ID, Root: root}); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(filepath.Join(root, "hello.txt")); err != nil || string(raw) != "hello from A" {
		t.Fatalf("L5 restored content %q: %v", raw, err)
	}
}
