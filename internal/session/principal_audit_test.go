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
// A session declares its checks the same way a unit of work does — in prose, in
// backticks or after a shell prompt (task_checks.go's [declaredChecks]) — and
// the two must never disagree about what a check is, which is why the same
// reading answers both.
func TestTheSessionsChecksComeOffItsOwnAskAndAcceptance(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	steward := agent.steward()
	steward.hear("port the parser; check it with `go build ./...`")
	steward.setAcceptance("every fixture parses and `go test ./parser` passes")

	checks := agent.sessionChecks()
	want := map[string]bool{"go build ./...": false, "go test ./parser": false}
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

	_, found := agent.terminalAudit(context.Background())
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

	_, found := agent.terminalAudit(context.Background())
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

	if got := agent.decideRemains(context.Background(), "", "all done"); got.Verb != DecideDone {
		t.Fatalf("a person's stopped turn did not end: %+v", got)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a person's session ran a check nobody asked it to run")
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

	got := agent.decideRemains(context.Background(), "", "That completes the port.")
	if got.Verb != DecideCarryOn {
		t.Fatalf("a tree that fails its own check was allowed to finish: %+v", got)
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
	landOne(agent, TaskRunning, "write the tests", "")

	landings, landed, running := agent.landings()
	if !landed || len(landings) != 2 {
		t.Fatalf("the settled work is not what was read: landed=%v %+v", landed, landings)
	}
	// AND THE WORK THAT HAS NOT COME HOME IS ANSWERED RATHER THAN DROPPED, by its
	// own title, so a goal owner can tell a tidy graph from a busy one.
	if len(running) != 1 || running[0] != "write the tests" {
		t.Fatalf("the work still going was not carried out of the graph: %v", running)
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
func TestASessionWithOnlyRunningWorkHasFinishedNothing(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	landOne(agent, TaskRunning, "write the tests", "")
	if _, landed, _ := agent.landings(); landed {
		t.Fatal("work still running was counted as finished")
	}
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
	agent.decideRemains(context.Background(), "the handlers are still unwired", "I have made a start.")
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

	got := agent.decideRemains(context.Background(), "there is plenty left to do", "I have made a start.")
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

	got := agent.decideRemains(context.Background(), "the handlers are still unwired", "I have made a start.")
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
	got := agent.decideRemains(context.Background(), "", "The version is bumped.")
	if got.Verb != DecideDone {
		t.Fatalf("a finished ask was carried on: %+v", got)
	}
	if strings.Contains(got.Brief, "does not pass") {
		t.Fatalf("something that never ran was reported as not passing:\n%s", got.Brief)
	}
}
