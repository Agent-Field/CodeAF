package run

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/session"
)

// THE PRODUCT MUST WRITE THE REAL EXIT, NOT A TEST. A real command is run
// through the belt's own bash tool, its result is turned into the same tool
// event the session loop emits, and the run's own recorder writes the step.
// Then the store judges a holds verdict off that record. No test writes the
// exit line by hand, so a recorder that wrote the wrong exit, or none, fails
// here: a real passing check holds and a real failing check is refused.
func TestHoldsRestsOnTheRecordersOwnExitFromARealCommand(t *testing.T) {
	const check = "grep vault walls.md"
	cases := []struct {
		name     string
		contents string
		hold     bool
	}{
		{name: "a real passing check holds", contents: "the vault is here\n", hold: true},
		{name: "a real failing check is refused", contents: "nothing here\n", hold: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ground := t.TempDir()
			if err := os.WriteFile(filepath.Join(ground, "walls.md"), []byte(tc.contents), 0o644); err != nil {
				t.Fatal(err)
			}
			event := runRealBashStep(t, ground, check)

			store, err := plandb.Open(filepath.Join(t.TempDir(), "plan.json"), "run-test", "root", "The run", "drive")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			if _, err := store.AddMany([]plandb.TaskSpec{{ID: "review", Title: "check: leaf", Role: plandb.RoleCheck, Checks: []string{check}}}); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Claim("review", "review"); err != nil {
				t.Fatal(err)
			}
			rec := stepRecorder{store: store, storeDir: filepath.Dir(store.Path()), taskID: "review", children: childrenOf(store, "review")}
			if err := rec.record(1, event); err != nil {
				t.Fatalf("record the real step: %v", err)
			}

			_, err = store.Done("review", "review", "holds: the acceptance is met", nil, nil)
			switch {
			case tc.hold && err != nil:
				t.Fatalf("a real zero-exit command was refused holds: %v", err)
			case !tc.hold && err == nil:
				t.Fatal("a real failing command earned holds")
			}
		})
	}
}

// runRealBashStep runs one command through the belt's own bash tool in ground
// and turns its result into the tool event the session loop would emit: a
// non-zero exit is an isError result and an EventToolFailed, a clean one an
// EventToolEnd. This is exactly the event the recorder reads, so the exit the
// recorder writes is derived, not supplied.
func runRealBashStep(t *testing.T, ground, command string) session.Event {
	t.Helper()
	var bash bare.Tool
	found := false
	for _, tool := range bare.Tools(ground) {
		if tool.Name == "bash" {
			bash, found = tool, true
			break
		}
	}
	if !found {
		t.Fatal("the belt has no bash tool")
	}
	args, err := json.Marshal(struct {
		Command string `json:"command"`
	}{Command: command})
	if err != nil {
		t.Fatal(err)
	}
	text, isError, err := bash.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("run the real command: %v", err)
	}
	kind := session.EventToolEnd
	if isError {
		kind = session.EventToolFailed
	}
	return session.Event{Kind: kind, Tool: "bash", Args: string(args), Output: text}
}

// A NEW BUILD CUT OFF BEFORE ITS ENDING IS REFUSED HOLDS. The run stamped its
// opening line, then the checker's only attempt was refused (a step with no
// exit) and the run was cut off before any ending (issue 1224, the window
// closed mid-run). The opening stamp still marks the record new, so the store
// refuses a holds verdict rather than fall back to reading.
func TestANewBuildCutOffBeforeItsEndingIsRefusedHolds(t *testing.T) {
	const check = "grep vault walls.md"
	store, err := plandb.Open(filepath.Join(t.TempDir(), "plan.json"), "run-test", "root", "The run", "drive")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.AddMany([]plandb.TaskSpec{{ID: "review", Title: "check: leaf", Role: plandb.RoleCheck, Checks: []string{check}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Claim("review", "review"); err != nil {
		t.Fatal(err)
	}
	storeDir := filepath.Dir(store.Path())
	// The run stamps its opening line before any step, through the product's own
	// append, so a record cut off after it is still known to be new.
	if err := appendTrajectory(storeDir, "review", Step{Kind: trajectoryBeginKind, ExitsRecorded: true}); err != nil {
		t.Fatalf("append the opening line: %v", err)
	}
	rec := stepRecorder{store: store, storeDir: storeDir, taskID: "review", children: childrenOf(store, "review")}
	// A harness refusal: the command never ran, so the recorder writes a step
	// with no exit at all. No ending line follows: the run was cut off.
	refused := session.Event{Kind: session.EventToolFailed, Tool: "bash", HarnessMade: true, Args: `{"command":"grep vault walls.md"}`, Output: "refused: the check did not run"}
	if err := rec.record(1, refused); err != nil {
		t.Fatalf("record the refused step: %v", err)
	}
	steps, err := Trajectory(storeDir, "review")
	if err != nil || len(steps) != 1 || !steps[0].NotRun {
		t.Fatalf("refused event did not establish not-run on its recorded step: steps=%+v err=%v", steps, err)
	}
	// THE RECORD KEEPS WHICH KIND OF ANSWER IT WAS. The event above carries no
	// attempted action a door refused, so it is a correction and nothing more; the
	// same event WITH the fact is recorded as a refused action, which is the one
	// kind the task page draws.
	if steps[0].Refused {
		t.Fatalf("a harness answer with no refused door was recorded as a refused action: %+v", steps[0])
	}
	door := refused
	door.Refused = true
	if err := rec.record(2, door); err != nil {
		t.Fatalf("record the refused action: %v", err)
	}
	steps, err = Trajectory(storeDir, "review")
	if err != nil || len(steps) != 2 || !steps[1].NotRun || !steps[1].Refused || steps[1].ExitCode != nil {
		t.Fatalf("a refused action was not recorded as not run, refused and without an exit: steps=%+v err=%v", steps, err)
	}
	if _, err := store.Done("review", "review", "holds: the acceptance is met", nil, nil); err == nil {
		t.Fatal("a new build cut off before its ending earned holds by reading")
	}
}
