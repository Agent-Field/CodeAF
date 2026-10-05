package store

import (
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
	relearn, err := s.AppendContextualEvidence(base)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := s.ContextualEvidenceEligible(relearn, nil, time.Now()); err != nil || ok {
		t.Fatalf("relearn eligible=%v err=%v", ok, err)
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
