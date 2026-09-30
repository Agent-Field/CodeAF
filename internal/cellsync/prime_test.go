package cellsync

import (
	"context"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/directory"
)

// primedFetcherFor is a second device's fetcher that can read the plan off the
// directory record, over store, with its own engine.
func primedFetcherFor(t *testing.T, store blobstore.Store, dir directory.Client) (*Fetcher, cell.Cell) {
	t.Helper()
	f, c := fetcherFor(t, store)
	f.Dir = dir
	return f, c
}

// A take whose record names frames primes from them: one get per frame, none
// per object, and the head materializes whole.
func TestFetchPrimesFromThePlan(t *testing.T) {
	r := newRig(t)
	head := r.publishFirst(map[string]string{"a": "one", "sub/b": "two", "c": "three"})
	plan := r.head(cellID).Frames
	if len(plan) == 0 {
		t.Fatal("publish recorded no plan")
	}
	count := blobstore.NewCounting(r.mem)
	f, c := primedFetcherFor(t, count, r.dir.For(devB))
	if err := f.Fetch(context.Background(), c, head); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"a": "one", "sub/b": "two", "c": "three"}
	if got := readFiles(t, c.Root); !reflect.DeepEqual(got, want) {
		t.Fatalf("materialized %v, want %v", got, want)
	}
	if got := count.Count().Gets; got != len(plan) {
		t.Fatalf("gets = %d, want one per plan frame (%d)", got, len(plan))
	}
}

// A plan that lies — it names a frame the store never held — costs the take
// nothing but the miss: the want loop completes the head anyway.
func TestFetchSurvivesALyingPlan(t *testing.T) {
	r := newRig(t)
	head := r.publishFirst(map[string]string{"a": "one", "b": "two"})
	lie := append([]string{otherHead}, r.head(cellID).Frames...)
	if _, err := r.dir.For(devB).Publish(context.Background(), cellID, directory.Publish{
		Fence: r.head(cellID).Lease.Fence, OldHead: head, Head: head, Class: "chat", Frames: lie,
	}); err != nil {
		t.Fatal(err)
	}
	f, c := primedFetcherFor(t, r.mem, r.dir.For(devB))
	if err := f.Fetch(context.Background(), c, head); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"a": "one", "b": "two"}
	if got := readFiles(t, c.Root); !reflect.DeepEqual(got, want) {
		t.Fatalf("materialized %v, want %v", got, want)
	}
}

// A frame packs one publish's objects, and later turns orphan some of them:
// priming delivers frame-mates the head does not want, and ImportPrimed
// deletes them instead of failing the take.
func TestFetchDropsFrameMatesTheHeadOrphaned(t *testing.T) {
	r := newRig(t)
	r.publishFirst(map[string]string{"a": "one", "b": "two"})
	head := r.seal(map[string]string{"a": "edited", "b": "two"})
	if err := r.pub.Publish(context.Background(), r.drv, head, r.info()); err != nil {
		t.Fatal(err)
	}
	f, c := primedFetcherFor(t, r.mem, r.dir.For(devB))
	if err := f.Fetch(context.Background(), c, head); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"a": "edited", "b": "two"}
	if got := readFiles(t, c.Root); !reflect.DeepEqual(got, want) {
		t.Fatalf("materialized %v, want %v", got, want)
	}
}

// A fetcher without a directory — the rotation path — runs the want loop
// exactly as before plans existed.
func TestFetchWithoutADirectoryLoops(t *testing.T) {
	r := newRig(t)
	head := r.publishFirst(map[string]string{"a": "one"})
	f, c := fetcherFor(t, r.mem)
	if err := f.Fetch(context.Background(), c, head); err != nil {
		t.Fatal(err)
	}
	if got := readFiles(t, c.Root); !reflect.DeepEqual(got, map[string]string{"a": "one"}) {
		t.Fatalf("materialized %v", got)
	}
}

// A publish records the frames it uploaded, and the next publish extends the
// list rather than replacing it.
func TestPublishRecordsAndExtendsThePlan(t *testing.T) {
	r := newRig(t)
	r.publishFirst(map[string]string{"a": "one"})
	first := r.head(cellID).Frames
	if len(first) != r.puts() {
		t.Fatalf("plan = %d frames, want the %d put", len(first), r.puts())
	}
	head := r.seal(map[string]string{"a": "one", "b": "two"})
	if err := r.pub.Publish(context.Background(), r.drv, head, r.info()); err != nil {
		t.Fatal(err)
	}
	plan := r.head(cellID).Frames
	if len(plan) <= len(first) {
		t.Fatalf("plan after two publishes = %d frames, want more than %d", len(plan), len(first))
	}
	for _, id := range first {
		found := false
		for _, got := range plan {
			if got == id {
				found = true
			}
		}
		if !found {
			t.Fatalf("plan %v lost the first publish's frame %s", plan, id)
		}
	}
}
