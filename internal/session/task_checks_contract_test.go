package session

// WHAT A CHECKER MAY MAKE HAPPEN, MEASURED ON A SCRIPT THAT COUNTS ITS OWN RUNS.
//
// The live failure these cases are written from: a task was asked to run a slow
// build script ONCE and report the marker it wrote. The worker ran it, exit 0,
// and read the marker back — and the checker, which read that receipt as a door,
// ran the same script again. Every case here is about the difference between
// evidence of what ran and permission to run it, and the counter file is what
// makes the difference something a test can see rather than something a comment
// asserts.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

// countingGround is a tree holding one script that DOES SOMETHING WHEN IT RUNS:
// it appends a line to a file outside the tree and prints a marker. The counter
// lives outside on purpose — the checker judges a clean restore, so a counter
// inside the tree would be reset by the restore and the second run would be
// invisible, which is exactly how this defect stayed unseen.
func countingGround(t *testing.T) (ground, counter string) {
	t.Helper()
	ground = t.TempDir()
	counter = filepath.Join(t.TempDir(), "runs")
	script := "#!/bin/sh\necho ran >> " + counter + "\necho QUARTZLINE > build.log\n"
	writeCheckFile(t, ground, "count.sh", script, 0o755)
	return ground, counter
}

// timesRun is how many times the counting script has run, which is the whole
// measurement.
func timesRun(t *testing.T, counter string) int {
	t.Helper()
	body, err := os.ReadFile(counter)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatalf("reading the counter: %v", err)
	}
	return len(strings.Fields(string(body)))
}

// checkerBash is the auditor's own bash, gated by a real door and backed by a
// real shell in the ground. The execution is real because a gate tested against
// a stub proves only that the stub was not called.
func checkerBash(t *testing.T, door auditDoor, ground string) bare.Tool {
	t.Helper()
	return readingOnlyBash(bare.Tool{
		Name: "bash",
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var fields struct {
				Command string `json:"command"`
			}
			if err := json.Unmarshal(args, &fields); err != nil {
				return "", true, err
			}
			run := exec.CommandContext(ctx, "/bin/sh", "-c", fields.Command)
			run.Dir = ground
			out, err := run.CombinedOutput()
			return string(out), err != nil, nil
		},
	}, door, auditShell)
}

// typedAtTheChecker runs one command the way the checker would type it, and says
// what came back and whether the gate refused it.
func typedAtTheChecker(t *testing.T, tool bare.Tool, command string) (string, bool) {
	t.Helper()
	args, err := json.Marshal(struct {
		Command string `json:"command"`
	}{Command: command})
	if err != nil {
		t.Fatal(err)
	}
	text, _, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("executing %q: %v", command, err)
	}
	return text, strings.HasPrefix(text, "refused:")
}

// THE REQUESTED ACTION HAPPENS ONCE, AND THE CHECKER STILL CHECKS IT.
//
// The node's own document names the script in every way prose names a command —
// backticked in the brief, on a prompt line in the done-condition — and its
// worker's receipt shows the run. None of that is a door. The counter is still 1
// when the checker is finished, and the checker could still read the artifact the
// run produced, which is what settles whether the requested action was carried
// out.
func TestTheCheckerAssessesTheWorkWithoutRunningItASecondTime(t *testing.T) {
	ground, counter := countingGround(t)

	// The worker's one run, which is the work itself and not a check.
	run := exec.Command("/bin/sh", "-c", "./count.sh")
	run.Dir = ground
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("the work's own run failed: %v\n%s", err, out)
	}
	if got := timesRun(t, counter); got != 1 {
		t.Fatalf("the work ran the script %d times, want 1", got)
	}

	node := checkedNode("Run `./count.sh` once and report the marker it writes.",
		"build.log holds the marker:\n$ ./count.sh",
		toolReceipt{tool: "bash", args: `{"command":"./count.sh"}`, result: "exit 0"},
		toolReceipt{tool: "bash", args: `{"command":"cat build.log"}`, result: "QUARTZLINE"},
	)
	door := auditDoorFor(node, ground)
	if len(door.checks) != 0 {
		t.Fatalf("a node that declared no check was handed a door onto %q", door.checks)
	}
	tool := checkerBash(t, door, ground)

	for _, typed := range []string{
		"./count.sh", "count.sh", "sh count.sh", "bash count.sh",
		filepath.Join(ground, "count.sh"),
		"sh " + filepath.Join(ground, "count.sh"),
	} {
		if text, refused := typedAtTheChecker(t, tool, typed); !refused {
			t.Fatalf("the checker was allowed to repeat the work as %q:\n%s", typed, text)
		}
	}
	if got := timesRun(t, counter); got != 1 {
		t.Fatalf("the requested action ran %d times; the checker repeated the work", got)
	}

	// AND IT CAN STILL DO ITS JOB. The artifact the run left is readable, and the
	// receipts are in front of it saying the run happened, which together are an
	// answer to "was the requested action carried out".
	text, refused := typedAtTheChecker(t, tool, "cat build.log")
	if refused {
		t.Fatalf("the checker cannot read the artifact it is judging:\n%s", text)
	}
	if !strings.Contains(text, "QUARTZLINE") {
		t.Fatalf("the artifact does not hold the marker: %q", text)
	}
	if !strings.Contains(door.line(), "DECLARED NO REPEATABLE CHECK") {
		t.Fatalf("the checker is not told it has nothing to re-run:\n%s", door.line())
	}
}

// AND A DECLARED CHECK IS STILL RUN, AND STILL SAYS NO.
//
// The other half of the same law: removing the receipt door must not turn every
// check into a reading. A contract that DECLARES its verification gets it run for
// real, and a verification that examines the artifact refuses a wrong one.
func TestADeclaredCheckRunsAndRejectsAWrongArtifact(t *testing.T) {
	ground := t.TempDir()
	writeCheckFile(t, ground, "verify.sh", "#!/bin/sh\ngrep -q QUARTZLINE build.log\n", 0o755)
	writeCheckFile(t, ground, "build.log", "GRANITE\n", 0o644)

	node := declaringNode("./verify.sh")
	door := auditDoorFor(node, ground)
	if len(door.checks) != 1 {
		t.Fatalf("the declared verification did not open the door: %q", door.checks)
	}
	tool := checkerBash(t, door, ground)

	if text, refused := typedAtTheChecker(t, tool, "./verify.sh"); refused {
		t.Fatalf("the declared check was refused by the door that exists to run it:\n%s", text)
	}
	// The gate returns the shell's own output; what the check DECIDED is read off
	// the artifact, which is the only thing that could have made it pass.
	if err := os.WriteFile(filepath.Join(ground, "build.log"), []byte("QUARTZLINE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	right := exec.Command("/bin/sh", "-c", "./verify.sh")
	right.Dir = ground
	if err := right.Run(); err != nil {
		t.Fatalf("the declared check fails on the artifact it should accept: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ground, "build.log"), []byte("GRANITE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wrong := exec.Command("/bin/sh", "-c", "./verify.sh")
	wrong.Dir = ground
	if err := wrong.Run(); err == nil {
		t.Fatal("the declared check accepts an artifact that does not hold the marker")
	}
}

// A COMMAND SOMEBODY QUOTED IS NOT A VERB THE CHECKER HAS.
//
// The person's pasted transcript, the brief's own prose, the acceptance, and the
// worker's receipts are four accounts of what was said and done. None of them is
// a declaration, so none of them reaches the door — and the one command that was
// declared is not widened by any of them.
func TestQuotedAndExecutedCommandsAreNotAdmittedAsCheckerVerbs(t *testing.T) {
	ground := t.TempDir()
	writeCheckFile(t, ground, "deploy.sh", "#!/bin/sh\nexit 0\n", 0o755)
	writeCheckFile(t, ground, "verify.sh", "#!/bin/sh\nexit 0\n", 0o755)

	node := checkedNode("Deploy it with `./deploy.sh`; I ran it myself as:\n$ ./deploy.sh --prod",
		"it is deployed and `./deploy.sh` exited 0",
		toolReceipt{tool: "bash", args: `{"command":"./deploy.sh"}`, result: "exit 0"},
	)
	node.spec.request = "here is what I did:\n$ ./deploy.sh --prod"
	node.Checks = []string{"./verify.sh"}

	door := auditDoorFor(node, ground)
	if len(door.checks) != 1 || door.checks[0] != "./verify.sh" {
		t.Fatalf("the door is %q, want the one command the contract declared", door.checks)
	}
	for _, refused := range []string{"./deploy.sh", "./deploy.sh --prod", "deploy.sh", "sh deploy.sh"} {
		if _, ok := doorRefusal(refused, door); ok {
			t.Fatalf("%q became a checker verb without anybody declaring it", refused)
		}
	}
	if strings.Contains(door.offer(), "deploy") {
		t.Fatalf("the checker is offered the work it was never meant to repeat:\n%s", door.offer())
	}
}

// A DECLARED CONTRACT SURVIVES A RESTART, AND AN OLD CHECKPOINT DECLARES NOTHING.
//
// The field is additive: a record written before it existed decodes without it,
// and the node it rebuilds is checked by reading rather than by a command guessed
// out of its prose.
func TestDeclaredChecksSurviveTheCheckpointAndAnOldRecordDeclaresNone(t *testing.T) {
	graph := newTaskGraph()
	node := &TaskNode{graph: graph, id: 7, done: make(chan struct{}), state: TaskDone,
		spec:   taskSpec{title: "port the parser", acceptance: "it is ported"},
		Checks: []string{"go test ./parser"},
	}
	graph.nodes[7] = node

	node.graph.mu.Lock()
	record := node.recordLocked()
	node.graph.mu.Unlock()
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("writing the record: %v", err)
	}
	if !strings.Contains(string(encoded), `"checks":["go test ./parser"]`) {
		t.Fatalf("the declared verification is not on the checkpoint:\n%s", encoded)
	}
	var read taskRecord
	if err := json.Unmarshal(encoded, &read); err != nil {
		t.Fatalf("reading the record: %v", err)
	}
	back := restoreNode(newTaskGraph(), read)
	if got := back.repeatableChecks(); len(got) != 1 || got[0] != "go test ./parser" {
		t.Fatalf("the restored node is checked by %q", got)
	}

	// AND A RECORD FROM BEFORE THE FIELD EXISTED. It decodes, it runs, and it
	// declares nothing — which is the honest answer rather than a hole. Its
	// `familyChecks` is the load-bearing half: an older build filled that list by
	// harvesting prose, so it carries no provenance anybody can read now, and a
	// restored node must not be handed fresh execution on the strength of it.
	var old taskRecord
	if err := json.Unmarshal([]byte(`{"id":8,"title":"an older node","acceptance":"it is done",
		"brief":"do the thing, checked with `+"`go test ./parser`"+`",
		"familyChecks":["sh ./counter.sh --all"]}`), &old); err != nil {
		t.Fatalf("reading an older record: %v", err)
	}
	older := restoreNode(newTaskGraph(), old)
	if got := older.repeatableChecks(); len(got) != 0 {
		t.Fatalf("a checkpoint written before this field declared %q", got)
	}
	if family := older.familyChecks(); len(family) != 1 {
		t.Fatalf("the older record's own record of what it owns was lost: %q", family)
	}
	door := auditDoorFor(older, t.TempDir())
	if len(door.checks) != 0 {
		t.Fatalf("an older record was guessed into a door: %q", door.checks)
	}
	if _, ok := doorRefusal("sh ./counter.sh --all", door); ok {
		t.Fatal("a family list an older build harvested from prose became executable verification")
	}
}

// A REVISION THAT MOVES THE GOAL TAKES THE OLD GOAL'S CHECKS WITH IT.
//
// This is the real road and not a fabricated version number: a person says "CSV
// instead of JSON", the worker folds that direction in with `revise_assignment`,
// and the node's version advances. A check declared about the JSON output is an
// assertion about a goal nobody is working towards any more — running it would
// ask a question nobody asked, and passing it would be a verdict nobody earned —
// so it goes, along with both halves of what this node owed its family.
func TestARevisionRevokesTheOldGoalsOwnAndFamilyChecks(t *testing.T) {
	graph := newTaskGraph()
	node := &TaskNode{graph: graph, id: 1, done: make(chan struct{}),
		spec:           taskSpec{title: "export the report", acceptance: "report.json exists"},
		Checks:         []string{"go test ./export -run TestJSON"},
		Family:         []string{"go test ./export/..."},
		FamilyDeclared: []string{"go test ./export/..."},
	}
	graph.nodes[1] = node
	ground := t.TempDir()

	// Before the person says anything, the contract stands and its checker holds
	// both the node's own check and the family's declared one.
	if door := auditDoorFor(node, ground); len(door.checks) != 2 {
		t.Fatalf("the admitted contract opens %q, want the node's own check and the family's", door.checks)
	}

	said := node.heardDirection("CSV instead of JSON", directionFromPerson, spokenSource{}).id
	version, err := node.reviseAssignment(said, 0, assignmentEdit{acceptance: "report.csv exists"})
	if err != nil {
		t.Fatalf("revise: %v", err)
	}
	if version != 1 {
		t.Fatalf("the assignment is at revision %d, want 1", version)
	}

	held := node.verification()
	if len(held.checks) != 0 || len(held.family) != 0 {
		t.Fatalf("the old goal's checks survived the correction: %+v", held)
	}
	if !held.current() || held.written != version {
		t.Fatalf("the verification was left stamped for another revision: %+v", held)
	}
	door := auditDoorFor(node, ground)
	if len(door.checks) != 0 {
		t.Fatalf("the checker still holds a check made about the old goal: %q", door.checks)
	}
	if !strings.Contains(door.line(), "DECLARED NO REPEATABLE CHECK") {
		t.Fatalf("the checker is not told the corrected goal has nothing to re-run:\n%s", door.line())
	}

	// AND WHAT THE WORKER WAS TOLD TO RUN FOR ITS FAMILY IS NOT A REQUIREMENT ANY
	// MORE EITHER. Leaving it standing would be an instruction from the old goal
	// contradicting the new one — and it is kept as history rather than dropped.
	if told := node.instruction(); strings.Contains(told, "go test ./export/...") {
		t.Fatalf("the corrected assignment still orders the old goal's family check:\n%s", told)
	}
	if was := node.familyChecksWas(); len(was) != 1 || was[0] != "go test ./export/..." {
		t.Fatalf("what the family owed before the correction was lost: %q", was)
	}
}

// AND A REVISION MAY PUT THE NEW GOAL'S OWN VERIFICATION UNDER CONTRACT in the
// same breath, which is the only way a corrected task is exercised rather than
// read. An acceptance-only correction that names none leaves it judged by
// reading, which is the honest answer when nobody has said how the new goal is
// checked.
func TestARevisionMayDeclareTheNewGoalsOwnChecks(t *testing.T) {
	graph := newTaskGraph()
	node := &TaskNode{graph: graph, id: 1, done: make(chan struct{}),
		spec:   taskSpec{title: "export the report", acceptance: "report.json exists"},
		Checks: []string{"go test ./export -run TestJSON"},
	}
	graph.nodes[1] = node

	said := node.heardDirection("CSV instead of JSON", directionFromPerson, spokenSource{}).id
	if _, err := node.reviseAssignment(said, 0, assignmentEdit{
		acceptance: "report.csv exists",
		checks:     []string{"go test ./export -run TestCSV"},
	}); err != nil {
		t.Fatalf("revise: %v", err)
	}
	held := node.verification()
	if len(held.checks) != 1 || held.checks[0] != "go test ./export -run TestCSV" {
		t.Fatalf("the corrected goal is checked by %q", held.checks)
	}
	if door := auditDoorFor(node, t.TempDir()); len(door.checks) != 1 {
		t.Fatalf("the corrected goal's own check does not open its door: %q", door.checks)
	}

	// A WORK-ONLY CORRECTION MOVES THE GOAL TOO, and it takes the checks with it:
	// what changed is how the work is to be done, and a command declared about the
	// old way is no more current than an acceptance about it.
	next := node.heardDirection("do it with the streaming writer", directionFromPerson, spokenSource{}).id
	if _, err := node.reviseAssignment(next, 1, assignmentEdit{work: "use the streaming writer"}); err != nil {
		t.Fatalf("revise: %v", err)
	}
	if held := node.verification(); len(held.checks) != 0 {
		t.Fatalf("a work-only correction kept %q", held.checks)
	}
}

// A REVISION SURVIVES THE CHECKPOINT AND A CONTINUATION, and a restored node is
// not re-armed by anything an older build left in its family list.
func TestARevisedContractSurvivesTheCheckpointAndAContinuation(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	node := landOne(agent, TaskFailed, "export the report", "it wrote JSON")
	node.graph.mu.Lock()
	node.Checks = []string{"go test ./export -run TestJSON"}
	node.Family = []string{"go test ./export/..."}
	node.FamilyDeclared = []string{"go test ./export/..."}
	node.state = TaskRunning
	node.graph.mu.Unlock()

	said := node.heardDirection("CSV instead of JSON", directionFromPerson, spokenSource{}).id
	if _, err := node.reviseAssignment(said, 0, assignmentEdit{
		acceptance: "report.csv exists",
		checks:     []string{"go test ./export -run TestCSV"},
	}); err != nil {
		t.Fatalf("revise: %v", err)
	}
	node.graph.mu.Lock()
	node.state = TaskFailed
	record := node.recordLocked()
	node.graph.mu.Unlock()

	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("writing the record: %v", err)
	}
	var read taskRecord
	if err := json.Unmarshal(encoded, &read); err != nil {
		t.Fatalf("reading the record: %v", err)
	}
	back := restoreNode(newTaskGraph(), read)
	held := back.verification()
	if len(held.checks) != 1 || held.checks[0] != "go test ./export -run TestCSV" {
		t.Fatalf("the restored node is checked by %q", held.checks)
	}
	if !held.current() {
		t.Fatalf("a restored node reads as checked for another revision: %+v", held)
	}
	if len(held.family) != 0 {
		t.Fatalf("a restored node was re-armed with the old goal's family checks: %q", held.family)
	}
	if was := back.familyChecksWas(); len(was) != 1 {
		t.Fatalf("the history of what the family owed did not survive: %q", was)
	}

	// AND A CONTINUATION IS NOT A REVISION: same assignment, same verification.
	if err := agent.ContinueTask(node.id, "try again"); err != nil {
		t.Fatalf("ContinueTask: %v", err)
	}
	if got := node.repeatableChecks(); len(got) != 1 || got[0] != "go test ./export -run TestCSV" {
		t.Fatalf("a continued task is checked by %q", got)
	}
}

// A CHECKER SAMPLING WHILE A CORRECTION LANDS SEES ONE GOAL OR THE OTHER, NEVER
// A MIXTURE — and work checked at one version is still not published as meeting
// the next.
func TestACheckerSnapshotNeverStraddlesARevision(t *testing.T) {
	graph := newTaskGraph()
	node := &TaskNode{graph: graph, id: 1, done: make(chan struct{}),
		spec:   taskSpec{title: "export the report"},
		Checks: []string{"go test ./export -run TestJSON"},
	}
	graph.nodes[1] = node
	ids := make([]uint64, 0, 8)
	for i := 0; i < 8; i++ {
		said := node.heardDirection("correction", directionFromPerson, spokenSource{}).id
		ids = append(ids, said)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i, said := range ids {
			if _, err := node.reviseAssignment(said, uint64(i), assignmentEdit{
				acceptance: "report.csv exists",
				checks:     []string{fmt.Sprintf("go test ./export -run Test%d", i)},
			}); err != nil {
				panic(err)
			}
		}
	}()
	for i := 0; i < 400; i++ {
		held := node.verification()
		if !held.current() {
			t.Fatalf("a reader caught the checks and the version apart: %+v", held)
		}
		if held.written > 0 {
			want := fmt.Sprintf("go test ./export -run Test%d", held.written-1)
			if len(held.checks) != 1 || held.checks[0] != want {
				t.Fatalf("revision %d was read holding %q, want %q", held.written, held.checks, want)
			}
		}
	}
	<-done

	// AND THE PUBLICATION BOUNDARY STILL GATES ON THE VERSION. Work checked at an
	// earlier revision does not land as meeting the latest one.
	node.checkAt(0)
	if claim := node.claimPublication(); claim.granted || !claim.stale {
		t.Fatalf("claim = %+v, want work checked at a superseded revision refused", claim)
	}
	node.checkAt(node.assignmentVersion())
	if claim := node.claimPublication(); !claim.granted {
		t.Fatalf("claim = %+v, want work checked at the revision in force to land", claim)
	}
}

// A PROPOSAL DECLARES ITS VERIFICATION AND THE NODE IS ADMITTED WITH IT, and a
// declaration nobody could run is refused where the model can still repair it.
func TestAProposalCarriesItsDeclaredChecksOntoTheNode(t *testing.T) {
	spec, problem := parseTaskArguments(json.RawMessage(`{"title":"port the parser",
		"summary":"move it across","brief":"port it","deliverable":"the parser","acceptance":"it is ported",
		"checks":["go test ./parser","go vet ./parser"]}`))
	if problem != "" {
		t.Fatalf("an ordinary proposal was refused: %s", problem)
	}
	if len(spec.checks) != 2 || spec.checks[0] != "go test ./parser" {
		t.Fatalf("the proposal's declared verification is %q", spec.checks)
	}

	// ADMITTED THROUGH THE ONE DOOR EVERY TASK COMES THROUGH, with a runner that
	// holds the node still: what is under test is what admission writes onto the
	// node, not what a worker would then do with it.
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	graph := agent.graph()
	held := make(chan struct{})
	t.Cleanup(func() { close(held) })
	graph.run = func(*TaskNode) { <-held }
	id := graph.reserve()
	graph.admit(id, spec)
	node := graph.node(id)
	if node == nil {
		t.Fatal("the proposal was not admitted")
	}
	if got := node.repeatableChecks(); len(got) != 2 {
		t.Fatalf("the admitted node is checked by %q", got)
	}
	if door := auditDoorFor(node, t.TempDir()); len(door.checks) != 2 {
		t.Fatalf("the node's own checker was handed %q", door.checks)
	}

	// AND A COMPOSED DECLARATION IS ANSWERED RATHER THAN DROPPED. A contract
	// quietly emptied of its verification is this whole field going missing
	// without anybody being told.
	_, problem = parseTaskArguments(json.RawMessage(`{"title":"port the parser",
		"summary":"move it across","brief":"port it","deliverable":"the parser","acceptance":"it is ported",
		"checks":["cd parser && go test ./..."]}`))
	if problem == "" {
		t.Fatal("a composed check was accepted, and the checker could never run it")
	}
	if !strings.Contains(problem, "checks") {
		t.Fatalf("the refusal does not name the field the model has to repair: %s", problem)
	}
}
