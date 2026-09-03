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
				ID: 1, Title: "bearer case sensitivity", State: TaskDone, Merged: true,
				Files: []string{"tests/reef_service/test_auth.py", "reef/service/auth.py"},
			},
			{
				ID: 3, Title: "Add focused tests for case-insensitive bearer scheme",
				State: TaskFailed, Ending: TaskEndingUpstream,
				Report: "it ended with an error: API error (404): All providers have been ignored",
				Files:  []string{"tests/reef_service/test_auth.py"},
			},
		},
		Checks: []CheckRun{{Command: "pytest", Passed: true}},
	})
	if decision.Verb != DecideDone {
		t.Fatalf("a finished tree was carried on over a sibling that died on the wire: %+v", decision)
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
	refused := &provider.APIError{Status: 404, Message: "All providers have been ignored"}
	if !terminalProviderFailure(refused) {
		t.Fatal("a provider that refused the request was not read as one")
	}
	if diedOnTheWire(refused) {
		t.Fatal("a terminal refusal was read as a dropped connection")
	}
	// AND AN ORDINARY FAILURE IS NEITHER, so it keeps the ending it had and stays
	// a finding about the work.
	ours := errors.New("mkdir /nowhere/tree: read-only file system")
	if terminalProviderFailure(ours) {
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
				ID: 1, Title: "bearer case sensitivity", State: TaskDone, Merged: true,
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

	// AND THE THREE WAYS IT IS REFUSED. A unit that changed nothing cannot be
	// shown to be done; a landing that finished and could not come home is not
	// the tree holding anything; and half of somebody's work is not their work.
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
				Acceptance: "the existing test suite passes (run `tox -e py`)",
				Landed:     true,
				Landings:   []Landing{{ID: 1, Title: "fix the subclass init", State: TaskDone, Merged: true}},
				Checks:     tc.after,
				WasFailing: tc.before,
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
			{Command: "tox -e py", Passed: false, Tail: "1 failed"},
			{Command: "go build ./...", Passed: false},
		},
		WasFailing: []string{"tox -e py"},
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
