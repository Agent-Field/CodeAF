package store

import (
	"path/filepath"
	"testing"
	"time"
)

func attemptFixture(id, owner, key, hash, status string) ContextualAttempt {
	e := ContextualAttempt{
		ID: id, Owner: owner, SessionID: "s", TurnID: "t", Tool: "bash",
		Action: "bash: go test ./pkg", Goal: "make the pkg tests pass",
		Status: status, Observation: "undefined: priorOutcomeContext",
		SourceKey: key, SourceHash: hash,
	}
	if status != AttemptUnknown {
		e.ReceiptIDs = []string{"call1"}
	}
	return e
}

// A DEMONSTRATED OUTCOME NEEDS A RECEIPT. A blocked call needs none and must
// never be dressed as a proven failure.
func TestContextualAttemptStatusGuardsReceipt(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "attempts.db"))
	if _, err := s.AppendContextualAttempt(attemptFixture("f", "user", "k1", "h1", AttemptFailed)); err != nil {
		t.Fatalf("failed attempt with a receipt was refused: %v", err)
	}
	blocked := attemptFixture("b", "user", "k2", "h2", AttemptUnknown)
	if _, err := s.AppendContextualAttempt(blocked); err != nil {
		t.Fatalf("blocked attempt was refused: %v", err)
	}
	bare := attemptFixture("bare", "user", "k3", "h3", AttemptFailed)
	bare.ReceiptIDs = nil
	if _, err := s.AppendContextualAttempt(bare); err == nil {
		t.Fatal("a failure with no receipt was accepted")
	}
	if _, err := s.AppendContextualAttempt(attemptFixture("w", "user", "k4", "h4", "maybe")); err == nil {
		t.Fatal("an unknown status word was accepted")
	}
}

// A DUPLICATE RECEIPT IS IDEMPOTENT; a genuinely fresh observation is not.
func TestContextualAttemptDuplicateIsIdempotentFreshIsNew(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "attempts.db"))
	first, err := s.AppendContextualAttempt(attemptFixture("a1", "user", "turn:1:call1", "same", AttemptFailed))
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.AppendContextualAttempt(attemptFixture("a2", "user", "turn:1:call1", "same", AttemptFailed))
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.Seq != first.Seq {
		t.Fatalf("duplicate receipt created a new row: %+v vs %+v", first, second)
	}
	fresh, err := s.AppendContextualAttempt(attemptFixture("a3", "user", "turn:1:call1", "different", AttemptFailed))
	if err != nil {
		t.Fatal(err)
	}
	if fresh.ID == first.ID {
		t.Fatal("a fresh observation was collapsed onto the duplicate")
	}
	rows, err := s.ContextualAttemptsApplicable("user", nil, time.Now(), ContextualAttemptLimit)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("want two distinct observations, got %d", len(rows))
	}
}

// FORGET RETIRES PROVENANCE, NOT THE PROJECT. An attempt sharing the source of
// a forgotten claim is retired with it; an unrelated attempt and a fresh
// observation survive.
func TestContextualAttemptForgetScopesToProvenance(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "attempts.db"))
	owner := OwnerProject("p")
	// The claim the attempt is provenance of, sharing its source key and hash.
	if _, err := s.AppendContextualEvidence(ContextualEvidence{ID: "claim", MemoryID: "m1", Owner: owner, Actor: "tool", Authority: "observation", Observation: "a receipt", SourceKey: "turn:1:call1", SourceHash: "h1"}); err != nil {
		t.Fatal(err)
	}
	linked := attemptFixture("linked", owner, "turn:1:call1", "h1", AttemptFailed)
	if _, err := s.AppendContextualAttempt(linked); err != nil {
		t.Fatal(err)
	}
	unrelated := attemptFixture("unrelated", owner, "turn:2:call9", "h9", AttemptFailed)
	if _, err := s.AppendContextualAttempt(unrelated); err != nil {
		t.Fatal(err)
	}
	if err := s.SuppressContextualMemorySources(owner, "m1", "explicit forget"); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ContextualAttemptsApplicable(owner, nil, time.Now(), ContextualAttemptLimit)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != "unrelated" {
		t.Fatalf("forget did not scope to provenance: %+v", rows)
	}
	// The forgotten record is never handed back authoritative, even to the same
	// receipt re-sent.
	if _, err := s.AppendContextualAttempt(linked); err == nil {
		t.Fatal("a suppressed attempt's receipt was accepted")
	}
	// A fresh independent observation in the same project is allowed.
	fresh := attemptFixture("fresh", owner, "turn:3:call3", "h3", AttemptFailed)
	fresh.Observation = "a different failure"
	if _, err := s.AppendContextualAttempt(fresh); err != nil {
		t.Fatalf("a fresh observation was blocked by an unrelated forget: %v", err)
	}
}

// OWNERS DO NOT SEE EACH OTHER'S OUTCOMES.
func TestContextualAttemptOwnerIsolation(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "attempts.db"))
	if _, err := s.AppendContextualAttempt(attemptFixture("a", OwnerProject("p1"), "k", "h", AttemptFailed)); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ContextualAttemptsApplicable(OwnerProject("p2"), nil, time.Now(), ContextualAttemptLimit)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("a project saw another project's outcomes: %+v", rows)
	}
	if _, err := s.ContextualAttemptsApplicable("not an owner", nil, time.Now(), ContextualAttemptLimit); err == nil {
		t.Fatal("an invalid owner was accepted")
	}
}

// Retired evidence cannot come back as an authoritative attempt before the
// suppression check runs.
func TestContextualAttemptSuppressedSourceRefusedBeforeDuplicateLookup(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "attempts.db"))
	a := attemptFixture("a", "user", "turn:1:call1", "h", AttemptFailed)
	if _, err := s.AppendContextualAttempt(a); err != nil {
		t.Fatal(err)
	}
	if err := s.SuppressContextualSource("user", "turn:1:call1", "h", "forgotten"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendContextualAttempt(a); err == nil {
		t.Fatal("a suppressed source's receipt was returned instead of refused")
	}
}
