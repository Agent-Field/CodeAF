package store

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A CLAIM AND ITS PROVENANCE LAND TOGETHER. WriteContextual admits the memory
// row and its evidence row in one transaction, and the eligible projection sees
// the claim immediately afterwards.
func TestWriteContextualLandsRowAndEvidenceAtomically(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "publish.db"))
	result, evidence, err := s.WriteContextual(
		WriteRequest{Owner: "project:ledger", Type: MemoryDecision, Title: "offline release", Text: "Release runtime uses standard library only."},
		ContextualEvidence{ID: "ev", Owner: "project:ledger", SessionID: "s", TurnID: "t", Actor: "user", Authority: "approved_rule",
			Observation: "release runtime must use the standard library", Verification: "asserted",
			SourceKey: "s:t", SourceHash: "h"})
	if err != nil {
		t.Fatalf("write contextual: %v", err)
	}
	if result.Outcome != WriteOutcomeAdded || result.Memory.ID == "" {
		t.Fatalf("outcome=%q memory=%+v", result.Outcome, result.Memory)
	}
	if evidence.MemoryID != result.Memory.ID || evidence.Seq == 0 {
		t.Fatalf("evidence did not land with the row: %+v", evidence)
	}
	eligible, err := s.ContextualEvidenceEligible(evidence, map[string]string{"project": "ledger"}, time.Now())
	if err != nil || !eligible {
		t.Fatalf("published evidence not eligible: %v/%v", eligible, err)
	}
}

// A REFUSED EVIDENCE ROW ROLLS THE MEMORY ROW BACK WITH IT. Before the write
// doors shared a transaction, applyCandidate's add committed and then the
// evidence append failed, leaving an active tagged row with no provenance. Here
// the suppressed source must leave NOTHING behind.
func TestWriteContextualRollsBackARowWhoseEvidenceIsSuppressed(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "publish.db"))
	if err := s.SuppressContextualSource("project:ledger", "s:t", "h", "forgotten"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.WriteContextual(
		WriteRequest{Owner: "project:ledger", Type: MemoryDecision, Title: "relearned", Text: "The ledger uses exact decimals."},
		ContextualEvidence{ID: "relearn", Owner: "project:ledger", SessionID: "s", TurnID: "t", Actor: "user", Authority: "approved_rule",
			Observation: "relearned", Verification: "asserted", SourceKey: "s:t", SourceHash: "h"}); err == nil {
		t.Fatal("a suppressed source was republished")
	}
	rows, err := s.ListMemories([]string{"project:ledger"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("the refused write left rows behind: %+v", rows)
	}
}

// FORGET RETIRES THE ROW AND ITS PROVENANCE IN ONE CALL. The provenance
// suppression used to be a second transaction the session ran, so a crash
// between them left an active row whose sources were already unusable.
func TestForgetMemorySuppressesProvenanceInOneCall(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "forget.db"))
	m, err := s.AddMemory(Memory{ID: "m", Owner: "project:ledger", Type: MemoryDecision, Scope: MemoryScopeProject, Title: "t", Text: "b"})
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := s.AppendContextualEvidence(ContextualEvidence{ID: "e", MemoryID: m.ID, Owner: "project:ledger",
		Actor: "user", Authority: "user", Observation: "a claim", SourceKey: "s:t", SourceHash: "h"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ForgetMemoryForOwners([]string{"project:ledger"}, m.ID, "conv"); err != nil {
		t.Fatal(err)
	}
	record, found, err := s.MemoryRecord(m.ID)
	if err != nil || !found || record.Status != MemoryForgotten {
		t.Fatalf("row not forgotten: %+v/%v/%v", record, found, err)
	}
	eligible, err := s.ContextualEvidenceEligible(evidence, nil, time.Now())
	if err != nil || eligible {
		t.Fatalf("forget did not retire the provenance in the same call: %v/%v", eligible, err)
	}
}

// THE APPROVED-RULE READ IS AN INDEX SEEK, NOT A PARTITION WALK. With rare
// binding rules under thousands of newer observations, the floor query must use
// the authority expression index rather than scanning the owner's partition.
func TestContextualApprovedQueryUsesAuthorityIndex(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "scale.db"))
	owner := "project:ledger"
	for i := 0; i < 2000; i++ {
		n := strconv.Itoa(i)
		if _, err := s.AppendContextualEvidence(ContextualEvidence{ID: "obs-" + n, MemoryID: "n-" + n,
			Owner: owner, Actor: "tool", Tool: "bash", ReceiptIDs: []string{"r-" + n}, Authority: "observation",
			Observation: "incidental", SourceKey: "s:obs-" + n, SourceHash: "h-" + n}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.AppendContextualEvidence(ContextualEvidence{ID: "rule", MemoryID: "rule-mem", Owner: owner,
		Actor: "user", Authority: "approved_rule", Observation: "bind me", Verification: "asserted",
		SourceKey: "s:t", SourceHash: "h"}); err != nil {
		t.Fatal(err)
	}
	const floor = `SELECT COALESCE(MIN(seq),0)-1 FROM (SELECT seq FROM events WHERE node_id=? AND kind=? AND json_extract(payload,'$.Authority') IN ('approved_rule','confirmed_decision') ORDER BY seq DESC LIMIT ?)`
	rows, err := s.db.Query("EXPLAIN QUERY PLAN "+floor, contextualNode(owner), EventContextualEvidence, ContextualEvidenceLimit)
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
	if !strings.Contains(joined, "events_contextual_approved") {
		t.Fatalf("approved-rule read does not use the authority index: %s", joined)
	}
	if strings.Contains(joined, "SCAN events") {
		t.Fatalf("approved-rule read still scans the evidence partition: %s", joined)
	}
}

// EVIDENCE CANNOT FORGE ANOTHER OWNER. A publication whose provenance names a
// different blast radius than the row is refused, and nothing lands.
func TestWriteContextualRefusesCrossOwnerEvidence(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "publish.db"))
	_, _, err := s.WriteContextual(
		WriteRequest{Owner: "project:ledger", Type: MemoryDecision, Title: "t", Text: "the ledger uses exact decimals"},
		ContextualEvidence{ID: "e", Owner: "project:other", SessionID: "s", TurnID: "t", Actor: "user",
			Authority: "approved_rule", Observation: "x", Verification: "asserted", SourceKey: "s:t", SourceHash: "h"})
	if err == nil {
		t.Fatal("cross-owner evidence was accepted")
	}
	for _, owner := range []string{"project:ledger", "project:other"} {
		rows, err := s.ListMemories([]string{owner}, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 0 {
			t.Fatalf("a refused publication left a row for %s: %+v", owner, rows)
		}
	}
}
