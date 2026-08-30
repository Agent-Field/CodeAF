package bare

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/verify"
)

// suiteWorkspace stages a project that says how it is checked — a Makefile with
// a `test` target, which is what verify.Discover reads — and wires that target
// to a script whose output and exit status this test controls between readings.
//
// Every run of the suite appends a line to a log OUTSIDE the workspace, so
// counting the readings is a fact about the world rather than a claim by
// anything under test, and so the counting itself never shows up as a change to
// the tree the leaf is being measured against.
type suiteWorkspace struct {
	workspace *exec.Workspace
	runLog    string
	output    string
	exitCode  string
}

func stageSuite(t *testing.T) *suiteWorkspace {
	t.Helper()
	root := t.TempDir()
	outside := t.TempDir()
	stage := &suiteWorkspace{
		runLog:   filepath.Join(outside, "runs"),
		output:   filepath.Join(outside, "output"),
		exitCode: filepath.Join(outside, "exit"),
	}
	makefile := "test:\n" +
		"\t@echo ran >> " + stage.runLog + "\n" +
		"\t@cat " + stage.output + "\n" +
		"\t@exit `cat " + stage.exitCode + "`\n"
	if err := os.WriteFile(filepath.Join(root, "Makefile"), []byte(makefile), 0o644); err != nil {
		t.Fatal(err)
	}
	stage.says(t, "", 0)
	workspace, err := exec.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	stage.workspace = workspace
	return stage
}

// says sets what the next reading of the suite will print and how it will exit.
func (s *suiteWorkspace) says(t *testing.T, output string, exit int) {
	t.Helper()
	if err := os.WriteFile(s.output, []byte(output), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.exitCode, []byte(strconv.Itoa(exit)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// readings is how many times the project's own test command actually ran.
func (s *suiteWorkspace) readings(t *testing.T) int {
	t.Helper()
	body, err := os.ReadFile(s.runLog)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return len(strings.Fields(string(body)))
}

// A leaf too short to afford the measurement does not take it, rather than
// spending its whole life measuring. Sixty seconds of wall buys 7.5 seconds a
// reading, which cannot hold even the fastest project suite anybody has
// measured here — so the command is never run at all, and PERF.md says why.
func TestALeafWhoseWallCannotAffordTheReadingTakesNoPhotographAtAll(t *testing.T) {
	stage := stageSuite(t)
	worker := New(stage.workspace, "model", "key", "http://127.0.0.1:1", time.Minute)

	reading, _ := worker.photographBefore(context.Background(), exec.Task{Goal: t.Name()})

	if reading.Taken {
		t.Errorf("a sixty-second leaf took a reading: %#v", reading)
	}
	if got := stage.readings(t); got != 0 {
		t.Errorf("the project's own test command ran %d time(s) for a leaf that cannot "+
			"afford it; it must run none", got)
	}
	// The same project, on a wall that can afford it, is photographed — so the
	// refusal above is about the wall and not about the discovery.
	if _, affordable := verify.ReadingBudget(90 * time.Minute); !affordable {
		t.Errorf("a ninety-minute wall cannot afford a reading; the share is wrong")
	}
	if budget, _ := verify.ReadingBudget(90 * time.Minute); budget != 11*time.Minute+15*time.Second {
		t.Errorf("a ninety-minute wall affords %v a reading, want 11m15s", budget)
	}
}

// A leaf that changed nothing cannot have regressed anything, so the second
// reading is not taken. Whether the tree moved is the workspace's own
// before-and-after comparison, which the caller passes in; nothing here stats
// the world a second time.
func TestALeafThatChangedNothingTakesNoSecondPhotograph(t *testing.T) {
	stage := stageSuite(t)
	worker := New(stage.workspace, "model", "key", "http://127.0.0.1:1", 90*time.Minute)

	reading, _ := worker.photographBefore(context.Background(), exec.Task{Goal: t.Name()})
	if !reading.Taken {
		t.Fatalf("the first reading was not taken from a project with a make test target")
	}
	if got := stage.readings(t); got != 1 {
		t.Fatalf("the first reading ran the command %d time(s), want 1", got)
	}

	outcome := &exec.Outcome{}
	worker.photographAfter(context.Background(), exec.Task{Goal: t.Name()}, reading, false, false, outcome)

	if got := stage.readings(t); got != 1 {
		t.Errorf("the command ran %d time(s) for a leaf that changed nothing; the second "+
			"reading must not be taken", got)
	}
	if outcome.Regressed != nil {
		t.Errorf("Regressed = %#v, want nil — nobody looked twice, so there is no claim",
			outcome.Regressed)
	}
}

// The whole point, end to end: the check that was green before this work and is
// red after it is named, the one that was already red is not, and the
// pre-existing red is still owed to the judge in words because the judge cannot
// rerun anything.
func TestOnlyTheCheckThisWorkTurnedRedIsNamedOnTheOutcome(t *testing.T) {
	stage := stageSuite(t)
	stage.says(t, "FAILED tests/test_env.py::test_needs_root - PermissionError\n", 1)
	worker := New(stage.workspace, "model", "key", "http://127.0.0.1:1", 90*time.Minute)

	reading, _ := worker.photographBefore(context.Background(), exec.Task{Goal: t.Name()})
	if !reading.Taken {
		t.Fatalf("the first reading was not taken")
	}

	// The work: something that also breaks a check nobody asked about.
	stage.says(t, "FAILED tests/test_env.py::test_needs_root - PermissionError\n"+
		"FAILED tests/test_igel.py::test_results_path - AttributeError\n", 1)
	outcome := &exec.Outcome{}
	worker.photographAfter(context.Background(), exec.Task{Goal: t.Name()}, reading, true, false, outcome)

	want := []string{"tests/test_igel.py::test_results_path"}
	if len(outcome.Regressed) != 1 || outcome.Regressed[0] != want[0] {
		t.Errorf("Regressed = %#v, want exactly %#v", outcome.Regressed, want)
	}
	if len(outcome.Baseline) != 1 || !strings.Contains(outcome.Baseline[0], "test_needs_root") {
		t.Errorf("Baseline = %#v, want one sentence naming the pre-existing red check",
			outcome.Baseline)
	}
	if strings.Contains(outcome.Baseline[0], "test_results_path") {
		t.Errorf("Baseline names a check this work broke: %q", outcome.Baseline[0])
	}
}

// THE BASELINE IS THE JOB'S, AND EVERY ROUND IS MEASURED AGAINST IT. A repair
// round is a new leaf standing in a tree its own job has already changed, and a
// round that photographed what IT found took the broken tree as its baseline —
// so a check the first round turned red subtracted to nothing in the second and
// was never a finding again. textual s5 walked twenty project checks down to one
// across four rounds and raised no regression at any of them.
//
// The second round also spends no suite run on a baseline it already has, which
// is where the inherited reading pays for itself.
func TestARepairRoundIsMeasuredAgainstTheTreeTheJobStartedWith(t *testing.T) {
	verify.ForgetBaselines()
	t.Cleanup(verify.ForgetBaselines)

	stage := stageSuite(t)
	stage.says(t, "PASSED tests/test_igel.py::test_results_path\n"+
		"PASSED tests/test_igel.py::test_fit\n", 0)
	job := exec.Task{Goal: "persist the feature schema"}

	// Round one breaks a check the repository already had, and its own after
	// reading names it.
	first := New(stage.workspace, "model", "key", "http://127.0.0.1:1", 90*time.Minute)
	reading, inherited := first.photographBefore(context.Background(), job)
	if !reading.Taken || inherited {
		t.Fatalf("the first round of a job inherited a baseline: taken=%v inherited=%v",
			reading.Taken, inherited)
	}
	stage.says(t, "PASSED tests/test_igel.py::test_fit\n"+
		"FAILED tests/test_igel.py::test_results_path - AttributeError\n", 1)
	outcome := &exec.Outcome{}
	first.photographAfter(context.Background(), job, reading, true, inherited, outcome)
	if len(outcome.Regressed) != 1 {
		t.Fatalf("the round that broke the check did not name it: %#v", outcome.Regressed)
	}
	readingsAfterRoundOne := stage.readings(t)

	// Round two is a new leaf, in a new workspace object, on the same tree and
	// the same job. It repairs something else and leaves the broken check
	// broken.
	space, err := exec.NewWorkspace(stage.workspace.Root())
	if err != nil {
		t.Fatal(err)
	}
	second := New(space, "model", "key", "http://127.0.0.1:1", 90*time.Minute)
	carried, inherited := second.photographBefore(context.Background(), job)
	if !inherited {
		t.Fatal("a continuation of the same job in the same tree took its own " +
			"baseline, so every check an earlier round broke is now invisible")
	}
	if got := stage.readings(t); got != readingsAfterRoundOne {
		t.Errorf("the continuation ran the suite %d time(s) for a baseline it "+
			"already had", got-readingsAfterRoundOne)
	}
	second.photographAfter(context.Background(), job, carried, false, inherited, outcome)
	if len(outcome.Regressed) != 1 || outcome.Regressed[0] != "tests/test_igel.py::test_results_path" {
		t.Errorf("Regressed = %#v; the check the job broke two rounds ago is not "+
			"named against the tree the job started with", outcome.Regressed)
	}
}

// A READING THAT COULD NOT BE TAKEN IS JOURNALED, AND THE JOB PAYS FOR FINDING
// THAT OUT ONCE.
//
// textual s6 is the run this is about. The bare leaf spent five minutes and
// twenty-seven seconds of its wall on a reading that was killed at its ceiling,
// then made its first model call, and the finished store held no row saying any
// of it had happened — indistinguishable, from outside, from a project that
// declares no verification at all. Every way of having no reading now writes its
// own sentence, and a continuation of the same job inherits that answer rather
// than spending the wall again to reach it.
func TestAReadingThatCouldNotBeTakenIsJournaledAndNotRepeated(t *testing.T) {
	verify.ForgetBaselines()
	t.Cleanup(verify.ForgetBaselines)

	stage := stageSuite(t)
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "task-2", Brief: "expose the follow state", Stage: 1,
	}}}, store.Provenance{
		Origin: store.OriginUser, SessionID: "s1", Intent: "expose the follow state",
	}); err != nil {
		t.Fatal(err)
	}
	job := exec.Task{Goal: "expose the follow state", StoreNodeID: "task-2"}

	// A minute of wall affords 7.5 seconds a reading, which is under the floor,
	// so nothing is run — and that refusal is the news.
	worker := New(stage.workspace, "model", "key", "http://127.0.0.1:1", time.Minute).WithStore(graph)
	reading, inherited := worker.photographBefore(context.Background(), job)
	if reading.Taken || inherited {
		t.Fatalf("a sixty-second leaf took a reading: %#v", reading)
	}
	if got := stage.readings(t); got != 0 {
		t.Errorf("the suite ran %d time(s) for a leaf that could not afford it", got)
	}
	rows, err := graph.VerificationsFor("task-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("a reading that could not be taken was journaled as %d rows, want 1", len(rows))
	}
	if rows[0].Read {
		t.Error("a reading nobody took was journaled as one somebody did")
	}
	if !strings.Contains(rows[0].Why, "cannot afford") {
		t.Errorf("the row does not say why nothing was read: %+v", rows[0])
	}
	if rows[0].Command == "" {
		t.Error("the row does not name the command that was not run, so an autopsy " +
			"cannot tell where the reader was looking")
	}

	// And the outcome carries the reason too, so the gate is handed a reason
	// rather than a void.
	outcome := &exec.Outcome{}
	worker.photographAfter(context.Background(), job, reading, true, inherited, outcome)
	if outcome.Verification.Taken || outcome.Verification.Unread == "" {
		t.Errorf("the outcome does not say why nothing was measured: %#v", outcome.Verification)
	}
	if outcome.Regressed != nil {
		t.Errorf("Regressed = %#v, want nil — nobody looked, so there is no claim",
			outcome.Regressed)
	}

	// The continuation inherits the answer instead of spending the wall to
	// reach it again.
	space, err := exec.NewWorkspace(stage.workspace.Root())
	if err != nil {
		t.Fatal(err)
	}
	second := New(space, "model", "key", "http://127.0.0.1:1", 90*time.Minute).WithStore(graph)
	carried, inherited := second.photographBefore(context.Background(), job)
	if !inherited {
		t.Fatal("a continuation of the same job re-derived a refusal that cannot have " +
			"changed, and paid for it again")
	}
	if carried.Taken || carried.Unread == "" {
		t.Errorf("the inherited answer lost its reason: %#v", carried)
	}
	if got := stage.readings(t); got != 0 {
		t.Errorf("the continuation ran the suite %d time(s) for an answer it had", got)
	}
	rows, _ = graph.VerificationsFor("task-2")
	if len(rows) != 2 || !rows[1].Inherited {
		t.Errorf("the continuation's own row is missing or not marked inherited: %+v", rows)
	}
}
