package session

// LoadLandedForJudge rebuilds a judgeable landing from a persisted checkpoint, so
// the restart sweep can score a run whose process is gone. The high seat is the
// one field that has to survive on the record for this to be worth anything, so
// that is what this pins.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestLoadLandedForJudgeRebuildsAFinalNodeWithItsHighSeat writes a checkpoint
// with one Done node carrying a worker and a checker, reads it back through
// LoadLandedForJudge, and asserts the landing carries both seats and the token
// count off the record. Before CheckedOn was persisted, High came back empty —
// this is the pin that it no longer does.
func TestLoadLandedForJudgeRebuildsAFinalNodeWithItsHighSeat(t *testing.T) {
	doc := taskDocument{
		Type:    taskDocumentType,
		Version: taskFileVersion,
		Seq:     9,
		Nodes: []taskRecord{{
			ID:          3,
			Title:       "read ALPHA",
			Brief:       "read ALPHA and say what it holds",
			Acceptance:  "the reading names what ALPHA holds",
			Deliverable: "the reading",
			State:       TaskDone,
			Report:      "the report the node landed with",
			Model:       "crew/worker",
			CheckedOn:   "crew/high",
			Input:       100_000,
			Output:      50_000,
			CostUSD:     0.5,
			Changed:     []string{"alpha.md"},
			Checks:      []string{"go test ./..."},
			Attempt:     1,
		}},
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal checkpoint: %v", err)
	}
	path := filepath.Join(t.TempDir(), "tasks.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write checkpoint: %v", err)
	}

	landed, err := LoadLandedForJudge(path)
	if err != nil {
		t.Fatalf("LoadLandedForJudge: %v", err)
	}
	if len(landed) != 1 {
		t.Fatalf("read %d landings, want the 1 final node", len(landed))
	}
	got := landed[0]
	if got.Worker != "crew/worker" {
		t.Errorf("Worker = %q, want crew/worker", got.Worker)
	}
	if got.High != "crew/high" {
		t.Errorf("High = %q, want crew/high (the persisted checker seat)", got.High)
	}
	if got.Tokens != 150_000 {
		t.Errorf("Tokens = %d, want 150000 (Input+Output off the record)", got.Tokens)
	}
	if got.Changed != 1 {
		t.Errorf("Changed = %d, want 1", got.Changed)
	}
	if got.Attempt != 1 {
		t.Errorf("Attempt = %d, want 1", got.Attempt)
	}
	if got.ID != 3 || got.State != TaskDone {
		t.Errorf("ID/State = %d/%v, want 3/Done", got.ID, got.State)
	}
}

// TestLoadLandedForJudgeSkipsNodesStillInFlight: a node the checkpoint holds in a
// non-final state is not a landing and must not be handed to the judge.
func TestLoadLandedForJudgeSkipsNodesStillInFlight(t *testing.T) {
	doc := taskDocument{
		Type:    taskDocumentType,
		Version: taskFileVersion,
		Seq:     2,
		Nodes: []taskRecord{{
			ID:         5,
			Title:      "still running",
			Brief:      "a node that has not landed",
			Acceptance: "it lands",
			State:      TaskRunning,
			Model:      "crew/worker",
		}},
	}
	data, _ := json.Marshal(doc)
	path := filepath.Join(t.TempDir(), "tasks.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write checkpoint: %v", err)
	}
	landed, err := LoadLandedForJudge(path)
	if err != nil {
		t.Fatalf("LoadLandedForJudge: %v", err)
	}
	if len(landed) != 0 {
		t.Fatalf("read %d landings from a still-running node, want 0", len(landed))
	}
}
