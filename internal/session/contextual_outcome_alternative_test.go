package session

// COVERAGE for the observed-alternative improvement. The failed acceptance this
// answers: a fresh turn was shown the pandas-import failure but no note that the
// SAME work had already succeeded with the standard library, so it repeated the
// dead import to rediscover the stdlib path. These tests drive the real
// boundary \u2014 [Agent.recordOutcome] for a session and the collector for a
// worker \u2014 and the real read seam, [Agent.priorOutcomeContext], rather than a
// helper assembled for the test.
//
// They prove the mechanism, not model behavior: none of them asserts what a
// model would choose with the line in front of it.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/store"
)

func seedAlternative(t *testing.T, brain *store.Store, owner, goal, action, observation, key, hash, snapshot, altOf string) store.ContextualAttempt {
	t.Helper()
	e := store.ContextualAttempt{
		ID: store.NewMemoryID(), Owner: owner, SessionID: "s", TurnID: "t", Tool: "bash",
		Action: action, Goal: goal, Status: store.AttemptSucceeded, Observation: observation,
		ReceiptIDs: []string{"c"}, Snapshot: snapshot, AlternativeOf: altOf,
		SourceKey: key, SourceHash: hash, ValidFrom: time.Now(),
	}
	row, err := brain.AppendContextualAttempt(e)
	if err != nil {
		t.Fatalf("seed alternative: %v", err)
	}
	return row
}

func seedFailure(t *testing.T, brain *store.Store, owner, goal, action, observation, key, hash, snapshot string) store.ContextualAttempt {
	t.Helper()
	e := store.ContextualAttempt{
		ID: store.NewMemoryID(), Owner: owner, SessionID: "s", TurnID: "t", Tool: "bash",
		Action: action, Goal: goal, Status: store.AttemptFailed, Observation: observation,
		ReceiptIDs: []string{"c"}, Snapshot: snapshot,
		SourceKey: key, SourceHash: hash, ValidFrom: time.Now(),
	}
	row, err := brain.AppendContextualAttempt(e)
	if err != nil {
		t.Fatalf("seed failure: %v", err)
	}
	return row
}

// A FAILED IMPORT AND THE LATER STDLIB SUCCESS FORM ONE PAIR AT THE BOUNDARY,
// and a turn full of further successes does not retain them: only the one
// success that answers the recorded failure is kept.
func TestBoundaryCarriesObservedAlternativeAndKeepsOnlyIt(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
	ctx := context.Background()
	a.prepareBindingContext(ctx, "cross-check week.csv ledger totals and report the exact grand total")
	pre := a.captureSourceSnapshot(ctx).Identity

	fail := delegatedBashCall("f1", `.venv/bin/python -c "import pandas; print(pandas.__version__)"`)
	a.recordOutcome(ctx, 1, fail, toolResult{text: "ModuleNotFoundError: No module named 'pandas'", isError: true}, pre)
	ok := delegatedBashCall("s1", `.venv/bin/python ledger.py week.csv; echo ---; .venv/bin/python -c "import csv; from decimal import Decimal"`)
	a.recordOutcome(ctx, 1, ok, toolResult{text: "food: 0.30\ntravel: 12.05\ngrand total: 12.35"}, pre)

	rows := attemptsForProject(t, brain, a)
	if len(rows) != 2 {
		t.Fatalf("failure+alternative should be exactly two rows, got %+v", rows)
	}
	var failed, succeeded *store.ContextualAttempt
	for i := range rows {
		switch rows[i].Status {
		case store.AttemptFailed:
			failed = &rows[i]
		case store.AttemptSucceeded:
			succeeded = &rows[i]
		}
	}
	if failed == nil || succeeded == nil {
		t.Fatalf("pair was not written: %+v", rows)
	}
	if succeeded.AlternativeOf != failed.SourceKey {
		t.Fatalf("alternative did not name the failure's source key: alt=%q failure=%q", succeeded.AlternativeOf, failed.SourceKey)
	}
	if len(succeeded.ReceiptIDs) == 0 {
		t.Fatalf("a demonstrated success carried no receipt: %+v", succeeded)
	}
	if !strings.Contains(succeeded.Observation, "12.35") {
		t.Fatalf("the alternative's own receipt was not kept: %q", succeeded.Observation)
	}

	// NOT EVERY SUCCESS IS RETAINED: a second relevant success writes nothing.
	more := delegatedBashCall("s2", `.venv/bin/python -c "import csv; print('again')"`)
	a.recordOutcome(ctx, 1, more, toolResult{text: "again"}, pre)
	if again := attemptsForProject(t, brain, a); len(again) != 2 {
		t.Fatalf("a second success was retained past the one bounded alternative: %+v", again)
	}
}

// AN UNRELATED SUCCESS AND A BARE LOOKUP ARE NEVER DRESSED UP AS THE WAY THE
// WORK GOT DONE.
func TestAlternativeRefusesUnrelatedAndLookupSuccesses(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
	ctx := context.Background()
	a.prepareBindingContext(ctx, "make the foobar parser tests pass")
	pre := a.captureSourceSnapshot(ctx).Identity
	a.recordOutcome(ctx, 1, delegatedBashCall("f1", "go test ./foobar"), toolResult{text: "undefined: foobar.Token", isError: true}, pre)

	// A successful read and a successful unrelated bash command: neither is the
	// alternative to the failed action.
	a.recordOutcome(ctx, 1, delegatedReadCall(t, dir, "r1"), toolResult{text: "some file body"}, pre)
	a.recordOutcome(ctx, 1, delegatedBashCall("s1", "make docs"), toolResult{text: "docs built"}, pre)
	if rows := attemptsForProject(t, brain, a); len(rows) != 1 {
		t.Fatalf("noise was retained as an alternative: %+v", rows)
	}
	// The per-rule unit: a lookup is refused, a same-class related action passes.
	if alternativeEligible(delegatedReadCall(t, dir, "r2"), "bash", "bash: go test ./foobar") {
		t.Fatal("a bare read was eligible as an action alternative")
	}
	if !alternativeEligible(delegatedBashCall("s2", "go test ./foobar -run TestToken"), "bash", "bash: go test ./foobar") {
		t.Fatal("the same-class related action was not eligible")
	}
}

// A SUCCESS IS RENDERED ONLY BESIDE ITS FAILURE, WITH ITS OWN CIRCUMSTANCES.
func TestPriorOutcomeRendersObservedAlternativeWithOwnCircumstances(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
	owner := store.OwnerProject("p")
	failed := seedFailure(t, brain, owner, "fix the foobar parser", "bash: go test ./foobar", "undefined: foobar.Token", "k1", "h1", "clean:abc")
	seedAlternative(t, brain, owner, "fix the foobar parser", "bash: go test ./foobar -run TestToken", "PASS foobar 0.12s", "k2", "h2", "clean:abc", failed.SourceKey)

	got := a.priorOutcomeContext("fix the foobar parser", "clean:abc")
	if !strings.Contains(got, "Observed successful alternative") {
		t.Fatalf("the alternative was not rendered beside its failure:\n%s", got)
	}
	if !strings.Contains(got, "succeeded at the tool boundary") {
		t.Fatalf("the alternative was not stated as an observed success:\n%s", got)
	}
	if !strings.Contains(got, "not proof of cause") || !strings.Contains(got, "a source snapshot is not the environment") {
		t.Fatalf("the alternative was not labelled as observation, not cause, and not environment:\n%s", got)
	}
	if !strings.Contains(got, "same source snapshot") {
		t.Fatalf("matching circumstances were not labelled:\n%s", got)
	}
	// A changed source is an invitation, never a ban.
	if changed := a.priorOutcomeContext("fix the foobar parser", "dirty:abc:def"); !strings.Contains(changed, "different source snapshot") {
		t.Fatalf("changed circumstances were not labelled honestly:\n%s", changed)
	}
	if unknown := a.priorOutcomeContext("fix the foobar parser", "unknown"); !strings.Contains(unknown, "circumstances unknown") {
		t.Fatalf("unknown circumstances were not labelled honestly:\n%s", unknown)
	}
	// AN UNRELATED GOAL SEES NOTHING AT ALL.
	if quiet := a.priorOutcomeContext("update the deployment documentation", "clean:abc"); quiet != "" {
		t.Fatalf("an unrelated goal surfaced the pair:\n%s", quiet)
	}
}

// FORGETTING THE FAILURE RETIRES THE PAIR: the original and its alternative
// provenance go together, and a fresh observation of the same work is still new.
func TestForgetRetiresFailureAndItsAlternative(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
	owner := store.OwnerProject("p")
	failed := seedFailure(t, brain, owner, "fix the foobar parser", "bash: go test ./foobar", "undefined: foobar.Token", "k1", "h1", "clean:abc")
	seedAlternative(t, brain, owner, "fix the foobar parser", "bash: go test ./foobar -run TestToken", "PASS", "k2", "h2", "clean:abc", failed.SourceKey)

	title, err := a.Forget("fix the foobar parser")
	if err != nil {
		t.Fatalf("forget: %v", err)
	}
	if title == "" {
		t.Fatal("the pair was not retired")
	}
	if rows := attemptsForProject(t, brain, a); len(rows) != 0 {
		t.Fatalf("forget left the pair visible: %+v", rows)
	}
	if got := a.priorOutcomeContext("fix the foobar parser", "clean:abc"); got != "" {
		t.Fatalf("a forgotten pair was still shown:\n%s", got)
	}
}

// A READ-ONLY BINDING POSTURE WRITES NO ALTERNATIVE, at the session boundary and
// at the collector boundary.
func TestBindingOnlyMemoryWritesNoAlternative(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p"; c.bindingOnlyMemory = true })
	ctx := context.Background()
	a.prepareBindingContext(ctx, "make the foobar parser tests pass")
	pre := a.captureSourceSnapshot(ctx).Identity
	a.recordOutcome(ctx, 1, delegatedBashCall("f1", "go test ./foobar"), toolResult{text: "undefined: foobar.Token", isError: true}, pre)
	a.recordOutcome(ctx, 1, delegatedBashCall("s1", "go test ./foobar -run TestToken"), toolResult{text: "PASS"}, pre)
	if rows := attemptsForProject(t, brain, a); len(rows) != 0 {
		t.Fatalf("a binding-only session wrote an alternative: %+v", rows)
	}

	// The collector boundary refuses the same way.
	worker, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	worker.outcomes = a.outcomes
	worker.origin = delegatedOrigin{Session: a.memorySourceSession(), Owner: store.OwnerProject("p"), Turn: "turn:1", Goal: "make the foobar parser tests pass", Task: "task:1"}
	worker.recordOutcome(ctx, 1, delegatedBashCall("w1", "go test ./foobar"), toolResult{text: "undefined: y", isError: true}, pre)
	worker.recordOutcome(ctx, 1, delegatedBashCall("w2", "go test ./foobar -run TestToken"), toolResult{text: "PASS"}, pre)
	if rows := attemptsForProject(t, brain, a); len(rows) != 0 {
		t.Fatalf("a binding-only collector wrote an alternative: %+v", rows)
	}
}

// A DELEGATED WORKER'S FAILURE ALSO CARRIES ITS LATER OBSERVED ALTERNATIVE,
// through the same frozen origin and the same canonical row.
func TestDelegatedFailureCarriesObservedAlternative(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	root, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
	root.prepareBindingContext(context.Background(), "make the foobar parser tests pass")
	worker := spawnTaskWorker(t, root, dir)
	ctx := context.Background()
	pre := worker.captureSourceSnapshot(ctx).Identity
	worker.recordOutcome(ctx, 0, delegatedBashCall("f1", "go test ./foobar"), toolResult{text: "undefined: foobar.Token", isError: true}, pre)
	worker.recordOutcome(ctx, 0, delegatedBashCall("s1", "go test ./foobar -run TestToken"), toolResult{text: "PASS foobar 0.12s"}, pre)

	rows := attemptsForProject(t, brain, root)
	if len(rows) != 2 {
		t.Fatalf("delegated failure+alternative should be two rows: %+v", rows)
	}
	var failed, succeeded *store.ContextualAttempt
	for i := range rows {
		switch rows[i].Status {
		case store.AttemptFailed:
			failed = &rows[i]
		case store.AttemptSucceeded:
			succeeded = &rows[i]
		}
	}
	if failed == nil || succeeded == nil || succeeded.AlternativeOf != failed.SourceKey {
		t.Fatalf("delegated pair was not linked: %+v", rows)
	}
	if succeeded.TurnID != failed.TurnID || succeeded.Goal != failed.Goal {
		t.Fatalf("the alternative lost the frozen origin: alt=%+v failure=%+v", succeeded, failed)
	}
}

// THE ALTERNATIVE IS IN FRONT OF THE MODEL AT THE FIRST PROVIDER REQUEST OF A
// FRESH TURN, before any action is chosen.
func TestObservedAlternativeBeforeFirstRequestThroughRunTurn(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	script := &reflexScript{}
	a, brain := brainAgent(t, script, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
	owner := store.OwnerProject("p")
	failed := seedFailure(t, brain, owner, "cross-check week.csv ledger totals", "bash: .venv/bin/python -c \"import pandas\"", "ModuleNotFoundError: No module named 'pandas'", "turn:9:f", "hf", "clean:abc")
	seedAlternative(t, brain, owner, "cross-check week.csv ledger totals", "bash: .venv/bin/python ledger.py week.csv", "grand total: 12.35", "turn:9:s", "hs", "clean:abc", failed.SourceKey)

	collect(t, mustSubmit(t, a, "cross-check week.csv ledger totals and report the grand total"))

	script.mu.Lock()
	requests := append([]string(nil), script.requests...)
	script.mu.Unlock()
	found := false
	for _, request := range requests {
		if strings.Contains(request, "Observed successful alternative") && strings.Contains(request, "grand total: 12.35") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no first provider request carried the observed alternative before it chose an action: %v", requests)
	}
}
