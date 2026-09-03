package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// ── the fixtures, one per ending this road has ──────────────────────────────

// budgetLeft is a Steward with hours it has not spent and no money counted at
// all, which is the ordinary shape of a run mid-evening.
func budgetLeft(t *testing.T) *Steward {
	t.Helper()
	return NewSteward("port the parser", Budget{Wall: 8 * time.Hour}, nil)
}

// (a) A LANDING THAT DID NOT FINISH BECOMES THE NEXT BRIEF.
//
// This is the measured dead end in one assertion. A unit of work came home
// unfinished with budget left; the note that used to be written offered a
// follow-up to a person who was not there, and the run ended on it. What the
// goal owner answers now is the audit's own account of the gap, and the note the
// model reads tells it to carry that on itself.
func TestAFailedLandingWithBudgetLeftBecomesTheNextBrief(t *testing.T) {
	steward := budgetLeft(t)
	landing := Landing{
		ID: 4, Title: "wire the handlers", State: TaskFailed,
		Report:    incompleteLead + "the request router still has no route for PATCH",
		Signature: "no route for PATCH",
	}
	brief := steward.Report(landing)
	if brief != landing.Report {
		t.Fatalf("the goal owner did not carry the report over as the brief:\n got %q\nwant %q", brief, landing.Report)
	}
	note := taskNote(TaskNotice{
		ID: landing.ID, Title: landing.Title, State: landing.State, Report: landing.Report,
	}, "", TaskSettleAuto, landingAddress{brief: brief})
	if !strings.Contains(note, "carry it on yourself from here") {
		t.Fatalf("the note does not tell the model to carry it on:\n%s", note)
	}
	if strings.Contains(note, "offer them a follow-up") {
		t.Fatalf("the note still offers a follow-up to a person who is not there:\n%s", note)
	}
}

// (b) THE SAME THING THREE TIMES STOPS THE RUN, WITH A REPORT.
//
// Two is a coincidence and four is an evening ([stewardRepeats]). The third
// landing produces no brief at all and every answer after it is the same stop,
// which is what makes the guard a rail rather than a mood.
func TestTheSameFailureThreeTimesStopsWithAReport(t *testing.T) {
	steward := budgetLeft(t)
	landing := Landing{
		ID: 4, Title: "wire the handlers", State: TaskFailed,
		Report:    incompleteLead + "the request router still has no route for PATCH",
		Signature: "no route for PATCH",
	}
	for round := 1; round <= 2; round++ {
		if brief := steward.Report(landing); brief == "" {
			t.Fatalf("round %d: the goal owner gave up before the guard fired", round)
		}
	}
	if brief := steward.Report(landing); brief != "" {
		t.Fatalf("the third identical failure still asked for another go: %q", brief)
	}
	decision := steward.Decide(Remains{Landed: true})
	if decision.Verb != DecideStop {
		t.Fatalf("the guard fired and the goal owner did not stop: %+v", decision)
	}
	if !strings.Contains(decision.Reason, "3 times running") ||
		!strings.Contains(decision.Reason, "no route for PATCH") {
		t.Fatalf("the stop does not report what stopped it: %q", decision.Reason)
	}
	// AND IT STAYS STOPPED. A principal that changed its mind after saying stop
	// would be a rail with a hole in it.
	if again := steward.Decide(Remains{Reader: "there is plenty left to do", Landed: true}); again.Verb != DecideStop {
		t.Fatalf("a stopped goal owner was talked back into working: %+v", again)
	}
}

// A DIFFERENT FAILURE EACH TIME IS NOT THE SAME FAILURE, which is the guard's
// other half: three different problems is a session making progress on three
// problems, and stopping it would be the guard doing the damage.
func TestThreeDifferentFailuresDoNotStopTheRun(t *testing.T) {
	steward := budgetLeft(t)
	for _, signature := range []string{"no route for PATCH", "the parser drops comments", "the writer never flushes"} {
		brief := steward.Report(Landing{
			ID: 4, State: TaskFailed, Report: incompleteLead + signature, Signature: signature,
		})
		if brief == "" {
			t.Fatalf("%q was refused a go of its own", signature)
		}
	}
	if decision := steward.Decide(Remains{Landed: true}); decision.Verb == DecideStop {
		t.Fatalf("three different problems stopped the run: %+v", decision)
	}
}

// AN UNSIGNED LANDING IS NEVER COUNTED. A failure nothing could classify is not
// evidence that the road is going nowhere, and folding every unsigned one into a
// single bucket would stop a session on three unrelated problems.
func TestAnUnsignedFailureIsNeverCountedByTheGuard(t *testing.T) {
	steward := budgetLeft(t)
	for round := 1; round <= 5; round++ {
		if brief := steward.Report(Landing{ID: 1, State: TaskFailed, Report: "something went wrong"}); brief == "" {
			t.Fatalf("round %d: an unsigned failure was counted by the guard", round)
		}
	}
}

// (c) THE TREE OUTRANKS THE CONVERSATION.
//
// A turn that ended in words alone, a reader that read the ask as met, an
// acceptance nobody has shown to hold, and a declared check that does not pass:
// the old rule ended the run here, on the strength of what the session said
// about itself. The goal owner carries on, and the brief names both the
// acceptance and the gap.
func TestAnUnmetCheckCarriesTheRunOnEvenWhenTheReaderIsSilent(t *testing.T) {
	steward := budgetLeft(t)
	decision := steward.Decide(Remains{
		Said:       "That completes the port. Everything is wired up.",
		Reader:     "",
		Acceptance: "the parser handles every fixture and `go build ./...` passes",
		Landed:     true,
		Landings:   []Landing{{ID: 1, Title: "port the parser", State: TaskDone}},
		Checks:     []CheckRun{{Command: "go build ./...", Passed: false, Tail: "undefined: parseHeader"}},
		// The baseline landed and the tree was clean, so this red is this run's
		// own ([Remains.BaselineRead]).
		BaselineRead: true,
	})
	if decision.Verb != DecideCarryOn {
		t.Fatalf("a tree that does not build was called finished: %+v", decision)
	}
	if !strings.Contains(decision.Brief, "go build ./... does not pass") {
		t.Fatalf("the brief does not name the check that failed:\n%s", decision.Brief)
	}
	if !strings.Contains(decision.Brief, "the parser handles every fixture") {
		t.Fatalf("the brief does not name what finished means:\n%s", decision.Brief)
	}
}

// A LANDING THAT DID NOT FINISH IS ITSELF A REASON TO CARRY ON, whatever the
// checks say, because a unit of work nobody finished is work that is left.
func TestAnUnfinishedLandingCarriesTheRunOn(t *testing.T) {
	steward := budgetLeft(t)
	decision := steward.Decide(Remains{
		Acceptance: "everything asked for is done",
		Landed:     true,
		Landings: []Landing{
			{ID: 1, Title: "port the parser", State: TaskDone},
			{ID: 2, Title: "wire the handlers", State: TaskFailed},
		},
		Checks: []CheckRun{{Command: "go build ./...", Passed: true}},
	})
	if decision.Verb != DecideCarryOn {
		t.Fatalf("a unit that did not finish was called finished: %+v", decision)
	}
	if !strings.Contains(decision.Brief, "wire the handlers did not finish") {
		t.Fatalf("the brief does not name the unfinished work:\n%s", decision.Brief)
	}
}

// ── a session that changed the deliverable has finished something (#513) ────

// WORK THIS SESSION DID WITH ITS OWN HANDS IS FINISHED WORK, WITH THE READER
// AGREEING.
//
// The reef cell did the whole fix inline — the change, a 196-line test file, 43
// tests green — and never started a task. [Remains.Landed] is a reading of the
// task graph, so it stayed false over that tree, "nothing has been finished yet"
// was the first line of both briefs, and the standstill compared two identical
// briefs and stopped the run over green work one second after tidying up.
//
// THE READER IS THE WITNESS AND IT IS REQUIRED. A session's own files are not
// evidence about themselves; what makes inline work count is somebody who did
// not write them saying nothing is left.
func TestInlineWorkWithTheReaderAgreeingIsFinishedWork(t *testing.T) {
	steward := budgetLeft(t)
	decision := steward.Decide(Remains{
		Said:           "The scheme parsing is fixed and the tests pass.",
		Acceptance:     "the bearer scheme is case-insensitive and the suite passes",
		Made:           true,
		ReaderSaysDone: true,
	})
	if decision.Verb != DecideDone {
		t.Fatalf("a session that wrote the whole fix itself was told it had finished nothing: %+v", decision)
	}

	// AND WITHOUT THE WITNESS IT IS NOT. A reader that was never asked, and one
	// whose call failed, both answer the same silence, and silence is not
	// agreement.
	alone := steward.Decide(Remains{
		Said:       "The scheme parsing is fixed and the tests pass.",
		Acceptance: "the bearer scheme is case-insensitive and the suite passes",
		Made:       true,
	})
	if alone.Verb != DecideCarryOn {
		t.Fatalf("a session graded its own inline work with nobody agreeing: %+v", alone)
	}
	if !strings.Contains(alone.Brief, "nothing has been finished yet") {
		t.Fatalf("the brief does not say what is missing:\n%s", alone.Brief)
	}
}

// AND A READER NAMING A GAP IN A SESSION WITH NOTHING ON THE RAIL IS A GAP.
//
// #468's law — a settled landing outranks a reading of the transcript — stands
// where there IS a landing. With none, the reader is the only account of the work
// anybody has, so what it says is left is left.
func TestAReaderNamingAGapWithNoTasksCarriesTheRunOn(t *testing.T) {
	steward := budgetLeft(t)
	decision := steward.Decide(Remains{
		Said:       "I have fixed the parsing.",
		Reader:     "the scopes are still parsed case-sensitively",
		Acceptance: "the bearer scheme is case-insensitive and the suite passes",
		Made:       true,
	})
	if decision.Verb != DecideCarryOn {
		t.Fatalf("a gap the reader named was passed over: %+v", decision)
	}
	if !strings.Contains(decision.Brief, "the scopes are still parsed case-sensitively") {
		t.Fatalf("the brief does not carry the reader's own words:\n%s", decision.Brief)
	}
}

// AND A SESSION THAT MADE NOTHING IS STILL A SESSION THAT HAS FINISHED NOTHING.
//
// The control, and the sentence people actually read: no task, no files, nothing
// to point at. Whatever the transcript sounds like, the answer is the one it
// always was.
func TestASessionWithNothingMadeAndNothingLandedHasFinishedNothing(t *testing.T) {
	steward := budgetLeft(t)
	decision := steward.Decide(Remains{
		Said:           "That completes the port. Everything is wired up.",
		Acceptance:     "the parser handles every fixture",
		ReaderSaysDone: true,
	})
	if decision.Verb != DecideCarryOn {
		t.Fatalf("a session that finished nothing was called finished: %+v", decision)
	}
	if !strings.Contains(decision.Brief, "nothing has been finished yet") {
		t.Fatalf("the brief does not say what is missing:\n%s", decision.Brief)
	}
}

// ── what is left is read from the tree, not from a sibling's death (#513) ───

// A UNIT THAT DIED ON THE WIRE FOUND NOTHING OUT, SO IT IS NOT WORK THAT IS
// LEFT.
//
// The reef cell: the parent wrote `tests/reef_service/test_auth.py` itself, went
// green on its own check and merged home. Its child — the unit that was to write
// that very file — had died an hour earlier on an API 404, which is a fact about
// who served the request and about nothing else. It was read as "Add focused
// tests … did not finish", and the run carried on over a finished tree until its
// wall.
func TestASiblingThatDiedOnTheWireIsNotWorkThatIsLeft(t *testing.T) {
	steward := budgetLeft(t)
	decision := steward.Decide(Remains{
		Acceptance: "the bearer scheme is case-insensitive and the suite passes",
		Landed:     true,
		Landings: []Landing{
			{
				ID: 1, Title: "bearer case sensitivity", State: TaskDone, Merged: true, Checked: true,
				Files: []string{"tests/reef_service/test_auth.py", "reef/service/auth.py"},
			},
			{
				ID: 3, Title: "Add focused tests for case-insensitive bearer scheme",
				State: TaskFailed, Ending: TaskEndingUpstream,
				Report: "it ended with an error: API error (404): All providers have been ignored",
				Files:  []string{"tests/reef_service/test_auth.py"},
			},
		},
		Checks:       []CheckRun{{Command: "pytest", Passed: true, Ran: true}},
		BaselineRead: true,
	})
	if decision.Verb != DecideDone {
		t.Fatalf("a run was carried on over a sibling the provider could not serve: %+v", decision)
	}

	// AND THE CONNECTION ITSELF IS THE OTHER HALF OF THE SAME LAW, with nothing
	// of the failed unit's work done by anybody: a dropped stream found nothing
	// out either, so it is not a gap in the ask on its own account.
	dropped := steward.Decide(Remains{
		Acceptance: "the bearer scheme is case-insensitive and the suite passes",
		Landed:     true,
		Landings: []Landing{
			{ID: 1, Title: "bearer case sensitivity", State: TaskDone, Merged: true, Checked: true},
			{
				ID: 3, Title: "Add focused tests for case-insensitive bearer scheme",
				State: TaskFailed, Ending: TaskEndingWire,
				Report: "lost the connection to the model: read: connection reset by peer",
			},
		},
		Checks:       []CheckRun{{Command: "pytest", Passed: true, Ran: true}},
		BaselineRead: true,
	})
	if dropped.Verb != DecideDone {
		t.Fatalf("a run was carried on over a sibling whose connection dropped: %+v", dropped)
	}
}

// AND THE CLASS IS READ OFF THE ERROR'S OWN TYPE, WHICH IS WHY IT NEEDED A WORD
// OF ITS OWN.
//
// A provider refusing the request is not a dropped connection: it is terminal,
// so [diedOnTheWire] answers false for it and the node used to end as
// [TaskEndingError] — the same word as a working copy that could not be made.
// [terminalProviderFailure] is the typed reading that tells them apart, on the
// error's Go type and never on its sentence.
func TestAProviderRefusalIsToldFromAnErrorAndFromTheWire(t *testing.T) {
	// THE SERVICE FAILING TO SERVE: no route left, a limit still refusing after
	// the retries, an account that could not be served, the service itself down.
	for _, err := range []error{
		&provider.APIError{Status: 404, Message: "All providers have been ignored"},
		&provider.APIError{Status: 503, Message: "service unavailable"},
		&provider.APIError{Status: 429, Message: "rate limit"},
		&provider.APIError{Status: 401, Message: "no key"},
		&provider.APIError{Status: 403, Message: "not permitted"},
	} {
		if !providerCouldNotServe(err) {
			t.Fatalf("%v was not read as the provider failing to serve", err)
		}
		if diedOnTheWire(err) {
			t.Fatalf("%v was read as a dropped connection", err)
		}
	}
	// AND THE PROVIDER ANSWERING IS THE WORK, whatever the answer was: a model
	// that read the request and refused it, and a 4xx about what the request
	// itself held. Both leave the job undone, so both stay counted.
	for _, err := range []error{
		&provider.RefusalError{
			Model:    "a-model",
			Attempts: 3,
			Refusal:  &provider.APIError{Status: 404, Message: "no endpoint can serve it"},
		},
		&provider.APIError{Status: 400, Message: "invalid request body"},
		&provider.APIError{Status: 413, Message: "payload too large"},
		&provider.APIError{Status: 422, Message: "unprocessable"},
	} {
		if providerCouldNotServe(err) {
			t.Fatalf("%v was read as the service failing rather than as an answer", err)
		}
	}
	// AND AN ORDINARY FAILURE IS NEITHER, so it keeps the ending it had and stays
	// a finding about the work.
	ours := errors.New("mkdir /nowhere/tree: read-only file system")
	if providerCouldNotServe(ours) {
		t.Fatal("a working copy that could not be made was blamed on the provider")
	}
	if !(Landing{State: TaskFailed, Ending: TaskEndingError}).aboutTheWork() {
		t.Fatal("an ordinary failure stopped being a finding about the work")
	}
	for _, ending := range []TaskEnding{TaskEndingWire, TaskEndingUpstream} {
		if (Landing{State: TaskFailed, Ending: ending}).aboutTheWork() {
			t.Fatalf("a node that ended %q was read as a finding about the work", ending)
		}
	}
}

// AND A UNIT THAT FAILED AT THE WORK IS STILL WORK THAT IS LEFT.
//
// This is the control, and it is the whole risk of the clause above: a check
// that found gaps, a loop guard, a threshold, a working copy that could not be
// made are all findings ABOUT THE JOB, and nothing about them is answered by
// somebody else's afternoon.
func TestAUnitThatFailedAtTheWorkIsStillLeft(t *testing.T) {
	steward := budgetLeft(t)
	decision := steward.Decide(Remains{
		Acceptance: "the bearer scheme is case-insensitive and the suite passes",
		Landed:     true,
		Landings: []Landing{
			{ID: 1, Title: "bearer case sensitivity", State: TaskDone, Merged: true},
			{
				ID: 3, Title: "Add focused tests for case-insensitive bearer scheme",
				State: TaskFailed, Ending: TaskEndingRefused,
				Files: []string{"tests/reef_service/test_auth.py"},
			},
		},
		Checks: []CheckRun{{Command: "pytest", Passed: true}},
	})
	if decision.Verb != DecideCarryOn {
		t.Fatalf("a unit the check refused was called finished: %+v", decision)
	}
	if !strings.Contains(decision.Brief, "Add focused tests for case-insensitive bearer scheme did not finish") {
		t.Fatalf("the brief does not name the unfinished work:\n%s", decision.Brief)
	}
}

// AND A UNIT WHOSE WORK SOMEBODY ELSE BROUGHT HOME IS ABSORBED, AND THE FILE
// SAYS BY WHOM.
//
// The failed unit's only file is on the person's branch, put there by a unit
// that finished and merged. Naming it as unfinished sends the run back to write
// a file that is already written. What the reading may not do is close a gap
// silently, so the absorption is a line of its own.
func TestAUnitWhoseWorkCameHomeAnywayIsAbsorbedAndSaidSo(t *testing.T) {
	remains := Remains{
		Landed: true,
		Landings: []Landing{
			{
				ID: 1, Title: "bearer case sensitivity", State: TaskDone, Merged: true, Checked: true,
				Files: []string{"tests/reef_service/test_auth.py", "reef/service/auth.py"},
			},
			{
				ID: 3, Title: "Add focused tests", State: TaskFailed, Ending: TaskEndingSteps,
				Files: []string{"tests/reef_service/test_auth.py"},
			},
		},
	}
	if left := remains.unmet(); len(left) != 0 {
		t.Fatalf("work already on the person's branch is still being asked for: %q", left)
	}
	said := remains.absorbed()
	if len(said) != 1 || !strings.Contains(said[0], "Add focused tests was absorbed by bearer case sensitivity") {
		t.Fatalf("the absorption is not said whole: %q", said)
	}

	// AND THE FOUR WAYS IT IS REFUSED. A unit that changed nothing cannot be
	// shown to be done; a landing that finished and could not come home is not
	// the tree holding anything; a landing NOBODY JUDGED is not evidence about
	// anybody's job, least of all somebody else's; and half of somebody's work is
	// not their work.
	nothingChanged := remains
	nothingChanged.Landings[1].Files = nil
	if left := nothingChanged.unmet(); len(left) != 1 {
		t.Fatalf("a unit that changed nothing was absorbed anyway: %q", left)
	}
	nothingChanged.Landings[1].Files = []string{"tests/reef_service/test_auth.py"}

	neverCameHome := remains
	neverCameHome.Landings[0].Merged = false
	if left := neverCameHome.unmet(); len(left) != 1 {
		t.Fatalf("a landing that never came home absorbed another: %q", left)
	}
	neverCameHome.Landings[0].Merged = true

	nobodyJudged := remains
	nobodyJudged.Landings[0].Checked = false
	if left := nobodyJudged.unmet(); len(left) != 1 {
		t.Fatalf("a landing nobody checked spoke for somebody else's work: %q", left)
	}
	if said := nobodyJudged.absorbed(); len(said) != 0 {
		t.Fatalf("an absorption nobody judged was written down: %q", said)
	}
	nobodyJudged.Landings[0].Checked = true

	halfDone := remains
	halfDone.Landings[1].Files = []string{"tests/reef_service/test_auth.py", "reef/service/scopes.py"}
	if left := halfDone.unmet(); len(left) != 1 {
		t.Fatalf("a unit half of whose work was done was absorbed: %q", left)
	}
}

// ── the checks are read against the baseline (#513) ─────────────────────────

// A CHECK THAT WAS ALREADY RED BEFORE THE WORK IS THE PROJECT'S, NOT THE RUN'S.
//
// The attrs cell's acceptance was "the existing test suite passes (run
// `tox -e py`)" over a suite that had one failing test before anybody touched
// anything. The sentence could never come true, so the goal owner read somebody
// else's bug as work still to do and carried the run into it until the wall.
//
// Three arms, and they are the whole of the arithmetic: red before and green
// after is somebody fixing something and is not left; red before and red after
// is the project's and is not left; green before and red after is ours and is.
func TestOnlyTheRedThisWorkTurnedRedIsWhatIsLeft(t *testing.T) {
	for _, tc := range []struct {
		name   string
		before []string
		after  []CheckRun
		done   bool
	}{
		{
			name:   "red before and green after",
			before: []string{"tox -e py"},
			after:  []CheckRun{{Command: "tox -e py", Passed: true}},
			done:   true,
		},
		{
			name:   "red before and red after",
			before: []string{"tox -e py"},
			after:  []CheckRun{{Command: "tox -e py", Passed: false, Tail: "1 failed"}},
			done:   true,
		},
		{
			name:   "green before and red after",
			before: nil,
			after:  []CheckRun{{Command: "tox -e py", Passed: false, Tail: "1 failed"}},
			done:   false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			steward := budgetLeft(t)
			decision := steward.Decide(Remains{
				Acceptance:   "the existing test suite passes (run `tox -e py`)",
				Landed:       true,
				Landings:     []Landing{{ID: 1, Title: "fix the subclass init", State: TaskDone, Merged: true}},
				Checks:       tc.after,
				WasFailing:   tc.before,
				BaselineRead: true,
			})
			if tc.done && decision.Verb != DecideDone {
				t.Fatalf("a run was carried on into red that was not its own: %+v", decision)
			}
			if !tc.done && decision.Verb != DecideCarryOn {
				t.Fatalf("a check this work turned red was not counted: %+v", decision)
			}
			if !tc.done && !strings.Contains(decision.Brief, "tox -e py does not pass") {
				t.Fatalf("the brief does not name the check this work broke:\n%s", decision.Brief)
			}
		})
	}
}

// AND A CHECK THE BASELINE COULD NOT RUN KEEPS THE MEANING IT ALWAYS HAD.
//
// A command that would not start, or that the window closed over, taught nobody
// anything about the tree. Recording it as already-red would SILENCE a real
// failure on it later, which is this law pointing the wrong way — so only a
// check that ran to an answer and answered red is baseline-red.
func TestACheckTheBaselineCouldNotRunIsNotAlreadyRed(t *testing.T) {
	steward := budgetLeft(t)
	// COULD NOT RUN, AND RED NOW: it is left.
	left := steward.Decide(Remains{
		Acceptance:   "the suite passes",
		Landed:       true,
		Landings:     []Landing{{ID: 1, Title: "fix it", State: TaskDone, Merged: true, Checked: true}},
		Checks:       []CheckRun{{Command: "tox -e py", Passed: false, Ran: true}},
		WasFailing:   nil,
		BaselineRead: true,
	})
	if left.Verb != DecideCarryOn {
		t.Fatalf("a check nobody read before the work was treated as already broken: %+v", left)
	}
	if !strings.Contains(left.Brief, "tox -e py does not pass") {
		t.Fatalf("the brief does not name the check:\n%s", left.Brief)
	}
	// RAN RED, AND RED NOW: it is the project's and is not left.
	done := steward.Decide(Remains{
		Acceptance:   "the suite passes",
		Landed:       true,
		Landings:     []Landing{{ID: 1, Title: "fix it", State: TaskDone, Merged: true, Checked: true}},
		Checks:       []CheckRun{{Command: "tox -e py", Passed: false, Ran: true}},
		WasFailing:   []string{"tox -e py"},
		BaselineRead: true,
	})
	if done.Verb != DecideDone {
		t.Fatalf("a run was carried on into red the baseline had already seen: %+v", done)
	}
}

// AND THE READING ITSELF: ONE WINDOW, NO MUTATION, AND A CHECK NOBODY COULD RUN
// IS NOT RED.
//
// The baseline runs an arbitrary shell command, so all three of these are about
// not trusting it too far: two checks share the window rather than getting one
// each, a check that WROTE is a first edit and not a photograph, and a command
// the shell could not run answered nothing.
func TestTheBaselineSharesOneWindowAndRefusesAMutatingCheck(t *testing.T) {
	tree := t.TempDir()
	writeFile(t, filepath.Join(tree, "keep.txt"), "before\n")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = tree
		config.Unattended = true
		config.Budget = Budget{Wall: time.Hour}
	})

	// A check that writes into the tree, one that simply fails, and one that is
	// not a command at all.
	agent.readBaseline(context.Background(), []string{
		"printf after > keep.txt",
		"exit 1",
		"aforge-no-such-command-anywhere",
	})

	red, unread, read := agent.baselineRedChecks()
	if !read {
		t.Fatal("the reading never landed")
	}
	if len(red) != 1 || red[0] != "exit 1" {
		t.Fatalf("the baseline is %q, want only the check that actually ran and failed", red)
	}
	// AND BOTH OF THE OTHERS SAY THEY WERE NOT READ rather than passing for
	// green: the one that wrote into the tree and the one nobody could run.
	if len(unread) != 2 {
		t.Fatalf("the unread list is %q, want the writing check and the one nobody could run", unread)
	}

	// AND ONE WINDOW ACROSS THE SET, not one each: a window already closed reads
	// nothing at all, however many checks are named.
	closed, stop := context.WithCancel(context.Background())
	stop()
	second, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = t.TempDir()
		config.Unattended = true
		config.Budget = Budget{Wall: time.Hour}
	})
	second.readBaseline(closed, []string{"exit 1", "exit 1"})
	if red, unread, read := second.baselineRedChecks(); !read || len(red) != 0 || len(unread) != 2 {
		t.Fatalf("a closed window read red=%q unread=%q, want nothing red and both unread", red, unread)
	}
}

// AND A READING ASSEMBLED BEFORE THE BASELINE LANDS COUNTS NOTHING.
//
// The reading runs in the background so the first turn is not held behind
// somebody's suite, which means the first few readings have no baseline at all.
// With none, no check is this run's own red — naming one before anybody knows
// what was already failing is the same mistake in a hurry — and the brief says
// the reading is still going, so silence is never read as "they pass".
func TestWithNoBaselineYetNoCheckIsCountedAsNewRed(t *testing.T) {
	steward := budgetLeft(t)
	decision := steward.Decide(Remains{
		Acceptance: "the suite passes",
		Landed:     true,
		Landings: []Landing{
			{ID: 1, Title: "fix it", State: TaskDone, Merged: true, Checked: true},
			{ID: 2, Title: "write the repro", State: TaskFailed, Ending: TaskEndingRefused},
		},
		Checks: []CheckRun{{Command: "tox -e py", Passed: false, Ran: true}},
	})
	if decision.Verb != DecideCarryOn {
		t.Fatalf("a unit the check refused was called finished: %+v", decision)
	}
	if strings.Contains(decision.Brief, "tox -e py does not pass") {
		t.Fatalf("a check was counted before anybody knew what was already red:\n%s", decision.Brief)
	}
	if !strings.Contains(decision.Brief, baselineStillReading) {
		t.Fatalf("the brief never says the reading is still going:\n%s", decision.Brief)
	}
	// AND WITH NOTHING ELSE LEFT IT IS STILL NOT DONE OVER AN UNREAD CHECK —
	// the red is not counted as ours, and it is not counted as the project's
	// either, so what the checks say simply is not part of this answer yet.
	if quiet := steward.Decide(Remains{
		Acceptance: "the suite passes",
		Landed:     true,
		Landings:   []Landing{{ID: 1, Title: "fix it", State: TaskDone, Merged: true, Checked: true}},
		Checks:     []CheckRun{{Command: "tox -e py", Passed: false, Ran: true}},
	}); quiet.Verb != DecideDone {
		t.Fatalf("an unread check was counted against the ask: %+v", quiet)
	}
}

// AND A CHECK THAT WRITES ONLY WHAT THE REPOSITORY IGNORES IS STILL READ.
//
// The attrs cell's only declared check was `python -m pytest tests/`. pytest
// writes `.pytest_cache/` and `.hypothesis/`, both in the project's own
// `.gitignore`, so the reading was thrown away — and with the only check
// discarded the baseline came back empty, which reads as a clean tree, over a
// project with 85 pre-existing failures.
//
// THE REPOSITORY'S OWN ANSWER IS THE ONE THAT COUNTS: the person already wrote
// down what is not theirs.
func TestABaselineCheckThatWritesOnlyIgnoredPathsIsStillRead(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, ".gitignore"), ".pytest_cache/\n.hypothesis/\n")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = repo
		config.Unattended = true
		config.Budget = Budget{Wall: time.Hour}
	})

	// A check that writes exactly what pytest writes, and fails the way the
	// project's suite already fails.
	agent.readBaseline(context.Background(),
		[]string{"mkdir -p .pytest_cache .hypothesis && touch .pytest_cache/v && exit 1"})

	red, unread, read := agent.baselineRedChecks()
	if !read {
		t.Fatal("the reading never landed")
	}
	if len(unread) != 0 {
		t.Fatalf("a check that wrote only ignored paths was thrown away: %q", unread)
	}
	if len(red) != 1 {
		t.Fatalf("the baseline is %q, want the check that was already failing", red)
	}
}

// AND A CHECK WHOSE READING WAS THROWN AWAY IS NEVER COUNTED EITHER WAY.
//
// The reading could not be taken, so nobody knows whose red it is — and an empty
// WasFailing is a clean tree only when nothing was unread. Counting it would send
// a finished run into somebody else's suite; calling it the project's would let
// this run break it in silence. It is left out of the arithmetic entirely.
func TestARedCheckWhoseBaselineWasUnreadIsNotCounted(t *testing.T) {
	steward := budgetLeft(t)
	decision := steward.Decide(Remains{
		Acceptance:   "the suite passes",
		Landed:       true,
		Landings:     []Landing{{ID: 1, Title: "fix it", State: TaskDone, Merged: true, Checked: true}},
		Checks:       []CheckRun{{Command: "python -m pytest tests/", Passed: false, Ran: true}},
		BaselineRead: true,
		Unread:       []string{"python -m pytest tests/"},
	})
	if decision.Verb != DecideDone {
		t.Fatalf("a red check nobody could attribute was counted against the run: %+v", decision)
	}
	// AND THE SAME RED WITH A READING BEHIND IT IS THIS RUN'S OWN.
	ours := steward.Decide(Remains{
		Acceptance:   "the suite passes",
		Landed:       true,
		Landings:     []Landing{{ID: 1, Title: "fix it", State: TaskDone, Merged: true, Checked: true}},
		Checks:       []CheckRun{{Command: "python -m pytest tests/", Passed: false, Ran: true}},
		BaselineRead: true,
	})
	if ours.Verb != DecideCarryOn {
		t.Fatalf("a check this work turned red was not counted: %+v", ours)
	}
}

// AND THE SENTENCES ABOUT THE CHECKS RIDE A READER'S BRIEF TOO.
//
// Where the mark reader supplied the brief, this package's own two sentences
// used to be skipped — so a worker was handed a line about the work with no word
// about which red was already there, and went and fixed somebody else's bug. A
// reader's line is about the WORK and cannot know that.
func TestTheAlreadyRedSentenceRidesAReaderSuppliedBrief(t *testing.T) {
	steward := budgetLeft(t)
	decision := steward.Decide(Remains{
		Reader:       "the scopes are still parsed case-sensitively",
		Acceptance:   "the suite passes",
		Landed:       true,
		Landings:     []Landing{{ID: 1, Title: "fix it", State: TaskFailed, Ending: TaskEndingRefused}},
		Checks:       []CheckRun{{Command: "tox -e py", Passed: false, Ran: true}},
		WasFailing:   []string{"tox -e py"},
		BaselineRead: true,
	})
	if decision.Verb != DecideCarryOn {
		t.Fatalf("a unit the check refused was called finished: %+v", decision)
	}
	if !strings.Contains(decision.Brief, "the scopes are still parsed case-sensitively") {
		t.Fatalf("the brief is not the reader's own line:\n%s", decision.Brief)
	}
	if !strings.Contains(decision.Brief,
		"1 check was already failing before this work and is not counted: tox -e py") {
		t.Fatalf("a reader's brief never says what was already broken:\n%s", decision.Brief)
	}

	// AND THE STILL-READING SENTENCE TOO, on the same road.
	early := steward.Decide(Remains{
		Reader:     "the scopes are still parsed case-sensitively",
		Acceptance: "the suite passes",
		Landed:     true,
		Landings:   []Landing{{ID: 1, Title: "fix it", State: TaskFailed, Ending: TaskEndingRefused}},
		Checks:     []CheckRun{{Command: "tox -e py", Passed: false, Ran: true}},
	})
	if !strings.Contains(early.Brief, baselineStillReading) {
		t.Fatalf("a reader's brief never says the reading is still going:\n%s", early.Brief)
	}
}

// AND WHAT WAS ALREADY BROKEN IS SAID OUT LOUD RATHER THAN SILENTLY DROPPED.
//
// A worker handed a brief that does not mention red it can plainly see will go
// and fix it, which is the same failure wearing the other coat. The sentence
// names how many and which, in a person's words.
func TestTheBriefSaysWhatWasAlreadyFailingBeforeTheWork(t *testing.T) {
	steward := budgetLeft(t)
	decision := steward.Decide(Remains{
		Acceptance: "the suite passes and the repro prints without error",
		Landed:     true,
		Landings: []Landing{
			{ID: 1, Title: "fix the subclass init", State: TaskDone, Merged: true},
			{ID: 2, Title: "write the repro", State: TaskFailed, Ending: TaskEndingRefused},
		},
		Checks: []CheckRun{
			{Command: "tox -e py", Passed: false, Tail: "1 failed", Ran: true},
			{Command: "go build ./...", Passed: false, Ran: true},
		},
		WasFailing:   []string{"tox -e py"},
		BaselineRead: true,
	})
	if decision.Verb != DecideCarryOn {
		t.Fatalf("a unit the check refused was called finished: %+v", decision)
	}
	if strings.Contains(decision.Brief, "tox -e py does not pass") {
		t.Fatalf("the brief asks for red that was there before the work:\n%s", decision.Brief)
	}
	if !strings.Contains(decision.Brief, "go build ./... does not pass") {
		t.Fatalf("the brief does not name the check this work broke:\n%s", decision.Brief)
	}
	if !strings.Contains(decision.Brief,
		"1 check was already failing before this work and is not counted: tox -e py") {
		t.Fatalf("the brief never says what was already broken:\n%s", decision.Brief)
	}
}

// (d) EVERYTHING MET IS THE ONE ANSWER THAT ENDS THE RUN.
func TestAcceptanceMetWithEveryCheckPassingIsDone(t *testing.T) {
	steward := budgetLeft(t)
	decision := steward.Decide(Remains{
		Acceptance: "the parser handles every fixture and `go build ./...` passes",
		Landed:     true,
		Landings:   []Landing{{ID: 1, Title: "port the parser", State: TaskDone}},
		Checks: []CheckRun{
			{Command: "go build ./...", Passed: true},
			{Command: "go test ./...", Passed: true},
		},
	})
	if decision.Verb != DecideDone {
		t.Fatalf("work that is finished was not allowed to finish: %+v", decision)
	}
}

// (g) A SESSION THAT HAS FINISHED NOTHING HAS NOT FINISHED THE ASK.
//
// The eighteen-minute ending, in one assertion: a conversation that talked and
// landed nothing read as met, and there was nothing anywhere to disagree.
func TestASessionThatLandedNothingIsNeverDone(t *testing.T) {
	steward := budgetLeft(t)
	decision := steward.Decide(Remains{
		Said:       "I have looked at the repository and I think the plan is sound.",
		Acceptance: "the parser handles every fixture",
	})
	if decision.Verb == DecideDone {
		t.Fatalf("a session that finished nothing declared the ask met: %+v", decision)
	}
	if !strings.Contains(decision.Brief, "nothing has been finished yet") {
		t.Fatalf("the brief does not say what is missing:\n%s", decision.Brief)
	}
}

// A BUDGET THAT IS SPENT STOPS THE RUN, AND SAYS WHICH CEILING.
func TestAnExhaustedBudgetStopsWithAReason(t *testing.T) {
	steward := NewSteward("port the parser", Budget{Wall: time.Hour, USD: 20}, func() float64 { return 25 })
	decision := steward.Decide(Remains{Reader: "there is plenty left", Landed: true})
	if decision.Verb != DecideStop {
		t.Fatalf("a spent budget did not stop the run: %+v", decision)
	}
	if !strings.Contains(decision.Reason, "$20.00") {
		t.Fatalf("the stop does not say which ceiling was reached: %q", decision.Reason)
	}
}

func TestABudgetNobodySetIsNeverExhausted(t *testing.T) {
	var budget Budget
	if budget.Set() {
		t.Fatal("the zero budget reads as a ceiling")
	}
	if spent, why := budget.Exhausted(); spent {
		t.Fatalf("the zero budget reads as spent: %q", why)
	}
}

// THE HOURS ARE READ OFF A CLOCK THIS TYPE HOLDS, so a run that has been open
// longer than its ceiling stops even if it never spent a penny.
func TestTheWallClockStopsARunThatSpentNothing(t *testing.T) {
	steward := NewSteward("port the parser", Budget{Wall: time.Hour}, nil)
	steward.now = func() time.Time { return steward.started.Add(90 * time.Minute) }
	decision := steward.Decide(Remains{Reader: "there is plenty left", Landed: true})
	if decision.Verb != DecideStop {
		t.Fatalf("a run past its hours did not stop: %+v", decision)
	}
	if !strings.Contains(decision.Reason, "hour") {
		t.Fatalf("the stop does not name the hours: %q", decision.Reason)
	}
}

// THE ACCEPTANCE IS WRITTEN ONCE AND FROZEN, because a done-condition the work
// can rewrite is a done-condition the work grades itself against.
func TestTheSessionAcceptanceIsWrittenOnceAndNeverAgain(t *testing.T) {
	steward := budgetLeft(t)
	if !steward.setAcceptance("every fixture parses") {
		t.Fatal("the first acceptance was refused")
	}
	if steward.setAcceptance("it looks about right to me") {
		t.Fatal("the acceptance was rewritten mid-session")
	}
	if steward.Acceptance() != "every fixture parses" {
		t.Fatalf("the acceptance moved: %q", steward.Acceptance())
	}
}

// AND THE GOAL IS FROZEN WITH IT, for the same reason: the acceptance is
// written against that sentence, and a goal that moved underneath it would
// leave the run measuring its work against a condition for something else.
func TestTheStewardsGoalIsTheFirstThingAsked(t *testing.T) {
	steward := NewSteward("", Budget{Wall: time.Hour}, nil)
	steward.hear("port the parser")
	steward.hear("actually never mind, tidy the imports")
	if steward.Ask() != "port the parser" {
		t.Fatalf("the goal moved under the acceptance: %q", steward.Ask())
	}
}

// ── (e) what the session left lying about ───────────────────────────────────

// THE SWEEP REMOVES SCRATCH AND NOTHING ELSE, and every clause of that sentence
// is a separate way this could do damage.
func TestTheSweepRemovesOnlyWhatTheSessionCreatedOutsideTheDeliverable(t *testing.T) {
	tree := t.TempDir()
	elsewhere := t.TempDir()

	deliverable := filepath.Join(tree, "parser.go")
	scratch := filepath.Join(elsewhere, "fixtures.jsonl")
	modified := filepath.Join(elsewhere, "somebody-elses.txt")
	gone := filepath.Join(elsewhere, "already-removed.txt")
	for _, path := range []string{deliverable, scratch, modified} {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	found := sweepScratch(reconcile([]fileChange{
		{path: deliverable, shown: "parser.go", created: true},
		{path: scratch, shown: scratch, created: true},
		// A file the session only CHANGED never reaches the ledger at all, and
		// this row is here to prove the sweep would not take it if it did.
		{path: modified, shown: modified, created: false},
		{path: gone, shown: gone, created: true},
	}, tree))

	if len(found.removed) != 1 || found.removed[0] != scratch {
		t.Fatalf("the sweep removed the wrong set: %+v", found.removed)
	}
	if len(found.kept) != 1 || found.kept[0] != "parser.go" {
		t.Fatalf("the deliverable was not kept: %+v", found.kept)
	}
	for _, path := range []string{deliverable, modified} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s was removed and must not have been: %v", path, err)
		}
	}
	if _, err := os.Stat(scratch); err == nil {
		t.Fatalf("%s is still there", scratch)
	}
}

// A MODIFIED FILE NEVER ENTERS THE LEDGER, which is the sweep's first line of
// defence and the one that does not depend on anything downstream being right.
func TestTheSessionLedgerHoldsOnlyFilesItCreated(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	made := filepath.Join(workspace, "made.go")
	changed := filepath.Join(workspace, "changed.go")
	agent.rememberCreated(fileChange{path: made, shown: "made.go", created: true})
	agent.rememberCreated(fileChange{path: changed, shown: "changed.go", created: false})
	agent.rememberCreated(fileChange{path: made, shown: "made.go", created: true})
	list := agent.createdList()
	if len(list) != 1 || list[0].path != made {
		t.Fatalf("the ledger is not what the session created: %+v", list)
	}
}

// A PATH THAT IS NOT UNDER THE TREE IS NOT UNDER THE TREE, and a string prefix
// says otherwise for exactly the directory names that are most likely to exist.
func TestUnderTreeIsNotAStringPrefix(t *testing.T) {
	for _, c := range []struct {
		tree, path string
		want       bool
	}{
		{"/work", "/work/parser.go", true},
		{"/work", "/work/deep/parser.go", true},
		{"/work", "/work-2/parser.go", false},
		{"/work", "/tmp/fixtures.jsonl", false},
		{"/work", "/work", true},
	} {
		if got := underTree(c.tree, c.path); got != c.want {
			t.Fatalf("underTree(%q, %q) = %v, want %v", c.tree, c.path, got, c.want)
		}
	}

	real := t.TempDir()
	alias := filepath.Join(t.TempDir(), "tree")
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	if path := filepath.Join(real, "not-made-yet.go"); !underTree(alias, path) {
		t.Fatalf("the canonical tree %q did not cover its symlinked spelling %q", real, alias)
	}
}

// ── (f) no budget is today's session, and one line saying so ────────────────

func TestWithoutABudgetTheSessionKeepsTodaysPrincipalAndSaysSo(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.Unattended = true })
	if agent.steward() != nil {
		t.Fatal("an unattended session with no ceiling was given a goal owner that spends")
	}
	if _, person := agent.who().(*Person); !person {
		t.Fatalf("the principal is not a person: %T", agent.who())
	}
	notice := UnattendedNotice(agent.config)
	if !strings.Contains(notice, "--max-hours") || !strings.Contains(notice, "--max-cost") {
		t.Fatalf("the notice does not say how to get the other thing: %q", notice)
	}
}

func TestWithABudgetTheSessionGetsAStewardAndSaysWhatItIs(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Unattended = true
		c.Budget = Budget{Wall: 6 * time.Hour, USD: 20}
	})
	if agent.steward() == nil {
		t.Fatal("an unattended session with a ceiling has no goal owner")
	}
	notice := UnattendedNotice(agent.config)
	if !strings.Contains(notice, "6 hours") || !strings.Contains(notice, "$20") {
		t.Fatalf("the notice does not say what the ceiling is: %q", notice)
	}
}

// AN ATTENDED SESSION IS TOLD NOTHING, whatever it was given, because none of
// this is about it.
func TestAnAttendedSessionIsShownNoNotice(t *testing.T) {
	if notice := UnattendedNotice(Config{Budget: Budget{Wall: time.Hour}}); notice != "" {
		t.Fatalf("an attended session was shown a line about carrying work on: %q", notice)
	}
}

// AND ITS SPEND IS THE SESSION'S OWN JOURNALED FIGURE — the one rail.go bounds
// against — rather than a second ledger this file keeps.
func TestTheStewardSpendsTheSessionsOwnFigure(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Unattended = true
		c.Budget = Budget{USD: 10}
	})
	agent.mu.Lock()
	agent.usage.CostUSD = 4.5
	agent.mu.Unlock()
	if spent := agent.who().Budget().SpentUSD; spent != 4.5 {
		t.Fatalf("the goal owner is reading a different ledger: %v", spent)
	}
}

// ── the unverified landing has somebody to settle it ────────────────────────

// A CARD ADDRESSED TO NOBODY IS A LANDING THAT WAITS FOREVER. An unattended
// session settles its own unverified work, which is the same rule a headless
// run already had, reached through the goal owner rather than through
// AskConsent alone.
func TestAnUnattendedSessionSettlesItsOwnUnverifiedWork(t *testing.T) {
	watched, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.AskConsent = true })
	if watched.settlePolicy() != TaskSettleAsk {
		t.Fatalf("a watched session stopped asking: %v", watched.settlePolicy())
	}
	unattended, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.AskConsent = true
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	if unattended.settlePolicy() != TaskSettleAuto {
		t.Fatalf("an unattended session is still waiting on a card nobody will answer: %v", unattended.settlePolicy())
	}
}

// ── the standing road ───────────────────────────────────────────────────────

// A CARD ADDRESSED TO A GOAL OWNER IS ANSWERED BY IT.
//
// "Nobody present means no" is the right law for a session somebody walked away
// from and the wrong one for a session somebody deliberately left running with a
// ceiling: it leaves the one road this build has for noticing that work has
// stalled unreachable from exactly the runs that need it. What the yes may cover
// is bounded by [standing.Item.Validate] and by the ticker's own rails, neither
// of which this touches.
func TestAnUnattendedSessionAnswersItsOwnStandingCard(t *testing.T) {
	agent, _ := questionSession(t, "dddd1111dddd2222", func(c *Config) {
		c.Unattended = true
		c.Budget = Budget{Wall: 6 * time.Hour}
	})
	events := watched(agent)

	answer, err := agent.askStanding(context.Background(), standingProbe())
	if err != nil {
		t.Fatalf("the goal owner's own card ended in an error: %v", err)
	}
	if !answer.Approved {
		t.Fatalf("the goal owner did not answer its own card: %+v", answer)
	}
	// AND NO CARD WAS RAISED INTO AN EMPTY ROOM. The proposal event is what a
	// surface draws and what another window is told to go and look at; a session
	// with nobody in it must not produce one it will never take down.
	select {
	case event := <-events:
		t.Fatalf("a card was raised for nobody to answer: %v", event.Kind)
	default:
	}
}

// AND A WATCHED SESSION STILL WAITS FOR THE PERSON, which is the half of this
// law that does not change.
func TestAWatchedSessionStillWaitsForThePersonsAnswer(t *testing.T) {
	agent, _ := questionSession(t, "eeee1111eeee2222", nil)
	events := watched(agent)
	ctx, stop := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := agent.askStanding(ctx, standingProbe()); err == nil {
			t.Error("the card was answered by something other than a person")
		}
	}()
	if event := <-events; event.Kind != EventStandingProposal {
		t.Fatalf("the standing lane sent %v", event.Kind)
	}
	stop()
	<-done
}

func standingProbe() *StandingNotice {
	return &StandingNotice{Item: standing.Item{Words: "tell me when a piece of work stalls"}}
}

// ── #468: WHAT LANDED AND WHAT RAN OUTRANK WHAT WAS SAID ────────────────────

// A LANDING THAT COVERS THE ASK IS FINISHED, WHATEVER A READER STILL HAS TO SAY.
//
// The measured run had its task merged home with every check green and was
// carried on past it for the rest of its wall, because a line a sidecar wrote
// about the TRANSCRIPT sat above the work in the order of the decision. The line
// is still worth having when something really is left; it is not evidence about
// work that is done.
func TestALandingThatCoversTheAskOutranksTheReadersLine(t *testing.T) {
	const reader = "the ledger files still look unfinished to me"
	settled := Remains{
		Said:       "The port is merged and the suite is green.",
		Reader:     reader,
		Acceptance: "the ledger merges home and its own tests pass",
		Landed:     true,
		Landings:   []Landing{{ID: 1, Title: "merge the ledger home", State: TaskDone}},
		Checks:     []CheckRun{{Command: "go test ./ledger", Passed: true}},
	}
	if got := budgetLeft(t).Decide(settled); got.Verb != DecideDone {
		t.Fatalf("a landed, checked ask was carried on over a reader's line: %+v", got)
	}
	// AND A LANDING THAT DID NOT FINISH IS EXACTLY AS IT WAS: the work says
	// something is left, and the reader's own words are what the next attempt
	// opens on because they are the more specific account of it.
	unfinished := settled
	unfinished.Landings = []Landing{{ID: 1, Title: "merge the ledger home", State: TaskFailed}}
	got := budgetLeft(t).Decide(unfinished)
	if got.Verb != DecideCarryOn {
		t.Fatalf("a unit of work that did not finish was called finished: %+v", got)
	}
	if got.Brief != reader {
		t.Fatalf("the carry-on lost the reader's own words:\n got %q\nwant %q", got.Brief, reader)
	}
}

// A STANDSTILL IS A STOP, NOT A THIRD GO.
//
// The evidence that a run is getting somewhere is that what is left CHANGES. The
// measured run wrote one byte-identical brief four times in a hundred seconds and
// then wrote it until its hours were up, because the only counter over it lived
// on the turn's meter and every landing built a fresh one.
func TestTheSameThingLeftTwiceRunningStopsTheRun(t *testing.T) {
	steward := budgetLeft(t)
	stuck := Remains{
		Acceptance: "every handler answers",
		Landed:     true,
		Landings:   []Landing{{ID: 1, Title: "wire the handlers", State: TaskFailed}},
	}
	if first := steward.Decide(stuck); first.Verb != DecideCarryOn {
		t.Fatalf("the first reading of something unfinished did not carry on: %+v", first)
	}
	second := steward.Decide(stuck)
	if second.Verb != DecideStop {
		t.Fatalf("the same thing left twice running was carried on again: %+v", second)
	}
	if !strings.Contains(second.Reason, "wire the handlers did not finish") {
		t.Fatalf("the stop does not say what is still left: %q", second.Reason)
	}
	if !strings.Contains(second.Reason, "saying it again would not change it") {
		t.Fatalf("the stop does not say it stopped rather than repeat itself: %q", second.Reason)
	}
	// AND IT HOLDS AGAINST A FRESH TURN. The floor lives on the goal owner, so
	// nothing a new turn builds for itself can talk it back into working.
	moved := stuck
	moved.Landings = append(moved.Landings, Landing{ID: 2, Title: "port the parser", State: TaskFailed})
	if again := steward.Decide(moved); again.Verb != DecideStop {
		t.Fatalf("a goal owner that stopped at a standstill was restarted: %+v", again)
	}
}

// AND A SET THAT MOVED IS NOT A STANDSTILL, which is the other half: a run
// making progress on a second problem is a run that gets another go.
func TestSomethingNewLeftIsNotAStandstill(t *testing.T) {
	steward := budgetLeft(t)
	stuck := Remains{
		Acceptance: "every handler answers",
		Landed:     true,
		Landings:   []Landing{{ID: 1, Title: "wire the handlers", State: TaskFailed}},
	}
	steward.Decide(stuck)
	moved := stuck
	moved.Landings = append([]Landing{}, stuck.Landings...)
	moved.Landings = append(moved.Landings, Landing{ID: 2, Title: "port the parser", State: TaskFailed})
	got := steward.Decide(moved)
	if got.Verb != DecideCarryOn {
		t.Fatalf("a run with something new left was stopped as a standstill: %+v", got)
	}
	if !strings.Contains(got.Brief, "port the parser did not finish") {
		t.Fatalf("the brief does not name what is newly left:\n%s", got.Brief)
	}
}

// WORK THAT IS STILL RUNNING IS NEITHER DONE NOR A STANDSTILL.
//
// A graph holding one finished unit and one still going used to read exactly like
// a graph holding one finished unit: the unsettled node was dropped on the way out
// and nothing downstream could tell. So a stopped turn could be called finished
// over the top of work that was still moving — and a settled failure beside it
// looked like the same thing twice running while the session was in fact waiting.
func TestWorkStillRunningIsNeitherDoneNorAStandstill(t *testing.T) {
	steward := budgetLeft(t)
	inFlight := Remains{
		Acceptance: "the parser is ported and the tests are written",
		Landed:     true,
		Landings:   []Landing{{ID: 1, Title: "port the parser", State: TaskDone}},
		Running:    []string{"write the tests"},
	}
	first := steward.Decide(inFlight)
	if first.Verb != DecideCarryOn {
		t.Fatalf("an ask with work still going did not carry on: %+v", first)
	}
	if !strings.Contains(first.Brief, "write the tests is still running") {
		t.Fatalf("the brief does not say what is still going:\n%s", first.Brief)
	}
	// AND THE SAME READING AGAIN IS STILL NOT A STANDSTILL, because nothing about
	// it says the session is repeating itself — it says the session is waiting.
	if second := steward.Decide(inFlight); second.Verb != DecideCarryOn {
		t.Fatalf("a session waiting on its own work was stopped as a standstill: %+v", second)
	}
	// AND THE WORK COMING HOME IS WHAT LETS IT FINISH.
	landed := inFlight
	landed.Running = nil
	landed.Landings = append(landed.Landings, Landing{ID: 2, Title: "write the tests", State: TaskDone})
	if got := steward.Decide(landed); got.Verb != DecideDone {
		t.Fatalf("an ask whose work all came home was not allowed to finish: %+v", got)
	}
}

// A DONE ANSWER FORGETS WHAT WAS LEFT LAST TIME.
//
// The floor under carrying on is about ONE STRETCH of it. A session that carried
// on, then finished, then met the same gap again is meeting it for the first time
// since — and a floor that remembered across the finish would stop it on that
// first carry-on and call a run that was working a run that had stalled.
func TestADoneAnswerForgetsWhatWasLeftLastTime(t *testing.T) {
	steward := budgetLeft(t)
	stuck := Remains{
		Acceptance: "every handler answers",
		Landed:     true,
		Landings:   []Landing{{ID: 1, Title: "wire the handlers", State: TaskFailed}},
	}
	if got := steward.Decide(stuck); got.Verb != DecideCarryOn {
		t.Fatalf("the first reading of something unfinished did not carry on: %+v", got)
	}
	if got := steward.Decide(Remains{Landed: true, Landings: []Landing{{ID: 2, State: TaskDone}}}); got.Verb != DecideDone {
		t.Fatalf("a finished ask was not allowed to finish: %+v", got)
	}
	if got := steward.Decide(stuck); got.Verb != DecideCarryOn {
		t.Fatalf("the first carry-on after a finished ask was stopped as a standstill: %+v", got)
	}
	// AND THE FLOOR IS STILL THERE UNDER THE NEW STRETCH: the second identical
	// reading of it stops, exactly as the first stretch's did.
	if got := steward.Decide(stuck); got.Verb != DecideStop {
		t.Fatalf("the floor did not come back under the new stretch: %+v", got)
	}
}
