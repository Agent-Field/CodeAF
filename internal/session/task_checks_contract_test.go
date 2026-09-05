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
// A check is an assertion about a particular piece of work: run against a changed
// assignment it is either a question nobody asked or a pass nobody earned. So the
// default is to clear, and a revision that names its own verification replaces
// rather than adds.
func TestARevisionClearsTheChecksUnlessItDeclaresItsOwn(t *testing.T) {
	graph := newTaskGraph()
	node := &TaskNode{graph: graph, id: 1, done: make(chan struct{}),
		spec:           taskSpec{title: "port the parser"},
		Checks:         []string{"go test ./parser"},
		Family:         []string{"go test ./..."},
		FamilyDeclared: []string{"go test ./..."},
	}
	graph.nodes[1] = node

	node.reviseChecks(2, nil)
	if got := node.verification(); len(got.checks) != 0 || len(got.family) != 0 {
		t.Fatalf("a revision that named no verification left %+v standing", got)
	}
	if door := auditDoorForRevision(node, t.TempDir(), 2); len(door.checks) != 0 {
		t.Fatalf("the checker still holds a check made about the old goal: %q", door.checks)
	}
	// AND WHAT THE WORKER WAS TOLD IT OWNS FOR ITS FAMILY IS STILL THE RECORD OF
	// what came off parts that were handed out; it is work in ordinary hands and
	// not a permission to verify.
	if family := node.familyChecks(); len(family) != 1 {
		t.Fatalf("the record of what was taken off the parts is %q", family)
	}

	node.reviseChecks(3, []string{"go test ./lexer"})
	got := node.repeatableChecks()
	if len(got) != 1 || got[0] != "go test ./lexer" {
		t.Fatalf("the revision's own verification is %q", got)
	}
	if door := auditDoorForRevision(node, t.TempDir(), 3); len(door.checks) != 1 {
		t.Fatalf("the revision's own verification did not open the door: %q", door.checks)
	}
	// AND A CHECKER JUDGING A DIFFERENT REVISION GETS NOTHING TO RUN. Stale checks
	// are worse than none: the old goal's command passing under the new goal is a
	// verdict nobody earned.
	stale := auditDoorForRevision(node, t.TempDir(), 4)
	if len(stale.checks) != 0 {
		t.Fatalf("a check written for another revision opened a door: %q", stale.checks)
	}
	if !strings.Contains(stale.line(), "DECLARED NO REPEATABLE CHECK") {
		t.Fatalf("a checker holding stale checks is not told it has nothing to re-run:\n%s", stale.line())
	}
}

// THE STAMP AND THE CHECKS ARE NEVER READ APART.
//
// A checker decides what it may run by comparing the revision the verification
// was written for against the revision it is judging, so the two have to come out
// of ONE hold of the lock. A writer that bumped the version and then took the
// lock again to move the checks would leave a window holding the old goal's
// commands under the new goal's number — and a checker sampling that window would
// run them and could pass on them. This runs the two against each other and
// insists every reading is one moment's.
func TestTheVerificationAndItsRevisionAreOneReading(t *testing.T) {
	graph := newTaskGraph()
	node := &TaskNode{graph: graph, id: 1, done: make(chan struct{}), spec: taskSpec{title: "port it"}}
	graph.nodes[1] = node

	rounds := 200
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < rounds; i++ {
			node.graph.mu.Lock()
			node.reviseChecksLocked(1, []string{"go test ./one"})
			node.graph.mu.Unlock()
			node.graph.mu.Lock()
			node.reviseChecksLocked(2, []string{"go test ./two"})
			node.graph.mu.Unlock()
		}
	}()
	for i := 0; i < rounds; i++ {
		held := node.verification()
		switch held.revision {
		case 0:
			// The node before the writer reached it.
		case 1:
			if len(held.checks) != 1 || held.checks[0] != "go test ./one" {
				t.Fatalf("revision 1 was read holding %q", held.checks)
			}
		case 2:
			if len(held.checks) != 1 || held.checks[0] != "go test ./two" {
				t.Fatalf("revision 2 was read holding %q", held.checks)
			}
		default:
			t.Fatalf("a revision nobody wrote: %d", held.revision)
		}
	}
	<-done
}

// AND A CONTINUATION IS NOT A REVISION. Continuing a task keeps its assignment —
// the brief and the done-condition it was admitted with — so the verification it
// is checked against is the same one, and a second attempt that lost it would be
// checked more weakly than the first for no reason anybody chose.
func TestAContinuedTaskKeepsTheChecksItWasAdmittedWith(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	node := landOne(agent, TaskFailed, "port the parser", "it did not build")
	node.graph.mu.Lock()
	node.Checks = []string{"go test ./parser"}
	node.graph.mu.Unlock()

	if err := agent.ContinueTask(node.id, "try again"); err != nil {
		t.Fatalf("ContinueTask: %v", err)
	}
	if got := node.repeatableChecks(); len(got) != 1 || got[0] != "go test ./parser" {
		t.Fatalf("a continued task is checked by %q", got)
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
