package profile

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A remembered parse must be invisible: the same answer as a fresh read, with
// its own records, and never stale after a write.
func TestLoadRemembersAParseWithoutSharingItOrGoingStale(t *testing.T) {
	dir := t.TempDir()

	first, err := Load(dir, "vendor/worker", "linear")
	if err != nil {
		t.Fatalf("load empty: %v", err)
	}
	if len(first.Records) != 0 {
		t.Fatalf("a profile with no file should be empty, got %d records", len(first.Records))
	}

	first.Add(Record{Title: "measured", Turns: 4, Tokens: 12000})
	first.Anchors = "three sentences"
	if err := first.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	second, err := Load(dir, "vendor/worker", "linear")
	if err != nil {
		t.Fatalf("load after save: %v", err)
	}
	if len(second.Records) != 1 || second.Anchors != "three sentences" {
		t.Fatalf("a write must be visible to the next read: %d records, anchors %q",
			len(second.Records), second.Anchors)
	}

	third, err := Load(dir, "vendor/worker", "linear")
	if err != nil {
		t.Fatalf("load again: %v", err)
	}
	// Mutating one load must not reach any other. This is the whole hazard of
	// remembering a parse.
	third.Add(Record{Title: "not shared", Turns: 9})
	if len(second.Records) != 1 {
		t.Fatalf("loads share a records slice: the earlier one grew to %d", len(second.Records))
	}

	// A rewrite from outside this process is picked up too.
	path := filepath.Join(dir, "profile-vendor-worker-linear.json")
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(path, []byte(`{"model":"vendor/worker","skill":"linear","anchors":"rewritten","records":[]}`), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	fourth, err := Load(dir, "vendor/worker", "linear")
	if err != nil {
		t.Fatalf("load after rewrite: %v", err)
	}
	if fourth.Anchors != "rewritten" || len(fourth.Records) != 0 {
		t.Fatalf("a rewritten file must not read back remembered: anchors %q, %d records",
			fourth.Anchors, len(fourth.Records))
	}

	// A deleted file goes back to the built-in prior rather than the last parse.
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	fifth, err := Load(dir, "vendor/worker", "linear")
	if err != nil {
		t.Fatalf("load after remove: %v", err)
	}
	if fifth.Anchors != "" || len(fifth.Records) != 0 {
		t.Fatalf("a removed profile must read empty: anchors %q, %d records",
			fifth.Anchors, len(fifth.Records))
	}
}
