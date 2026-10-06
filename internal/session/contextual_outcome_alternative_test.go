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
	"fmt"
	"strings"
	"sync"
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

	// THE RECORDED SHAPE: every bash action in the live ledger sessions begins by
	// cd-ing into the same project directory, so the failure and its genuine
	// alternative share that cwd as well as the work.
	const cwd = "/home/santosh/src/contextual-r2-work-20261005/ledger"
	fail := delegatedBashCall("f1", "cd "+cwd+` && .venv/bin/python -c "import pandas; print(pandas.__version__)"`)
	a.recordOutcome(ctx, 1, fail, toolResult{text: "ModuleNotFoundError: No module named 'pandas'", isError: true}, pre)
	ok := delegatedBashCall("s1", "cd "+cwd+` && .venv/bin/python ledger.py week.csv; echo ---; .venv/bin/python -c "import csv; from decimal import Decimal"`)
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
	more := delegatedBashCall("s2", "cd "+cwd+` && .venv/bin/python -c "import csv; print('again')"`)
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
	if alternativeEligible(delegatedReadCall(t, dir, "r2"), "bash", "bash: go test ./foobar", "make the foobar parser tests pass") {
		t.Fatal("a bare read was eligible as an action alternative")
	}
	if !alternativeEligible(delegatedBashCall("s2", "go test ./foobar -run TestToken"), "bash", "bash: go test ./foobar", "make the foobar parser tests pass") {
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

	events := collect(t, mustSubmit(t, a, "cross-check week.csv ledger totals and report the grand total"))

	// NO TOOL RAN BEFORE THE DECISION. The model answers the turn with no tool
	// call, so nothing it was shown could have come from a tool result; the
	// alternative therefore sat in front of it before any action was chosen.
	for _, event := range events {
		if event.Kind == EventToolBegin || event.Kind == EventToolEnd || event.Kind == EventToolFailed {
			t.Fatalf("a tool ran in a turn whose first request already carried the alternative: %+v", event)
		}
	}

	script.mu.Lock()
	requests := append([]string(nil), script.requests...)
	turns := script.turns
	script.mu.Unlock()
	if len(requests) == 0 || turns != 1 {
		t.Fatalf("expected exactly one turn's first request, got turns=%d requests=%d", turns, len(requests))
	}
	// THE TURN'S ACTUAL REQUEST #1, not merely SOME request of the turn: a later
	// re-request after a tool had run would prove nothing about ordering.
	if !strings.Contains(requests[0], "Observed successful alternative") || !strings.Contains(requests[0], "grand total: 12.35") {
		t.Fatalf("the first provider request did not carry the observed alternative before any action: %q", requests[0])
	}
}

// A1 REGRESSION. The live ledger sessions run every bash action from a shared
// cwd (`cd /home/santosh/src/contextual-r2-work-20261005/ledger && ...`). That
// shared prefix must not by itself make an unrelated bash metadata or lookup
// command the failure's alternative, while the genuine standard-library success
// from the same work stays eligible.
func TestAlternativeRefusesSharedCwdMetadataAndKeepsRealStdlibSuccess(t *testing.T) {
	const cwd = "/home/santosh/src/contextual-r2-work-20261005/ledger"
	const goal = "cross-check week.csv ledger totals and report the exact grand total"
	failed := "cd " + cwd + ` && .venv/bin/python -c "import pandas; print(pandas.__version__)"`
	real := "cd " + cwd + " && .venv/bin/python ledger.py week.csv"

	// A metadata read or a git lookup that shares only the cwd is never the way
	// the work got done, whether it is `ls`/`cat` or `git log`.
	for _, body := range []string{
		"cd " + cwd + " && ls -la && cat week.csv",
		"cd " + cwd + " && git log --oneline -5 && head week.csv",
		"cd " + cwd + " && find . -name '*.csv'",
	} {
		if alternativeEligible(delegatedBashCall("m", body), "bash", "bash: "+failed, goal) {
			t.Errorf("a shell metadata/lookup command sharing only the cwd was eligible: %q", body)
		}
	}
	if !alternativeEligible(delegatedBashCall("s", real), "bash", "bash: "+failed, goal) {
		t.Fatalf("the genuine standard-library alternative was not eligible")
	}

	// At the real boundary the metadata command is refused and the genuine
	// success is the ONE stored alternative.
	dir := t.TempDir()
	initRepo(t, dir)
	a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
	ctx := context.Background()
	a.prepareBindingContext(ctx, goal)
	pre := a.captureSourceSnapshot(ctx).Identity
	a.recordOutcome(ctx, 1, delegatedBashCall("f1", failed), toolResult{text: "ModuleNotFoundError: No module named 'pandas'", isError: true}, pre)
	a.recordOutcome(ctx, 1, delegatedBashCall("m1", "cd "+cwd+" && ls -la && cat week.csv"), toolResult{text: ".git\nweek.csv\nledger.py"}, pre)
	a.recordOutcome(ctx, 1, delegatedBashCall("s1", real), toolResult{text: "grand total: 12.35"}, pre)

	rows := attemptsForProject(t, brain, a)
	succeeded := 0
	var stored string
	for _, row := range rows {
		if row.Status == store.AttemptSucceeded {
			succeeded++
			stored = row.Action
		}
	}
	if succeeded != 1 {
		t.Fatalf("expected exactly one bounded alternative, got %d: %+v", succeeded, rows)
	}
	if stored != "bash: "+real {
		t.Fatalf("the stored alternative was not the genuine success: %q", stored)
	}
}

// A2 REGRESSION. One failure has ONE alternative even when the eligible
// successes arrive concurrently, as they really do from a tool batch (loop.go's
// runToolsWarm runs each call in its own goroutine). Before the repair both
// writers marked the slot only after the journal write, so every sibling
// appended.
func TestConcurrentSiblingSuccessesKeepOneAlternative(t *testing.T) {
	const failed = "go test ./foobar"
	const sibling = "go test ./foobar -run TestToken"
	countSucceeded := func(rows []store.ContextualAttempt) int {
		n := 0
		for _, row := range rows {
			if row.Status == store.AttemptSucceeded {
				n++
			}
		}
		return n
	}

	t.Run("session", func(t *testing.T) {
		dir := t.TempDir()
		initRepo(t, dir)
		a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
		ctx := context.Background()
		a.prepareBindingContext(ctx, "make the foobar parser tests pass")
		pre := a.captureSourceSnapshot(ctx).Identity
		a.recordOutcome(ctx, 1, delegatedBashCall("f1", failed), toolResult{text: "undefined: foobar.Token", isError: true}, pre)
		parallelOutcomes(8, func(i int) {
			a.recordOutcome(ctx, 1, delegatedBashCall(fmt.Sprintf("s%d", i), sibling), toolResult{text: "PASS"}, pre)
		})
		if n := countSucceeded(attemptsForProject(t, brain, a)); n != 1 {
			t.Fatalf("one failure kept %d alternatives under a concurrent tool batch", n)
		}
	})

	t.Run("delegated", func(t *testing.T) {
		dir := t.TempDir()
		initRepo(t, dir)
		root, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
		root.prepareBindingContext(context.Background(), "make the foobar parser tests pass")
		worker := spawnTaskWorker(t, root, dir)
		ctx := context.Background()
		pre := worker.captureSourceSnapshot(ctx).Identity
		worker.recordOutcome(ctx, 0, delegatedBashCall("f1", failed), toolResult{text: "undefined: foobar.Token", isError: true}, pre)
		parallelOutcomes(8, func(i int) {
			worker.recordOutcome(ctx, 0, delegatedBashCall(fmt.Sprintf("s%d", i), sibling), toolResult{text: "PASS"}, pre)
		})
		if n := countSucceeded(attemptsForProject(t, brain, root)); n != 1 {
			t.Fatalf("one delegated failure kept %d alternatives under a concurrent tool batch", n)
		}
	})
}

// parallelOutcomes starts n goroutines that all wait on one barrier and then
// run fn, so the calls really overlap rather than running one after another.
func parallelOutcomes(n int, fn func(i int)) {
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			fn(i)
		}(i)
	}
	close(start)
	wg.Wait()
}
