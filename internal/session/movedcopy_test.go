package session

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestC5FilingACopyKeepsItsRelativeTruth proves C5: naming a moved copy for
// its origin must not strand the places and task references that moved with it.
func TestC5FilingACopyKeepsItsRelativeTruth(t *testing.T) {
	from, to := filepath.Join(t.TempDir(), "incoming"), filepath.Join(t.TempDir(), "filed")
	if err := os.MkdirAll(filepath.Join(from, ".cell"), 0o700); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(from, "work")
	if err := SaveMeta(from, Meta{ID: "chat", Owned: true, Workspace: work, Origin: "/origin/proj", Places: []PlaceRef{{Path: filepath.Join(work, "src"), Arrival: PlaceSaid}}}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(from, ".cell", placeMetaTruth))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(from, to); err != nil {
		t.Fatal(err)
	}
	if err := RebaseOwnedCopy(to); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(to, ".cell", placeMetaTruth))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("filing rewrote sealed truth: %v", err)
	}
	meta, err := LoadMeta(to)
	if err != nil || meta.Workspace != filepath.Join(to, "work") || len(meta.Places) != 1 || meta.Places[0].Path != filepath.Join(to, "work", "src") {
		t.Fatalf("filed copy's references: %+v, %v", meta, err)
	}
}
