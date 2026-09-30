package blobstore

import (
	"testing"
	"time"
)

// The mark rule (contract §22.9): plans, the vault frame and young frames are
// live; an old frame nothing names is collectible.
func TestLiveFramesMarksPlansVaultAndYoung(t *testing.T) {
	now := time.Now()
	old := now.Add(-72 * time.Hour)
	frames := map[FrameID]time.Time{
		"planned":   old,
		"vaulted":   old,
		"fresh":     now.Add(-time.Minute),
		"abandoned": old,
	}
	plans := [][]string{{"planned", "also-planned"}}
	live := LiveFrames(frames, plans, "vaulted", now.Add(-24*time.Hour))
	for _, id := range []FrameID{"planned", "also-planned", "vaulted", "fresh"} {
		if !live[id] {
			t.Errorf("%s must be live", id)
		}
	}
	if live["abandoned"] {
		t.Error("an old frame no plan names must be collectible")
	}
}

// The re-point rule: an object in two frames stays answerable after the first
// frame is tombstoned; an object only in the tombstoned frame is gone.
func TestDropFrameKeepsObjectsAnotherFrameHolds(t *testing.T) {
	locs := Locations{
		"shared": {{Frame: "first", Off: 0, Len: 10}, {Frame: "second", Off: 4, Len: 10}},
		"only":   {{Frame: "first", Off: 10, Len: 5}},
	}
	locs = DropFrame(locs, "first")
	if got := locs["shared"]; len(got) != 1 || got[0].Frame != "second" {
		t.Errorf("shared object must keep its second location, got %v", got)
	}
	if _, ok := locs["only"]; ok {
		t.Error("an object no other frame holds must be gone")
	}
}

// Dropping a frame never invents locations and never touches other frames.
func TestDropFrameIsExact(t *testing.T) {
	locs := Locations{"a": {{Frame: "keep", Off: 0, Len: 1}}}
	locs = DropFrame(locs, "absent")
	if got := locs["a"]; len(got) != 1 || got[0].Frame != "keep" {
		t.Errorf("dropping an absent frame changed the index: %v", got)
	}
}
