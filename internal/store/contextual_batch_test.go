package store

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// THE PER-MEMORY LATEST READ IS A SEEK, NOT A PARTITION WALK. This pins the
// query plan the binding projection depends on: an indexed scan over
// events_contextual_memory, with no full scan of the owner's evidence partition.
func TestContextualLatestForMemoriesUsesMemoryIndex(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "latest-plan.db"))
	owner := "project:ledger"
	for i := 0; i < 500; i++ {
		n := strconv.Itoa(i)
		if _, err := s.AppendContextualEvidence(ContextualEvidence{ID: "obs-" + n, MemoryID: "n-" + n,
			Owner: owner, Actor: "tool", Tool: "bash", ReceiptIDs: []string{"r-" + n}, Authority: "observation",
			Observation: "incidental", SourceKey: "s:obs-" + n, SourceHash: "h-" + n}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.db.Query("EXPLAIN QUERY PLAN "+contextualLatestForMemoriesSQL(2),
		"m1", "m2", contextualNode(owner), EventContextualEvidence, contextualNode(owner), EventContextualEvidence)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(lines, " | ")
	if !strings.Contains(joined, "events_contextual_memory") {
		t.Fatalf("latest-by-memory read does not use the memory index: %s", joined)
	}
	if strings.Contains(joined, "SCAN events") {
		t.Fatalf("latest-by-memory read still scans the evidence partition: %s", joined)
	}
}

// AND THE LATEST READ IS NOT WINDOWED. A memory whose newest evidence sits
// behind a burst of newer rows is still answered, so no hidden total-claim
// ceiling can drop a live rule.
func TestContextualLatestForMemoriesIsNotWindowed(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "latest-cap.db"))
	owner := "project:ledger"
	if _, err := s.AppendContextualEvidence(ContextualEvidence{ID: "old-rule", MemoryID: "rule-mem", Owner: owner,
		Actor: "user", Authority: "approved_rule", Observation: "hold this rule"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < ContextualEvidenceLimit*2; i++ {
		n := strconv.Itoa(i)
		if _, err := s.AppendContextualEvidence(ContextualEvidence{ID: "obs-" + n, MemoryID: "n-" + n,
			Owner: owner, Actor: "tool", ReceiptIDs: []string{"r-" + n}, Authority: "observation", Observation: "noise"}); err != nil {
			t.Fatal(err)
		}
	}
	found, err := s.ContextualEvidenceLatestForMemories(owner, []string{"rule-mem"})
	if err != nil {
		t.Fatal(err)
	}
	if found["rule-mem"].ID != "old-rule" {
		t.Fatalf("the live rule behind %d newer rows was not answered: %+v", ContextualEvidenceLimit*2, found)
	}
}

// THE BATCHED GUARD ANSWERS EXACTLY WHAT THE SINGLE-RECORD DOOR ANSWERS, for a
// live record, an expired one, a suppressed memory, a suppressed source and a
// derivation whose parent is expired. The batched path must not become a second,
// weaker rule.
func TestContextualEligibleBatchMatchesTheSingleRecordDoor(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "eligible-batch.db"))
	owner := "user"
	at := time.Now()
	live, err := s.AppendContextualEvidence(ContextualEvidence{ID: "live", MemoryID: "m-live", Owner: owner,
		Actor: "user", Authority: "approved_rule", Observation: "bind me"})
	if err != nil {
		t.Fatal(err)
	}
	expired, err := s.AppendContextualEvidence(ContextualEvidence{ID: "expired", MemoryID: "m-expired", Owner: owner,
		Actor: "user", Authority: "approved_rule", Observation: "stale", ValidUntil: at.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	suppressed, err := s.AppendContextualEvidence(ContextualEvidence{ID: "suppressed", MemoryID: "m-suppressed", Owner: owner,
		Actor: "user", Authority: "approved_rule", Observation: "forgotten"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SuppressContextualMemorySources(owner, "m-suppressed", "forgotten"); err != nil {
		t.Fatal(err)
	}
	sourceGone, err := s.AppendContextualEvidence(ContextualEvidence{ID: "source", MemoryID: "m-source", Owner: owner,
		Actor: "tool", ReceiptIDs: []string{"r"}, Authority: "observation", Observation: "seen", SourceKey: "key", SourceHash: "h1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SuppressContextualSource(owner, "key", "h1", "retired"); err != nil {
		t.Fatal(err)
	}
	// The parent is valid when it is written but expired by the time the guard
	// reads it, which is the only way an append-time validation lets the derived
	// row exist at all.
	parentExpired, err := s.AppendContextualEvidence(ContextualEvidence{ID: "parent", MemoryID: "m-parent", Owner: owner,
		Actor: "user", Authority: "user", Observation: "old", ValidUntil: at.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	derived, err := s.AppendContextualEvidence(ContextualEvidence{ID: "derived", MemoryID: "m-derived", Owner: owner,
		Authority: "inference", Observation: "because", Derivations: []int64{parentExpired.Seq}})
	if err != nil {
		t.Fatal(err)
	}
	records := []ContextualEvidence{live, expired, suppressed, sourceGone, derived}
	conditions := map[string]string{"project": "ledger", "revision": "rev"}
	evaluated := at.Add(2 * time.Hour)
	got, err := s.ContextualEvidenceEligibleBatch(records, conditions, evaluated)
	if err != nil {
		t.Fatal(err)
	}
	want := make([]bool, len(records))
	for i, e := range records {
		canonical, err := readContextualEvidence(s.db, e.Owner, e.Seq)
		if err != nil {
			t.Fatal(err)
		}
		budget := ContextualEvidenceLimit
		usable, err := contextualUsable(contextualDB{s.db}, canonical, evaluated, map[int64]bool{}, &budget)
		if err != nil {
			t.Fatal(err)
		}
		eligible, err := contextualConditionsBounded(contextualDB{s.db}, canonical, conditions)
		if err != nil {
			t.Fatal(err)
		}
		want[i] = usable && eligible
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("batched eligibility %v does not match the single-record door %v", got, want)
	}
	if !want[0] || want[1] || want[2] || want[3] || want[4] {
		t.Fatalf("the fixture no longer exercises every guard: %v", want)
	}
}

// THE BATCHED GUARD IS OWNER-SCOPED. A record naming another owner is answered
// against ITS owner's journal, so a suppression in one owner cannot retire a
// claim in another.
func TestContextualEligibleBatchIsOwnerScoped(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "eligible-owner.db"))
	at := time.Now()
	other, err := s.AppendContextualEvidence(ContextualEvidence{ID: "other", MemoryID: "shared", Owner: "project:b",
		Actor: "user", Authority: "approved_rule", Observation: "b rule"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SuppressContextualMemorySources("project:a", "shared", "a forgot its own"); err != nil {
		t.Fatal(err)
	}
	got, err := s.ContextualEvidenceEligibleBatch([]ContextualEvidence{other}, map[string]string{"project": "b"}, at)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0] {
		t.Fatalf("another owner's suppression retired project:b's claim: %v", got)
	}
}
