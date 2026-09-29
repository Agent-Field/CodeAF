package blobstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sealedObject(rid, body string) Object {
	return Object{RID: rid, Bytes: []byte("AGEO\x01" + body)}
}

func newCrashDisk(t *testing.T) (*Disk, string, []byte, Object) {
	t.Helper()
	root := t.TempDir()
	d, err := NewDisk(root)
	if err != nil {
		t.Fatal(err)
	}
	o := sealedObject(strings.Repeat("ab", 32), "body")
	frame, err := Encode("", []Object{o})
	if err != nil {
		t.Fatal(err)
	}
	return d, root, frame, o
}

// pointers counts the pointer files under objects/.
func pointers(t *testing.T, root string) int {
	t.Helper()
	n := 0
	_ = filepath.WalkDir(filepath.Join(root, "objects"), func(_ string, e os.DirEntry, _ error) error {
		if !e.IsDir() {
			n++
		}
		return nil
	})
	return n
}

// The crash between "frame is durable" and "pointer is written" must leave a
// store that says not found, never one that answers with garbage.
func TestDiskCrashAfterFrameBeforePointer(t *testing.T) {
	ctx := context.Background()
	d, root, frame, o := newCrashDisk(t)
	crash := errors.New("power cut")
	d.afterFrame = func() error { return crash }

	if _, err := d.PutFrame(ctx, frame); !errors.Is(err, crash) {
		t.Fatalf("PutFrame = %v, want the injected failure", err)
	}
	if _, err := d.Get(ctx, o.RID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after the crash = %v, want ErrNotFound", err)
	}
	if have, _ := d.Has(ctx, []string{o.RID}); have[0] {
		t.Fatal("Has answered true for an object with no pointer")
	}
	if _, err := os.Stat(filepath.Join(root, "frames", IDOf(frame))); err != nil {
		t.Fatalf("the durable frame is missing: %v", err)
	}

	// After the restart the same frame is sent again, and the store heals.
	d.afterFrame = func() error { return nil }
	if _, err := d.PutFrame(ctx, frame); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got, err := d.Get(ctx, o.RID); err != nil || string(got) != string(o.Bytes) {
		t.Fatalf("Get after the retry = %q, %v", got, err)
	}
}

// If the frame cannot be made durable, no pointer may exist at all.
func TestDiskFrameNotDurableWritesNoPointer(t *testing.T) {
	ctx := context.Background()
	d, root, frame, o := newCrashDisk(t)
	d.sync = func(*os.File) error { return errors.New("disk full") }

	if _, err := d.PutFrame(ctx, frame); err == nil {
		t.Fatal("PutFrame succeeded although the frame could not be synced")
	}
	if n := pointers(t, root); n != 0 {
		t.Fatalf("%d pointers exist for a frame that is not durable", n)
	}
	if _, err := d.Get(ctx, o.RID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get = %v, want ErrNotFound", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(root, "frames")); len(entries) != 0 {
		t.Fatalf("frames/ holds %d files after a failed sync, want none", len(entries))
	}
}

// A pointer that names a frame which is gone is damage and must say so.
func TestDiskDanglingPointerIsNotNotFound(t *testing.T) {
	ctx := context.Background()
	d, root, frame, o := newCrashDisk(t)
	if _, err := d.PutFrame(ctx, frame); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "frames", IDOf(frame))); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Get(ctx, o.RID); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("Get with a missing frame = %v, want a damage error", err)
	}
}
