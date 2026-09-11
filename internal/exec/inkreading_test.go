package exec

// The second reading, held to ink's own shape.

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/verify"
)

// inkSuite stages a project whose suite behaves the way ink's does under this
// program's ceiling: it STREAMS its checks in TAP and then keeps going, so the
// reading is killed with a roster already in hand.
//
// The hang is a sleep rather than a real suite because what is under test is
// what this program does with a cut reading, not what a runner does.
func inkSuite(t *testing.T, streamed int, hang time.Duration) (*Workspace, verify.Strategy) {
	t.Helper()
	root := t.TempDir()
	script := "#!/bin/sh\n"
	for index := 1; index <= streamed; index++ {
		script += "echo 'ok " + strconv.Itoa(index) + " - grid > box layout " + strconv.Itoa(index) + "'\n"
	}
	script += ": > .checks-emitted\nsleep " + strconv.Itoa(int(hang.Seconds())) + "\n"
	suite := filepath.Join(root, "suite.sh")
	if err := os.WriteFile(suite, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	workspace, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	return workspace, verify.Strategy{
		Command: "./suite.sh", Read: verify.FormatPlain, Runner: "ava",
		Declared: "npm test", Source: "package.json#scripts.test", Scope: verify.ScopeWhole,
	}
}

// A CUT ROSTER IS STILL A ROSTER, ON BOTH SIDES OF THE PHOTOGRAPH.
//
// ink's after-reading has come back `read: false, named: 0` in two whole sweeps —
// every leaf of s10 and s11 — while the baseline of the same run, on the same
// `npx ava --tap`, named 44 checks and 156 in one case. Nothing was wrong with
// the reader, the scope or the room: the runner streamed its checks, the ceiling
// fired, and THIS SIDE THREW AWAY WHAT IT HAD ALREADY READ. The before half has
// kept a cut roster since ink s7; this half never did.
func TestACutSecondReadingKeepsWhatItNamed(t *testing.T) {
	workspace, strategy := inkSuite(t, 6, 30*time.Second)
	reading := verify.Reading{
		Taken: true, Budget: 30 * time.Second,
		Before: verify.Result{
			Strategy: strategy,
			Reported: []string{"grid > box layout 1", "grid > box layout 2"},
		},
	}
	// Observe the output before cutting it. A ceiling measured from process
	// launch can fire before the shell starts under full-suite load, which
	// tests an empty reading rather than preservation of a partial roster.
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	outcome := &Outcome{}
	go func() {
		defer close(finished)
		PhotographAfter(ctx, workspace, nil, time.Hour,
			Task{Goal: t.Name()}, Opening{Reading: reading}, true, outcome)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("the second reading did not settle after cancellation")
		}
	})
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
waiting:
	for {
		select {
		case <-tick.C:
			if _, err := os.Stat(filepath.Join(workspace.Root(), ".checks-emitted")); err == nil {
				break waiting
			}
		case <-deadline.C:
			t.Fatal("the fixture never emitted its checks")
		case <-finished:
			t.Fatal("the fixture ended before the requested cut")
		}
	}
	cancel()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("the second reading did not settle after cancellation")
	}

	after := outcome.Verification
	if !after.AfterTaken {
		t.Fatalf("a runner that named six checks before its ceiling was read as unread: %q",
			after.Unread)
	}
	want := []string{
		"grid > box layout 1", "grid > box layout 2", "grid > box layout 3",
		"grid > box layout 4", "grid > box layout 5", "grid > box layout 6",
	}
	if !slices.Equal(after.After.Reported, want) {
		t.Fatalf("the streamed roster was not preserved: got %v, want %v", after.After.Reported, want)
	}
	if !after.After.TimedOut {
		t.Error("the ceiling fired and the result does not say so")
	}
	// PARTIAL, so the subtraction is refused: the checks it never reached are
	// missing for a reason that has nothing to do with this change.
	if !after.Partial {
		t.Error("a cut second reading was offered for subtraction")
	}
	if len(outcome.Regressed) != 0 {
		t.Errorf("a partial roster was subtracted anyway: %v", outcome.Regressed)
	}
}

// AND A COMMAND THAT RAN IS NOT A TREE NOBODY COULD READ. Those are two facts and
// they used to share one sentence, which is the sentence a gate is handed when it
// has to say why nothing was measured.
func TestACommandThatRanAndNamedNothingSaysSo(t *testing.T) {
	workspace, strategy := inkSuite(t, 0, 30*time.Second)
	reading := verify.Reading{
		Taken: true, Budget: 900 * time.Millisecond,
		Before: verify.Result{Strategy: strategy, Reported: []string{"grid > box layout 1"}},
	}
	outcome := &Outcome{}
	PhotographAfter(context.Background(), workspace, nil, time.Hour,
		Task{Goal: t.Name()}, Opening{Reading: reading}, true, outcome)

	why := outcome.Verification.Unread
	if outcome.Verification.AfterTaken {
		t.Fatal("a runner that named nothing at all was read as a reading")
	}
	if !strings.Contains(why, "ran on the finished tree") {
		t.Errorf("the record does not say the command ran: %q", why)
	}
	if strings.Contains(why, "was not read") {
		t.Errorf("a command that ran is still spelled as a tree nobody could read: %q", why)
	}
}

// AND A DERIVED RUNG THAT WILL NOT RUN FALLS BACK TO THE ONE THE BASELINE PROVED.
//
// The after reading is allowed to differ from the before one — widened by the
// run's own checks, or aimed at the diff where the whole suite did not fit — and
// both of those choose a command the baseline never watched work. A selection
// this program built is the one thing here a retake can fix.
func TestASecondReadingRetakesOnTheBaselinesOwnRung(t *testing.T) {
	workspace, baseline := inkSuite(t, 4, 0)
	reading := verify.Reading{
		Taken: true, Budget: 2 * time.Second,
		Before: verify.Result{Strategy: baseline, Reported: []string{"grid > box layout 1"}},
	}
	// A rung this program derived and that names nothing — a runner handed a
	// path it cannot run on its own.
	derived := baseline
	derived.Base = "./suite.sh"
	derived.Selected = []string{"test/nothing-here.js"}
	derived.Command = "./suite.sh --files test/nothing-here.js >/dev/null"
	derived.Scope = "touched packages (1 file)"

	after, ok := readFinishedTree(context.Background(), workspace.Root(), derived, reading, nil)
	if !ok {
		t.Fatal("neither the derived rung nor the baseline's could be started")
	}
	if len(after.Reported) == 0 {
		t.Fatal("a derived rung that named nothing was reported without retaking on the " +
			"one command this job has watched work")
	}
	if after.Strategy.Command != baseline.Command {
		t.Errorf("the retake did not use the baseline's own rung: %q", after.Strategy.Command)
	}
	// And a rung that IS the baseline's is not run twice to learn the same thing.
	same, _ := readFinishedTree(context.Background(), workspace.Root(), baseline, reading, nil)
	if len(same.Reported) == 0 {
		t.Error("the baseline's own rung came back empty")
	}
}

// A SUITE THAT FAILED TO COLLECT IS REPORTED AS THAT, END TO END.
//
// ofetch's nemotron n1 run took four readings whose suite never ran a check —
// `vitest run --reporter=json` printed no JSON because an import would not
// resolve — and each was journaled `named: 1, red: 1` and subtracted against a
// baseline that had named 28. The gate failed the delivery with `This work broke
// checks that were passing before it: to.`
func TestAnAfterReadingWhoseSuiteDidNotCollectSaysSo(t *testing.T) {
	root := t.TempDir()
	suite := filepath.Join(root, "suite.sh")
	script := "#!/bin/sh\n" +
		"echo ' RUN  v0.34.6 /app'\n" +
		"echo 'Failed to load url ./circuit-breaker (resolved id: ./circuit-breaker) " +
		"in /app/test/index.test.ts. Does the file exist?'\n" +
		"echo ' Test Files  1 failed (1)'\n" +
		"exit 1\n"
	if err := os.WriteFile(suite, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	workspace, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	strategy := verify.Strategy{
		Command: "./suite.sh", Read: verify.FormatNodeJSON, Runner: "vitest",
		Declared: "pnpm test", Scope: verify.ScopeWhole,
	}
	reading := verify.Reading{
		Taken: true, Budget: 5 * time.Second,
		Before: verify.Result{
			Strategy: strategy,
			Reported: []string{"ofetch 404", "ofetch calls hooks", "ofetch hook errors"},
		},
	}
	outcome := &Outcome{}
	PhotographAfter(context.Background(), workspace, nil, time.Hour,
		Task{Goal: t.Name()}, Opening{Reading: reading}, true, outcome)

	if outcome.Verification.AfterTaken {
		t.Fatal("a suite that never ran a check was taken as a reading of the finished tree")
	}
	if len(outcome.Regressed) > 0 {
		t.Errorf("the work was convicted of breaking %v by a suite that ran nothing",
			outcome.Regressed)
	}
	why := outcome.Verification.Unread
	if !strings.Contains(why, "failed to collect") {
		t.Errorf("the record does not say the suite failed to collect: %q", why)
	}
	if !strings.Contains(why, "Failed to load url") {
		t.Errorf("the record does not carry the runner's own words: %q", why)
	}
}

// countingSuite stages a project whose declared suite RECORDS EVERY TIME IT IS
// RUN, in a file outside the tree so that running it changes nothing.
//
// Counting invocations is the only honest way to test a rule about not looking
// twice: what is under test is whether the command ran, and a roster cannot say
// whether it was read or reproduced.
func countingSuite(t *testing.T) (*Workspace, string) {
	t.Helper()
	root, log := t.TempDir(), filepath.Join(t.TempDir(), "runs.log")
	files := map[string]string{
		"go.mod":   "module example.com/thing\n\ngo 1.22\n",
		"Makefile": "test:\n\t@echo run >> " + log + "\n\t@echo 'ok 1 - a check'\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	workspace, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	return workspace, log
}

// readings is how many times the staged suite has been run.
func readings(t *testing.T, log string) int {
	t.Helper()
	body, err := os.ReadFile(log)
	if err != nil {
		return 0
	}
	return len(strings.Fields(string(body)))
}

// A READING IS RETAKEN ONLY WHEN THE TREE CHANGED.
//
// The errand measured in #429 was ordered to change no files, and it read the
// repository five times: once before its first leaf, and once on the "finished"
// tree of every leaf after it — each `go test -json ./...` over 4,587 tests,
// each killed at its two-minute ceiling, 82% of an 11m40s wall. Every one of
// those after-readings was bought by a leaf having INHERITED the job's reading,
// which every leaf after the first does whatever it touched.
func TestATreeNothingChangedIsNotReadTwice(t *testing.T) {
	verify.ForgetBaselines()
	t.Cleanup(verify.ForgetBaselines)
	workspace, log := countingSuite(t)
	ctx := context.Background()
	task := Task{Goal: "Run the command 'make test' in this workspace and report the " +
		"final line it prints. Change no files."}

	opening := PhotographBefore(ctx, workspace, nil, time.Hour, task)
	if !opening.Reading.Taken {
		t.Fatalf("the first leaf of the job took no reading: %q", opening.Reading.Unread)
	}
	if opening.Moved {
		t.Error("the first leaf of a job was told the job had already changed the tree")
	}
	if count := readings(t, log); count != 1 {
		t.Fatalf("the first reading ran the suite %d times, want 1", count)
	}

	// A second leaf of the same job, standing in the same tree. It inherits,
	// which it always did — and it is told the tree has not moved, which is what
	// decides whether it pays for a second reading.
	second := PhotographBefore(ctx, workspace, nil, time.Hour, task)
	if !second.Reading.Taken || second.Moved {
		t.Fatalf("a leaf standing in an unchanged tree was told otherwise: %#v", second.Moved)
	}
	outcome := &Outcome{}
	PhotographAfter(ctx, workspace, nil, time.Hour, task, second, false, outcome)
	if count := readings(t, log); count != 1 {
		t.Errorf("the suite was run %d times over a tree nothing changed, want 1", count)
	}
	// AND THE FINISHED TREE IS STILL MEASURED, because it is the tree that was
	// already read. A gate handed no after-reading cannot ask what the work
	// covered; this one is handed the roster that stands.
	if !outcome.Verification.AfterTaken {
		t.Fatal("an unchanged tree was left with no reading of it at all")
	}
	if len(outcome.Verification.After.Reported) != len(second.Reading.Before.Reported) {
		t.Errorf("the roster that stands is not the one that was read: %#v",
			outcome.Verification.After.Reported)
	}

	// AND A LEAF THAT DID CHANGE SOMETHING IS READ AGAIN. The rule is about a
	// tree that did not move, and nothing else.
	PhotographAfter(ctx, workspace, nil, time.Hour, task, second, true, outcome)
	if count := readings(t, log); count != 2 {
		t.Errorf("a leaf that changed the tree was not read again: %d readings", count)
	}

	// And a continuation that arrives holding what an earlier round produced is
	// told the tree moved, which is what buys it the second reading.
	continuation := task
	continuation.Inputs = []Input{{Artifacts: []string{"cmd/main.go"}}}
	if !PhotographBefore(ctx, workspace, nil, time.Hour, continuation).Moved {
		t.Error("a round standing on an earlier round's files was told the tree had not moved")
	}
}

// A DELETION IS A CHANGE, AND THE ARTIFACT LIST CANNOT SAY SO.
//
// Workspace.Artifacts holds what the tree STILL has — deliberately, because that
// list is also what the person is shown — so a leaf whose whole job was to take
// a file out reported an empty list. Read as "this leaf changed nothing", it
// skipped the second reading, which is the one that would have caught what the
// removal broke.
func TestALeafThatOnlyDeletedAFileIsStillRead(t *testing.T) {
	verify.ForgetBaselines()
	t.Cleanup(verify.ForgetBaselines)
	workspace, log := countingSuite(t)
	ctx, leaf := context.Background(), "task-2"
	doomed := filepath.Join(workspace.Root(), "doomed.go")
	if err := os.WriteFile(doomed, []byte("package thing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	task := Task{Goal: "Take the dead file out. Change nothing else.", NodeKey: leaf}

	opening := PhotographBefore(ctx, workspace, nil, time.Hour, task)
	if !opening.Reading.Taken {
		t.Fatalf("the leaf took no reading: %q", opening.Reading.Unread)
	}
	workspace.WatchTree(task.leafKey())
	if err := os.Remove(doomed); err != nil {
		t.Fatal(err)
	}
	workspace.RecordChanges(task.leafKey())

	if artifacts := workspace.Artifacts(task.leafKey()); len(artifacts) != 0 {
		t.Fatalf("a deletion reached the artifact list, so this test measures nothing: %#v",
			artifacts)
	}
	if !leafMovedTheTree(workspace, task.leafKey()) {
		t.Fatal("a leaf that deleted a file was read as a leaf that changed nothing")
	}
	outcome := &Outcome{Artifacts: workspace.Artifacts(task.leafKey())}
	PhotographAfter(ctx, workspace, nil, time.Hour, task, opening,
		leafMovedTheTree(workspace, task.leafKey()), outcome)
	if count := readings(t, log); count != 2 {
		t.Errorf("the tree lost a file and was read %d times, want the second reading", count)
	}
}
