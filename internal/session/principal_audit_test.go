package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// THE CHECKS ARE THE ONES THE WORK NAMED, AND THEY ARE RUN IN A FRESH PROCESS.
//
// A session declares its checks from the acceptance the work composed — in
// prose, in backticks or after a shell prompt (task_checks.go's
// [declaredChecks]) — and a unit of work uses the same reading, so the two can
// never disagree about what a check is.
func TestTheSessionsChecksComeOffItsOwnAcceptance(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	steward := agent.steward()
	steward.hear("port the parser; check it with `go build ./...`")
	steward.setAcceptance("every fixture parses and `go test ./parser` passes")

	checks := agent.sessionChecks()
	want := map[string]bool{"go test ./parser": false}
	for _, check := range checks {
		if _, named := want[check]; named {
			want[check] = true
		}
	}
	for check, found := range want {
		if !found {
			t.Fatalf("the session does not check what its own words name: %q is missing from %v", check, checks)
		}
	}
	if containsWord(checks, "go build ./...") {
		t.Fatalf("a command the ask mentions is not the work's own promise, yet it became a session check: %v", checks)
	}
}

// AN EDIT TO A FILE THE PROJECT ALREADY HAD IS WORK THE SESSION MADE.
//
// [Remains.Made] read the created ledger alone, so a session whose whole fix was
// one edit to an existing file — the commonest shape of a fix there is — was
// told `nothing has been finished yet` at every ending until the standstill
// stopped it over green work (#513, the tox cell). The changed ledger answers
// it, and it stays apart from the created one: only what the session made may
// ever be swept.
func TestAnEditToAFileTheProjectAlreadyHadIsWorkTheSessionMade(t *testing.T) {
	tree := t.TempDir()
	existing := filepath.Join(tree, "discover.py")
	if err := os.WriteFile(existing, []byte("def discover(): ...\n"), 0o644); err != nil {
		t.Fatalf("writing the project's file: %v", err)
	}
	// The digest the ledger takes at pre-action, which is what makes the edit
	// below a change rather than a path somebody wrote to.
	before := fileDigest(existing)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = tree
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	if agent.remainsFor("fixed", readerLine{}).Made {
		t.Fatal("a session that has written nothing was said to have made something")
	}

	if err := os.WriteFile(existing, []byte("def discover(): return 1\n"), 0o644); err != nil {
		t.Fatalf("editing the project's file: %v", err)
	}
	agent.rememberChange(fileChange{path: existing, shown: "discover.py", created: false, before: before})
	if !agent.remainsFor("fixed", readerLine{}).Made {
		t.Fatal("an edit to the project's own file was not counted as work the session made")
	}
	if created := agent.createdList(); len(created) != 0 {
		t.Fatalf("a modified file reached the ledger the tidy may sweep: %+v", created)
	}

	// AND A FILE CHANGED OUTSIDE THE TREE IS NOT THE WORK: a note the session
	// kept for itself somewhere else says nothing about the deliverable.
	aside, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = tree
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	elsewhere := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(elsewhere, []byte("notes\n"), 0o644); err != nil {
		t.Fatalf("writing the note: %v", err)
	}
	aside.rememberChange(fileChange{path: elsewhere, shown: elsewhere, created: false})
	if aside.remainsFor("fixed", readerLine{}).Made {
		t.Fatal("a file changed outside the deliverable was counted as work on it")
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
	if checks := promised.sessionChecks(); !containsWord(checks, "chmod 000 tox.ini") {
		t.Fatalf("the same step declared by the acceptance is not a session check: %v", checks)
	}
}

// A DONE-CONDITION THAT IS THE PERSON'S PASTED ASK CARRIES NO PROMPT STEP INTO
// THE SESSION'S CHECKS.
//
// When no one could write a done-condition, the person's words arrive behind
// [routeAskAcceptance]. The measured tox reproduction must still remain a
// story about seeing the bug rather than become `chmod 000 tox.ini` run against
// the deliverable tree.
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
	agent.steward().setAcceptance(routeAskAcceptance +
		"check it with `run_tests.sh`\nthe reproduction ends with:\n$ chmod 000 tox.ini")

	checks := agent.sessionChecks()
	if containsWord(checks, "chmod 000 tox.ini") {
		t.Fatalf("a step out of the ask-fallback done-condition became a session check: %v", checks)
	}
	if want := checkCommand(tree, "run_tests.sh"); want == "" || !containsWord(checks, want) {
		t.Fatalf("a backticked command in the same ask-fallback was not a session check: want %q in %v", want, checks)
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
	node.spec.acceptance = "the tox configuration remains readable and `run_tests.sh` passes"
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

// A PERSON'S SESSION NEVER DELETES ANYTHING. The list is worked out exactly as
// it is for a goal owner — that is what lets the same road be offered to them —
// and the removal is the half that belongs to nobody but a session with nobody
// in it.
func TestAPersonsSessionIsOfferedTheScratchAndNeverLosesIt(t *testing.T) {
	tree := t.TempDir()
	elsewhere := t.TempDir()
	scratch := filepath.Join(elsewhere, "fixtures.jsonl")
	if err := os.WriteFile(scratch, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.Workspace = tree })
	agent.rememberCreated(fileChange{path: scratch, shown: scratch, created: true})

	_, found, _ := agent.terminalAudit(context.Background())
	if len(found.scratch) != 1 || found.scratch[0] != scratch {
		t.Fatalf("the scratch was not found: %+v", found.scratch)
	}
	found = agent.sweepSession(found)
	if len(found.removed) != 0 {
		t.Fatalf("a person's session deleted %v", found.removed)
	}
	if _, err := os.Stat(scratch); err != nil {
		t.Fatalf("a person's file was removed: %v", err)
	}
}

// AND AN UNATTENDED ONE PICKS UP AFTER ITSELF, which is the ending two measured
// runs were zeroed for not having.
func TestAnUnattendedSessionPicksUpItsOwnScratch(t *testing.T) {
	tree := t.TempDir()
	elsewhere := t.TempDir()
	scratch := filepath.Join(elsewhere, "fixtures.jsonl")
	if err := os.WriteFile(scratch, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = tree
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	agent.rememberCreated(fileChange{path: scratch, shown: scratch, created: true})

	_, found, _ := agent.terminalAudit(context.Background())
	found = agent.sweepSession(found)
	if len(found.removed) != 1 || found.removed[0] != scratch {
		t.Fatalf("the scratch was not picked up: %+v", found)
	}
	if _, err := os.Stat(scratch); err == nil {
		t.Fatalf("%s is still there", scratch)
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

	if got := agent.decideRemains(context.Background(), readerLine{answered: true, nothingLeft: true}, "all done"); got.Verb != DecideDone {
		t.Fatalf("a person's stopped turn did not end: %+v", got)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a person's session ran a check nobody asked it to run")
	}
}

// AND A SESSION THAT DID THE WHOLE JOB INLINE REACHES THE CHECK AT ALL.
//
// The reef cell wrote the fix and its tests itself and never started a task, so
// the reading said "nothing has been finished yet" over a green tree and the run
// stopped at a standstill. With the reader agreeing that nothing is left, the
// work it MADE is finished work — and what that buys is not a done answer taken
// on trust but the second reading: the session's declared checks are run against
// the tree, and they are what actually settles it.
func TestInlineWorkWithNoTasksReachesTheTerminalCheck(t *testing.T) {
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

	got := agent.decideRemains(context.Background(), readerLine{answered: true, nothingLeft: true},
		"The scheme parsing is fixed and the tests pass.")

	if got.Verb != DecideDone {
		t.Fatalf("a session that wrote the whole fix itself was carried on: %+v", got)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("the terminal check never ran over the work the session made: %v", err)
	}
}

// AND A GOAL OWNER'S IS, WHICH IS THE WHOLE POINT: the reader said the ask was
// met, the check says the tree does not hold, and the turn carries on.
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
	landOne(agent, TaskDone, "port the parser", "")

	got := agent.decideRemains(context.Background(), readerLine{answered: true, nothingLeft: true}, "That completes the port.")
	if got.Verb != DecideCarryOn {
		t.Fatalf("a tree that fails its own check was allowed to finish: %+v", got)
	}
	if !strings.Contains(got.Brief, "false does not pass") {
		t.Fatalf("the brief does not name the check:\n%s", got.Brief)
	}
}

// AND THE SAME READING IS TAKEN AT A HANDOVER. Done seals a handover now
// (checkpoint.go's [Agent.endTurnUnderSteward]), so a done reached there has to
// survive the declared checks exactly as one reached at a stopped turn does — or
// a run could finish on a done nobody checked, on the one road that used to skip
// the reading because done did not end it.
func TestADoneAtAHandoverIsCheckedAgainstTheTreeFirst(t *testing.T) {
	tree := t.TempDir()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = tree
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	steward := agent.steward()
	steward.hear("port the parser; check it with `false`")
	steward.setAcceptance("the parser builds and `false` passes")
	landOne(agent, TaskDone, "port the parser", "")

	got := agent.decideHandover(context.Background(), readerLine{answered: true, nothingLeft: true}, "That completes the port.")
	if got.Verb != DecideCarryOn {
		t.Fatalf("a handover over a tree that fails its own check was allowed to finish: %+v", got)
	}
	if !strings.Contains(got.Brief, "false does not pass") {
		t.Fatalf("the brief does not name the check:\n%s", got.Brief)
	}
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
	agent.decideRemains(context.Background(), readerLine{said: "the handlers are still unwired", answered: true}, "I have made a start.")
	if err := agent.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}
	content, err := os.ReadFile(transcript)
	if err != nil {
		t.Fatalf("reading the journal: %v", err)
	}
	return string(content)
}

// A RUN THAT STOPPED STILL PICKS UP AFTER ITSELF.
//
// The budget running out is the ending most likely to leave a mess, and it goes
// nowhere near the "is the ask met" reading — so the tidy is owed to whoever
// comes to look at the tree afterwards however the run ended.
func TestARunThatStoppedOnItsBudgetStillPicksUpAfterItself(t *testing.T) {
	tree := t.TempDir()
	elsewhere := t.TempDir()
	scratch := filepath.Join(elsewhere, "fixtures.jsonl")
	if err := os.WriteFile(scratch, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = tree
		c.Unattended = true
		c.Budget = Budget{USD: 1}
	})
	agent.mu.Lock()
	agent.usage.CostUSD = 5
	agent.mu.Unlock()
	agent.rememberCreated(fileChange{path: scratch, shown: scratch, created: true})

	got := agent.decideRemains(context.Background(), readerLine{said: "there is plenty left to do", answered: true}, "I have made a start.")
	if got.Verb != DecideStop {
		t.Fatalf("a spent budget did not stop the run: %+v", got)
	}
	if _, err := os.Stat(scratch); err == nil {
		t.Fatalf("the run stopped and left %s behind", scratch)
	}
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
	agent.rememberCreated(fileChange{path: working, shown: working, created: true})

	got := agent.decideRemains(context.Background(), readerLine{said: "the handlers are still unwired", answered: true}, "I have made a start.")
	if got.Verb != DecideCarryOn {
		t.Fatalf("the run did not carry on: %+v", got)
	}
	if _, err := os.Stat(working); err != nil {
		t.Fatalf("a run that is still working lost its own material: %v", err)
	}
}

// AND THE CHECKS A WORKER ACTUALLY RAN ARE THE SESSION'S CHECKS TOO.
//
// A worker that hammered one command for an hour has said what the check is
// more clearly than any document, and reading it through the same door that
// node's own auditor used ([auditDoorFor]) is what keeps a session and its
// units of work checking the same things.
func TestTheSessionAlsoChecksWhatItsWorkersRan(t *testing.T) {
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
	node.receipts = []toolReceipt{{command: "go test ./parser"}}
	node.graph.mu.Unlock()

	var found bool
	for _, check := range agent.sessionChecks() {
		if check == "go test ./parser" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the session does not check what its own worker ran: %v", agent.sessionChecks())
	}
}

// ── #468: A CHECK IS A COMMAND, NEVER A PATH ────────────────────────────────

// A SPAN THAT CANNOT BE INVOKED IS NOT A FAILING CHECK, IT IS NOT A CHECK.
//
// Prose names a source file in backticks as readily as it names a build, and the
// harvest used to admit any backticked span the tree happened to hold — then run
// it under a shell. A file with no executable bit and no line saying what starts
// it exits 126 every time, so the session reported "does not pass" about it on
// every round of a whole evening and could never finish. A file the tree can
// really start is opened the way the FILE says it opens, which is the same fact a
// node's own door already reads off it (task_checks.go's [fileFacts]).
func TestASessionsCheckIsACommandAndNeverABarePath(t *testing.T) {
	tree := t.TempDir()
	// The file the acceptance quotes: it is there, and nothing about it says that
	// starting it is a thing that happens.
	if err := os.WriteFile(filepath.Join(tree, "version.py"), []byte("VERSION = \"1.0\"\n"), 0o644); err != nil {
		t.Fatalf("writing the source file: %v", err)
	}
	// And one that does say: its first line names the program that runs it.
	script := filepath.Join(tree, "checks.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o644); err != nil {
		t.Fatalf("writing the script: %v", err)
	}

	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = tree
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	steward := agent.steward()
	steward.hear("bump the version")
	steward.setAcceptance("`version.py` reads 1.1 and `checks.sh` passes")

	checks := agent.sessionChecks()
	for _, check := range checks {
		if strings.Contains(check, "version.py") {
			t.Fatalf("a file nothing can start was harvested as a check: %v", checks)
		}
	}
	want := "sh " + shellQuoted(script)
	if !containsWord(checks, want) {
		t.Fatalf("the script was not opened the way its own first line says:\n got %v\nwant %q", checks, want)
	}
	// AND THE WHOLE ROAD ENDS AT DONE. The one unit of work finished, the one
	// check that can be run passes, and there is no permanent failure left over
	// from a span nobody could ever have typed.
	landOne(agent, TaskDone, "bump the version", "")
	got := agent.decideRemains(context.Background(), readerLine{answered: true, nothingLeft: true}, "The version is bumped.")
	if got.Verb != DecideDone {
		t.Fatalf("a finished ask was carried on: %+v", got)
	}
	if strings.Contains(got.Brief, "does not pass") {
		t.Fatalf("something that never ran was reported as not passing:\n%s", got.Brief)
	}
}

// ── #468: RUNNING MEANS IN FLIGHT ───────────────────────────────────────────

// WORK QUEUED BEHIND WORK THAT SETTLED SHORT IS STUCK, NOT MOVING.
//
// An unverified prerequisite deliberately does not cascade: its dependent stays
// queued, waiting on a person to resolve it (task_run.go's
// [TaskGraph.readinessLocked]). In an unattended run there is no person, so that
// node waits for ever — and counted as work in flight it held the floor under
// carrying on open on every single turn, which is a session that can never stop.
func TestQueuedBehindWorkThatSettledShortIsStuckAndNotMoving(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	graph := stubbedGraph(agent, func(node *TaskNode) {
		node.finish("nobody could say whether the port holds", nil, "", "")
		node.graph.complete(node, TaskUnverified)
	})
	first := graph.reserve()
	second := graph.reserve()
	graph.admit(first, taskSpec{title: "port the parser", brief: "b", acceptance: "a"})
	graph.admit(second, taskSpec{title: "wire the handlers", brief: "b", acceptance: "a",
		dependsOn: []uint64{first}})
	waitDoneNode(t, graph.node(first))

	_, _, flight := agent.landings()
	if len(flight.moving) != 0 {
		t.Fatalf("work that will never start was called moving: %+v", flight)
	}
	if len(flight.stuck) != 1 {
		t.Fatalf("the work that will not start was not said at all: %+v", flight)
	}
	if want := "wire the handlers is waiting on port the parser, which needs your look"; flight.stuck[0] != want {
		t.Fatalf("the stuck work does not say what it waits on:\n got %q\nwant %q", flight.stuck[0], want)
	}

	// AND WHAT IS STUCK IS PART OF WHAT IS LEFT, so the same reading twice
	// running is a standstill and the run stops — which is the whole defect.
	remains := agent.remainsFor("That completes the port.", readerLine{})
	steward := agent.steward()
	if got := steward.Decide(remains); got.Verb != DecideCarryOn {
		t.Fatalf("an ask with work that will not start was not carried on: %+v", got)
	}
	got := steward.Decide(remains)
	if got.Verb != DecideStop {
		t.Fatalf("a run that cannot get anywhere carried on again: %+v", got)
	}
	if !strings.Contains(got.Reason, "wire the handlers is waiting on port the parser") {
		t.Fatalf("the stop does not name what is stuck: %q", got.Reason)
	}
}

// AND THE SAME READING OF A FAILED PREREQUISITE, WHICH IS THE RACED WINDOW.
//
// The frontier fails the dependent of a failed node rather than leaving it queued
// (task_test.go's TestFrontierFailsDependentsOfAFailedNode), so this shape lives
// only in the moment between the landing and the cascade — and a stopped turn can
// be read inside it. The graph is built by hand for that reason: what is under
// test is the READING, and the frontier would have taken the shape away.
func TestQueuedBehindWorkThatDidNotFinishIsStuckToo(t *testing.T) {
	graph := newTaskGraph()
	graph.nodes[1] = &TaskNode{graph: graph, id: 1, spec: taskSpec{title: "port the parser"}, state: TaskFailed}
	// The edges live on the NODE and not on the spec — [TaskGraph.admit] copies
	// them across on the way in — and this graph never went through that door.
	graph.nodes[2] = &TaskNode{graph: graph, id: 2, state: TaskQueued,
		dependsOn: []uint64{1}, spec: taskSpec{title: "wire the handlers"}}
	graph.order = []uint64{1, 2}

	graph.mu.Lock()
	flight := graph.flightLocked()
	graph.mu.Unlock()

	if len(flight.moving) != 0 {
		t.Fatalf("work behind a failure was called moving: %+v", flight)
	}
	if want := "wire the handlers is waiting on port the parser, which did not finish"; len(flight.stuck) != 1 ||
		flight.stuck[0] != want {
		t.Fatalf("the stuck work does not say what it waits on:\n got %+v\nwant %q", flight.stuck, want)
	}
	// AND WORK QUEUED BEHIND WORK THAT IS MOVING IS MOVING, which is the other
	// half of the same rule: it has a turn coming.
	graph.mu.Lock()
	graph.nodes[1].state = TaskRunning
	flight = graph.flightLocked()
	graph.mu.Unlock()
	if len(flight.stuck) != 0 || len(flight.moving) != 2 {
		t.Fatalf("work queued behind something that is moving was called stuck: %+v", flight)
	}
}

// A FILE WRITTEN BACK TO WHAT IT WAS IS NOT WORK THE SESSION MADE.
//
// [Remains.Made] used to turn on the PATH being in the changed ledger, and a
// path is not a change. Measured on the canary of 2026-09-03: the attrs cell
// edited the file that held the fix, ran the suite, then ran `git stash` to
// compare its work against the baseline and never popped it — and the door said
// `finishing here · what was asked is done` over a tree with zero changed files.
// A revert and an edit that puts a file back the way it was fail identically.
func TestAFileWrittenBackToWhatItWasIsNotMade(t *testing.T) {
	tree := t.TempDir()
	existing := filepath.Join(tree, "_make.py")
	original := "def make(): ...\n"
	if err := os.WriteFile(existing, []byte(original), 0o644); err != nil {
		t.Fatalf("writing the project's file: %v", err)
	}
	before := fileDigest(existing)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = tree
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})

	if err := os.WriteFile(existing, []byte("def make(): return 1\n"), 0o644); err != nil {
		t.Fatalf("editing the project's file: %v", err)
	}
	agent.rememberChange(fileChange{path: existing, shown: "_make.py", created: false, before: before})
	if !agent.remainsFor("fixed", readerLine{}).Made {
		t.Fatal("an edit whose content is in the tree was not counted as work the session made")
	}

	// AND THE WORK GOES BACK OUT OF THE TREE. The ledger still holds the path and
	// the file is still there; what is gone is the difference.
	if err := os.WriteFile(existing, []byte(original), 0o644); err != nil {
		t.Fatalf("putting the project's file back: %v", err)
	}
	if agent.remainsFor("fixed", readerLine{}).Made {
		t.Fatal("a file written back to exactly what it was counted as work the session made")
	}

	// A CREATED FILE COUNTS AS IT ALWAYS DID: there was no content before it for
	// its content to be the same as.
	fresh := filepath.Join(tree, "test_make.py")
	if err := os.WriteFile(fresh, []byte("def test_make(): ...\n"), 0o644); err != nil {
		t.Fatalf("writing the new file: %v", err)
	}
	agent.rememberChange(fileChange{path: fresh, shown: "test_make.py", created: true})
	if !agent.remainsFor("fixed", readerLine{}).Made {
		t.Fatal("a file the session created was not counted as work it made")
	}

	// AND A WRITE THROUGH A LINK INSIDE THE TREE IS A WRITE TO WHAT IT POINTS AT.
	// The ledger's digest is taken through the link (os.Stat and os.Open follow
	// one), so the reading has to resolve the path too — an Lstat here answered
	// "not a regular file" and never counted the work at all.
	linked := t.TempDir()
	target := filepath.Join(linked, "real.py")
	if err := os.WriteFile(target, []byte("def real(): ...\n"), 0o644); err != nil {
		t.Fatalf("writing the link's target: %v", err)
	}
	link := filepath.Join(linked, "linked.py")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("this filesystem has no symlinks: %v", err)
	}
	through, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = linked
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	linkBefore := fileDigest(link)
	if err := os.WriteFile(link, []byte("def real(): return 1\n"), 0o644); err != nil {
		t.Fatalf("writing through the link: %v", err)
	}
	through.rememberChange(fileChange{path: link, shown: "linked.py", created: false, before: linkBefore})
	if !through.remainsFor("fixed", readerLine{}).Made {
		t.Fatal("a write through a link inside the tree was not counted as work the session made")
	}
	if err := os.WriteFile(target, []byte("def real(): ...\n"), 0o644); err != nil {
		t.Fatalf("putting the link's target back: %v", err)
	}
	if through.remainsFor("fixed", readerLine{}).Made {
		t.Fatal("a link whose target was written back counted as work the session made")
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
	reader := readerLine{answered: true, nothingLeft: true}
	remains := agent.remainsFor("fixed", reader)
	if !remains.Made || !remains.ReaderSaysDone {
		t.Fatalf("the fixture is not the case under test: %+v", remains)
	}
	if got := agent.who().Decide(remains); got.Verb != DecideDone {
		t.Fatalf("the first reading, which has not looked at the tree, answered %+v", got)
	}

	decision, _ := agent.decideOverTheChecks(context.Background(), remains)
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

	_, _, stashed := agent.terminalAudit(context.Background())
	if stashed != 0 {
		t.Fatalf("the terminal reading reported %d stash entries over a plain directory", stashed)
	}
	remains := agent.remainsFor("fixed", readerLine{answered: true, nothingLeft: true})
	remains.Stashed = stashed
	for _, line := range remains.unmet() {
		if strings.Contains(line, "stash") {
			t.Fatalf("a plain directory was told about a stash: %q", line)
		}
	}
}
