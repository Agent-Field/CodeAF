package run_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/run"
)

func fixtureCommands(t *testing.T, root, task string) []string {
	t.Helper()
	steps, err := run.Trajectory(root, task)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	commands := make([]string, 0, len(steps))
	for _, step := range steps {
		commands = append(commands, step.Command)
	}
	if len(commands) == 0 {
		t.Fatal("fixture has no recorded commands")
	}
	return commands
}

func writeCheckRuns(t *testing.T, store *plandb.Store, id string, runs map[string]int) {
	t.Helper()
	dir := plandb.TaskDir(filepath.Dir(store.Path()), id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("make trajectory directory: %v", err)
	}
	file, err := os.Create(filepath.Join(dir, "trajectory.jsonl"))
	if err != nil {
		t.Fatalf("create trajectory: %v", err)
	}
	defer file.Close()
	step := 0
	for command, exitCode := range runs {
		step++
		if _, err := fmt.Fprintf(file, "{\"kind\":\"step\",\"step\":%d,\"command\":%q,\"exit_code\":%d}\n", step, command, exitCode); err != nil {
			t.Fatalf("write trajectory: %v", err)
		}
	}
}

// THE DECLARATION IS THE CONTRACT. Recorded work can contain any number and
// shape of commands, but the review node receives exactly what the worker put
// on the leaf. The two fixtures deliberately come from different project
// trees, so this proof cannot depend on recognizing a particular runner word.
func TestReviewContractComesOnlyFromTheWorkersDeclarationAcrossFixtures(t *testing.T) {
	fixtures := []struct {
		name string
		root string
		task string
	}{
		{name: "long recorded work", root: "testdata/c249", task: "jcrhzh"},
		{name: "alternate recorded work", root: "testdata/declared", task: "alternate"},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			recorded := fixtureCommands(t, fixture.root, fixture.task)
			declared := []string{recorded[len(recorded)-1], recorded[0]}
			store := runOpenStore(t)
			seat := newFakeSeat()
			seat.actions["root"] = splitRoot(t, store, plandb.TaskSpec{
				ID: "leaf", Title: "the leaf", Checks: declared,
			})
			supervisor := run.NewSupervisor(store, t.TempDir(), 2, run.Limits{ReviewRound: true}, seat.workerFor)
			if outcome := supervisor.Run(runContext(t)); outcome != run.OutcomeDone {
				t.Fatalf("outcome = %q, want done", outcome)
			}
			checks := tasksWithRole(store, plandb.RoleCheck)
			if len(checks) != 1 {
				t.Fatalf("review nodes = %d, want one", len(checks))
			}
			if !reflect.DeepEqual(checks[0].Checks, declared) {
				t.Fatalf("review contract = %#v, want declaration %#v", checks[0].Checks, declared)
			}
		})
	}
}

// A HOLDS CONCLUSION NEEDS EVERY DECLARED COMMAND. Each must appear in the
// checker's own record with a recorded zero exit, and each must pass the same
// read-only audit law used by the checker. A command merely present in the
// worker's earlier record proves none of those facts.
func TestHoldsRequiresEveryDeclaredRunWithZeroExitAndAuditApproval(t *testing.T) {
	declared := []string{"./verify focused", "./verify boundary"}
	cases := []struct {
		name string
		runs map[string]int
	}{
		{name: "one declaration missing", runs: map[string]int{declared[0]: 0}},
		{name: "one declaration failed", runs: map[string]int{declared[0]: 0, declared[1]: 9}},
		{name: "declaration is not auditable", runs: map[string]int{"*": 0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checks := declared
			if tc.name == "declaration is not auditable" {
				checks = []string{"*"}
			}
			store := runOpenStore(t)
			if _, err := store.AddMany([]plandb.TaskSpec{{ID: "review", Title: "check: leaf", Role: plandb.RoleCheck, Checks: checks}}); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Claim("review", "review"); err != nil {
				t.Fatal(err)
			}
			writeCheckRuns(t, store, "review", tc.runs)
			if _, err := store.Done("review", "review", "holds: the acceptance is met", nil, nil); err == nil {
				t.Fatal("holds accepted without every declared zero-exit audited run")
			}
		})
	}
}

// WITH NO DECLARATION, THE CHECKER READS. Holds can therefore land without a
// command record, while a does-not-hold conclusion is never gated even when a
// declaration exists and was not run.
func TestReadingCanHoldAndDoesNotHoldIsUngated(t *testing.T) {
	cases := []struct {
		name       string
		checks     []string
		conclusion string
	}{
		{name: "reading holds", conclusion: "holds: reading proves the acceptance"},
		{name: "finding is ungated", checks: []string{"./verify focused"}, conclusion: "does not hold: reading found a defect"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := runOpenStore(t)
			if _, err := store.AddMany([]plandb.TaskSpec{{ID: "review", Title: "check: leaf", Role: plandb.RoleCheck, Checks: tc.checks}}); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Claim("review", "review"); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Done("review", "review", tc.conclusion, nil, nil); err != nil {
				t.Fatalf("reading conclusion refused: %v", err)
			}
		})
	}
}
