package session

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/orchestrate"
)

// A row that does not say what sort of work it was is a row a surface has to
// guess about, and the guesses available before this field — the shape of the
// transcript path, the "run · " prefix on a title — were both about facts the
// engine already held.
func TestAnAdaptiveRunsRowsSayTheyAreAdaptive(t *testing.T) {
	var index string
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
		index = TaskIndexPath(config.SessionFile)
	})
	family := agent.newOrchestrateFamily("audit the pricing code", "", "run-3")
	family.upsert([]orchestrate.NodeStatus{
		{Node: orchestrate.Node{ID: "tariff", Goal: "read the tariff table"}, State: orchestrate.Done, Digest: "found regional prices"},
	})

	rows := ReadTaskIndex(index)
	if len(rows) == 0 {
		t.Fatal("the run wrote no rows at all")
	}
	for _, row := range rows {
		if row.Kind != TaskKindAdaptive {
			t.Fatalf("row %q carries kind %q, want %q", row.Title, row.Kind, TaskKindAdaptive)
		}
	}
}

// The field is additive, so every row this project has already written has to
// keep decoding — as ordinary work, which is what it was.
func TestARowWrittenBeforeKindsDecodesAsOrdinaryWork(t *testing.T) {
	var row TaskIndexEntry
	if err := json.Unmarshal([]byte(`{"id":"4","name":"fix-the-nil-map","label":"fix the nil map","title":"fix the nil map","status":"done"}`), &row); err != nil {
		t.Fatalf("an old row no longer decodes: %v", err)
	}
	if row.Kind != "" {
		t.Fatalf("an old row decoded with kind %q, want none", row.Kind)
	}
	if word := TaskKindWord(row.Kind); word != "" {
		t.Fatalf("ordinary work reads as %q, want nothing at all", word)
	}
}

// A kind that is written is a kind that stays off the wire when it is absent:
// two thousand rows of ordinary work must not each carry an empty field.
func TestOrdinaryWorkWritesNoKindField(t *testing.T) {
	line, err := json.Marshal(TaskIndexEntry{ID: "1", Name: "x", Label: "x", Title: "x", Status: "done"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := string(line); strings.Contains(got, `"kind"`) {
		t.Fatalf("an ordinary row spelled a kind: %s", got)
	}
}

// THE WORDS ARE THE SURFACE'S AND NOT THE ENGINE'S. A screen that said
// "subharness" would be the machine's own noun for a saved shape of work, which
// is the vocabulary law this codebase pins everywhere else.
func TestTheKindWordsAreWordsAPersonUses(t *testing.T) {
	for kind, want := range map[TaskKind]string{
		TaskKindAdaptive:   "adaptive",
		TaskKindSubharness: "saved shape",
		TaskKindHarness:    "making a saved shape",
		TaskKindJob:        "background job",
		"":                 "",
		"something-new":    "",
	} {
		if got := TaskKindWord(kind); got != want {
			t.Fatalf("TaskKindWord(%q) = %q, want %q", kind, got, want)
		}
	}
	for _, banned := range []string{"subharness", "harness", "node", "orchestrate"} {
		for _, kind := range []TaskKind{TaskKindAdaptive, TaskKindSubharness, TaskKindHarness, TaskKindJob} {
			if strings.Contains(TaskKindWord(kind), banned) {
				t.Fatalf("the word for %q says %q, which is machinery", kind, banned)
			}
		}
	}
}
