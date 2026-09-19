package plandb

import (
	"encoding/json"
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

// EVERY COMPOSED FORM IS REFUSED BY THE STORE'S OWN AUDIT, whatever its exit.
// These are the proposal door's own never-run forms (task_checks_contract_test.go);
// the store asks the same one-command question the door does, so a holds verdict
// can never rest on one of them.
func TestAuditableDeclaredCheckAdmitsAQuotedBarAndRefusesEveryComposedForm(t *testing.T) {
	const quotedBar = `grep -iE 'handoff|vault|wall' walls.md`
	if !auditableDeclaredCheck(quotedBar) {
		t.Fatalf("the store refused a quoted bar that is one command: %q", quotedBar)
	}
	for _, form := range []string{
		quotedBar + ` && touch RAN`,
		quotedBar + ` ; touch RAN`,
		`grep "$(touch RAN)" walls.md`,
		"grep \"`touch RAN`\" walls.md",
		`grep "a\"; touch RAN; \"" walls.md`,
		`grep 'unclosed walls.md ; touch RAN`,
		quotedBar + ` > RAN`,
		quotedBar + ` | tee RAN`,
	} {
		if auditableDeclaredCheck(form) {
			t.Errorf("the store admitted a check that is more than one command: %q", form)
		}
	}
}

// AN EXIT THE RECORDER NEVER WROTE IS UNKNOWN, AND UNKNOWN EARNS NOTHING. A step
// with no exit_code field, which is every step of a record written before the
// field existed, does not read as a zero. When the whole record carries no exit
// the verdict is a reading one and reading can hold; when some exit was recorded
// but the declared command's was not, or its exit was non-zero, holds is refused.
// The earned path is proven end to end through the real recorder in internal/run,
// never from a hand-written zero here.
func TestCheckVerdictBasisTreatsAnAbsentExitAsUnknown(t *testing.T) {
	const check = "grep vault walls.md"
	checkTask := &Task{TaskSpec: TaskSpec{ID: "review", Role: RoleCheck, Checks: []string{check}}}

	t.Run("a genuinely old record with no marker holds by reading and names what it did not observe", func(t *testing.T) {
		store := planOpen(t, filepath.Join(t.TempDir(), "plan.db"))
		defer store.Close()
		writeTrajectoryLines(t, store, "review", `{"kind":"step","step":1,"command":"grep vault walls.md"}`)
		basis, earned := store.checkVerdictBasis(checkTask)
		if !earned || basis.Kind != "reading" {
			t.Fatalf("an old record read as %#v earned=%v, want a reading that holds", basis, earned)
		}
		if len(basis.Unobserved) != 1 || basis.Unobserved[0] != check {
			t.Fatalf("reading basis unobserved = %#v, want the declared check %q", basis.Unobserved, check)
		}
	})

	t.Run("a new build that observed no run refuses holds", func(t *testing.T) {
		store := planOpen(t, filepath.Join(t.TempDir(), "plan.db"))
		defer store.Close()
		// The build recorded exits, so its ending line is stamped, but the declared
		// check has no zero-exit run: holds is refused rather than falling back to
		// reading, which is the state an old record cannot be told apart from.
		writeTrajectoryLines(t, store, "review",
			`{"kind":"step","step":1,"command":"grep vault walls.md"}`,
			`{"kind":"end","exits_recorded":true}`)
		if _, earned := store.checkVerdictBasis(checkTask); earned {
			t.Fatal("a new build that never ran the declared check earned holds by reading")
		}
	})

	t.Run("a new build cut off before its ending refuses holds", func(t *testing.T) {
		store := planOpen(t, filepath.Join(t.TempDir(), "plan.db"))
		defer store.Close()
		// The opening line marks the record new; the run was cut off before any
		// ending and the declared check has no recorded exit, so holds is refused
		// rather than read as old (issue 1224, the window closed mid-run).
		writeTrajectoryLines(t, store, "review",
			`{"kind":"begin","exits_recorded":true}`,
			`{"kind":"step","step":1,"command":"grep vault walls.md"}`)
		if _, earned := store.checkVerdictBasis(checkTask); earned {
			t.Fatal("a new build cut off before its ending earned holds by reading")
		}
	})

	t.Run("a recorded exit elsewhere but not for the check refuses holds", func(t *testing.T) {
		store := planOpen(t, filepath.Join(t.TempDir(), "plan.db"))
		defer store.Close()
		writeTrajectoryLines(t, store, "review",
			`{"kind":"step","step":1,"command":"ls","exit_code":0}`,
			`{"kind":"step","step":2,"command":"grep vault walls.md"}`)
		if _, earned := store.checkVerdictBasis(checkTask); earned {
			t.Fatal("holds earned though the declared check has no recorded exit")
		}
	})

	t.Run("a recorded non-zero exit refuses holds", func(t *testing.T) {
		store := planOpen(t, filepath.Join(t.TempDir(), "plan.db"))
		defer store.Close()
		writeTrajectoryLines(t, store, "review", `{"kind":"step","step":1,"command":"grep vault walls.md","exit_code":1}`)
		if _, earned := store.checkVerdictBasis(checkTask); earned {
			t.Fatal("holds earned on a recorded non-zero exit")
		}
	})
}

// writeTrajectoryLines writes raw trajectory lines for a task, for the decode
// tests that must set a step's exit field present or absent. It never writes the
// earned path (an auditable command at a recorded zero); that is proven through
// the real recorder in internal/run so no fake can hide a recorder that writes
// no exit.
func writeTrajectoryLines(t *testing.T, store *Store, id string, lines ...string) {
	t.Helper()
	dir := TaskDir(filepath.Dir(store.path), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "trajectory.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
