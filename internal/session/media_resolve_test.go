package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMediaReferenceFindsLateOwnerRecordWithoutPromotion(t *testing.T) {
	withSmallScan(t, 1024, 2048)
	dir := t.TempDir()
	index := filepath.Join(dir, "artifacts.jsonl")
	path := mediaFile(t, dir, "target.png")
	fact := GeneratedMedia(path, pngOfSize(t, 4, 3), "")
	fact.Origin = MediaEvidence
	fact.Verb = "looked_at"
	fact.Run = "real-run"
	fact.Tool = "actual-view"
	id := RecordMedia(index, Artifact{Path: path, Session: "owner", Title: "chart", Kind: "image", Created: time.Now()}, fact)
	other := mediaFile(t, dir, "other.png")
	for i := 0; i < 100; i++ {
		RecordMedia(index, Artifact{Path: other, Session: "other", Title: fmt.Sprint(i), Kind: "image", Created: time.Now()}, GeneratedMedia(other, nil, ""))
	}
	// Even a newer same-path record belonging to someone else is not ours.
	RecordMedia(index, Artifact{Path: path, Session: "foreign", Title: "foreign", Kind: "image", Created: time.Now()}, GeneratedMedia(path, nil, ""))
	row, err := FindMediaByPath(context.Background(), index, "owner", path, time.Now().Add(time.Second))
	if err != nil || row.Fact.ID != id || row.Fact.Origin != MediaEvidence || row.Fact.Tool != "actual-view" || row.Fact.Run != "real-run" {
		t.Fatalf("late source %+v %v", row, err)
	}
	before := MediaIndexScans()
	if got, err := FindMedia(index, "owner", id, time.Now().Add(time.Second)); err != nil || got.Fact.ID != id {
		t.Fatalf("cache %v", err)
	}
	if MediaIndexScans() != before {
		t.Fatal("resolved record was not cached for normal media.get")
	}
	latest := RecordMedia(index, Artifact{Path: path, Session: "owner", Title: "newest", Kind: "image", Created: time.Now()}, fact)
	if got, err := FindMediaByPath(context.Background(), index, "owner", path, time.Now().Add(time.Second)); err != nil || got.Fact.ID != latest {
		t.Fatalf("newest %+v %v", got, err)
	}
}

func TestMediaReferenceDoesNotDiscoverForeignOrUnrecordedFiles(t *testing.T) {
	dir := t.TempDir()
	index := filepath.Join(dir, "artifacts.jsonl")
	path := mediaFile(t, dir, "actual.png")
	RecordMedia(index, Artifact{Path: path, Session: "other", Title: "actual", Kind: "image", Created: time.Now()}, GeneratedMedia(path, nil, ""))
	for _, query := range []string{path, mediaFile(t, dir, "unrecorded.png")} {
		if _, err := FindMediaByPath(context.Background(), index, "owner", query, time.Now().Add(time.Second)); !errors.Is(err, ErrMediaNotFound) {
			t.Fatalf("discovery %q: %v", query, err)
		}
	}
	for _, path := range []string{"", "relative.png", "/a/../b.png", "/x\x00.png", "/" + strings.Repeat("x", 4096)} {
		if _, err := FindMediaByPath(context.Background(), index, "owner", path, time.Time{}); !errors.Is(err, ErrMediaReferencePath) {
			t.Fatalf("invalid path %q: %v", path, err)
		}
	}
	if _, err := FindMediaByPath(context.Background(), index, strings.Repeat("o", 129), path, time.Time{}); !errors.Is(err, ErrMediaReferencePath) {
		t.Fatal(err)
	}
}

func TestMediaReferenceCancellationAndSharedGlobalLookupCapacity(t *testing.T) {
	dir := t.TempDir()
	index := filepath.Join(dir, "index")
	path := filepath.Join(dir, "path.png")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := FindMediaByPath(ctx, index, "owner", path, time.Time{}); !errors.Is(err, ErrMediaBusy) {
		t.Fatalf("canceled absence: %v", err)
	}
	if _, err := FindMediaByPath(context.Background(), index, "owner", path, time.Now().Add(-time.Second)); !errors.Is(err, ErrMediaBusy) {
		t.Fatalf("deadline absence: %v", err)
	}
	key := mediaCacheKey{index: index, session: "owner", id: "resolve:" + path}
	flight, run, err := joinMediaFlight(key)
	if err != nil || !run {
		t.Fatal("reserve", err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := FindMediaByPath(ctx, index, "owner", path, time.Time{}); !errors.Is(err, ErrMediaBusy) {
		t.Fatalf("waiter %v", err)
	}
	// The canceled waiter must not settle the actual lookup's slot.
	if _, run, err := joinMediaFlight(key); err != nil || run {
		t.Fatal("canceled waiter released owner", err)
	}
	other := mediaCacheKey{index: index, session: "other", id: NewMediaID()}
	held, run, err := joinMediaFlight(other)
	if err != nil || !run {
		t.Fatal("reserve second", err)
	}
	if _, err := FindMediaByPath(context.Background(), index, "new-owner", path, time.Time{}); !errors.Is(err, ErrMediaBusy) {
		t.Fatalf("separate unbounded slots: %v", err)
	}
	finishMediaFlight(other, held, MediaRow{}, ErrMediaNotFound)
	finishMediaFlight(key, flight, MediaRow{}, ErrMediaNotFound)
	if _, err := FindMediaByPath(context.Background(), index, "new-owner", path, time.Time{}); !errors.Is(err, ErrMediaNotFound) {
		t.Fatalf("capacity not released: %v", err)
	}
}

func TestMediaReferenceRefusesNonRegularIndexAndReleasesLookup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.png")
	if _, err := FindMediaByPath(context.Background(), dir, "owner", path, time.Time{}); err == nil {
		t.Fatal("directory read as index")
	}
	index := filepath.Join(dir, "index")
	if err := os.WriteFile(index, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := FindMediaByPath(context.Background(), index, "owner", path, time.Time{}); !errors.Is(err, ErrMediaNotFound) {
		t.Fatal(err)
	}
}
