package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Neither a composed acceptance nor a pasted request grants execution rights.
func TestSessionAcceptanceDoesNotDeclareExecutableChecks(t *testing.T) {
	for _, acceptance := range []string{"every fixture passes `go test ./parser`", "run `./deploy.sh` once", "finish:\n$ sh ./deploy.sh", "Original request: " + "run `./deploy.sh`"} {
		agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.Unattended = true; c.Budget = Budget{Wall: time.Hour} })
		agent.steward().setAcceptance(acceptance)
		if got := agent.sessionChecks(); len(got) != 0 {
			t.Fatalf("prose granted execution: %q => %v", acceptance, got)
		}
	}
}

// NOTHING HARVESTED FROM THE ASK IS EXECUTED AGAINST THE TREE. A pasted
// reproduction says how the person saw the bug, while the acceptance is the
// work's own promise about what proves it.
func TestAStepOutOfThePastedReproductionIsNeverASessionCheck(t *testing.T) {
	tree := t.TempDir()
	if err := os.WriteFile(filepath.Join(tree, "tox.ini"), []byte("[tox]\n"), 0o644); err != nil {
		t.Fatalf("writing tox.ini: %v", err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = tree
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	steward := agent.steward()
	steward.hear("the pasted reproduction ends with:\n$ chmod 000 tox.ini")
	steward.setAcceptance("the tox configuration remains readable")

	if checks := agent.sessionChecks(); containsWord(checks, "chmod 000 tox.ini") {
		t.Fatalf("a step out of the pasted reproduction became a session check: %v", checks)
	}

	// THE ACCEPTANCE IS IMMUTABLE FOR THE SESSION, so the other half of the law
	// is read off a second session whose acceptance names the same step.
	promised, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = tree
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	promised.steward().hear("the pasted reproduction ends with:\n$ chmod 000 tox.ini")
	promised.steward().setAcceptance("the fix passes:\n$ chmod 000 tox.ini")
	if checks := promised.sessionChecks(); len(checks) != 0 {
		t.Fatalf("the same step declared by the acceptance is not a session check: %v", checks)
	}
}

// A DONE-CONDITION THAT IS THE PERSON'S PASTED ASK CARRIES NOTHING AT ALL INTO
// THE SESSION'S CHECKS — NOT A PROMPT STEP, AND NOT A BACKTICKED COMMAND.
//
// When no one could write a done-condition, the person's words arrive behind
// the original ask, and a request is not a contract. The measured tox
// reproduction must remain a story about seeing the bug rather than become
// `chmod 000 tox.ini` run against the deliverable tree — and the same is true of
// a command the request names, because the commands in a request are the WORK:
// "run `./slow-build.sh` and report the marker" says to do it once, and a session
// harvesting it would do it at baseline and again after delivery.
func TestASessionWhoseDoneWhenIsThePastedAskRunsNoStepOutOfIt(t *testing.T) {
	tree := t.TempDir()
	if err := os.WriteFile(filepath.Join(tree, "tox.ini"), []byte("[tox]\n"), 0o644); err != nil {
		t.Fatalf("writing tox.ini: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tree, "run_tests.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing run_tests.sh: %v", err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = tree
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	agent.steward().setAcceptance("Original request: " +
		"check it with `run_tests.sh`\nthe reproduction ends with:\n$ chmod 000 tox.ini")

	checks := agent.sessionChecks()
	backticked := checkCommand(tree, "run_tests.sh")
	if backticked == "" {
		t.Fatal("the tree's run_tests.sh is not invocable, so the source boundary cannot be exercised")
	}
	if len(checks) != 0 {
		t.Fatalf("the person's own request became a list of commands the harness runs: %v", checks)
	}
}

// A SETTLED UNIT'S DOOR CARRIES NO PROMPT STEP OUT OF THE PERSON'S PASTED ASK
// INTO THE SESSION'S CHECKS.
//
// The node door is the only path from a landing into the terminal check list;
// this pins the full path that would otherwise run the measured tox
// reproduction against the deliverable tree.
func TestASettledNodeNeverContributesAStepOutOfThePastedAsk(t *testing.T) {
	tree := t.TempDir()
	if err := os.WriteFile(filepath.Join(tree, "tox.ini"), []byte("[tox]\n"), 0o644); err != nil {
		t.Fatalf("writing tox.ini: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tree, "run_tests.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing run_tests.sh: %v", err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = tree
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	agent.steward().setAcceptance("the tox configuration remains readable")
	node := landOne(agent, TaskDone, "repair tox", "done")
	node.graph.mu.Lock()
	node.spec.request = "the reproduction ends with:\n$ chmod 000 tox.ini"
	node.spec.brief = "repair the tox configuration"
	node.brief = node.spec.brief
	node.spec.acceptance = "the tox configuration remains readable"
	// THE NODE'S OWN CHECK IS THE ONE IT DECLARED, not one read out of its prose:
	// a session harvests its units of work through the same door their own
	// checkers use (task_checks.go).
	node.Checks = []string{"run_tests.sh"}
	node.graph.mu.Unlock()

	checks := agent.sessionChecks()
	if containsWord(checks, "chmod 000 tox.ini") {
		t.Fatalf("a settled node carried a pasted reproduction step into the session checks: %v", checks)
	}
	if want := checkCommand(tree, "run_tests.sh"); want == "" || !containsWord(checks, want) {
		t.Fatalf("the settled node's own check did not reach the session checks: want %q in %v", want, checks)
	}
}

// A CHECK THAT PASSES AND A CHECK THAT DOES NOT ARE DIFFERENT NEWS, and a check
// that could not be started at all is the second of the two — a session must
// never declare itself finished because its build command was misspelled.
func TestAChecksVerdictIsWhatTheProcessSaid(t *testing.T) {
	tree := t.TempDir()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.Workspace = tree })
	ran := agent.runSessionChecks(context.Background(), []string{
		"true",
		"false",
		"this-command-does-not-exist-anywhere",
		"pwd",
	})
	if len(ran) != 4 {
		t.Fatalf("not every check was run: %+v", ran)
	}
	if !ran[0].Passed || ran[1].Passed || ran[2].Passed {
		t.Fatalf("the verdicts are not what the processes said: %+v", ran)
	}
	// AND IT RAN IN THE DELIVERABLE TREE, not wherever the test process was
	// standing, which is the whole of what "from clean" has to mean about place.
	if !strings.Contains(ran[3].Tail, filepath.Base(tree)) {
		t.Fatalf("the check did not run in the deliverable tree: %q", ran[3].Tail)
	}
}

// AND A FAILED CHECK IS READ FROM ITS END, because what a check concluded is in
// its last lines and a reading kept from the head shows only that it started.
func TestAFailedChecksOutputIsKeptFromItsEnd(t *testing.T) {
	long := strings.Repeat("a line of build noise nobody needs\n", 200)
	tail := checkTail(long + "FAILED: undefined parseHeader")
	if !strings.HasSuffix(tail, "FAILED: undefined parseHeader") {
		t.Fatalf("the conclusion was cut off:\n%s", tail)
	}
	if len(tail) > sessionCheckTail+8 {
		t.Fatalf("the tail is unbounded: %d bytes", len(tail))
	}
}

// THE SECOND READING IS TAKEN ONLY WHERE IT CAN CHANGE ANYTHING.
//
// A [Person] holds no acceptance, so a session of theirs never runs a check, never
// sweeps a directory and never pays for either — which is the emptiness law
// reaching the one road in this feature that costs real time.
func TestAPersonsStoppedTurnTakesNoSecondReading(t *testing.T) {
	tree := t.TempDir()
	marker := filepath.Join(tree, "the-check-ran")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.Workspace = tree })
	agent.hearAsk("write the parser; check it with `touch " + marker + "`")

	if got := agent.decideRemains(context.Background(), "all done"); got.Verb != DecideDone {
		t.Fatalf("a person's stopped turn did not end: %+v", got)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a person's session ran a check nobody asked it to run")
	}
}

// TestAWatchedCompletionRunsNoUndeclaredChecks preserves the declared-check boundary without a completion reader.
func TestAWatchedCompletionRunsNoUndeclaredChecks(t *testing.T) {
	tree := t.TempDir()
	marker := filepath.Join(tree, "the-check-ran")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.Workspace = tree })
	agent.hearAsk("write the parser; check it with `touch " + marker + "`")

	got := agent.decideRemains(context.Background(), "all done")
	if got.Verb != DecideDone {
		t.Fatalf("a watched session's ordinary completion changed: %+v", got)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a watched session ran an undeclared check")
	}
}

// Inline work reaches the same declared-check boundary as delegated work.
// A command pasted into prose remains prose, not authority to run it again.
func TestInlineWorkIsAssessedWithoutInventingARepeatableCheck(t *testing.T) {
	tree := t.TempDir()
	made := filepath.Join(tree, "test_auth.py")
	if err := os.WriteFile(made, []byte("def test_scheme():\n    assert True\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(tree, "the-check-ran")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = tree
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	steward := agent.steward()
	steward.hear("fix the bearer scheme; check it with `touch " + marker + "`")
	steward.setAcceptance("the scheme is case-insensitive and `touch " + marker + "` passes")
	// The session's own hands: one file, made by this session, under the tree.
	agent.rememberCreated(fileChange{path: made, shown: "test_auth.py", created: true})

	got := agent.decideRemains(context.Background(), "The scheme parsing is fixed and the tests pass.")

	if got.Verb != DecideDone {
		t.Fatalf("a session that wrote the whole fix itself was carried on: %+v", got)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("prose caused the action to run again: %v", err)
	}
}

// TestDeclaredChecksRunAtCompletedInlineTurns preserves the declared-check boundary without a completion reader.
func TestDeclaredChecksRunAtCompletedInlineTurns(t *testing.T) {
	tree := t.TempDir()
	made := filepath.Join(tree, "test_auth.py")
	if err := os.WriteFile(made, []byte("def test_scheme():\n    assert True\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(tree, "the-check-ran")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = tree
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	steward := agent.steward()
	steward.hear("fix the bearer scheme; check it with `touch " + marker + "`")
	steward.setAcceptanceContract(steward.Ask(), "the scheme is case-insensitive and `touch "+marker+"` passes", []string{"touch " + marker})
	agent.rememberCreated(fileChange{path: made, shown: "test_auth.py", created: true})

	got := agent.decideRemains(context.Background(), "The scheme parsing is fixed and the tests pass.")

	if got.Verb != DecideDone {
		t.Fatalf("green checked inline work was carried on after the model finished: %+v", got)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("the declared check never ran over the tree: %v", err)
	}
}

// TestACompletedTurnNamesItsActualFailedCheck preserves the declared-check boundary without a completion reader.
func TestACompletedTurnNamesItsActualFailedCheck(t *testing.T) {
	tree := t.TempDir()
	made := filepath.Join(tree, "parser.go")
	if err := os.WriteFile(made, []byte("package parser\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = tree
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	steward := agent.steward()
	steward.hear("port the parser; check it with `false`")
	steward.setAcceptanceContract(steward.Ask(), "the parser builds and `false` passes", []string{"false"})
	recordCoveredBaseline(agent, "false")
	agent.rememberCreated(fileChange{path: made, shown: "parser.go", created: true})

	got := agent.decideRemains(context.Background(), "That completes the port.")
	if got.Verb != DecideCarryOn {
		t.Fatalf("a red declared check did not carry on: %+v", got)
	}
	if !strings.Contains(got.Brief, "false does not pass") {
		t.Fatalf("the brief does not name the red command:\n%s", got.Brief)
	}
	if strings.Contains(got.Brief, "nothing has been finished yet") {
		t.Fatalf("the brief says no work finished instead of naming the red check:\n%s", got.Brief)
	}
}

// An ordinary ending is done on the first reading and cannot latch standstill
// merely because the model made no explicit file-tool calls.
func TestAnEndingAsksTheGoalOwnerOnce(t *testing.T) {
	tree := t.TempDir()
	made := filepath.Join(tree, "parser.go")
	if err := os.WriteFile(made, []byte("package parser\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = tree
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	steward := agent.steward()
	steward.hear("port the parser")
	steward.setAcceptance("the parser accepts every fixture")
	agent.rememberCreated(fileChange{path: made, shown: "parser.go", created: true})

	got := agent.decideRemains(context.Background(), "That completes the port.")
	if got.Verb != DecideDone {
		t.Fatalf("ordinary completion was reopened: %+v", got)
	}
	steward.mu.Lock()
	stopped := steward.stopped
	steward.mu.Unlock()
	if stopped != "" {
		t.Fatalf("one ending latched the standstill floor: %q", stopped)
	}
}

// A declared failing check remains a reason to continue after a completed turn.
func TestAGoalOwnersMetAskIsCheckedAgainstTheTree(t *testing.T) {
	tree := t.TempDir()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = tree
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	steward := agent.steward()
	steward.hear("port the parser; check it with `false`")
	steward.setAcceptance("the parser builds and `false` passes")
	// A landing, so the session is not carried on merely for having finished
	// nothing — the assertion below is about the CHECK. It is placed in the
	// graph rather than admitted, because admitting one starts a worker and what
	// is under test here is the reading taken after one has already landed.
	node := landOne(agent, TaskDone, "port the parser", "")
	node.Checks = []string{"false"}
	recordCoveredBaseline(agent, "false")

	got := agent.decideRemains(context.Background(), "That completes the port.")
	if got.Verb != DecideCarryOn {
		t.Fatalf("a tree that fails its own check was allowed to finish: %+v", got)
	}
	if !strings.Contains(got.Brief, "false does not pass") {
		t.Fatalf("the brief does not name the check:\n%s", got.Brief)
	}
}

// A CHECK DECLARED BY A LATE TASK HAS NO BEFORE-READING. The terminal door may
// run it and show its red result, but cannot turn that result into fresh work.
func TestALateTaskCheckIsUnreadAtTheTerminalDoor(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = t.TempDir()
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	steward := agent.steward()
	steward.hear("write a short account of the city council vote")
	steward.setAcceptance("the account states the result")
	agent.openBaseline(context.Background())

	node := landOne(agent, TaskDone, "write the account", "the account states the result")
	node.Checks = []string{"false"}
	got := agent.decideRemains(context.Background(), "The account is complete.")
	if got.Verb != DecideDone {
		t.Fatalf("an uncovered late check became a new requirement: %+v", got)
	}
	if !strings.Contains(got.Brief, "no usable before-reading") || !strings.Contains(got.Brief, "false") {
		t.Fatalf("the done path hid the unknown red check:\n%s", got.Brief)
	}
	if strings.Contains(got.Brief, "false does not pass") {
		t.Fatalf("the unknown red check was called a regression:\n%s", got.Brief)
	}
}

func TestADeclaredGreenBaselineThatTurnsRedStillCarriesOn(t *testing.T) {
	tree := t.TempDir()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = tree
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	steward := agent.steward()
	steward.hear("write a short account and keep the workspace healthy")
	const check = "test ! -f regression.flag"
	steward.setAcceptanceContract(steward.Ask(), "the account is written and the workspace remains healthy", []string{check})
	agent.openBaseline(context.Background())
	agent.awaitBaseline(context.Background())
	if err := os.WriteFile(filepath.Join(tree, "regression.flag"), []byte("red\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	landOne(agent, TaskDone, "write the account", "the account is complete")

	got := agent.decideRemains(context.Background(), "The account is complete.")
	if got.Verb != DecideCarryOn || !strings.Contains(got.Brief, check+" does not pass") {
		t.Fatalf("a check read green before and red after was not preserved as a regression: %+v", got)
	}
	if strings.Contains(got.Brief, "no usable before-reading") {
		t.Fatalf("a covered check was described as unknown:\n%s", got.Brief)
	}
}

// recordCoveredBaseline states the full before-reading fact needed by tests
// whose terminal command is deliberately red. An empty old-red list alone no
// longer implies that an undeclared command was read green.
func recordCoveredBaseline(agent *Agent, commands ...string) {
	agent.mu.Lock()
	agent.baselineTaken = true
	agent.baselineRead = true
	agent.baselineDeclared = append([]string(nil), commands...)
	agent.mu.Unlock()
}

// landOne puts one settled node into a session's graph, the way a landing would
// have left it. It is the fixture every reading in this file is taken against,
// and it is a placement rather than an admission because admitting a node
// starts a worker on it.
func landOne(agent *Agent, state TaskState, title, report string) *TaskNode {
	graph := agent.graph()
	graph.mu.Lock()
	defer graph.mu.Unlock()
	id := uint64(len(graph.order) + 1)
	node := &TaskNode{graph: graph, id: id, spec: taskSpec{title: title}, state: state, report: report}
	graph.nodes[id] = node
	graph.order = append(graph.order, id)
	return node
}

// THE LANDINGS A GOAL OWNER IS SHOWN ARE THIS SESSION'S SETTLED WORK, and a
// node still running makes the session "nothing finished" rather than nothing
// at all — a session with work in flight has not finished the ask.
func TestTheLandingsShownAreThisSessionsSettledWork(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	landOne(agent, TaskDone, "port the parser", "done")
	landOne(agent, TaskFailed, "wire the handlers", "incomplete — no route for PATCH")

	landings, landed, _ := agent.landings()
	if !landed || len(landings) != 2 {
		t.Fatalf("the settled work is not what was read: landed=%v %+v", landed, landings)
	}
	if landings[1].Signature != "incomplete — no route for PATCH" {
		t.Fatalf("a failure came back unsigned: %+v", landings[1])
	}
	if landings[0].Signature != "" {
		t.Fatalf("finished work was given a failure signature: %+v", landings[0])
	}
}

// AND A SESSION WITH NOTHING SETTLED HAS FINISHED NOTHING, whatever it is
// running.
//
// The node is really admitted and really started — the fixture below holds a
// stubbed runner inside its own work — because what is under test is a session
// with work IN FLIGHT, and a node placed in the graph with nobody behind it is
// not that.
func TestASessionWithOnlyRunningWorkHasFinishedNothing(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	holdOneRunning(t, agent, "write the tests")
	if _, landed, _ := agent.landings(); landed {
		t.Fatal("work still running was counted as finished")
	}
}

// WORK THAT IS MOVING IS ANSWERED APART FROM WORK THAT LANDED.
//
// A graph holding one finished unit and one still going used to read exactly like
// a graph holding one finished unit — the unsettled node was dropped on the way
// out and nothing downstream could tell (#468).
func TestWorkThatIsMovingIsAnsweredApartFromWhatLanded(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	// THE ADMITTED NODE GOES IN FIRST. A real admission takes the graph's next
	// reserved id and [landOne] places at the end of the order it can see, so
	// the other way round the placement would land on top of the running node.
	holdOneRunning(t, agent, "write the tests")
	landOne(agent, TaskDone, "port the parser", "done")

	landings, landed, flight := agent.landings()
	if !landed || len(landings) != 1 {
		t.Fatalf("the settled work is not what was read: landed=%v %+v", landed, landings)
	}
	if len(flight.moving) != 1 || flight.moving[0] != "write the tests" {
		t.Fatalf("the work still going was not carried out of the graph: %+v", flight)
	}
	if len(flight.stuck) != 0 {
		t.Fatalf("work that is moving was called stuck: %+v", flight)
	}
}

// holdOneRunning admits one node and HOLDS IT RUNNING for the length of the test.
//
// It is a real admission through the graph's own frontier, with a stubbed runner
// standing in for the worker (task_test.go's [stubbedGraph]), so the node is in
// exactly the state a started unit of work is in: TaskRunning, with somebody
// inside its run. The hold is released before the session is closed, which the
// cleanup order gives us for nothing: cleanups run last-registered-first, and the
// session's own close was registered when it was built.
func holdOneRunning(t *testing.T, agent *Agent, title string) uint64 {
	t.Helper()
	started := make(chan uint64, 1)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	graph := stubbedGraph(agent, func(node *TaskNode) {
		started <- node.id
		<-release
	})
	id := graph.reserve()
	graph.admit(id, taskSpec{title: title, brief: "b", acceptance: "a"})
	return waitStarted(t, started)
}

// THE ONE FACT THAT CANNOT BE RE-DERIVED SURVIVES A RESUME.
//
// Whether a file was there before the session touched it is a MEASUREMENT, and
// the only moment it can be taken is the instant before the call that writes it
// (recovery.go). A resumed session that lost it would end its evening looking at
// everything it made and being unable to say what it had made — so the ledger is
// journaled a line at a time and read back on the way in.
func TestWhatTheSessionMadeSurvivesAResume(t *testing.T) {
	dir := t.TempDir()
	transcript := filepath.Join(dir, "transcript.jsonl")
	made := filepath.Join(dir, "parser.go")

	first, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = dir
		c.SessionFile = transcript
	})
	first.rememberCreated(fileChange{path: made, shown: "parser.go", created: true})
	if err := first.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}

	second, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = dir
		c.SessionFile = transcript
	})
	list := second.createdList()
	if len(list) != 1 || list[0].path != made || !list[0].created {
		t.Fatalf("the resumed session does not know what it made: %+v", list)
	}
}

// A GOAL OWNER'S DECISIONS ARE WRITTEN DOWN AND A PERSON'S ARE NOT.
//
// The first half is the autopsy: a run that carried on and a run that stopped
// read identically in the journal before this line existed. The second is
// [Person]'s emptiness law reaching the file — a new line in somebody's own
// transcript is something they can tell.
func TestOnlyAGoalOwnerWritesItsDecisionsDown(t *testing.T) {
	stewardLines := principalLines(t, func(c *Config) {
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	if !strings.Contains(stewardLines, `"event":"decided"`) {
		t.Fatalf("a goal owner's decision reached no line of the journal:\n%s", stewardLines)
	}
	if !strings.Contains(stewardLines, `"who":"steward"`) {
		t.Fatalf("the journal does not say whose decision it was:\n%s", stewardLines)
	}
	if personLines := principalLines(t, nil); strings.Contains(personLines, `"type":"principal"`) {
		t.Fatalf("a person's own transcript grew a line about machinery:\n%s", personLines)
	}
}

// principalLines drives one stopped turn's decision through a real journal and
// hands back what the file holds.
func principalLines(t *testing.T, mutate func(*Config)) string {
	t.Helper()
	dir := t.TempDir()
	transcript := filepath.Join(dir, "transcript.jsonl")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = dir
		c.SessionFile = transcript
		if mutate != nil {
			mutate(c)
		}
	})
	agent.hearAsk("port the parser")
	agent.decideRemains(context.Background(), "I have made a start.")
	if err := agent.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}
	content, err := os.ReadFile(transcript)
	if err != nil {
		t.Fatalf("reading the journal: %v", err)
	}
	return string(content)
}

// AND A RUN THAT IS CARRYING ON DOES NOT, which is the failure the split
// exists to avoid: a session about to read its own working material would
// otherwise have this feature delete it halfway through.
func TestARunThatIsCarryingOnKeepsItsOwnWorkingMaterial(t *testing.T) {
	tree := t.TempDir()
	elsewhere := t.TempDir()
	working := filepath.Join(elsewhere, "half-done.jsonl")
	if err := os.WriteFile(working, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = tree
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	landOne(agent, TaskRunning, "finish the analysis", "")
	agent.rememberCreated(fileChange{path: working, shown: working, created: true})

	got := agent.decideRemains(context.Background(), "I have made a start.")
	if got.Verb != DecideCarryOn {
		t.Fatalf("the run did not carry on: %+v", got)
	}
	if _, err := os.Stat(working); err != nil {
		t.Fatalf("a run that is still working lost its own material: %v", err)
	}
}

// AND THE CHECKS A UNIT OF WORK DECLARED ARE THE SESSION'S CHECKS TOO.
//
// A task put under contract to be verified by one command has said what the
// check is, and reading it through the same door that node's own checker used
// ([auditDoorFor]) is what keeps a session and its units of work checking the
// same things. What the worker merely RAN is not read here for the reason it is
// not read there: a receipt is evidence, not a licence to repeat the work
// (task_checks.go).
func TestTheSessionAlsoChecksWhatItsWorkersDeclared(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	agent.steward().hear("port the parser")
	node := landOne(agent, TaskDone, "port the parser", "done")
	node.graph.mu.Lock()
	node.spec.brief = "port it"
	node.brief = "port it"
	node.spec.acceptance = "it is ported"
	node.Checks = []string{"go test ./parser"}
	node.receipts = []toolReceipt{{tool: "bash", args: `{"command":"go run ./cmd/port"}`}}
	node.graph.mu.Unlock()

	var found bool
	for _, check := range agent.sessionChecks() {
		if check == "go test ./parser" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the session does not check what its own unit of work declared: %v", agent.sessionChecks())
	}
	for _, check := range agent.sessionChecks() {
		if strings.Contains(check, "cmd/port") {
			t.Fatalf("the session picked up a command a worker merely ran: %v", agent.sessionChecks())
		}
	}
}

// THE SESSION DOES NOT REDO WHAT A DELIVERED TASK ALREADY DID.
//
// The per-task checker stopped treating a receipt as a door, but the session runs
// its own checks over the delivered tree when the ask is finished — so a command
// the composed acceptance happens to name, which a task has just carried out, was
// still going to be carried out a second time here, by the harness, after
// delivery. Only the explicit repeatable contract contributes executable checks.
func TestTheSessionDoesNotRerunAnActionADeliveredTaskAlreadyPerformed(t *testing.T) {
	tree := t.TempDir()
	writeCheckFile(t, tree, "deploy.sh", "#!/bin/sh\nexit 0\n", 0o755)
	writeCheckFile(t, tree, "verify.sh", "#!/bin/sh\nexit 0\n", 0o755)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = tree
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	agent.steward().setAcceptance("the release is out: `./deploy.sh` has run and `./verify.sh` passes")
	node := landOne(agent, TaskDone, "ship the release", "deployed")
	node.graph.mu.Lock()
	// What the task DID, and what it was put under contract to be checked by.
	node.receipts = []toolReceipt{{tool: "bash", args: `{"command":"sh ./deploy.sh"}`}}
	node.Checks = []string{"./verify.sh"}
	node.graph.mu.Unlock()

	checks := agent.sessionChecks()
	for _, check := range checks {
		if strings.Contains(check, "deploy.sh") {
			t.Fatalf("the session would deploy a second time after delivery: %v", checks)
		}
	}
	// AND THE DECLARED CHECK IS STILL RUN. Suppressing the re-run must not
	// suppress verification: a test the work declared stays runnable.
	var kept bool
	for _, check := range checks {
		if strings.Contains(check, "verify.sh") {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("the task's declared verification was dropped with the action: %v", checks)
	}
}

// A STASH HOLDS WORK THAT IS NOT IN THE TREE, AND THE READING SAYS SO — BUT
// ONLY THE ENTRIES THIS RUN PUT THERE.
//
// The stash is the one account of the session's own work that nothing else in
// the building can take: the checks, the reconciliation and the session's ledger
// all read the tree as it stands, and a tree with the fix stashed out of it
// looks exactly like a tree the fix was never written into. What it may not do
// is name somebody's older stash, which would tell every run over that
// repository that work was left undone at every ending, for ever.
func TestAStashedFixIsNotDone(t *testing.T) {
	tree := t.TempDir()
	revertRepo(t, tree)
	project := filepath.Join(tree, "_make.py")
	if err := os.WriteFile(project, []byte("def make(): ...\n"), 0o644); err != nil {
		t.Fatalf("writing the project's file: %v", err)
	}
	revertCommit(t, tree, "the project as it was")

	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = tree
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	agent.steward().setAcceptance("the reproduction runs and the suite passes")

	// THE PERSON'S OWN STASH, TAKEN BEFORE THE RUN BEGAN. Nothing this session
	// does may ever name it.
	if err := os.WriteFile(project, []byte("half a thought, from last week\n"), 0o644); err != nil {
		t.Fatalf("writing the person's own work: %v", err)
	}
	if out, err := git(tree, "stash"); err != nil {
		t.Fatalf("the person's git stash: %v (%s)", err, out)
	}
	// AND WITH NO BEFORE-READING NOTHING IS COUNTED, because nobody yet knows
	// which entries appeared since.
	if got := agent.stashedWork(); got != 0 {
		t.Fatalf("a session that has taken no before-reading counted %d stash entries", got)
	}
	agent.openBaseline(context.Background())
	if got := agent.stashedWork(); got != 0 {
		t.Fatalf("a stash the person took before the run counted as %d entries of this run's work", got)
	}

	// The session leaves a file of its own behind, so that what it MADE is not
	// what is in question here — and then edits the project's file and stashes
	// the edit to compare against the baseline, exactly as the attrs cell did.
	note := filepath.Join(tree, "NOTES.md")
	if err := os.WriteFile(note, []byte("what I found\n"), 0o644); err != nil {
		t.Fatalf("writing the session's own file: %v", err)
	}
	agent.rememberChange(fileChange{path: note, shown: "NOTES.md", created: true})
	if err := os.WriteFile(project, []byte("def make(): return 1\n"), 0o644); err != nil {
		t.Fatalf("editing the project's file: %v", err)
	}
	if out, err := git(tree, "stash"); err != nil {
		t.Fatalf("git stash: %v (%s)", err, out)
	}

	if got := agent.stashedWork(); got != 1 {
		t.Fatalf("the reading found %d stash entries of this run's own, want 1", got)
	}
	remains := agent.remainsFor("fixed")
	if got := agent.who().Decide(remains); got.Verb != DecideDone {
		t.Fatalf("the first reading, which has not looked at the tree, answered %+v", got)
	}

	decision := agent.decideOverTheChecks(context.Background(), remains)
	if decision.Verb != DecideCarryOn {
		t.Fatalf("a done was decided over a stashed fix: %+v", decision)
	}
	const said = "1 stash entry holds work that is not in the tree"
	if !containsWord(decision.Observed, said) {
		t.Fatalf("the reading never said what it found: %v", decision.Observed)
	}
	if !strings.Contains(decision.Brief, said) {
		t.Fatalf("the brief the next turn opens on never mentions the stash:\n%s", decision.Brief)
	}

	// AND THE PLURAL IS THE PLURAL. A second stash of this run's own is a second
	// entry, said as entries rather than as one of them — and the person's is
	// still not among them.
	if err := os.WriteFile(project, []byte("def make(): return 2\n"), 0o644); err != nil {
		t.Fatalf("editing the project's file again: %v", err)
	}
	if out, err := git(tree, "stash"); err != nil {
		t.Fatalf("git stash: %v (%s)", err, out)
	}
	if got := agent.stashedWork(); got != 2 {
		t.Fatalf("the reading found %d stash entries of this run's own, want 2", got)
	}
	remains.Stashed = 2
	if !containsWord(remains.unmet(), "2 stash entries hold work that is not in the tree") {
		t.Fatalf("two stashes were not said as two: %v", remains.unmet())
	}
}

// AND THE GROUND LADDER'S OWN STASH IS NOT WORK LEFT LYING ABOUT.
//
// A landing that has to merge into a ground holding uncommitted work sets that
// work aside with `git stash push` and puts it back on every road out
// ([taskTree.carryGroundWork]). The window is short and every road pops, but a
// terminal reading taken inside it would see the harness's own entry and tell a
// finished run to carry on. The message the push was given is what tells them
// apart.
func TestTheGroundLaddersOwnStashIsNotWorkLeftLyingAbout(t *testing.T) {
	tree := t.TempDir()
	revertRepo(t, tree)
	project := filepath.Join(tree, "_make.py")
	if err := os.WriteFile(project, []byte("def make(): ...\n"), 0o644); err != nil {
		t.Fatalf("writing the project's file: %v", err)
	}
	revertCommit(t, tree, "the project as it was")

	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = tree
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	agent.steward().setAcceptance("the suite passes")
	agent.openBaseline(context.Background())

	if err := os.WriteFile(project, []byte("the person's own uncommitted work\n"), 0o644); err != nil {
		t.Fatalf("writing the ground's uncommitted work: %v", err)
	}
	if out, err := git(tree, "stash", "push", "-m", groundStashMessage("task/fix-a-1")); err != nil {
		t.Fatalf("the ground ladder's git stash push: %v (%s)", err, out)
	}
	if got := agent.stashedWork(); got != 0 {
		t.Fatalf("a landing's own set-aside work counted as %d entries of unfinished work", got)
	}

	// AND AN ENTRY THE SESSION PUSHED ITSELF IS STILL COUNTED, so the exception
	// is the harness's own message and not the stash as a whole.
	if err := os.WriteFile(project, []byte("def make(): return 1\n"), 0o644); err != nil {
		t.Fatalf("editing the project's file: %v", err)
	}
	if out, err := git(tree, "stash"); err != nil {
		t.Fatalf("git stash: %v (%s)", err, out)
	}
	if got := agent.stashedWork(); got != 1 {
		t.Fatalf("the reading found %d stash entries of this run's own, want 1", got)
	}

	// AND A SUBJECT THAT MERELY MENTIONS THE SENTENCE IS STILL COUNTED. Somebody
	// writing about what aforge did is describing their own work, and swallowing
	// it would be this reading going quiet about the entry it exists to name.
	if out, err := git(tree, "stash", "pop"); err != nil {
		t.Fatalf("git stash pop: %v (%s)", err, out)
	}
	if out, err := git(tree, "stash", "push", "-m",
		"before I ask "+groundStashMessage("the parser")); err != nil {
		t.Fatalf("the person's own git stash push: %v (%s)", err, out)
	}
	if got := agent.stashedWork(); got != 1 {
		t.Fatalf("a person's stash that quotes the ladder's sentence counted as %d, want 1", got)
	}
	for _, subject := range []string{
		"On main: " + groundStashMessage("task/fix-a-1"),
		"On (no branch): " + groundStashMessage("task/fix-a-1"),
	} {
		if !isGroundStash(subject) {
			t.Fatalf("the ladder's own entry was not recognised: %q", subject)
		}
	}
	for _, subject := range []string{
		"WIP on main: 6f67812 " + groundStashMessage("task/fix-a-1"),
		"On main: before I ask " + groundStashMessage("the parser"),
		"On main: fixing the parser",
	} {
		if isGroundStash(subject) {
			t.Fatalf("a person's own entry was taken for the ladder's: %q", subject)
		}
	}
}

// A STASH READING THAT COULD NOT BE TAKEN IS NOT AN EMPTY STASH.
//
// The two answer the same empty list and mean opposite things. Collapsed into
// one value, a before-reading over a directory that was not a repository yet, or
// on a machine with no git, marked the baseline as taken — and every entry the
// run found afterwards counted as its own doing.
func TestAStashReadingThatCouldNotBeTakenIsNotAnEmptyStash(t *testing.T) {
	tree := t.TempDir()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = tree
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	agent.steward().setAcceptance("the suite passes")

	// The before-reading is taken over a directory that is not a repository, so
	// there was nothing to read and the flag stays down.
	agent.openBaseline(context.Background())
	agent.mu.Lock()
	read := agent.stashBeforeRead
	agent.mu.Unlock()
	if read {
		t.Fatal("a reading that could not be taken was recorded as having happened")
	}

	// And now the tree becomes a repository with a stash in it. Nobody knows
	// whether this run put it there, so nobody says it did.
	revertRepo(t, tree)
	project := filepath.Join(tree, "_make.py")
	if err := os.WriteFile(project, []byte("def make(): ...\n"), 0o644); err != nil {
		t.Fatalf("writing the project's file: %v", err)
	}
	revertCommit(t, tree, "the project as it was")
	if err := os.WriteFile(project, []byte("def make(): return 1\n"), 0o644); err != nil {
		t.Fatalf("editing the project's file: %v", err)
	}
	if out, err := git(tree, "stash"); err != nil {
		t.Fatalf("git stash: %v (%s)", err, out)
	}
	if got, read := stashList(tree); !read || len(got) != 1 {
		t.Fatalf("the tree now holds read=%v with %d entries, want one that reads", read, len(got))
	}
	if got := agent.stashedWork(); got != 0 {
		t.Fatalf("a run with no before-reading counted %d stash entries as its own", got)
	}
}

// AND A WORKSPACE THAT IS NOT A REPOSITORY SAYS NOTHING ABOUT STASHES. There is
// nobody to ask, so the honest answer is silence rather than a sentence about a
// stash that cannot exist — and the same answer covers a machine with no git.
func TestANonRepositoryWorkspaceSaysNothingAboutStashes(t *testing.T) {
	tree := t.TempDir()
	if err := os.WriteFile(filepath.Join(tree, "_make.py"), []byte("def make(): ...\n"), 0o644); err != nil {
		t.Fatalf("writing the project's file: %v", err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = tree
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	if got, read := stashList(tree); read || len(got) != 0 {
		t.Fatalf("a directory that is no repository answered read=%v with %d entries", read, len(got))
	}
	if got, read := stashList(""); read || len(got) != 0 {
		t.Fatalf("a session with no deliverable tree answered read=%v with %d entries", read, len(got))
	}
	agent.openBaseline(context.Background())

	_, stashed := agent.terminalAudit(context.Background())
	if stashed != 0 {
		t.Fatalf("the terminal reading reported %d stash entries over a plain directory", stashed)
	}
	remains := agent.remainsFor("fixed")
	remains.Stashed = stashed
	for _, line := range remains.unmet() {
		if strings.Contains(line, "stash") {
			t.Fatalf("a plain directory was told about a stash: %q", line)
		}
	}
}
