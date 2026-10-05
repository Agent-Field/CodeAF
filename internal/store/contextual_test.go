package store

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestContextualSuppressionPropagatesAndBlocksRelearning(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "context.db"))
	base := ContextualEvidence{ID: "source", MemoryID: "m1", Owner: "user", Actor: "user", Authority: "user", Observation: "a claim", SourceKey: "turn:1", SourceHash: "hash1"}
	parent, err := s.AppendContextualEvidence(base)
	if err != nil {
		t.Fatal(err)
	}
	child := ContextualEvidence{ID: "derived", MemoryID: "m2", Owner: "user", Authority: "inference", Observation: "a derived claim", Derivations: []int64{parent.Seq}}
	derived, err := s.AppendContextualEvidence(child)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SuppressContextualSource("user", base.SourceKey, base.SourceHash, "forgotten"); err != nil {
		t.Fatal(err)
	}
	for _, e := range []ContextualEvidence{parent, derived} {
		ok, err := s.ContextualEvidenceEligible(e, nil, time.Now())
		if err != nil || ok {
			t.Fatalf("suppressed eligible=%v err=%v", ok, err)
		}
	}
	base.ID = "relearn"
	base.MemoryID = "m3"
	if _, err = s.AppendContextualEvidence(base); err == nil {
		t.Fatal("suppressed source relearned")
	}
	if _, err = s.AppendContextualEvidence(ContextualEvidence{ID: "bad", MemoryID: "m4", Owner: "user", Authority: "inference", Observation: "bad", Derivations: []int64{derived.Seq}}); err == nil {
		t.Fatal("suppressed parent accepted")
	}
	base.ID = "fresh"
	base.SourceHash = "hash2"
	fresh, err := s.AppendContextualEvidence(base)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := s.ContextualEvidenceEligible(fresh, nil, time.Now()); err != nil || !ok {
		t.Fatalf("fresh eligible=%v err=%v", ok, err)
	}
	if err = s.Rebuild(); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.ContextualEvidenceEligible(derived, nil, time.Now()); err != nil || ok {
		t.Fatalf("rebuild resurrected derived=%v err=%v", ok, err)
	}
}

func TestContextualConditionsRevisionValidityAndOwner(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "context.db"))
	at := time.Now().UTC()
	e, err := s.AppendContextualEvidence(ContextualEvidence{ID: "e", MemoryID: "m", Owner: "project:a", Actor: "tool", Authority: "observation", Observation: "tool receipt", ReceiptIDs: []string{"call1"}, Revision: "sha1", ValidUntil: at.Add(time.Hour), Conditions: map[string]string{"project": "a"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, ctx := range []map[string]string{nil, {"project": "b", "revision": "sha1"}, {"project": "a", "revision": "sha2"}} {
		if ok, err := s.ContextualEvidenceEligible(e, ctx, at); err != nil || ok {
			t.Fatalf("wrong context eligible=%v err=%v", ok, err)
		}
	}
	ctx := map[string]string{"project": "a", "revision": "sha1"}
	if ok, err := s.ContextualEvidenceEligible(e, ctx, at); err != nil || !ok {
		t.Fatalf("right context eligible=%v err=%v", ok, err)
	}
	if ok, err := s.ContextualEvidenceEligible(e, ctx, at.Add(time.Hour)); err != nil || ok {
		t.Fatalf("expired eligible=%v err=%v", ok, err)
	}
	if _, err = s.ContextualEvidenceByID("project:b", "e"); err == nil {
		t.Fatal("foreign evidence exposed")
	}
	if _, err = s.AppendContextualEvidence(ContextualEvidence{ID: "foreign", MemoryID: "other", Owner: "project:b", Authority: "inference", Observation: "foreign", Derivations: []int64{e.Seq}}); err == nil {
		t.Fatal("foreign parent accepted")
	}
	if _, err = s.AppendContextualEvidence(ContextualEvidence{ID: "assistant", MemoryID: "other", Owner: "project:a", Actor: "assistant", Authority: "user", Observation: "assistant says user"}); err == nil {
		t.Fatal("assistant promoted to user")
	}
	found, err := s.ContextualEvidenceForMemory("project:a", "m")
	if err != nil || found.Seq != e.Seq {
		t.Fatalf("memory evidence=%+v %v", found, err)
	}
}

func TestContextualSourceChangeInvalidatesAncestors(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "revision.db"))
	e := ContextualEvidence{ID: "first", MemoryID: "m", Owner: "user", Actor: "user", Authority: "user", Observation: "old", SourceKey: "file:a", SourceHash: "one", Revision: "one"}
	old, err := s.AppendContextualEvidence(e)
	if err != nil {
		t.Fatal(err)
	}
	child, err := s.AppendContextualEvidence(ContextualEvidence{ID: "derived", MemoryID: "d", Owner: "user", Authority: "inference", Observation: "derived", Derivations: []int64{old.Seq}})
	if err != nil {
		t.Fatal(err)
	}
	e.ID = "second"
	e.SourceHash = "two"
	e.Revision = "two"
	if _, err = s.AppendContextualEvidence(e); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.ContextualEvidenceEligible(child, map[string]string{"revision": "one"}, time.Now()); err != nil || ok {
		t.Fatalf("stale parent eligible=%v err=%v", ok, err)
	}
	all, err := s.ContextualEvidenceApplicable("user", map[string]string{"revision": "two"}, time.Now(), 1000)
	if err != nil || len(all) != 1 || all[0].ID != "second" {
		t.Fatalf("projection=%+v err=%v", all, err)
	}
}

func TestContextualAssistantCannotBecomeObservation(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "actor.db"))
	_, err := s.AppendContextualEvidence(ContextualEvidence{ID: "a", MemoryID: "m", Owner: "user", Actor: "assistant", Authority: "observation", Observation: "self report"})
	if err == nil {
		t.Fatal("assistant self report became observation")
	}
}

func TestContextualMemorySuppressionIncludesAllSources(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "memory-suppression.db"))
	e := ContextualEvidence{ID: "old", MemoryID: "m", Owner: "user", Actor: "user", Authority: "user", Observation: "old", SourceKey: "turn:old", SourceHash: "hash-old"}
	old, err := s.AppendContextualEvidence(e)
	if err != nil {
		t.Fatal(err)
	}
	e.ID = "new"
	e.SourceKey = "turn:new"
	e.SourceHash = "hash-new"
	latest, err := s.AppendContextualEvidence(e)
	if err != nil {
		t.Fatal(err)
	}
	derived, err := s.AppendContextualEvidence(ContextualEvidence{ID: "d", MemoryID: "derived", Owner: "user", Authority: "inference", Observation: "derived", Derivations: []int64{old.Seq}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SuppressContextualMemorySources("user", "m", "forget"); err != nil {
		t.Fatal(err)
	}
	for _, record := range []ContextualEvidence{old, latest, derived} {
		if ok, err := s.ContextualEvidenceEligible(record, nil, time.Now()); err != nil || ok {
			t.Fatalf("suppressed record=%s usable=%v err=%v", record.ID, ok, err)
		}
	}
	for _, source := range []ContextualEvidence{old, latest} {
		source.ID += "relearn"
		source.MemoryID = "new-id"
		if _, err = s.AppendContextualEvidence(source); err == nil {
			t.Fatalf("old source %s relearned", source.SourceKey)
		}
	}
	e.ID = "fresh"
	e.MemoryID = "fresh-memory"
	e.SourceHash = "different"
	if _, err = s.AppendContextualEvidence(e); err != nil {
		t.Fatal(err)
	}
	if err = s.Rebuild(); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.ContextualEvidenceEligible(old, nil, time.Now()); err != nil || ok {
		t.Fatalf("rebuild resurrected evidence usable=%v err=%v", ok, err)
	}
}

func TestContextualReasoningMetadataBounds(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "metadata.db"))
	e := ContextualEvidence{ID: "meta", MemoryID: "m", Owner: "user", Actor: "user", Authority: "user", Observation: "claim", Applicability: []string{"only project a"}, Rationale: "the owner said why", Rejected: []string{"option b"}, Reconsider: "when dependencies change"}
	written, err := s.AppendContextualEvidence(e)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.ContextualEvidenceByID("user", written.ID)
	if err != nil || got.Rationale != e.Rationale || len(got.Applicability) != 1 || len(got.Rejected) != 1 {
		t.Fatalf("metadata=%+v err=%v", got, err)
	}
	e.ID = "too-many"
	e.Applicability = make([]string, 9)
	if _, err = s.AppendContextualEvidence(e); err == nil {
		t.Fatal("unbounded applicability accepted")
	}
}

// A RARE APPROVED RULE IS NOT STARVED BY A BURST OF NEWER INCIDENTAL NOISE.
// The general projection reads a newest window of every observation, so 128
// later tool observations would push an older approval out of it; the binding
// projection spends its window on authority and still finds it, while the
// suppression and validity guards keep deciding whether it is live.
func TestContextualApprovedRuleSurvivesIncidentalNoise(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "noise.db"))
	owner := OwnerProject("noise")
	rule := ContextualEvidence{ID: "rule", MemoryID: "m-rule", Owner: owner, Actor: "user",
		Authority: "approved_rule", Observation: "release artifacts must run offline",
		SourceKey: "turn:rule", SourceHash: "hash-rule"}
	if _, err := s.AppendContextualEvidence(rule); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < ContextualEvidenceLimit+2; i++ {
		noise := ContextualEvidence{ID: fmt.Sprintf("obs-%d", i), MemoryID: fmt.Sprintf("m-%d", i),
			Owner: owner, Actor: "tool", Tool: "bash", ReceiptIDs: []string{fmt.Sprintf("r-%d", i)},
			Authority: "observation", Observation: "incidental output",
			SourceKey: fmt.Sprintf("turn:%d", i), SourceHash: fmt.Sprintf("hash-%d", i)}
		if _, err := s.AppendContextualEvidence(noise); err != nil {
			t.Fatal(err)
		}
	}
	approved, err := s.ContextualEvidenceApproved(owner, map[string]string{}, time.Now(), ContextualEvidenceLimit)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range approved {
		if e.ID == "rule" {
			found = true
		}
	}
	if !found {
		t.Fatalf("approved rule starved by incidental noise: %+v", approved)
	}
	general, err := s.ContextualEvidenceApplicable(owner, map[string]string{}, time.Now(), 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range general {
		if e.ID == "rule" {
			t.Fatal("the general window reached an approval older than its bound, which this assertion exists to show it cannot")
		}
	}
	// A FORGOTTEN RULE STAYS GONE EVEN IN THE BINDING PROJECTION.
	if err := s.SuppressContextualSource(owner, "turn:rule", "hash-rule", "explicit forget"); err != nil {
		t.Fatal(err)
	}
	approved, err = s.ContextualEvidenceApproved(owner, map[string]string{}, time.Now(), ContextualEvidenceLimit)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range approved {
		if e.ID == "rule" {
			t.Fatalf("suppressed rule resurfaced in the binding projection: %+v", approved)
		}
	}
}
