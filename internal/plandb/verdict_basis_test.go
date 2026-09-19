package plandb

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

// A HOLDS VERDICT MUST REST ON ONE COMMAND THE SHELL WOULD RUN AS ONE COMMAND.
// The store is the second reader of the check door's law: even with a recorded
// zero exit, it credits a holds verdict only to a declared check that is one
// audited command, judged by the same quote-aware reader the proposal door uses
// ([approval.FirstCompositionOutsideQuotes]). A quoted bar is one command and
// holds; every form that is more than one command is refused, so a verdict can
// never rest on it.
func TestCheckVerdictBasisAdmitsAQuotedBarAndRefusesEveryNeverRunForm(t *testing.T) {
	const quotedBar = `grep -iE 'handoff|vault|wall' walls.md`
	store := planOpen(t, filepath.Join(t.TempDir(), "plan.db"))
	defer store.Close()

	writeCheckRuns(t, store, "quoted-bar", map[string]int{quotedBar: 0})
	basis, earned := store.checkVerdictBasis(&Task{TaskSpec: TaskSpec{ID: "quoted-bar", Role: RoleCheck, Checks: []string{quotedBar}}})
	if !earned {
		t.Fatalf("the store did not credit a holds verdict to a quoted-bar check run to exit 0: %#v", basis)
	}
	if basis.Kind != "run" || len(basis.Runs) != 1 || basis.Runs[0].Command != quotedBar || basis.Runs[0].ExitCode != 0 {
		t.Fatalf("basis = %#v, want one recorded run of the quoted bar at exit 0", basis)
	}

	// The never-run forms of the proposal door's own contract test
	// (task_checks_contract_test.go). Each is more than one command, so the store
	// must refuse a holds verdict on it even though a zero exit is on file.
	neverRun := []string{
		quotedBar + ` && touch RAN`,
		quotedBar + ` ; touch RAN`,
		`grep "$(touch RAN)" walls.md`,
		"grep \"`touch RAN`\" walls.md",
		`grep "a\"; touch RAN; \"" walls.md`,
		`grep 'unclosed walls.md ; touch RAN`,
		quotedBar + ` > RAN`,
		quotedBar + ` | tee RAN`,
	}
	for i, form := range neverRun {
		id := fmt.Sprintf("never-%d", i)
		writeCheckRuns(t, store, id, map[string]int{form: 0})
		if _, earned := store.checkVerdictBasis(&Task{TaskSpec: TaskSpec{ID: id, Role: RoleCheck, Checks: []string{form}}}); earned {
			t.Errorf("the store credited a holds verdict to a check that is more than one command: %q", form)
		}
	}
}

// writeCheckRuns writes the checker's trajectory for a task so that
// [Store.recordedRuns] reads back one recorded step per command with its exit.
func writeCheckRuns(t *testing.T, store *Store, id string, runs map[string]int) {
	t.Helper()
	dir := TaskDir(filepath.Dir(store.path), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for command, exit := range runs {
		line, err := json.Marshal(map[string]any{"kind": "step", "command": command, "exit_code": exit})
		if err != nil {
			t.Fatal(err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(dir, "trajectory.jsonl"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}
