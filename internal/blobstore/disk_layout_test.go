package blobstore_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
)

// tree lists every path under root with the bytes of every file, so two
// snapshots can be compared for a created path or an overwritten file.
func tree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			out[rel+"/"] = ""
			return nil
		}
		b, err := os.ReadFile(path)
		out[rel] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestDiskCreatesOnlyFramesAndObjects(t *testing.T) {
	root := t.TempDir()
	d, err := blobstore.NewDisk(root)
	if err != nil {
		t.Fatal(err)
	}
	frame, _ := blobstore.Encode(testKey, []blobstore.Object{obj("a", "one"), obj("b", "two")})
	if _, err := d.PutFrame(context.Background(), frame); err != nil {
		t.Fatal(err)
	}
	for path := range tree(t, root) {
		top := strings.TrimSuffix(strings.SplitN(path, string(filepath.Separator), 2)[0], "/")
		if top != "frames" && top != "objects" {
			t.Errorf("unexpected path %q under the store root", path)
		}
	}
	if entries, _ := os.ReadDir(root); len(entries) != 2 {
		t.Errorf("root holds %d entries, want frames and objects only", len(entries))
	}
}

func TestDiskLeavesNoTemporaryFiles(t *testing.T) {
	root := t.TempDir()
	d, _ := blobstore.NewDisk(root)
	frame, _ := blobstore.Encode(testKey, []blobstore.Object{obj("a", "one")})
	if _, err := d.PutFrame(context.Background(), frame); err != nil {
		t.Fatal(err)
	}
	for path := range tree(t, root) {
		if strings.Contains(path, ".tmp-") {
			t.Errorf("temporary file left behind: %s", path)
		}
	}
}

func TestDiskNeverOverwritesAnExistingFile(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	d, _ := blobstore.NewDisk(root)
	first, _ := blobstore.Encode(testKey, []blobstore.Object{obj("a", "one"), obj("b", "two")})
	if _, err := d.PutFrame(ctx, first); err != nil {
		t.Fatal(err)
	}
	before := tree(t, root)

	// The same frame again, then a new frame that shares an object with it, then
	// a frame that conflicts: none may change a byte that was already written.
	again, _ := blobstore.Encode(testKey, []blobstore.Object{obj("b", "two"), obj("c", "three")})
	conflicting, _ := blobstore.Encode(testKey, []blobstore.Object{{RID: obj("a", "").RID, Bytes: []byte("AGEO\x01other")}})
	for _, f := range [][]byte{first, again, conflicting} {
		_, _ = d.PutFrame(ctx, f)
	}

	after := tree(t, root)
	for path, was := range before {
		if now, ok := after[path]; !ok || now != was {
			t.Errorf("%s changed or vanished", path)
		}
	}
	if _, ok := after[filepath.Join("frames", blobstore.IDOf(conflicting))]; ok {
		t.Error("a conflicting frame was written to frames/")
	}
}
