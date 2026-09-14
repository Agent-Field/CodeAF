package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestTheSweepReportsOnlyLiveOlderBuilds pins the gate's two rules: a fresh
// presence row on a different rev is reported, and three wrong kinds of row
// — same rev, stale file age, and empty build — are not.
func TestTheSweepReportsOnlyLiveOlderBuilds(t *testing.T) {
	now := time.Now()
	root := t.TempDir()
	write := func(bucket, session string, p SessionPresence) {
		dir := filepath.Join(root, bucket, session)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		payload, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "presence.json"), payload, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("bucketA", "sesold", SessionPresence{
		Schema: presenceSchema, SessionID: "sesold", Workspace: "/w/one",
		Build: "f5e1dd98e built 2026-09-12 11:50", PID: 17376,
		UpdatedAt: now, State: PresenceIdle,
	})
	write("bucketB", "sesame", SessionPresence{
		Schema: presenceSchema, SessionID: "sesame", Workspace: "/w/two",
		Build: "75a1fc2b1 built 2026-09-12 13:58", PID: 21896,
		UpdatedAt: now, State: PresenceWorking,
	})
	write("bucketC", "sestaleage", SessionPresence{
		Schema: presenceSchema, SessionID: "sestaleage", Workspace: "/w/old",
		Build: "f5e1dd98e built 2026-09-12 11:50", PID: 40684,
		UpdatedAt: now.Add(-presenceWindow - time.Second), State: PresenceIdle,
	})
	write("bucketD", "senobuild", SessionPresence{
		Schema: presenceSchema, SessionID: "senobuild", Workspace: "/w/nob",
		UpdatedAt: now, State: PresenceIdle,
	})

	rows := SweepStaleBuilds(root, "75a1fc2b1", now)
	if len(rows) != 1 || rows[0].SessionID != "sesold" {
		t.Fatalf("SweepStaleBuilds returned %+v — want exactly the one fresh row on the other rev", rows)
	}
	if rows[0].PID != 17376 || rows[0].Build != "f5e1dd98e built 2026-09-12 11:50" {
		t.Fatalf("row carried the wrong facts: %+v", rows[0])
	}
}

// TestTheSweepIgnoresUnkowableRows pins the fail-open half: an unreadable
// directory, a corrupt file and a missing rev argument each answer with no
// rows rather than a guess about who is stale.
func TestTheSweepIgnoresUnkowableRows(t *testing.T) {
	if rows := SweepStaleBuilds(t.TempDir(), "  ", time.Now()); len(rows) != 0 {
		t.Fatalf("an empty rev argument must not sweep, got %v", rows)
	}
	root := t.TempDir()
	bad := filepath.Join(root, "bucket", "ses")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "presence.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rows := SweepStaleBuilds(root, "rev9", time.Now()); len(rows) != 0 {
		t.Fatalf("a corrupt file sweeps nothing, got %v", rows)
	}
}
