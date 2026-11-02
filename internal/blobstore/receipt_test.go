package blobstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// R21 requires durable bytes AND a retrievable index before a head receipt.
func TestR21ReceiptRefusesEveryIncompleteUploadBoundary(t *testing.T) {
	ctx := context.Background()
	for _, cut := range []string{"frame-sync", "after-frame", "pointer-sync", "directory-sync"} {
		t.Run(cut, func(t *testing.T) {
			d, root, frame, o := newCrashDisk(t)
			calls := 0
			failure := errors.New("cut")
			actual := d.sync
			d.sync = func(files []*os.File) error {
				calls++
				if cut == "frame-sync" && calls == 1 || cut == "pointer-sync" && calls == 3 || cut == "directory-sync" && calls == 4 {
					return failure
				}
				return actual(files)
			}
			if cut == "after-frame" {
				d.afterFrame = func() error { return failure }
			}
			if _, e := d.PutFrame(ctx, frame); e == nil {
				t.Fatal("fault did not fire")
			}
			// An indexed-but-unflushed pointer is repaired only by a successful
			// durability completion, never by observing that its pathname exists.
			d.sync = func([]*os.File) error { return failure }
			if e := CheckReferences(ctx, d, o.RID, []string{IDOf(frame)}); e == nil {
				t.Fatal("incomplete upload produced a receipt")
			}
			d.sync = actual
			d.afterFrame = func() error { return nil }
			if _, e := d.PutFrame(ctx, frame); e != nil {
				t.Fatal(e)
			}
			if e := CheckReferences(ctx, d, o.RID, []string{IDOf(frame)}); e != nil {
				t.Fatal(e)
			}
			if e := os.WriteFile(filepath.Join(root, "frames", IDOf(frame)), append(append([]byte(nil), frame[:len(frame)-1]...), frame[len(frame)-1]^1), 0600); e != nil {
				t.Fatal(e)
			}
			if e := CheckReferences(ctx, d, o.RID, nil); !errors.Is(e, ErrDamaged) {
				t.Fatal(e)
			}
		})
	}
}
