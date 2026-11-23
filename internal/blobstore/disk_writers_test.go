package blobstore_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
)

// Live transfer (spec rule 1, "many writers"): several codeaf processes of one
// computer write to the same live store at once, and each opens its own Disk
// over the folder. Two Disks over one root are the closest a test gets to two
// processes without forking one, and the store has to come out whole: every
// frame readable, every object found with the bytes that were written, and no
// half-written file left for the next process to trip over.
func TestDiskIsSafeUnderManyWritersOverOneFolder(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	const writers, framesEach, shared = 6, 12, 4
	disks := make([]*blobstore.Disk, writers)
	for i := range disks {
		d, err := blobstore.NewDisk(root)
		if err != nil {
			t.Fatal(err)
		}
		disks[i] = d
	}

	// Every writer's frames carry the SAME few shared objects (identical bytes,
	// as two computers' copies of one chat would) next to objects of their own,
	// so the writers race on the same files as well as on different ones.
	want := map[string][]byte{}
	var wantMu sync.Mutex
	frames := make([][][]byte, writers)
	for w := range frames {
		for n := 0; n < framesEach; n++ {
			objects := make([]blobstore.Object, 0, shared+3)
			for s := 0; s < shared; s++ {
				objects = append(objects, obj(fmt.Sprintf("shared-%d", s), strings.Repeat("s", 40+s)))
			}
			for o := 0; o < 3; o++ {
				objects = append(objects, obj(fmt.Sprintf("w%d-f%d-o%d", w, n, o), strings.Repeat("x", 100*(o+1))))
			}
			frame, err := blobstore.Encode(testKey, objects)
			if err != nil {
				t.Fatal(err)
			}
			frames[w] = append(frames[w], frame)
			wantMu.Lock()
			for _, o := range objects {
				want[o.RID] = o.Bytes
			}
			wantMu.Unlock()
		}
	}

	var wg sync.WaitGroup
	errs := make(chan error, writers*framesEach)
	for w := range disks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, frame := range frames[w] {
				if _, err := disks[w].PutFrame(ctx, frame); err != nil {
					errs <- fmt.Errorf("writer %d: %w", w, err)
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	reader, err := blobstore.NewDisk(root)
	if err != nil {
		t.Fatal(err)
	}
	for rid, bytesWant := range want {
		got, err := reader.Get(ctx, rid)
		if err != nil {
			t.Fatalf("object %s: %v", rid, err)
		}
		if !bytes.Equal(got, bytesWant) {
			t.Fatalf("object %s came back with other bytes", rid)
		}
	}
	for path := range tree(t, root) {
		if strings.Contains(filepath.Base(path), ".tmp-") {
			t.Errorf("temporary file left behind by a writer: %s", path)
		}
	}
	if entries, err := os.ReadDir(filepath.Join(root, "frames")); err != nil || len(entries) == 0 {
		t.Fatalf("frames folder: %d entries, %v", len(entries), err)
	}
}
