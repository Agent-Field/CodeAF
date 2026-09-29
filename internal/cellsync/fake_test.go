package cellsync

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
)

func TestFakeEngineRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := blobstore.NewMemory()
	a := NewFakeEngine(t.TempDir())
	src := cell.Cell{ID: cellID, Root: t.TempDir()}
	a.Seal(src, map[string]string{"a.txt": "one"})
	head := a.Seal(src, map[string]string{"a.txt": "one", "dir/b.txt": "two"})

	ex, err := a.Export(ctx, src, head)
	if err != nil || ex.HeadRID != head || ex.Objects == 0 || len(ex.Frames) != 1 {
		t.Fatalf("export = %+v, %v", ex, err)
	}
	var paths []string
	for _, f := range ex.Frames {
		frame, err := os.ReadFile(f.Path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.PutFrame(ctx, frame); err != nil {
			t.Fatalf("store refused the fake's frame: %v", err)
		}
		paths = append(paths, f.Path)
	}
	if err := a.Published(ctx, src, paths); err != nil {
		t.Fatal(err)
	}
	if again, _ := a.Export(ctx, src, head); len(again.Frames) != 0 {
		t.Fatalf("a published head exported again: %+v", again)
	}

	b := NewFakeEngine(t.TempDir())
	dst := cell.Cell{ID: cellID, Root: t.TempDir()}
	inbox := t.TempDir()
	for {
		want, err := b.Want(ctx, dst, head)
		if err != nil {
			t.Fatal(err)
		}
		if len(want) == 0 {
			break
		}
		for _, rid := range want {
			raw, err := store.Get(ctx, rid)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(inbox, rid), raw, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if n, err := b.Import(ctx, dst, head, inbox); err != nil || n != len(want) {
			t.Fatalf("import = %d, %v; want %d", n, err, len(want))
		}
	}
	if err := b.Materialize(ctx, dst, head); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"a.txt": "one", "dir/b.txt": "two"}
	if got := readFiles(t, dst.Root); !reflect.DeepEqual(got, want) {
		t.Fatalf("materialized %v, want %v", got, want)
	}
}

func TestFakeEngineImportRefusesUnwantedOrTamperedObjects(t *testing.T) {
	ctx := context.Background()
	a := NewFakeEngine(t.TempDir())
	c := cell.Cell{ID: cellID, Root: t.TempDir()}
	head := a.Seal(c, map[string]string{"a": "x"})

	b := NewFakeEngine(t.TempDir())
	inbox := t.TempDir()
	rid, raw := pack(kindBlob, []byte("stranger"))
	os.WriteFile(filepath.Join(inbox, rid), raw, 0o600)
	if _, err := b.Import(ctx, c, head, inbox); err == nil {
		t.Fatal("an unwanted object was imported")
	}
	os.Remove(filepath.Join(inbox, rid))
	os.WriteFile(filepath.Join(inbox, head), raw, 0o600) // right name, other bytes
	if _, err := b.Import(ctx, c, head, inbox); err == nil {
		t.Fatal("a tampered object was imported")
	}
	if want, _ := b.Want(ctx, c, head); len(want) != 1 {
		t.Fatalf("a refused import changed the graph: want %v", want)
	}
}
