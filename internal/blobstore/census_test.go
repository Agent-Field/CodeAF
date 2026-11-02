package blobstore_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
)

func TestDiskCensusInventoriesEveryLocationWithoutClaimingReachability(t *testing.T) {
	root := t.TempDir()
	store, err := blobstore.NewDisk(root)
	if err != nil {
		t.Fatal(err)
	}
	a, b, c := obj("a", "first"), obj("b", "second"), obj("c", "third")
	var frameBytes int64
	for _, objects := range [][]blobstore.Object{{a, b}, {b, c}, {a, c}} {
		frame, err := blobstore.Encode(testKey, objects)
		if err != nil {
			t.Fatal(err)
		}
		frameBytes += int64(len(frame))
		if _, err := store.PutFrame(context.Background(), frame); err != nil {
			t.Fatal(err)
		}
	}
	before := tree(t, root)
	census, err := store.Census(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if census.Frames.Count != 3 || census.Pointers.Count != 3 || census.Frames.LogicalBytes != frameBytes {
		t.Fatalf("wrong file inventory: %+v", census)
	}
	if census.UniqueObjects != 3 || census.ObjectOccurrences != 6 || census.VerifiedPointers != 3 {
		t.Fatalf("wrong object inventory: %+v", census)
	}
	payload := int64(len(a.Bytes) + len(b.Bytes) + len(c.Bytes))
	if census.UniquePayloadBytes != payload || census.RepeatedPayloadBytes != payload {
		t.Fatalf("wrong payload inventory: %+v", census)
	}
	for _, object := range []blobstore.Object{a, b, c} {
		if len(census.Locations[object.RID]) != 2 {
			t.Fatal("missing alternate location")
		}
	}
	if census.FramesWithoutIndexPointers != 1 || census.UnclassifiedFrames != 3 {
		t.Fatalf("index observations confused with reachability: %+v", census)
	}
	if !census.Complete || census.StableSnapshot {
		t.Fatal("inventory must state its unfenced snapshot limitation")
	}
	if census.BytesRead <= frameBytes || census.Pointers.LogicalBytes <= 0 {
		t.Fatal("read and pointer costs missing")
	}
	if census.AllocationKnown {
		if census.Inodes != census.Directories+census.Frames.Count+census.Pointers.Count {
			t.Fatal("distinct inode inventory mismatch")
		}
	}
	if after := tree(t, root); !reflect.DeepEqual(before, after) {
		t.Fatal("read-only census changed store")
	}
}

func TestDiskCensusReportsDamageWithoutFollowingUnknownEntries(t *testing.T) {
	root := t.TempDir()
	store, err := blobstore.NewDisk(root)
	if err != nil {
		t.Fatal(err)
	}
	object := obj("a", "first")
	frame, err := blobstore.Encode(testKey, []blobstore.Object{object})
	if err != nil {
		t.Fatal(err)
	}
	id, err := store.PutFrame(context.Background(), frame)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "frames", id), []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "unclassified.tmp"), []byte("mock pending bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	census, err := store.Census(context.Background())
	if !errors.Is(err, blobstore.ErrDamaged) || census.Complete {
		t.Fatalf("damage hidden: %+v %v", census, err)
	}
	if census.Frames.Count != 1 || census.Other.Count != 1 || census.Pointers.Count != 1 || len(census.Problems) == 0 {
		t.Fatalf("unverified bytes disappeared: %+v", census)
	}
	if census.UnclassifiedFrames != 1 || census.UniqueObjects != 0 {
		t.Fatal("damaged frame contents cannot be counted as known objects")
	}
}

func TestDiskCensusCancellationIsPartialAndReadOnly(t *testing.T) {
	root := t.TempDir()
	store, err := blobstore.NewDisk(root)
	if err != nil {
		t.Fatal(err)
	}
	before := tree(t, root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	census, err := store.Census(ctx)
	if !errors.Is(err, context.Canceled) || census.Complete {
		t.Fatalf("cancellation hidden: %+v %v", census, err)
	}
	if after := tree(t, root); !reflect.DeepEqual(before, after) {
		t.Fatal("canceled census changed store")
	}
}

func TestDiskCensusReportsMissingAndCorruptPointers(t *testing.T) {
	for _, kind := range []string{"missing-pointer", "malformed-pointer", "wrong-target", "missing-frame"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			store, err := blobstore.NewDisk(root)
			if err != nil {
				t.Fatal(err)
			}
			a, b := obj("a", "first"), obj("b", "second")
			frame, err := blobstore.Encode(testKey, []blobstore.Object{a, b})
			if err != nil {
				t.Fatal(err)
			}
			id, err := store.PutFrame(context.Background(), frame)
			if err != nil {
				t.Fatal(err)
			}
			pointer := filepath.Join(root, "objects", b.RID[:2], b.RID[2:])
			switch kind {
			case "missing-pointer":
				if err := os.Remove(pointer); err != nil {
					t.Fatal(err)
				}
			case "malformed-pointer":
				if err := os.WriteFile(pointer, []byte("malformed\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "wrong-target":
				raw, err := os.ReadFile(filepath.Join(root, "objects", a.RID[:2], a.RID[2:]))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(pointer, raw, 0600); err != nil {
					t.Fatal(err)
				}
			case "missing-frame":
				if err := os.Remove(filepath.Join(root, "frames", id)); err != nil {
					t.Fatal(err)
				}
			}
			before := tree(t, root)
			census, err := store.Census(context.Background())
			if !errors.Is(err, blobstore.ErrDamaged) || census.Complete {
				t.Fatalf("unresolved pointer state hidden: %+v %v", census, err)
			}
			if kind == "missing-pointer" {
				if _, err := store.Get(context.Background(), b.RID); !errors.Is(err, blobstore.ErrNotFound) {
					t.Fatal("fixture object unexpectedly addressable")
				}
				if census.ObjectsWithoutIndexPointers != 1 || census.Problems["object-without-pointer"] != 1 || len(census.Locations[b.RID]) != 1 {
					t.Fatalf("recoverable frame was confused with available object: %+v", census)
				}
			}
			if after := tree(t, root); !reflect.DeepEqual(before, after) {
				t.Fatal("census repaired or removed data")
			}
		})
	}
}

func TestDiskCensusDistinguishesConflictingBytesFromRepeatedPayload(t *testing.T) {
	root := t.TempDir()
	store, err := blobstore.NewDisk(root)
	if err != nil {
		t.Fatal(err)
	}
	a := obj("a", "first")
	frame, err := blobstore.Encode(testKey, []blobstore.Object{a})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutFrame(context.Background(), frame); err != nil {
		t.Fatal(err)
	}
	conflict, err := blobstore.Encode(testKey, []blobstore.Object{{RID: a.RID, Bytes: []byte("AGEO\x01different")}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "frames", blobstore.IDOf(conflict)), conflict, 0600); err != nil {
		t.Fatal(err)
	}
	census, err := store.Census(context.Background())
	if !errors.Is(err, blobstore.ErrDamaged) || census.ConflictingOccurrences != 1 || census.RepeatedPayloadBytes != 0 {
		t.Fatalf("conflict counted as duplicate savings: %+v %v", census, err)
	}
	if len(census.Locations[a.RID]) != 2 || census.UnclassifiedFrames != 2 {
		t.Fatal("alternative bytes must remain inventoried and unclassified")
	}
}

func TestDiskCensusDoesNotFollowUnknownSymlinks(t *testing.T) {
	root := t.TempDir()
	store, err := blobstore.NewDisk(root)
	if err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "mock.txt"), []byte("external mock data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(root, "unexpected-link")); err != nil {
		t.Skip("symlink creation unavailable")
	}
	census, err := store.Census(context.Background())
	if !errors.Is(err, blobstore.ErrDamaged) || census.Other.Count != 1 || census.BytesRead != 0 || census.Problems["non-regular-entry"] != 1 {
		t.Fatalf("unknown link followed or hidden: %+v %v", census, err)
	}
}

func BenchmarkDiskCensus(b *testing.B) {
	root := b.TempDir()
	store, err := blobstore.NewDisk(root)
	if err != nil {
		b.Fatal(err)
	}
	objects := make([]blobstore.Object, 1000)
	for n := range objects {
		objects[n] = obj(fmt.Sprintf("mock-%04d", n), strings.Repeat("x", 64)+fmt.Sprintf("-%04d", n))
	}
	for _, group := range [][]blobstore.Object{objects[:750], objects[250:]} {
		frame, err := blobstore.Encode(testKey, group)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := store.PutFrame(context.Background(), frame); err != nil {
			b.Fatal(err)
		}
	}
	census, err := store.Census(context.Background())
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(census.BytesRead)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := store.Census(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(census.Frames.LogicalBytes), "frame-logical-bytes")
	b.ReportMetric(float64(census.Pointers.LogicalBytes), "pointer-logical-bytes")
	b.ReportMetric(float64(census.Pointers.AllocatedBytes), "pointer-allocated-bytes")
	b.ReportMetric(float64(census.DirectoryAllocatedBytes), "directory-allocated-bytes")
	b.ReportMetric(float64(census.AllocatedBytes), "reported-allocated-bytes")
	b.ReportMetric(float64(census.RepeatedPayloadBytes), "repeated-payload-bytes")
}

func TestDiskCensusDoesNotReadOversizedFrame(t *testing.T) {
	root := t.TempDir()
	store, err := blobstore.NewDisk(root)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "frames", strings.Repeat("a", 64))
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(blobstore.MaxFrame + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	census, err := store.Census(context.Background())
	if !errors.Is(err, blobstore.ErrDamaged) || census.BytesRead != 0 || census.Frames.LogicalBytes != blobstore.MaxFrame+1 || census.Problems["frame-read"] != 1 {
		t.Fatalf("oversized frame was read or omitted: %+v %v", census, err)
	}
}
