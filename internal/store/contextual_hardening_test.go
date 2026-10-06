package store

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestContextualAttemptRetentionIsLogicalAndTombstoneOutlives(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "retention.db"))
	owner := OwnerProject("p")
	old := attemptFixture("a1", owner, "turn:1:call1", "h1", AttemptFailed)
	if _, err := s.AppendContextualAttempt(old); err != nil {
		t.Fatal(err)
	}
	if err := s.SuppressContextualSource(owner, "turn:1:call1", "h1", "forgotten"); err != nil {
		t.Fatal(err)
	}
	// THE JOURNAL IS APPEND-ONLY: no path may physically prune an attempt.
	if _, err := s.db.Exec(`DELETE FROM events WHERE node_id=? AND kind=?`, contextualNode(owner), EventContextualAttempt); err == nil {
		t.Fatal("an attempt row was physically deleted from the canonical journal")
	}
	// The tombstone is a separate event and still blocks the source, so a forget
	// cannot be undone by any retention path.
	if _, err := s.AppendContextualAttempt(old); err == nil {
		t.Fatal("a forget tombstone did not survive")
	}
	// The read is bounded whatever the journal holds: one window, not the whole.
	for i := 0; i < ContextualAttemptLimit+20; i++ {
		if _, err := s.AppendContextualAttempt(attemptFixture(fmt.Sprintf("x%d", i), owner, fmt.Sprintf("turn:%d:call", i+2), "h", AttemptFailed)); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.ContextualAttemptsApplicable(owner, nil, time.Now(), ContextualAttemptLimit)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) > ContextualAttemptLimit {
		t.Fatalf("read returned %d attempts past the %d window", len(rows), ContextualAttemptLimit)
	}
}

// AN UNKNOWN SNAPSHOT PROVES NOTHING. Two captures that each failed to identify
// the source are not the same source, so "unknown" must never compare equal to
// "unknown" — while a rule the person wrote with NO revision is intent and
// applies wherever it is read.
func TestContextualUnknownRevisionNeverMatches(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "unknown.db"))
	if _, err := s.AppendContextualEvidence(ContextualEvidence{ID: "u", MemoryID: "m", Owner: "user", Actor: "tool", Authority: "observation", Observation: "saw it", Revision: "unknown"}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ContextualEvidenceApplicable("user", map[string]string{"revision": "unknown"}, time.Now(), 100)
	if err != nil || len(rows) != 0 {
		t.Fatalf("an unknown snapshot matched another unknown: %+v / %v", rows, err)
	}
	if _, err := s.AppendContextualEvidence(ContextualEvidence{ID: "w", MemoryID: "mw", Owner: "user", Actor: "user", Authority: "approved_rule", Observation: "always use exact decimals"}); err != nil {
		t.Fatal(err)
	}
	rows, err = s.ContextualEvidenceApplicable("user", map[string]string{"revision": "unknown"}, time.Now(), 100)
	if err != nil || len(rows) != 1 || rows[0].ID != "w" {
		t.Fatalf("a user-authored rule with no revision did not apply: %+v / %v", rows, err)
	}
}
