package plandb

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
)

// THE VERDICT BASIS IS PART OF THE LEAF RECORD. A reader must be able to tell
// whether the checker read the work or ran the declared proof, including the
// recorded exit from every run, without reopening the trajectory.
func TestVerdictBasisPersistsForRunAndReadingJudgments(t *testing.T) {
	cases := []struct {
		name  string
		basis VerdictBasis
	}{
		{
			name: "declared proof was run",
			basis: VerdictBasis{Kind: "run", Runs: []VerdictRun{
				{Command: "./verify focused", ExitCode: 0},
				{Command: "./verify boundary", ExitCode: 7},
			}},
		},
		{name: "work was read", basis: VerdictBasis{Kind: "reading"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "plan.db")
			store := planOpen(t, path)
			planAdd(t, store, TaskSpec{ID: "leaf", Title: "Checked leaf"})
			if _, err := store.SetVerdictBasis("leaf", tc.basis); err != nil {
				t.Fatalf("record verdict basis: %v", err)
			}
			if err := store.Close(); err != nil {
				t.Fatalf("close store: %v", err)
			}

			reopened := planReopen(t, path)
			defer reopened.Close()
			leaf := reopened.Task("leaf")
			if !reflect.DeepEqual(leaf.VerdictBasis, tc.basis) {
				t.Fatalf("persisted basis = %#v, want %#v", leaf.VerdictBasis, tc.basis)
			}
		})
	}
}

// THE TASKS READING CARRIES THE SAME FIELD THE STORE PERSISTS. This is the
// structured view consumed by task readers, so dropping it here would make the
// durable basis unavailable even while it remained in the database.
func TestTaskJSONCarriesThePersistedVerdictBasis(t *testing.T) {
	store := planOpen(t, filepath.Join(t.TempDir(), "plan.db"))
	defer store.Close()
	planAdd(t, store, TaskSpec{ID: "leaf", Title: "Checked leaf"})
	basis := VerdictBasis{Kind: "run", Runs: []VerdictRun{{Command: "./verify focused", ExitCode: 0}}}
	leaf, err := store.SetVerdictBasis("leaf", basis)
	if err != nil {
		t.Fatalf("record verdict basis: %v", err)
	}

	data, err := json.Marshal(cliTaskObject(store, leaf))
	if err != nil {
		t.Fatalf("write task reading: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("read task reading: %v", err)
	}
	var got VerdictBasis
	if err := json.Unmarshal(fields["verdict_basis"], &got); err != nil {
		t.Fatalf("read verdict basis: %v in %s", err, data)
	}
	if !reflect.DeepEqual(got, basis) {
		t.Fatalf("task reading basis = %#v, want %#v", got, basis)
	}
}
