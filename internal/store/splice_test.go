package store

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// A spec that does not survive replay is prose again by the second attempt.
//
// The store holds it as opaque bytes and is asked to guarantee exactly one
// thing about them: what went in comes back out, through a reopen and through a
// full rebuild from the journal, like every other node field. A node spliced
// without one has to come back empty rather than as "{}" — an empty spec must
// be indistinguishable from a node admitted before specs existed, or the
// fallback path is not the path it was before and the rollback is not one.
func TestSpecSurvivesSpliceJournalAndRebuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spec.db")
	graph := openTestStore(t, path)

	spec := json.RawMessage(`{"instruction":"write the thing","method":"read before writing",` +
		`"done":{"produces":["the named result"],` +
		`"conditions":[{"kind":"run","check":"the stated command","expect":"it reports success"}]}}`)
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "job", Brief: "produce the thing", Stage: 1, Spec: spec},
		{ID: "job-n2", Parent: "job", Brief: "a part of it", Stage: 2},
	}}, Provenance{Origin: OriginUser, SessionID: "s", Intent: "produce the thing"}); err != nil {
		t.Fatal(err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openTestStore(t, path)
	assert := func(stage string) {
		t.Helper()
		node, found, err := reopened.Node("job")
		if err != nil || !found {
			t.Fatalf("%s: read job: found=%t err=%v", stage, found, err)
		}
		var restored struct {
			Instruction string `json:"instruction"`
			Method      string `json:"method"`
			Done        struct {
				Produces   []string `json:"produces"`
				Conditions []struct {
					Kind   string `json:"kind"`
					Check  string `json:"check"`
					Expect string `json:"expect"`
				} `json:"conditions"`
			} `json:"done"`
		}
		if err := json.Unmarshal(node.Spec, &restored); err != nil {
			t.Fatalf("%s: the spec came back unreadable (%q): %v", stage, node.Spec, err)
		}
		if restored.Instruction != "write the thing" || restored.Method != "read before writing" {
			t.Fatalf("%s: spec = %+v", stage, restored)
		}
		if len(restored.Done.Produces) != 1 || len(restored.Done.Conditions) != 1 ||
			restored.Done.Conditions[0].Kind != "run" {
			t.Fatalf("%s: the criterion did not survive: %+v", stage, restored.Done)
		}
		// A node spliced without one carries nothing, not an empty object.
		child, found, err := reopened.Node("job-n2")
		if err != nil || !found {
			t.Fatalf("%s: read job-n2: found=%t err=%v", stage, found, err)
		}
		if len(child.Spec) != 0 {
			t.Fatalf("%s: a spec-less node came back holding %q", stage, child.Spec)
		}
	}
	assert("reopened")
	if err := reopened.Rebuild(); err != nil {
		t.Fatal(err)
	}
	assert("rebuilt")
}
