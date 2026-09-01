package session

// THE HANDOFF CONTRACT, PROVED (issue #143): a brief, a ground, and something
// checkable between them, answered before the first model call.
//
// The counterfactual every test here is written from is the measured one: a
// brief that named a symbol of a world its worker was never given, and a worker
// that spent twenty-two minutes and $7.99 finding that out four lines at a time.

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// THE ACCEPTANCE'S FIRST CLAUSE: a brief naming a file that is absent from the
// ground fails in under one model call, with a report naming the file.
func TestABriefNamingAnAbsentFileLandsBeforeOneModelCall(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, workspace := newTestAgent(t, completer, nil)
	writeFile(t, filepath.Join(workspace, "taskchip.go"), "package tui3\n")
	updates := agent.TaskUpdates()

	graph := agent.graph()
	id := graph.reserve()
	graph.admit(id, taskSpec{
		title: "port the strip tests",
		// A DIVIDER ALWAYS NAMES ITS PARTS (task_divide.go admits with named
		// true), so the namer's own call is not part of what this measures.
		named:      true,
		brief:      "taskstrip.go is gone; the survivors live in taskchip.go",
		acceptance: "the tests pass",
		expects: []Expectation{
			{Path: "internal/tui3/topbar.go"},
		},
	})

	notice := awaitNotice(t, updates, id, func(n TaskNotice) bool { return n.State == TaskFailed })
	if notice.Ending != TaskEndingStale {
		t.Fatalf("ending = %q, want %q", notice.Ending, TaskEndingStale)
	}
	if !strings.Contains(notice.Report, "internal/tui3/topbar.go") {
		t.Fatalf("the report does not name the file:\n%s", notice.Report)
	}
	if !strings.Contains(notice.Report, "it is not there") {
		t.Fatalf("the report does not say what was found:\n%s", notice.Report)
	}
	// UNDER ONE MODEL CALL MEANS NONE OF THE WORK'S OWN. The whole value of the
	// contract is that a mismatch is cheaper than the first step of working on
	// it, so what this asks is whether a worker was ever handed this brief.
	if askedAbout(completer, "the survivors live in taskchip.go") {
		t.Fatal("a worker was handed the brief before the contract was answered")
	}
}

// THE ACCEPTANCE'S SECOND CLAUSE, which is the loop-shaped counterfactual
// itself: a stale ground and a brief naming a symbol land as a stale ground and
// never as a working phase.
func TestAStaleGroundLandsInsteadOfWorkingOnIt(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, workspace := newTestAgent(t, completer, nil)
	// The world the worker really gets: the file is there, and the symbol the
	// parent's own uncommitted work put in it is not.
	writeFile(t, filepath.Join(workspace, "taskchip.go"), "package tui3\n\nfunc chipKey() {}\n")
	updates := agent.TaskUpdates()

	graph := agent.graph()
	id := graph.reserve()
	graph.admit(id, taskSpec{
		title:      "the strip and chip tests",
		named:      true,
		brief:      "taskstrip.go is gone; the survivors live in taskchip.go (stripKey, ParentID)",
		acceptance: "the tests pass",
		expects: []Expectation{
			{Path: "taskchip.go", Holds: "stripKey", Fact: "the survivors of taskstrip.go live in taskchip.go"},
			{Path: "taskstrip.go", Absent: true},
		},
	})

	notice := awaitNotice(t, updates, id, func(n TaskNotice) bool { return n.State == TaskFailed })
	if notice.Ending != TaskEndingStale {
		t.Fatalf("ending = %q, want %q", notice.Ending, TaskEndingStale)
	}
	if !strings.Contains(notice.Report, "stripKey") {
		t.Fatalf("the report does not name the symbol the brief leaned on:\n%s", notice.Report)
	}
	// THE DIVIDER'S OWN WORDS ARE WHAT THE PARENT READS, because the divider can
	// say why the thing mattered and the harness cannot.
	if !strings.Contains(notice.Report, "the survivors of taskstrip.go live in taskchip.go") {
		t.Fatalf("the report drops the divider's own account of the assumption:\n%s", notice.Report)
	}
	// AND IT IS ACTIONABLE. Three things the parent can do, in its own words.
	for _, word := range []string{"start it again", "hand the part out again", "which of the two is right"} {
		if !strings.Contains(notice.Report, word) {
			t.Fatalf("the report does not tell the parent to %q:\n%s", word, notice.Report)
		}
	}
	if askedAbout(completer, "the survivors live in taskchip.go (stripKey, ParentID)") {
		t.Fatal("a working phase started: a worker was handed the brief")
	}
	// The expectation that DID hold is not in the report, because a report that
	// listed everything checked would bury the two lines that matter.
	if strings.Contains(notice.Report, "is gone") {
		t.Fatalf("the report carries an expectation that held:\n%s", notice.Report)
	}
}

// THE ACCEPTANCE'S THIRD CLAUSE: the manifest is optional, and a brief with no
// expectations preflights nothing.
func TestABriefWithNoExpectationsPreflightsNothing(t *testing.T) {
	folder := t.TempDir()
	if unmet := preflightExpectations(folder, nil); unmet != nil {
		t.Fatalf("a brief that assumes nothing was answered with %v", unmet)
	}
	if section := expectsSection(nil); section != "" {
		t.Fatalf("a worker with no manifest is shown %q", section)
	}
	// And the door reads an absent list as an absent list rather than as a
	// mistake.
	expects, problem := parseExpectations(nil)
	if problem != "" || expects != nil {
		t.Fatalf("parseExpectations(nil) = %v, %q", expects, problem)
	}
}

// THE MANIFEST IS NEVER INVENTED BY THE HARNESS. Both doors that take a brief
// from a model admit exactly what the model wrote, and nothing when it wrote
// nothing — a harness mining paths out of prose would be guessing at which
// sentences the brief depends on and then landing somebody's work on the guess.
func TestTheHarnessNeverWritesAnExpectation(t *testing.T) {
	parsed, problem := parseDivideArguments([]byte(`{"evidence":"eleven adapters","parts":[
		{"title":"one","summary":"s","brief":"edit internal/one.go","acceptance":"a"},
		{"title":"two","summary":"s","brief":"edit internal/two.go","acceptance":"a"}]}`))
	if problem != "" {
		t.Fatalf("parseDivideArguments: %s", problem)
	}
	for i, part := range parsed.Parts {
		if len(part.Expects) != 0 {
			t.Fatalf("part %d was given %d expectations nobody wrote: %+v", i+1, len(part.Expects), part.Expects)
		}
	}
}

// AN EXPECTATION WITH NOWHERE TO LOOK IS REFUSED AT THE DOOR, because the whole
// value of the manifest is that every line of it is answered before the money is
// spent. The refusal is a sentence, not a silent drop.
func TestAnExpectationNeedsSomewhereToLookAndOneMeaning(t *testing.T) {
	if _, problem := parseExpectations([]Expectation{{Fact: "the staging endpoint answers"}}); problem == "" {
		t.Fatal("an expectation with no path was accepted")
	}
	if _, problem := parseExpectations([]Expectation{{Path: "a.go", Absent: true, Holds: "x"}}); problem == "" {
		t.Fatal("an expectation that must be gone and must hold something was accepted")
	}
	many := make([]Expectation, expectsRemembered+1)
	for i := range many {
		many[i] = Expectation{Path: "a.go"}
	}
	if _, problem := parseExpectations(many); problem == "" {
		t.Fatal("a survey of the folder was accepted as a manifest")
	}
}

// THE PREFLIGHT ANSWERS A PLACE, AND OPTIONALLY SOMETHING IN IT — which is the
// whole taxonomy, and covers a file, a folder, a note, a dataset and a
// transcript without naming any of them.
func TestThePreflightAnswersEveryShapeOfExpectation(t *testing.T) {
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, "notes", "day-one.md"), "# what we found\n\nthe latency column is p95_ms\n")
	writeFile(t, filepath.Join(folder, "rows.csv"), "run,p95_ms\n1,20\n")

	for _, one := range []struct {
		name   string
		expect Expectation
		unmet  string
	}{
		{"a place that is there", Expectation{Path: "rows.csv"}, ""},
		{"a place that is not", Expectation{Path: "gone.csv"}, "it is not there"},
		{"a place that must be gone", Expectation{Path: "gone.csv", Absent: true}, ""},
		{"a place that is still there", Expectation{Path: "rows.csv", Absent: true}, "it is still there"},
		{"a column that is in it", Expectation{Path: "rows.csv", Holds: "p95_ms"}, ""},
		{"a column that is not", Expectation{Path: "rows.csv", Holds: "latency_ms"}, "it does not say latency_ms"},
		{"a folder that says it", Expectation{Path: "notes", Holds: "p95_ms"}, ""},
		{"a folder that does not", Expectation{Path: "notes", Holds: "p50_ms"}, "nothing under it says p50_ms"},
	} {
		t.Run(one.name, func(t *testing.T) {
			unmet := preflightExpectations(folder, []Expectation{one.expect})
			switch {
			case one.unmet == "" && len(unmet) != 0:
				t.Fatalf("it held and was reported anyway: %v", unmet)
			case one.unmet == "":
			case len(unmet) != 1:
				t.Fatalf("it did not hold and was reported as %v", unmet)
			case !strings.Contains(unmet[0], one.unmet):
				t.Fatalf("the sentence reads %q, want it to say %q", unmet[0], one.unmet)
			}
		})
	}
}

// THE WORKER IS SHOWN WHAT WAS CHECKED FOR IT. A worker that knows its brief was
// held up against its folder and held knows how far it can trust the sentences
// above; one told nothing is the worker that starts by re-reading the folder.
func TestTheWorkerIsToldWhatItsHandoffAssumed(t *testing.T) {
	brief := composeBrief("do the thing", "the work", "a file", "it passes",
		expectsSection([]Expectation{
			{Path: "taskchip.go", Holds: "stripKey"},
			{Path: "taskstrip.go", Absent: true},
		}))
	for _, want := range []string{briefExpectsHeading, "taskchip.go holds stripKey", "taskstrip.go is gone"} {
		if !strings.Contains(brief, want) {
			t.Fatalf("the worker's document does not carry %q:\n%s", want, brief)
		}
	}
	// THE EMPTINESS LAW, applied to a document: no manifest, no heading over
	// nothing.
	if plain := composeBrief("do the thing", "the work", "a file", "it passes", ""); strings.Contains(plain, briefExpectsHeading) {
		t.Fatalf("a handoff with no manifest got a heading over nothing:\n%s", plain)
	}
}

// askedAbout reports whether any request this completer has seen carried a
// phrase — which is how these tests ask "did a worker ever start" without
// counting calls.
//
// COUNTING WOULD BE A RACE, and a measured one: a task that lands wakes the
// conversation, so a session that has just refused a handoff is about to make a
// model call of its own about the landing. That call is not the work, and a
// test that counted it would fail on the machine that scheduled it quickly.
func askedAbout(completer *scriptedCompleter, phrase string) bool {
	for index := 0; index < completer.requests(); index++ {
		for _, message := range completer.request(index) {
			if strings.Contains(messageText(message), phrase) {
				return true
			}
		}
	}
	return false
}

// THE DIVIDER'S DOOR CARRIES THE MANIFEST AND THE CONVERSATION'S DOES NOT. The
// schema is parsed here because a malformed one is not a wrong answer, it is a
// belt that will not build — and it happened once on this very change.
func TestTheDividersSchemaCarriesTheManifestAndParses(t *testing.T) {
	var shape map[string]any
	if err := json.Unmarshal([]byte(divideSchemaJSON), &shape); err != nil {
		t.Fatalf("the divider's schema does not parse: %v", err)
	}
	if !strings.Contains(divideSchemaJSON, `"expects"`) {
		t.Fatalf("the divider's schema does not carry the handoff's manifest")
	}
	// AND propose_task DOES NOT CARRY ONE, which is a bill and not a principle:
	// its schema rides in front of every request of every conversation and the
	// fixed prefix had no room (handoffcontract.go says so where it is decided,
	// and prefixbudget_test.go is what would fail).
	if strings.Contains(taskSchemaJSON, `"expects"`) {
		t.Fatal("propose_task grew a manifest; the fixed-prefix budget is what pays for it")
	}
}
