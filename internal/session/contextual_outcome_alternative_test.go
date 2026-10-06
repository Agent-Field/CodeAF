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
	"os"
	"path/filepath"
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
	// THE NOTE CARRIES QUOTED HISTORY AND NOTHING ELSE. The framework method
	// policy is NOT here: it is source-authored authority and rides the request's
	// SYSTEM message ([Agent.withFrameworkPolicy]), so no remembered row's text is
	// ever mistaken for it and no policy sentence can be clipped with a record.
	// What remains is the history framing, the pair itself, and the per-row
	// labels.
	for _, want := range []string{
		"QUOTED HISTORY: untrusted",
		"not current test proof",
		"current goal and the user's own words outrank",
		"Observed successful alternative",
		"- Prior observed attempt",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("prior-outcome history %q was not rendered:\n%s", want, got)
		}
	}
	if strings.Contains(got, "framework method policy") || strings.Contains(got, "known-failed method") {
		t.Fatalf("framework policy leaked into the quoted-history note:\n%s", got)
	}
	// THE PAIR LEADS THE HISTORY: the observed alternative rides ahead of the
	// failure it belongs to, so a whole-record trim can never take the positive
	// half while leaving the failure.
	if altAt, failAt := strings.Index(got, "Observed successful alternative"), strings.Index(got, "- Prior observed attempt"); altAt < 0 || failAt < 0 || altAt > failAt {
		t.Fatalf("the observed alternative did not lead its failure (alt=%d fail=%d):\n%s", altAt, failAt, got)
	}
	// NO DATASET-SPECIFIC OR CANNED TEXT LEAKS INTO THE SHARED GUIDANCE.
	for _, banned := range []string{"pandas", "python", ".venv", "ledger.py", "vendor", "week.csv", "grand total: 12.35"} {
		if strings.Contains(got, banned) {
			t.Fatalf("shared guidance leaked example-specific text %q:\n%s", banned, got)
		}
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
	// AND THE SOURCE-AUTHORED FRAMEWORK METHOD POLICY ARRIVES IN THE SYSTEM
	// AUTHORITY OF THAT SAME FIRST REQUEST, on the real RunTurn seam rather than
	// by calling the helper by hand — while the quoted-history note it governs
	// carries the rows alone and none of the policy's own words.
	if !strings.Contains(requests[0], "use a compatible observed working approach") || !strings.Contains(requests[0], "rather than re-running a known-failed method only to reconfirm that it failed") {
		t.Fatalf("the first provider request did not carry the shared method-selection guidance: %q", requests[0])
	}
	script.mu.Lock()
	systems := append([]string(nil), script.systems...)
	script.mu.Unlock()
	if len(systems) == 0 || !strings.Contains(systems[0], "Framework method policy") {
		t.Fatalf("the first request's SYSTEM message did not carry the framework method policy: %q", systems)
	}
	// THE POLICY IS IN THE SYSTEM MESSAGE, NOT THE USER NOTE: a remembered row's
	// text must never stand where framework policy is trusted.
	note := noteBodyBetween(requests[0], memoryNoteOpening)
	if strings.Contains(note, "Framework method policy") || strings.Contains(note, "known-failed method") {
		t.Fatalf("the framework method policy leaked into the quoted-history note: %q", note)
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

// A3 REGRESSION (exact live boundary). The hosted bbc86b8c2 ledger run stored a
// delegated shell METADATA WRAPPER (cd + echo headers + cat) as the failure's
// sole observed alternative, consuming the one slot before the genuine
// standard-library calculation.
func assertOneStoredAlternative(t *testing.T, rows []store.ContextualAttempt, scope, wantAction string) {
	t.Helper()
	succeeded, failedRows := 0, 0
	var stored, altOf string
	for _, row := range rows {
		switch row.Status {
		case store.AttemptSucceeded:
			succeeded++
			stored, altOf = row.Action, row.AlternativeOf
		case store.AttemptFailed:
			failedRows++
		}
	}
	if failedRows != 1 || succeeded != 1 {
		t.Fatalf("%s: expected one failure and one alternative, got %d/%d: %+v", scope, failedRows, succeeded, rows)
	}
	if stored != wantAction {
		t.Fatalf("%s: the stored alternative was not the genuine stdlib success: %q", scope, stored)
	}
	for _, row := range rows {
		if row.Status == store.AttemptFailed && row.SourceKey != altOf {
			t.Fatalf("%s: the alternative did not name the frozen failure: alt=%q failure=%q", scope, altOf, row.SourceKey)
		}
	}
}

func TestAlternativeRefusesMetadataWrapperAndKeepsStdlibSuccess(t *testing.T) {
	const cwd = "/home/santosh/src/contextual-final-work-20261005/ledger"
	const failed = "cd " + cwd + ` && .venv/bin/python -c 'import pandas; print(pandas.__version__)'`
	const wrapper = "cd " + cwd + ` && echo '===== week.csv =====' && cat week.csv && echo '===== ledger.py =====' && cat ledger.py`
	const real = "cd " + cwd + " && .venv/bin/python ledger.py week.csv --summary"
	const goal = "Independently verify the ledger grand total for week.csv. First diagnose whether this environment can use pandas: run exactly .venv/bin/python -c 'import pandas; print(pandas.__version__)' as a standalone command, with no pipe, appended commands, or error masking. If unavailable, complete the verification using the available standard library and the ledger utility. Read-only."

	if alternativeEligible(delegatedBashCall("m", wrapper), "bash", "bash: "+failed, goal) {
		t.Fatalf("the echo/cat metadata wrapper was eligible as the alternative")
	}
	if !alternativeEligible(delegatedBashCall("s", real), "bash", "bash: "+failed, goal) {
		t.Fatalf("the genuine stdlib calculation was not eligible")
	}

	t.Run("root", func(t *testing.T) {
		dir := t.TempDir()
		initRepo(t, dir)
		a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
		ctx := context.Background()
		a.prepareBindingContext(ctx, goal)
		pre := a.captureSourceSnapshot(ctx).Identity
		a.recordOutcome(ctx, 1, delegatedBashCall("f1", failed), toolResult{text: "ModuleNotFoundError: No module named 'pandas'", isError: true}, pre)
		a.recordOutcome(ctx, 1, delegatedBashCall("m1", wrapper), toolResult{text: "===== week.csv =====\ncategory,amount\n===== ledger.py =====\n#!/usr/bin/env python3"}, pre)
		a.recordOutcome(ctx, 1, delegatedBashCall("s1", real), toolResult{text: "food: 0.30\ntravel: 12.05\nTOTAL: 12.35"}, pre)
		assertOneStoredAlternative(t, attemptsForProject(t, brain, a), "root", "bash: "+real)
	})

	t.Run("delegated", func(t *testing.T) {
		dir := t.TempDir()
		initRepo(t, dir)
		root, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
		root.prepareBindingContext(context.Background(), goal)
		worker := spawnTaskWorker(t, root, dir)
		ctx := context.Background()
		pre := worker.captureSourceSnapshot(ctx).Identity
		worker.recordOutcome(ctx, 0, delegatedBashCall("f1", failed), toolResult{text: "ModuleNotFoundError: No module named 'pandas'", isError: true}, pre)
		worker.recordOutcome(ctx, 0, delegatedBashCall("m1", wrapper), toolResult{text: "===== week.csv =====\ncategory,amount\n===== ledger.py =====\n#!/usr/bin/env python3"}, pre)
		worker.recordOutcome(ctx, 0, delegatedBashCall("s1", real), toolResult{text: "food: 0.30\ntravel: 12.05\nTOTAL: 12.35"}, pre)
		assertOneStoredAlternative(t, attemptsForProject(t, brain, root), "delegated", "bash: "+real)
	})
}

func TestShellMetadataOnlyReadsThroughNavigationAndHeaders(t *testing.T) {
	metadata := []string{
		"cd /tmp && ls -la",
		"cd /home/santosh/src/contextual-final-work-20261005/ledger && ls -la && cat week.csv",
		`cd /home/santosh/src/contextual-final-work-20261005/ledger && echo '===== week.csv =====' && cat week.csv`,
		`printf '%s\n' '===== week.csv =====' && cat week.csv`,
		"git log --oneline -5",
		`plandb list --status ready 2>&1 | head -40`,
		`plandb task notes t-1`,
	}
	for _, body := range metadata {
		if !shellMetadataOnly(body) {
			t.Errorf("a pure lookup/metadata wrapper was not classified as metadata: %q", body)
		}
	}
	work := []string{
		"cd /tmp && go " + "test ./...",
		`cd /home/santosh/src/contextual-final-work-20261005/ledger && .venv/bin/python ledger.py week.csv --summary`,
		`plandb ` + "done --agent" + ` claude ` + "--result" + ` 'finished'`,
	}
	for _, body := range work {
		if shellMetadataOnly(body) {
			t.Errorf("real work was mislabelled as metadata: %q", body)
		}
	}
}

func TestAlternativeRefusesPlanStatusReadAfterFailedDone(t *testing.T) {
	const doneVerb = `plandb ` + "done --agent" + ` claude ` + "--result" + ` 'Distilled answer'`
	const failed = `cd /home/santosh/src/contextual-final-work-20261005/weather-core && git status --short; echo "---"; ls -a; echo "--- done ---"; ` + doneVerb
	const read = `plandb list --status ready 2>&1 | head -40; echo "==="; plandb status --full 2>&1 | head -40`
	const goal = "Read this producer and explain what amount means, then run it. Read-only."
	if alternativeEligible(delegatedBashCall("r", read), "bash", "bash: "+failed, goal) {
		t.Fatalf("a pure plandb list/status read was eligible as the alternative")
	}
	if alternativeEligible(delegatedBashCall("o", `plandb task overview`), "bash", "bash: "+failed, goal) {
		t.Fatalf("a pure plandb task overview read was eligible as the alternative")
	}
	if !alternativeEligible(delegatedBashCall("w", `plandb task note t-1 'amount is cents'`), "bash", "bash: "+failed, goal) {
		t.Fatalf("a plandb write verb was wrongly refused as a read")
	}
}

// The same weather read at the REAL boundary: the failure is the turn's only
// stored row and the status read is not dressed up as its remedy.
func TestBoundaryRefusesPlanStatusReadAsRemedy(t *testing.T) {
	doneVerb := `plandb ` + "done --agent" + ` claude ` + "--result" + ` 'Distilled answer'`
	failed := `cd /home/santosh/src/contextual-final-work-20261005/weather-core && git status --short; echo "---"; ls -a; echo "--- done ---"; ` + doneVerb
	read := `plandb list --status ready 2>&1 | head -40; echo "==="; plandb status --full 2>&1 | head -40`
	dir := t.TempDir()
	initRepo(t, dir)
	a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
	ctx := context.Background()
	a.prepareBindingContext(ctx, "Read this producer and explain what amount means, then run it. Read-only.")
	pre := a.captureSourceSnapshot(ctx).Identity
	a.recordOutcome(ctx, 1, delegatedBashCall("f1", failed), toolResult{text: "error: no running task found for agent 'claude'", isError: true}, pre)
	a.recordOutcome(ctx, 1, delegatedBashCall("r1", read), toolResult{text: "t-1 ready\nt-2 done"}, pre)
	rows := attemptsForProject(t, brain, a)
	if len(rows) != 1 || rows[0].Status != store.AttemptFailed {
		t.Fatalf("a plan status read was stored as the failure's remedy: %+v", rows)
	}
}

// ── THE ACTUAL ADVERSE HOSTED WEATHER CASE (project 50d9421c…) ──────────────
//
// The frozen raw actions of the hosted false pair, with a synthetic innocuous
// receipt ("status listing") standing in for the tool text: the ROW'S PROGRAM
// SHAPE is what is under test, and no printed payload grounds the operation. A
// failing administrative `plandb done` was followed by an unrelated success
// whose action was ONLY introspection — `env | grep` of the agent/task
// variables, an echo divider, `plandb task overview` and `head`. It was stored
// with AlternativeOf naming the failed bookkeeping call even though the
// introspection neither fixed the bookkeeping nor advanced the weather goal.
const (
	weatherAdminFailed = `plandb ` + "done --agent" + ` worker ` + "--result" + ` 'Answer delivered: weather'`
	// THE QUOTED PATTERN CARRIES `|`. A raw text split read the pattern's own
	// words ("task", "plandb") as phantom commands, so the wrapper looked like
	// work and its shared words tied it to the failure.
	weatherIntrospection = `env | grep -i -E 'agent|task|plandb|codeaf' ; echo ---; plandb task overview 2>&1 | head -40`
	weatherGoal          = "Read this weather producer and explain what amount means, then run weather.py with no arguments. Read-only."
)

// A wrapper that is nothing but status/admin introspection is refused as the
// remedy for a failed administrative call, and the failed call itself stays a
// stored observed receipt. Because the wrapper echoes the plan and agent words,
// the pair shares raw tokens: the refusal must come from the introspection
// rule, not from a silent absence of shared words.
func TestAlternativeRefusesWeatherIntrospectionAfterFailedAdminDone(t *testing.T) {
	if !shellMetadataOnly(weatherIntrospection) {
		t.Fatalf("the env/grep/plandb-task-overview wrapper was not read as introspection: %q", weatherIntrospection)
	}
	if alternativeEligible(delegatedBashCall("i", weatherIntrospection), "bash", "bash: "+weatherAdminFailed, weatherGoal) {
		t.Fatalf("an introspection-only wrapper was eligible as the remedy for a failed admin call")
	}
	failedWords, wrapperWords := actionTokens(weatherAdminFailed), actionTokens(weatherIntrospection)
	for _, want := range []string{"plandb", "agent"} {
		if failedWords[want] && wrapperWords[want] {
			continue
		}
		t.Fatalf("the frozen pair no longer shares the raw word %q; the case is not exercised", want)
	}
	if actionWorkTokens(weatherIntrospection)["plandb"] {
		t.Fatalf("a plan-read word survived the introspection filter")
	}

	assertRefused := func(t *testing.T, rows []store.ContextualAttempt) {
		t.Helper()
		if len(rows) != 1 || rows[0].Status != store.AttemptFailed {
			t.Fatalf("introspection was stored as the admin failure's remedy: %+v", rows)
		}
		if !strings.Contains(rows[0].Action, "plandb ") || !strings.Contains(rows[0].Action, "done --agent") {
			t.Fatalf("the failed administrative receipt was not kept: %+v", rows[0])
		}
	}

	t.Run("root", func(t *testing.T) {
		dir := t.TempDir()
		initRepo(t, dir)
		a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
		ctx := context.Background()
		a.prepareBindingContext(ctx, weatherGoal)
		pre := a.captureSourceSnapshot(ctx).Identity
		a.recordOutcome(ctx, 1, delegatedBashCall("f1", weatherAdminFailed), toolResult{text: "error: no running task found for agent 'worker'", isError: true}, pre)
		a.recordOutcome(ctx, 1, delegatedBashCall("i1", weatherIntrospection), toolResult{text: "status listing"}, pre)
		assertRefused(t, attemptsForProject(t, brain, a))
	})

	t.Run("delegated", func(t *testing.T) {
		dir := t.TempDir()
		initRepo(t, dir)
		root, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
		root.prepareBindingContext(context.Background(), weatherGoal)
		worker := spawnTaskWorker(t, root, dir)
		ctx := context.Background()
		pre := worker.captureSourceSnapshot(ctx).Identity
		worker.recordOutcome(ctx, 0, delegatedBashCall("f1", weatherAdminFailed), toolResult{text: "error: no running task found for agent 'worker'", isError: true}, pre)
		worker.recordOutcome(ctx, 0, delegatedBashCall("i1", weatherIntrospection), toolResult{text: "status listing"}, pre)
		assertRefused(t, attemptsForProject(t, brain, root))
	})
}

// Under a concurrent tool batch the same introspection cannot take the one
// alternative slot from genuine work: the wrapper contributes no work token and
// is refused, and the real command that USES the goal-named weather.py is the
// single stored alternative naming the frozen failure.
func TestWeatherIntrospectionDoesNotStealAlternativeUnderRace(t *testing.T) {
	const genuine = ".venv/bin/python weather.py --summary"

	t.Run("root", func(t *testing.T) {
		dir := t.TempDir()
		initRepo(t, dir)
		a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
		ctx := context.Background()
		a.prepareBindingContext(ctx, weatherGoal)
		pre := a.captureSourceSnapshot(ctx).Identity
		a.recordOutcome(ctx, 1, delegatedBashCall("f1", weatherAdminFailed), toolResult{text: "error: no running task found for agent 'worker'", isError: true}, pre)
		parallelOutcomes(8, func(i int) {
			if i%2 == 0 {
				a.recordOutcome(ctx, 1, delegatedBashCall(fmt.Sprintf("i%d", i), weatherIntrospection), toolResult{text: "status listing"}, pre)
				return
			}
			a.recordOutcome(ctx, 1, delegatedBashCall(fmt.Sprintf("s%d", i), genuine), toolResult{text: "rows: 3"}, pre)
		})
		assertRaceKeptGenuine(t, attemptsForProject(t, brain, a), "root", genuine)
	})

	t.Run("delegated", func(t *testing.T) {
		dir := t.TempDir()
		initRepo(t, dir)
		root, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
		root.prepareBindingContext(context.Background(), weatherGoal)
		worker := spawnTaskWorker(t, root, dir)
		ctx := context.Background()
		pre := worker.captureSourceSnapshot(ctx).Identity
		worker.recordOutcome(ctx, 0, delegatedBashCall("f1", weatherAdminFailed), toolResult{text: "error: no running task found for agent 'worker'", isError: true}, pre)
		parallelOutcomes(8, func(i int) {
			if i%2 == 0 {
				worker.recordOutcome(ctx, 0, delegatedBashCall(fmt.Sprintf("i%d", i), weatherIntrospection), toolResult{text: "status listing"}, pre)
				return
			}
			worker.recordOutcome(ctx, 0, delegatedBashCall(fmt.Sprintf("s%d", i), genuine), toolResult{text: "rows: 3"}, pre)
		})
		assertRaceKeptGenuine(t, attemptsForProject(t, brain, root), "delegated", genuine)
	})
}

// assertRaceKeptGenuine checks the one-slot outcome of the concurrent batch:
// exactly one stored success, it is the genuine command, and it names the
// frozen failure's source key.
func assertRaceKeptGenuine(t *testing.T, rows []store.ContextualAttempt, scope, genuine string) {
	t.Helper()
	succeeded := 0
	var stored, altOf, failedKey string
	for _, row := range rows {
		switch row.Status {
		case store.AttemptSucceeded:
			succeeded++
			stored, altOf = row.Action, row.AlternativeOf
		case store.AttemptFailed:
			failedKey = row.SourceKey
		}
	}
	if succeeded != 1 || stored != "bash: "+genuine || failedKey == "" || altOf != failedKey {
		t.Fatalf("%s: the introspection batch did not keep exactly the genuine alternative: n=%d stored=%q altOf=%q failure=%q rows=%+v", scope, succeeded, stored, altOf, failedKey, rows)
	}
}

// ── A4, THE EXACT LIVE FAILURE (session b050dd21f038f7cf) ────────────────────
//
// The canonical row proved only the standalone pandas import FAILED, and no
// succeeded AlternativeOf was written. The old rule required the success to
// share a meaningful token with the FAILED ACTION, but the genuine replacements
// ran a different command and a different library and shared only the turn's own
// GOAL and the goal-named artifact week.csv. These constants are the EXACT
// production actions and the EXACT live prompt from b050dd21f038f7cf — never a
// shortened command invented to share a token, and never an altered goal.
const (
	a4Goal   = "Independently verify the ledger grand total for week.csv in this project's .venv. First diagnose available tools by running exactly .venv/bin/python -c 'import pandas; print(pandas.__version__)' as a standalone command, with no pipes, appended commands or error masking. Then complete an independent calculation and compare it with the ledger utility. Read-only."
	a4Failed = ".venv/bin/python -c 'import pandas; print(pandas.__version__)'"
	a4CSV    = ".venv/bin/python -c '\nimport csv\nfrom decimal import Decimal\ntotal = Decimal(0)\nn = 0\nwith open(\"week.csv\", newline=\"\") as f:\n    for row in csv.DictReader(f):\n        total += Decimal(row[\"amount\"]); n += 1\nprint(\"rows:\", n)\nprint(\"independent grand total:\", total)\n'"
	a4Ledger = ".venv/bin/python ledger.py week.csv; echo \"---\"; .venv/bin/python ledger.py week.csv --summary grandtotal; echo \"---\"; .venv/bin/python ledger.py week.csv --json; echo; .venv/bin/python ledger.py week.csv --summary grandtotal --json"
)

func a4FailureResult() toolResult {
	return toolResult{text: "Traceback (most recent call last):\n  File \"<string>\", line 1, in <module>\nModuleNotFoundError: No module named 'pandas'\n\n\nCommand exited with code 1", isError: true}
}

// A STANDALONE DIAGNOSTIC FAILURE IS PAIRED WITH THE ACTUAL CSV/DECIMAL SUCCESS
// under the SAME frozen goal, at BOTH the session and the delegated boundary,
// even though the two commands share no library or cwd token. The assertion is
// that the observed computation was stored as the one alternative — not a claim
// that any causal learning is certain.
func TestStandaloneFailurePairsActualCSVDecimalSuccess(t *testing.T) {
	t.Run("root", func(t *testing.T) {
		dir := t.TempDir()
		initRepo(t, dir)
		a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
		ctx := context.Background()
		a.prepareBindingContext(ctx, a4Goal)
		pre := a.captureSourceSnapshot(ctx).Identity
		// The exact live batch: the failed diagnostic, an ls and two reads that
		// are never the way the work got done, then the two genuine calculations.
		a.recordOutcome(ctx, 1, delegatedBashCall("call_01a10f425f0e76c693856c10", a4Failed), a4FailureResult(), pre)
		a.recordOutcome(ctx, 1, delegatedBashCall("call_01a10f425f49743898e5a9d3", "ls -la"), toolResult{text: ".git/\n.gitignore\n.venv/\n__pycache__/\nledger.py\ntest_ledger.py\nweek.csv"}, pre)
		a.recordOutcome(ctx, 1, delegatedReadCall(t, "ledger.py", "call_01a10f4260fb76d1ba853b18"), toolResult{text: "#!/usr/bin/env python3\n\"\"\"Summarize a CSV spending ledger by category.\"\"\""}, pre)
		a.recordOutcome(ctx, 1, delegatedReadCall(t, "week.csv", "call_01a10f42612d746383a2dc8b"), toolResult{text: "category,amount\nfood,0.1\nfood,0.2\ntravel,12.05\n"}, pre)
		a.recordOutcome(ctx, 1, delegatedBashCall("call_01a10f4263637542833ad7e6", a4CSV), toolResult{text: "rows: 3\nindependent grand total: 12.35\n"}, pre)
		// A second genuine success in the same batch is NOT retained.
		a.recordOutcome(ctx, 1, delegatedBashCall("call_01a10f4263f670718eb7ac57", a4Ledger), toolResult{text: "food: 0.30\ntravel: 12.05\n---\ngrand total: 12.35"}, pre)

		rows := attemptsForProject(t, brain, a)
		if len(rows) != 2 {
			t.Fatalf("the standalone failure and its one actual success should be two rows, got %+v", rows)
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
		if failed.Action != "bash: "+a4Failed {
			t.Fatalf("the stored failure was not the exact standalone command: %q", failed.Action)
		}
		if succeeded.AlternativeOf != failed.SourceKey {
			t.Fatalf("the actual CSV/Decimal success did not name the standalone failure: alt=%q failure=%q", succeeded.AlternativeOf, failed.SourceKey)
		}
		if !strings.HasPrefix(succeeded.Action, "bash: .venv/bin/python -c '\nimport csv") || strings.Contains(succeeded.Action, "ledger.py") {
			t.Fatalf("the stored alternative was not the actual CSV/Decimal calculation: %q", succeeded.Action)
		}
		if succeeded.Goal != failed.Goal || succeeded.TurnID != failed.TurnID {
			t.Fatalf("the alternative lost the frozen goal/turn: alt=%+v failure=%+v", succeeded, failed)
		}
		if !strings.Contains(succeeded.Observation, "12.35") {
			t.Fatalf("the alternative's own receipt was not kept: %q", succeeded.Observation)
		}
	})

	t.Run("delegated", func(t *testing.T) {
		dir := t.TempDir()
		initRepo(t, dir)
		root, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
		root.prepareBindingContext(context.Background(), a4Goal)
		worker := spawnTaskWorker(t, root, dir)
		ctx := context.Background()
		pre := worker.captureSourceSnapshot(ctx).Identity
		worker.recordOutcome(ctx, 0, delegatedBashCall("call_01a10f425f0e76c693856c10", a4Failed), a4FailureResult(), pre)
		worker.recordOutcome(ctx, 0, delegatedBashCall("call_01a10f425f49743898e5a9d3", "ls -la"), toolResult{text: ".venv/\nledger.py\nweek.csv"}, pre)
		worker.recordOutcome(ctx, 0, delegatedBashCall("call_01a10f4263637542833ad7e6", a4CSV), toolResult{text: "rows: 3\nindependent grand total: 12.35\n"}, pre)
		worker.recordOutcome(ctx, 0, delegatedBashCall("call_01a10f4263f670718eb7ac57", a4Ledger), toolResult{text: "grand total: 12.35"}, pre)

		rows := attemptsForProject(t, brain, root)
		if len(rows) != 2 {
			t.Fatalf("delegated standalone failure+alternative should be two rows, got %+v", rows)
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
		if !strings.HasPrefix(succeeded.Action, "bash: .venv/bin/python -c '\nimport csv") {
			t.Fatalf("the delegated stored alternative was not the actual CSV/Decimal calculation: %q", succeeded.Action)
		}
	})
}

// A GENUINE CALCULATION OVER THE GOAL-NAMED FILE PAIRS EVEN WITH NO FRIENDLY
// LABEL TEXT, because the grounding is the artifact it operates on; a command
// that merely ECHOES the goal's words names no file and is refused.
func TestGoalFileOperandGroundsReplacementNotEchoedProse(t *testing.T) {
	const silentCSV = ".venv/bin/python -c '\nimport csv, decimal\nwith open(\"week.csv\") as f:\n    print(sum(decimal.Decimal(r[1]) for r in list(csv.reader(f))[1:]))\n'"
	const echoProse = `.venv/bin/python -c 'print("independent grand total")'`
	const cwdOnly = `cd /home/santosh/src/contextual-verified-work-20261005/ledger && .venv/bin/python -c 'print(1)'`

	// The grounding is the goal-named FILE, not a label: the calculation pairs
	// with no "grand total" text anywhere.
	if !alternativeEligible(delegatedBashCall("s", silentCSV), "bash", "bash: "+a4Failed, a4Goal) {
		t.Fatal("a genuine calculation over the goal-named file was not eligible without friendly label text")
	}
	// Echoing the goal's prose names no file and is refused.
	if alternativeEligible(delegatedBashCall("e", echoProse), "bash", "bash: "+a4Failed, a4Goal) {
		t.Fatal("a command that only echoed the goal's prose was eligible as the alternative")
	}
	// The shared cwd's own directory word ("ledger") is not a file operand, so a
	// no-operand probe under that cwd stays refused.
	if alternativeEligible(delegatedBashCall("d", cwdOnly), "bash", "bash: "+a4Failed, a4Goal) {
		t.Fatal("a no-operand probe under the shared cwd was eligible as the alternative")
	}
	// A PATH-QUALIFIED reference to the goal's file still grounds it: the file's
	// own name is stronger than the shared cwd, and must not be discarded as if
	// it were merely part of that cwd.
	const work = "/home/santosh/src/contextual-verified-work-20261005/ledger"
	absolute := "cd " + work + " && .venv/bin/python -c '\nimport csv, decimal\nf = open(\"/home/santosh/src/contextual-verified-work-20261005/ledger/week.csv\")\nprint(sum(decimal.Decimal(r[1]) for r in list(csv.reader(f))[1:]))\n'"
	if !alternativeEligible(delegatedBashCall("a", absolute), "bash", "bash: "+a4Failed, a4Goal, work) {
		t.Fatal("a path-qualified reference to the goal-named file did not ground the replacement")
	}
	// AN UNRELATED FILE WITH THE SAME BASE NAME UNDER ANOTHER DIRECTORY IS A
	// DIFFERENT FILE: the identity is the lexical path inside the effective
	// directory, never the base name alone.
	other := "cd " + work + " && .venv/bin/python -c '\nimport csv, decimal\nf = open(\"/other/project/week.csv\")\nprint(list(csv.reader(f)))\n'"
	if alternativeEligible(delegatedBashCall("o", other), "bash", "bash: "+a4Failed, a4Goal, work) {
		t.Fatal("an unrelated same-basename file under another directory grounded the replacement")
	}
	// WITH NO WORKSPACE THE RELATIVE GOAL AND AN ABSOLUTE PATH CANNOT BE PROVEN
	// TO NAME THE SAME FILE, so the comparison FAILS CLOSED.
	if alternativeEligible(delegatedBashCall("a", absolute), "bash", "bash: "+a4Failed, a4Goal) {
		t.Fatal("an ambiguous absolute path grounded the pairing with no workspace")
	}
	// A QUOTED GOAL NAME WITH SPACES SURVIVES: it is kept whole, not split into
	// two words, and the genuine open() over it is a real operand use.
	const spacedGoal = "verify the totals for \"weekly report.csv\" and report them"
	spaced := ".venv/bin/python -c '\nimport csv, decimal\nf = open(\"weekly report.csv\")\nprint(sum(decimal.Decimal(r[1]) for r in list(csv.reader(f))[1:]))\n'"
	if !alternativeEligible(delegatedBashCall("sp", spaced), "bash", "bash: "+a4Failed, spacedGoal) {
		t.Fatal("a quoted goal file name with spaces did not ground the genuine calculation")
	}
}

// A GOAL THAT NAMES SEVERAL ARTIFACTS DOES NOT TIE A NO-INPUT FAILURE TO A
// SUCCESS THAT MERELY USES ONE OF THEM. With two files in the goal — the live
// mixed shape "validate parser.py and total week.csv" — "the success touched
// one of the goal's files" is no evidence that it answered a failure that named
// no input at all; the pairing fails CLOSED. The unambiguous single-artifact
// goal still pairs on the identical shape, so the genuine CSV fixture is kept.
func TestGoalFileTieRefusesMultiArtifactGoal(t *testing.T) {
	const mixedGoal = "validate parser.py and total week.csv and report the grand total"
	const singleGoal = "total week.csv and report the grand total"
	if alternativeEligible(delegatedBashCall("s", a4CSV), "bash", "bash: "+a4Failed, mixedGoal) {
		t.Fatal("a success over one of several goal files tied a no-input failure")
	}
	if !alternativeEligible(delegatedBashCall("s", a4CSV), "bash", "bash: "+a4Failed, singleGoal) {
		t.Fatal("the single-artifact goal stopped tying the genuine success")
	}
	// A success that DEMONSTRATED THE WHOLE GOAL — it uses EVERY file the goal
	// names — is not "one of many": the live compound pair (week.csv AND
	// ledger.py) still ties a no-input failure.
	const whole = ".venv/bin/python calc.py parser.py week.csv"
	if !alternativeEligible(delegatedBashCall("s", whole), "bash", "bash: "+a4Failed, mixedGoal) {
		t.Fatal("a success that used every goal-named file stopped tying")
	}
	// A shared meaningful action token is the STRONGER tie and still pairs even
	// when the goal names several files.
	const shared = "parse parser.py and total week.csv"
	const sharedFailed = "python parse.py parser.py"
	const sharedSuccess = "python parse.py parser.py --week week.csv"
	if !alternativeEligible(delegatedBashCall("s", sharedSuccess), "bash", "bash: "+sharedFailed, shared) {
		t.Fatal("a shared action token stopped tying across a multi-artifact goal")
	}
}

// AN UNRELATED COMMAND, A BARE METADATA LOOKUP AND A CHECK-ONLY PROBE ARE ALL
// REFUSED as the alternative to the standalone import failure.
func TestAlternativeRefusesUnrelatedMetadataAndCheckOnlySuccess(t *testing.T) {
	const unrelated = "make docs"
	const metadata = "ls -la"

	// The per-rule unit: the genuine replacement passes; each negative does not.
	if !alternativeEligible(delegatedBashCall("s", a4CSV), "bash", "bash: "+a4Failed, a4Goal) {
		t.Fatal("the actual CSV/Decimal calculation was not eligible across the frozen goal")
	}
	if alternativeEligible(delegatedBashCall("u", unrelated), "bash", "bash: "+a4Failed, a4Goal) {
		t.Fatal("an unrelated command was eligible as the alternative")
	}
	if alternativeEligible(delegatedBashCall("m", metadata), "bash", "bash: "+a4Failed, a4Goal) {
		t.Fatal("a bare metadata lookup was eligible as the alternative")
	}
	// The check-only probe is a successful REPLAY of the failed diagnosis: it
	// confirms the environment, it does not do the work.
	if alternativeEligible(delegatedBashCall("c", a4Failed), "bash", "bash: "+a4Failed, a4Goal) {
		t.Fatal("a check-only probe replay was eligible as the alternative")
	}
	if alternativeEligible(delegatedBashCall("c2", ".venv/bin/python -c 'import pandas'"), "bash", "bash: "+a4Failed, a4Goal) {
		t.Fatal("a check-only import probe was eligible as the alternative")
	}

	// At the REAL boundary: the unrelated command, the bare lookup and the
	// successful check-only probe write nothing; only the genuine success does.
	dir := t.TempDir()
	initRepo(t, dir)
	a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
	ctx := context.Background()
	a.prepareBindingContext(ctx, a4Goal)
	pre := a.captureSourceSnapshot(ctx).Identity
	a.recordOutcome(ctx, 1, delegatedBashCall("f1", a4Failed), a4FailureResult(), pre)
	a.recordOutcome(ctx, 1, delegatedBashCall("u1", unrelated), toolResult{text: "docs built"}, pre)
	a.recordOutcome(ctx, 1, delegatedBashCall("m1", metadata), toolResult{text: ".venv\nledger.py\nweek.csv"}, pre)
	a.recordOutcome(ctx, 1, delegatedBashCall("c1", a4Failed), toolResult{text: "2.2.3"}, pre)
	if rows := attemptsForProject(t, brain, a); len(rows) != 1 || rows[0].Status != store.AttemptFailed {
		t.Fatalf("noise or a check-only probe was stored as the standalone failure's alternative: %+v", rows)
	}
	a.recordOutcome(ctx, 1, delegatedBashCall("s1", a4CSV), toolResult{text: "independent grand total: 12.35"}, pre)
	rows := attemptsForProject(t, brain, a)
	if len(rows) != 2 {
		t.Fatalf("the genuine success was not paired after the refused noise: %+v", rows)
	}
}

// ONE FAILURE KEEPS ONE ALTERNATIVE when the genuine replacements arrive as a
// concurrent tool batch, exactly as the live run issued them.
func TestStandaloneFailureKeepsOneAlternativeUnderConcurrentBatch(t *testing.T) {
	countSucceeded := func(rows []store.ContextualAttempt) int {
		n := 0
		for _, row := range rows {
			if row.Status == store.AttemptSucceeded {
				n++
			}
		}
		return n
	}
	both := []string{a4CSV, a4Ledger}

	t.Run("root", func(t *testing.T) {
		dir := t.TempDir()
		initRepo(t, dir)
		a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
		ctx := context.Background()
		a.prepareBindingContext(ctx, a4Goal)
		pre := a.captureSourceSnapshot(ctx).Identity
		a.recordOutcome(ctx, 1, delegatedBashCall("f1", a4Failed), a4FailureResult(), pre)
		parallelOutcomes(8, func(i int) {
			a.recordOutcome(ctx, 1, delegatedBashCall(fmt.Sprintf("s%d", i), both[i%2]), toolResult{text: "grand total: 12.35"}, pre)
		})
		if n := countSucceeded(attemptsForProject(t, brain, a)); n != 1 {
			t.Fatalf("the standalone failure kept %d alternatives under a concurrent batch", n)
		}
	})

	t.Run("delegated", func(t *testing.T) {
		dir := t.TempDir()
		initRepo(t, dir)
		root, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
		root.prepareBindingContext(context.Background(), a4Goal)
		worker := spawnTaskWorker(t, root, dir)
		ctx := context.Background()
		pre := worker.captureSourceSnapshot(ctx).Identity
		worker.recordOutcome(ctx, 0, delegatedBashCall("f1", a4Failed), a4FailureResult(), pre)
		parallelOutcomes(8, func(i int) {
			worker.recordOutcome(ctx, 0, delegatedBashCall(fmt.Sprintf("s%d", i), both[i%2]), toolResult{text: "grand total: 12.35"}, pre)
		})
		if n := countSucceeded(attemptsForProject(t, brain, root)); n != 1 {
			t.Fatalf("the delegated standalone failure kept %d alternatives under a concurrent batch", n)
		}
	})
}

// AN OWNER/PROJECT TRANSITION DROPS THE PENDING FAILED IDENTITY: a success under
// a new owner is never paired to a failure recorded under the old one, and the
// immutable failure receipt in the journal is left untouched.
func TestOwnerTransitionInvalidatesPendingFailurePairing(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
	ctx := context.Background()
	a.prepareBindingContext(ctx, a4Goal)
	pre := a.captureSourceSnapshot(ctx).Identity
	a.recordOutcome(ctx, 1, delegatedBashCall("f1", a4Failed), a4FailureResult(), pre)

	// The lifecycle anchor path calls this exactly where the owner changes.
	a.invalidateOutcomePairing()

	a.recordOutcome(ctx, 1, delegatedBashCall("s1", a4CSV), toolResult{text: "independent grand total: 12.35"}, pre)
	rows := attemptsForProject(t, brain, a)
	if len(rows) != 1 || rows[0].Status != store.AttemptFailed {
		t.Fatalf("a success was paired after an owner transition, or the failure was rewritten: %+v", rows)
	}
	if !strings.Contains(rows[0].Observation, "pandas") {
		t.Fatalf("the immutable failure receipt was altered: %q", rows[0].Observation)
	}
}

// ── A4 HARDENING: the printed/echoed goal file, masked probes and metadata ──
//
// The independent review reproduced a false positive the A4 repair left open:
// a command that only PRINTS the goal file name was stored as the failure's
// AlternativeOf and could steal the one slot before the genuine work. These
// tests pin the narrowing: a goal-named file grounds the pairing only when the
// success really USES it — a genuine open(), a bare file operand or a read
// redirect — never when the name merely appears inside printed prose, an echoed
// heredoc, a package-metadata read or a masked check-only probe.

const a4Echo = `.venv/bin/python -c 'print("week.csv")'`

func TestAlternativeRefusesEchoedGoalFileAndMaskedProbes(t *testing.T) {
	const work = "/home/santosh/src/contextual-verified-work-20261005/ledger"
	failed := "bash: " + a4Failed

	refuse := []struct {
		name string
		body string
	}{
		{"print echo of the goal file", a4Echo},
		{"double-quoted print echo", `.venv/bin/python -c "print('week.csv')"`},
		{"print echo of an absolute path", `.venv/bin/python -c 'print("/other/week.csv")'`},
		{"unrelated absolute file opened", `.venv/bin/python -c 'open("/other/project/week.csv")'`},
		{"multiline prose that also prints the name", `.venv/bin/python -c 'import sys\nprint("checking")\nprint("week.csv")'`},
		{"an open() hidden inside a printed string", `.venv/bin/python -c 'print("open('week.csv')")'`},
		{"an open() hidden in a double-quoted print string", `.venv/bin/python -c "print(\"open('week.csv')\")"`},
		{"an open() only in a comment", ".venv/bin/python -c '# open(\"week.csv\")\nprint(1)'"},
		{"heredoc that only echoes the name", ".venv/bin/python - <<'EOF'\nprint(\"week.csv\")\nEOF"},
		{"heredoc that only imports (check-only)", ".venv/bin/python - <<'EOF'\nimport pandas\nprint(pandas.__version__)\nEOF"},
		{"pip show metadata", "pip show pandas"},
		{"python -m pip show metadata", ".venv/bin/python -m pip show pandas"},
		{"masked failure with || true", `.venv/bin/python -c 'import pandas' || true`},
		{"masked failure with false ||", `false || .venv/bin/python -c 'import pandas'`},
		{"appended echo after the probe", `.venv/bin/python -c 'import pandas' && echo done`},
		{"redirect to the null device", `.venv/bin/python -c 'import pandas' 2>/dev/null`},
	}
	for _, tc := range refuse {
		if alternativeEligible(delegatedBashCall("r", tc.body), "bash", failed, a4Goal, work) {
			t.Errorf("%s was eligible as the alternative: %q", tc.name, tc.body)
		}
	}

	// THE GENUINE WORK STILL PAIRS. The stdlib calculation opens the goal file
	// for real, and the ledger utility receives it as a bare operand.
	if !alternativeEligible(delegatedBashCall("s", a4CSV), "bash", failed, a4Goal, work) {
		t.Fatal("the genuine CSV/Decimal calculation was refused")
	}
	if !alternativeEligible(delegatedBashCall("l", a4Ledger), "bash", failed, a4Goal, work) {
		t.Fatal("the genuine ledger utility run was refused")
	}
	// THE COMPATIBILITY BOUNDARY: with no workspace the relative goal file still
	// grounds a relative open(), while an unrelated absolute name cannot.
	if !alternativeEligible(delegatedBashCall("s", a4CSV), "bash", failed, a4Goal) {
		t.Fatal("the relative genuine calculation was refused with no workspace")
	}
	if alternativeEligible(delegatedBashCall("o", `.venv/bin/python -c 'open("/other/project/week.csv")'`), "bash", failed, a4Goal) {
		t.Fatal("an unrelated same-basename absolute file grounded the pairing with no workspace")
	}
}

// A PRINT ECHO OF THE GOAL FILE MUST NOT CONSUME THE ONE SLOT: the genuine
// calculation that follows is the stored alternative at the real root boundary.
func TestBoundaryEchoDoesNotStealAlternativeSlot(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
	ctx := context.Background()
	a.prepareBindingContext(ctx, a4Goal)
	pre := a.captureSourceSnapshot(ctx).Identity
	a.recordOutcome(ctx, 1, delegatedBashCall("f1", a4Failed), a4FailureResult(), pre)
	// The cheap echo arrives first...
	a.recordOutcome(ctx, 1, delegatedBashCall("e1", a4Echo), toolResult{text: "week.csv"}, pre)
	// ...and the genuine CSV/Decimal work still gets the slot.
	a.recordOutcome(ctx, 1, delegatedBashCall("s1", a4CSV), toolResult{text: "rows: 3\nindependent grand total: 12.35"}, pre)

	rows := attemptsForProject(t, brain, a)
	if len(rows) != 2 {
		t.Fatalf("expected one failure and one alternative, got %+v", rows)
	}
	var stored string
	for _, row := range rows {
		if row.Status == store.AttemptSucceeded {
			stored = row.Action
		}
	}
	if !strings.HasPrefix(stored, "bash: .venv/bin/python -c '\nimport csv") {
		t.Fatalf("the stored alternative was not the genuine calculation: %q", stored)
	}
}

// A METADATA READ, THEN THE GENUINE WORK: the metadata success
// writes nothing and the legitimate alternative is still kept.
func TestBoundaryMetadataThenLegitimateKeepsAlternative(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
	ctx := context.Background()
	a.prepareBindingContext(ctx, a4Goal)
	pre := a.captureSourceSnapshot(ctx).Identity
	a.recordOutcome(ctx, 1, delegatedBashCall("f1", a4Failed), a4FailureResult(), pre)
	a.recordOutcome(ctx, 1, delegatedBashCall("m1", "pip show pandas"), toolResult{text: "Name: pandas\nVersion: 2.2.3"}, pre)
	a.recordOutcome(ctx, 1, delegatedBashCall("e1", a4Echo), toolResult{text: "week.csv"}, pre)
	a.recordOutcome(ctx, 1, delegatedBashCall("l1", "ls -la"), toolResult{text: "week.csv\nledger.py"}, pre)
	a.recordOutcome(ctx, 1, delegatedBashCall("s1", a4Ledger), toolResult{text: "grand total: 12.35"}, pre)

	rows := attemptsForProject(t, brain, a)
	if len(rows) != 2 {
		t.Fatalf("expected one failure and one alternative, got %+v", rows)
	}
	for _, row := range rows {
		if row.Status == store.AttemptSucceeded && !strings.Contains(row.Action, "ledger.py week.csv") {
			t.Fatalf("a metadata/echo success was stored instead of the genuine work: %q", row.Action)
		}
	}
}

// THE DELEGATED BOUNDARY REFUSES THE SAME ECHOES and keeps the genuine work.
func TestDelegatedBoundaryEchoDoesNotStealAlternativeSlot(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	root, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
	root.prepareBindingContext(context.Background(), a4Goal)
	worker := spawnTaskWorker(t, root, dir)
	ctx := context.Background()
	pre := worker.captureSourceSnapshot(ctx).Identity
	worker.recordOutcome(ctx, 0, delegatedBashCall("f1", a4Failed), a4FailureResult(), pre)
	worker.recordOutcome(ctx, 0, delegatedBashCall("e1", a4Echo), toolResult{text: "week.csv"}, pre)
	worker.recordOutcome(ctx, 0, delegatedBashCall("m1", `.venv/bin/python -c 'import pandas' || true`), toolResult{text: "2.2.3"}, pre)
	worker.recordOutcome(ctx, 0, delegatedBashCall("s1", a4CSV), toolResult{text: "rows: 3\nindependent grand total: 12.35"}, pre)

	rows := attemptsForProject(t, brain, root)
	if len(rows) != 2 {
		t.Fatalf("expected one delegated failure and one alternative, got %+v", rows)
	}
	for _, row := range rows {
		if row.Status == store.AttemptSucceeded && !strings.HasPrefix(row.Action, "bash: .venv/bin/python -c '\nimport csv") {
			t.Fatalf("a delegated echo/masked probe was stored instead of the genuine work: %q", row.Action)
		}
	}
}

// ONE SLOT UNDER A RACE OF ECHOES AND GENUINE WORK: however the batch
// interleaves, exactly one alternative is stored and it is the genuine one.
func TestConcurrentEchoesAndGenuineKeepOneGenuineAlternative(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
	ctx := context.Background()
	a.prepareBindingContext(ctx, a4Goal)
	pre := a.captureSourceSnapshot(ctx).Identity
	a.recordOutcome(ctx, 1, delegatedBashCall("f1", a4Failed), a4FailureResult(), pre)
	batch := []string{a4Echo, a4CSV, a4Echo, a4Ledger, a4Echo, a4CSV}
	parallelOutcomes(len(batch), func(i int) {
		a.recordOutcome(ctx, 1, delegatedBashCall(fmt.Sprintf("b%d", i), batch[i]), toolResult{text: "grand total: 12.35"}, pre)
	})

	rows := attemptsForProject(t, brain, a)
	succeeded := 0
	for _, row := range rows {
		if row.Status != store.AttemptSucceeded {
			continue
		}
		succeeded++
		if !strings.Contains(row.Action, "import csv") && !strings.Contains(row.Action, "ledger.py week.csv") {
			t.Fatalf("an echo stole the slot under the race: %q", row.Action)
		}
	}
	if succeeded != 1 {
		t.Fatalf("expected exactly one stored alternative under the race, got %d: %+v", succeeded, rows)
	}
}

// THE EFFECTIVE DIRECTORY IS TRACKED PER SEGMENT: a `cd` re-bases the relative
// operands that follow, so `cd /other/project && python script.py week.csv` is
// NOT the workspace's week.csv, while `cd <workspace> && ... week.csv` still is.
func TestGoalFileIdentityTracksEffectiveDirectory(t *testing.T) {
	const work = "/home/santosh/src/contextual-r2-work-20261005/ledger"
	const goal = "cross-check week.csv ledger totals and report the exact grand total"
	const failed = "bash: " + a4Failed

	refuse := []struct {
		name string
		body string
	}{
		{"cd into an unrelated directory", "cd /other/project && .venv/bin/python script.py week.csv"},
		{"cd into a subdirectory of the workspace", "cd " + work + "/sub && .venv/bin/python script.py week.csv"},
		{"a bare cd with an unknown base", "cd && .venv/bin/python script.py week.csv"},
		{"an unrelated cd with an open()", `cd /other/project && .venv/bin/python -c 'open("week.csv")'`},
		{"an unrelated cd behind a wrapper", "sudo cd /other/project && .venv/bin/python script.py week.csv"},
		{"pushd into an unrelated directory", "pushd /other/project && .venv/bin/python script.py week.csv"},
	}
	for _, tc := range refuse {
		if alternativeEligible(delegatedBashCall("r", tc.body), "bash", failed, goal, work) {
			t.Errorf("%s was eligible as the alternative: %q", tc.name, tc.body)
		}
	}

	// The genuine b050 shape runs with no cd, in the workspace the turn opened
	// in; a cd that lands ON the workspace is the same directory and still pairs.
	direct := `.venv/bin/python -c 'open("week.csv")'`
	if !alternativeEligible(delegatedBashCall("d", direct), "bash", failed, goal, work) {
		t.Fatal("the direct workspace calculation was refused")
	}
	onWork := "cd " + work + " && .venv/bin/python script.py week.csv"
	if !alternativeEligible(delegatedBashCall("w", onWork), "bash", failed, goal, work) {
		t.Fatal("a cd onto the workspace itself stopped grounding the pairing")
	}
	// A relative cd whose base IS known resolves against the workspace, so a
	// file directly in the workspace still grounds.
	rel := "cd . && .venv/bin/python script.py week.csv"
	if !alternativeEligible(delegatedBashCall("r", rel), "bash", failed, goal, work) {
		t.Fatal("a cd . in the workspace stopped grounding the pairing")
	}
	// A wrapper before the cd does not hide the navigation when it lands on the
	// workspace: the effective directory is still the workspace.
	wrapped := "env FOO=1 cd " + work + " && .venv/bin/python script.py week.csv"
	if !alternativeEligible(delegatedBashCall("e", wrapped), "bash", failed, goal, work) {
		t.Fatal("a wrapped cd onto the workspace stopped grounding the pairing")
	}
}

// THE WORKER'S OWN WORKSPACE GROUNDS THE GOAL-FILE IDENTITY AT THE REAL
// DELEGATED BOUNDARY. This drives [Agent.recordOutcome] through a task worker, so
// it proves the delegated caller passes worker.config.Workspace: a success that
// opens the SAME goal-named file by an ABSOLUTE path inside the workspace pairs
// with the failure, while an unrelated file elsewhere with the same basename is
// refused.
func TestDelegatedBoundaryWorkspaceGroundsGoalFileIdentity(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	week := filepath.Join(dir, "week.csv")
	if err := os.WriteFile(week, []byte("merchant,amount\nStationery,1235\n"), 0600); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	otherWeek := filepath.Join(other, "week.csv")
	if err := os.WriteFile(otherWeek, []byte("merchant,amount\nOther,1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	run := func(success string) []store.ContextualAttempt {
		root, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
		root.prepareBindingContext(ctx, a4Goal)
		worker := spawnTaskWorker(t, root, dir)
		pre := worker.captureSourceSnapshot(ctx).Identity
		worker.recordOutcome(ctx, 0, delegatedBashCall("f1", a4Failed), a4FailureResult(), pre)
		worker.recordOutcome(ctx, 0, delegatedBashCall("s1", success), toolResult{text: "independent grand total: 12.35"}, pre)
		return attemptsForProject(t, brain, root)
	}

	// The absolute path to the workspace's OWN week.csv is the same file the goal
	// named, so the real calculation is the observed alternative.
	same := run(".venv/bin/python -c 'open(\"" + week + "\")'")
	if len(same) != 2 {
		t.Fatalf("the workspace's own goal file did not ground the pairing: %+v", same)
	}
	paired := false
	for i := range same {
		if same[i].Status == store.AttemptSucceeded && same[i].AlternativeOf != "" {
			paired = true
		}
	}
	if !paired {
		t.Fatalf("the delegated alternative was not linked to the failure: %+v", same)
	}

	// An unrelated file elsewhere with the SAME basename is a different file and
	// never grounds the pairing.
	foreign := run(".venv/bin/python -c 'open(\"" + otherWeek + "\")'")
	if len(foreign) != 1 || foreign[0].Status != store.AttemptFailed {
		t.Fatalf("an unrelated same-basename file grounded the delegated pairing: %+v", foreign)
	}
}

// ── A4 RESIDUAL HARDENING (F1/F2 of outcome-hardened-review-summary) ─────────
//
// The independent review reproduced two false positives the A4 hardening left
// open; both CONSUMED THE ONE ALTERNATIVE SLOT before the genuine calculation.
//
//   F1 — a bare goal-file operand of ANY non-metadata command (a file-maintenance
//        hand, or an unknown tool) was read as work. Only a recognised reader's
//        operand is the operation; everything else fails closed.
//   F2 — an error-masked command whose OWN receipt is a failure traceback was
//        stored as a success because the shell's overall exit was zero.

// F1: rm/touch/mv/chmod and an unknown tool are never the remedy, and the
// genuine CSV/Decimal calculation still gets the single slot at BOTH boundaries.
func TestA4BareOperandNonReadersDoNotStealAlternativeSlot(t *testing.T) {
	controls := []string{
		"rm week.csv",
		"touch week.csv",
		"mv week.csv /tmp/week.csv",
		"chmod 000 week.csv",
		"mytool week.csv",
		// THE PREFIX IMPOSTORS OF F1: a name that merely STARTS with the
		// interpreter family is still not a reader.
		"python-tool week.csv",
		"python-config week.csv",
		"pythonista week.csv",
		"pypyhelper week.csv",
	}
	for _, ctrl := range controls {
		if alternativeEligible(delegatedBashCall("n", ctrl), "bash", "bash: "+a4Failed, a4Goal) {
			t.Fatalf("a non-reader bare operand was eligible: %q", ctrl)
		}
	}
	if !alternativeEligible(delegatedBashCall("l", a4Ledger), "bash", "bash: "+a4Failed, a4Goal) {
		t.Fatal("the genuine ledger utility (a real reader's operand) was refused")
	}

	t.Run("root", func(t *testing.T) {
		dir := t.TempDir()
		initRepo(t, dir)
		a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
		ctx := context.Background()
		a.prepareBindingContext(ctx, a4Goal)
		pre := a.captureSourceSnapshot(ctx).Identity
		a.recordOutcome(ctx, 1, delegatedBashCall("f1", a4Failed), a4FailureResult(), pre)
		for i, body := range controls {
			a.recordOutcome(ctx, 1, delegatedBashCall(fmt.Sprintf("n%d", i), body), toolResult{text: "done"}, pre)
		}
		a.recordOutcome(ctx, 1, delegatedBashCall("s1", a4CSV), toolResult{text: "rows: 3\nindependent grand total: 12.35"}, pre)
		rows := attemptsForProject(t, brain, a)
		if len(rows) != 2 {
			t.Fatalf("a non-reader operand stole the slot at the root: %+v", rows)
		}
		for _, row := range rows {
			if row.Status == store.AttemptSucceeded && !strings.HasPrefix(row.Action, "bash: .venv/bin/python -c '\nimport csv") {
				t.Fatalf("the stored root alternative was not the genuine calculation: %q", row.Action)
			}
		}
	})

	t.Run("delegated", func(t *testing.T) {
		dir := t.TempDir()
		initRepo(t, dir)
		root, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
		root.prepareBindingContext(context.Background(), a4Goal)
		worker := spawnTaskWorker(t, root, dir)
		ctx := context.Background()
		pre := worker.captureSourceSnapshot(ctx).Identity
		worker.recordOutcome(ctx, 0, delegatedBashCall("f1", a4Failed), a4FailureResult(), pre)
		for i, body := range controls {
			worker.recordOutcome(ctx, 0, delegatedBashCall(fmt.Sprintf("n%d", i), body), toolResult{text: "done"}, pre)
		}
		worker.recordOutcome(ctx, 0, delegatedBashCall("s1", a4CSV), toolResult{text: "rows: 3\nindependent grand total: 12.35"}, pre)
		rows := attemptsForProject(t, brain, root)
		if len(rows) != 2 {
			t.Fatalf("a non-reader operand stole the slot on the delegated path: %+v", rows)
		}
		for _, row := range rows {
			if row.Status == store.AttemptSucceeded && !strings.HasPrefix(row.Action, "bash: .venv/bin/python -c '\nimport csv") {
				t.Fatalf("the stored delegated alternative was not the genuine calculation: %q", row.Action)
			}
		}
	})
}

// F2: a masked substep is refused before eligibility, and a success whose own
// receipt is a failure traceback is refused by the writers — at BOTH boundaries.
// Each control discriminates one half of the guard, and the genuine calculation
// still gets the slot behind the noise.
func TestA4MaskedOrFailedSubstepIsNeverTheAlternative(t *testing.T) {
	const masked = `.venv/bin/python -c 'open("week.csv")' || true`
	const unmasked = `.venv/bin/python -c 'open("week.csv")'`
	const traceback = "Traceback (most recent call last):\n  File \"<string>\", line 1, in <module>\nFileNotFoundError: [Errno 2] No such file or directory: 'week.csv'"

	// The taxonomy itself: a `||` fallback masks, a genuine multi-step `; echo`
	// does not; a traceback is a failure, a grand total is not.
	if !shellMasksExit(masked) || shellMasksExit(a4CSV) || shellMasksExit(a4Ledger) {
		t.Fatal("the shell masking taxonomy is wrong")
	}
	if !receiptShowsFailure(traceback) || receiptShowsFailure("rows: 3\nindependent grand total: 12.35") {
		t.Fatal("the receipt failure taxonomy is wrong")
	}
	// The masked body is refused by eligibility itself, with a happy receipt.
	if alternativeEligible(delegatedBashCall("m", masked), "bash", "bash: "+a4Failed, a4Goal) {
		t.Fatalf("a masked substep was eligible: %q", masked)
	}

	for _, tc := range []struct {
		name   string
		body   string
		result toolResult
	}{
		{"masked substep with a failing receipt", masked, toolResult{text: traceback}},
		{"masked substep with a happy receipt", masked, toolResult{text: "rows: 3\nindependent grand total: 12.35"}},
		{"unmasked substep whose receipt raised", unmasked, toolResult{text: traceback}},
	} {
		t.Run("root/"+tc.name, func(t *testing.T) {
			dir := t.TempDir()
			initRepo(t, dir)
			a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
			ctx := context.Background()
			a.prepareBindingContext(ctx, a4Goal)
			pre := a.captureSourceSnapshot(ctx).Identity
			a.recordOutcome(ctx, 1, delegatedBashCall("f1", a4Failed), a4FailureResult(), pre)
			a.recordOutcome(ctx, 1, delegatedBashCall("x1", tc.body), tc.result, pre)
			// The genuine work arrives after the noise and takes the one slot.
			a.recordOutcome(ctx, 1, delegatedBashCall("s1", a4CSV), toolResult{text: "rows: 3\nindependent grand total: 12.35"}, pre)
			rows := attemptsForProject(t, brain, a)
			if len(rows) != 2 {
				t.Fatalf("the masked/failed substep was stored as an alternative: %+v", rows)
			}
			for _, row := range rows {
				if row.Status == store.AttemptSucceeded {
					if !strings.HasPrefix(row.Action, "bash: .venv/bin/python -c '\nimport csv") {
						t.Fatalf("the stored alternative was not the genuine calculation: %q", row.Action)
					}
					if row.AlternativeOf == "" {
						t.Fatalf("the alternative lost its observed pair: %+v", row)
					}
				}
			}
		})
		t.Run("delegated/"+tc.name, func(t *testing.T) {
			dir := t.TempDir()
			initRepo(t, dir)
			root, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
			root.prepareBindingContext(context.Background(), a4Goal)
			worker := spawnTaskWorker(t, root, dir)
			ctx := context.Background()
			pre := worker.captureSourceSnapshot(ctx).Identity
			worker.recordOutcome(ctx, 0, delegatedBashCall("f1", a4Failed), a4FailureResult(), pre)
			worker.recordOutcome(ctx, 0, delegatedBashCall("x1", tc.body), tc.result, pre)
			worker.recordOutcome(ctx, 0, delegatedBashCall("s1", a4CSV), toolResult{text: "rows: 3\nindependent grand total: 12.35"}, pre)
			rows := attemptsForProject(t, brain, root)
			if len(rows) != 2 {
				t.Fatalf("the masked/failed substep was stored as a delegated alternative: %+v", rows)
			}
			for _, row := range rows {
				if row.Status == store.AttemptSucceeded && !strings.HasPrefix(row.Action, "bash: .venv/bin/python -c '\nimport csv") {
					t.Fatalf("the delegated stored alternative was not the genuine calculation: %q", row.Action)
				}
			}
		})
	}
}

// The residual F1/F2 noise under a CONCURRENT batch: however the siblings
// interleave, exactly one alternative is stored and it is the genuine CSV/Decimal
// calculation — never a file-maintenance hand and never a masked substep.
func TestA4ResidualNoiseUnderRaceKeepsGenuineAlternative(t *testing.T) {
	const maskedWithReceipt = `.venv/bin/python -c 'open("week.csv")' || true`
	traceback := "Traceback (most recent call last):\nFileNotFoundError: [Errno 2] No such file or directory: 'week.csv'"
	batch := []struct {
		body   string
		result toolResult
	}{
		{"rm week.csv", toolResult{text: "done"}},
		{"mytool week.csv", toolResult{text: "done"}},
		{maskedWithReceipt, toolResult{text: traceback}},
		{`.venv/bin/python -c 'open("week.csv")'`, toolResult{text: traceback}},
		{a4Echo, toolResult{text: "week.csv"}},
	}
	t.Run("root", func(t *testing.T) {
		dir := t.TempDir()
		initRepo(t, dir)
		a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
		ctx := context.Background()
		a.prepareBindingContext(ctx, a4Goal)
		pre := a.captureSourceSnapshot(ctx).Identity
		a.recordOutcome(ctx, 1, delegatedBashCall("f1", a4Failed), a4FailureResult(), pre)
		parallelOutcomes(16, func(i int) {
			if i%3 == 0 {
				a.recordOutcome(ctx, 1, delegatedBashCall(fmt.Sprintf("g%d", i), a4CSV), toolResult{text: "rows: 3\nindependent grand total: 12.35"}, pre)
				return
			}
			tc := batch[i%len(batch)]
			a.recordOutcome(ctx, 1, delegatedBashCall(fmt.Sprintf("n%d", i), tc.body), tc.result, pre)
		})
		rows := attemptsForProject(t, brain, a)
		succeeded := 0
		for _, row := range rows {
			if row.Status != store.AttemptSucceeded {
				continue
			}
			succeeded++
			if !strings.HasPrefix(row.Action, "bash: .venv/bin/python -c '\nimport csv") {
				t.Fatalf("residual noise stole the slot under the race: %q", row.Action)
			}
			if row.AlternativeOf == "" {
				t.Fatalf("the kept alternative lost its observed pair (a mark with no cause): %+v", row)
			}
		}
		if succeeded != 1 {
			t.Fatalf("the race kept %d alternatives, want exactly one", succeeded)
		}
	})
}

// EXPECTED FAILURE: the taxonomy helpers are pinned so a later edit cannot
// quietly widen what counts as a masked substep or a failing receipt.
func TestA4GuardTaxonomyPins(t *testing.T) {
	masked := []string{
		`.venv/bin/python -c 'open("week.csv")' || true`,
		`.venv/bin/python -c 'open("week.csv")' 2>/dev/null`,
		`false || .venv/bin/python ledger.py week.csv`,
		`.venv/bin/python -c 'open("week.csv")' ; true`,
		`.venv/bin/python -c 'open("week.csv")' && true`,
		`.venv/bin/python -c 'open("week.csv")' | cat`,
		`.venv/bin/python -c 'open("week.csv")' &`,
	}
	for _, body := range masked {
		if !shellMasksExit(body) {
			t.Fatalf("a masking shape was not recognised: %q", body)
		}
	}
	clean := []string{a4CSV, a4Ledger, `.venv/bin/python -c 'print("week.csv")'`}
	for _, body := range clean {
		if shellMasksExit(body) {
			t.Fatalf("a genuine action was misread as masking: %q", body)
		}
	}
	// THE GUARD READS ONLY THE LAST SUBSTEP, so a real multi-command run whose
	// final step keeps its exit is untouched — the `;`-separated ledger utility
	// and a pipeline into a real consumer both stay eligible.
	stillClean := []string{
		`.venv/bin/python ledger.py week.csv; echo "---"; .venv/bin/python ledger.py week.csv --summary grandtotal`,
		`.venv/bin/python ledger.py week.csv | sort`,
		`.venv/bin/python -c 'import csv, decimal' ; .venv/bin/python ledger.py week.csv`,
	}
	for _, body := range stillClean {
		if shellMasksExit(body) {
			t.Fatalf("a genuine multi-command action was misread as masking: %q", body)
		}
	}
	if !receiptShowsFailure("Traceback (most recent call last):") || !receiptShowsFailure("ModuleNotFoundError: No module named 'pandas'") {
		t.Fatal("a failing receipt was not recognised")
	}
	if receiptShowsFailure("rows: 3\nindependent grand total: 12.35") {
		t.Fatal("a genuine success receipt was misread as a failure")
	}
}

// THE INTERPRETER TAXONOMY IS EXACT, NOT A PREFIX. A real interpreter name \u2014
// bare, a major version or a major.minor \u2014 grounds a bare goal-file operand. A
// program that merely STARTS with the family word (a `-config` helper, a
// locally named wrapper, an unrelated `pythonista`/`pypyhelper`) does not: it can
// never steal the one alternative slot from the genuine calculation that follows.
func TestA4InterpreterTaxonomyIsExact(t *testing.T) {
	readers := []string{
		".venv/bin/python ledger.py week.csv",
		"python ledger.py week.csv",
		"python2 ledger.py week.csv",
		"python3 ledger.py week.csv",
		"python2.7 ledger.py week.csv",
		"python3.12 ledger.py week.csv",
		"pypy ledger.py week.csv",
		"pypy3 ledger.py week.csv",
		"pypy3.10 ledger.py week.csv",
	}
	for _, body := range readers {
		if !alternativeEligible(delegatedBashCall("r", body), "bash", "bash: "+a4Failed, a4Goal) {
			t.Errorf("a real interpreter's operand was refused: %q", body)
		}
	}
	impostors := []string{
		"python-tool week.csv",
		"python-config week.csv",
		"python3-config week.csv",
		"python3.12-config week.csv",
		"pythonista week.csv",
		"pythonw week.csv",
		"pypyhelper week.csv",
	}
	for _, body := range impostors {
		if alternativeEligible(delegatedBashCall("n", body), "bash", "bash: "+a4Failed, a4Goal) {
			t.Errorf("a non-interpreter prefix grounded an operand: %q", body)
		}
	}
}

// ── A4 RESIDUAL (session 52a5c88b845f362b): THE EXACT LIVE LONG-COMPOUND SUCCESS ──
//
// The canonical journal proved only the standalone `.venv/bin/python -c 'import
// pandas'` FAILED and wrote NO succeeded AlternativeOf row, even though the SAME
// turn and goal then ran the real independent check and it succeeded. The failed
// row was stored (the status came from the tool boundary); the replacement was
// dropped by the ASSOCIATION, which parsed the success through the 240-rune
// DISPLAY clip of its action — and the goal-named `week.csv` it actually OPENED
// sat at rune 237 of that clip. The clip is a rendering width, never a parser
// input, so the file-operand link could not see the artifact the replacement
// worked on.
//
// These are the EXACT live prompt, the EXACT failed command, the EXACT compound
// command (with the escaped double quotes and embedded newlines the shell tool
// received) and the EXACT receipts from the production run, never a shortened
// command invented to share a token and never an altered goal. The captured
// absolute workspace is relocated into a temp directory ONLY for the boundary
// test, consistently and through one named helper, so portable CI replays the
// real pipeline without the acceptance host's own tree.
const (
	a4bGoal        = "Run just .venv/bin/python -c 'import pandas' as a standalone tool call, without a shell wrapper that changes its exit status. Then finish the read-only independent grand-total check of week.csv and compare ledger.py."
	a4bFailed      = ".venv/bin/python -c 'import pandas'"
	a4bWS          = "/home/santosh/src/contextual-acceptance-work-20261006/ledger"
	a4bSuccess     = "cd /home/santosh/src/contextual-acceptance-work-20261006/ledger && echo \"--- independent grand total (.venv/bin/python, Decimal, not ledger.py) ---\" && .venv/bin/python -c \"\nfrom decimal import Decimal\nimport csv\nt=Decimal(0)\nwith open('week.csv',newline='') as f:\n    for r in csv.DictReader(f):\n        t+=Decimal(r['amount'])\nprint(t)\n\" && echo \"--- independent grand total (awk) ---\" && awk -F, 'NR>1{s+=$2} END{printf \"%.2f\\n\", s}' week.csv && echo \"--- ledger.py via .venv/bin/python, both modes ---\" && .venv/bin/python ledger.py week.csv && .venv/bin/python ledger.py --json week.csv"
	a4bFailReceipt = "Traceback (most recent call last):\n  File \"<string>\", line 1, in <module>\nModuleNotFoundError: No module named 'pandas'\n\n\nCommand exited with code 1"
	a4bOKReceipt   = "--- independent grand total (.venv/bin/python, Decimal, not ledger.py) ---\n12.35\n--- independent grand total (awk) ---\n12.35\n--- ledger.py via .venv/bin/python, both modes ---\nfood: 0.30\ntravel: 12.05\n{\"food\": \"0.30\", \"travel\": \"12.05\"}\n"
)

func a4bFailureResult() toolResult {
	return toolResult{text: a4bFailReceipt, isError: true}
}

func a4bSuccessResult() toolResult {
	return toolResult{text: a4bOKReceipt}
}

// relocateCapturedWorkspace rewrites the captured absolute workspace prefix of a
// frozen live command to the directory a test owns, and NOTHING else: the frozen
// goal, argv, shell escaping and pipeline shape are untouched. The operand and
// every later segment are re-based consistently, so the lexical file identity the
// association computes is the real one. It is what makes the exact live pair
// replayable in portable CI without the acceptance host's own path.
func relocateCapturedWorkspace(command, from, to string) string {
	return strings.ReplaceAll(command, from, to)
}

// THE REAL PAIR IS PAIRED AT THE PRODUCTION BOUNDARY. The standalone diagnostic
// failure and the long compound success over the SAME goal-named week.csv become
// two rows whose second names the first, at BOTH the session and the delegated
// boundary — proven on the exact live command, not a reduced one. The first
// assertion is filesystem-free: the exact raw command is eligible against the
// exact frozen workspace string, so the fix that reads its full width is pinned
// without needing that directory to exist.
func TestStandaloneFailurePairsActualLongCompoundLiveSuccess(t *testing.T) {
	if !alternativeEligible(delegatedBashCall("call_0dcb2d6851734402866add2b", a4bSuccess), "bash", "bash: "+a4bFailed, a4bGoal, a4bWS) {
		t.Fatal("the exact live compound success was not eligible against the frozen workspace (filesystem-free)")
	}
	if n := len([]rune(attemptActionBody(attemptAction(delegatedBashCall("x", a4bSuccess))))); n != 240 {
		t.Fatalf("the stored display clip should still be 240 runes, got %d", n)
	}

	// A workspace whose path is LONGER than the captured one keeps the goal-named
	// operand past the 240-rune display clip, so the boundary test exercises the
	// real regression rather than a command short enough to be invisible to it.
	ws := filepath.Join(t.TempDir(), "contextual-acceptance-work-20261006-relocated-ledger")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	relocated := relocateCapturedWorkspace(a4bSuccess, a4bWS, ws)
	idx := strings.Index(relocated, "week.csv")
	if idx < 240 {
		t.Fatalf("the relocated goal-named operand fell inside the display clip at rune %d", idx)
	}
	// THE CLIP ALONE NO LONGER GROUNDS THE PAIRING AND THE FULL WIDTH DOES: that
	// is exactly the boundary the live run fell through.
	if sharesGoalNamedFileOperand(attemptActionBody(attemptAction(delegatedBashCall("x", relocated))), a4bGoal, ws) {
		t.Fatal("the 240-rune display clip unexpectedly grounded the pairing; the regression no longer exercises it")
	}
	if !sharesGoalNamedFileOperand(relocated, a4bGoal, ws) {
		t.Fatal("the full-width relocated success did not ground the goal-named file")
	}

	t.Run("root", func(t *testing.T) {
		dir := t.TempDir()
		initRepo(t, dir)
		a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = ws; c.MemoryProjectKey = "p" })
		ctx := context.Background()
		a.prepareBindingContext(ctx, a4bGoal)
		pre := a.captureSourceSnapshot(ctx).Identity
		a.recordOutcome(ctx, 1, delegatedBashCall("call_d70e5c82b7634f4fb5bec3d3", a4bFailed), a4bFailureResult(), pre)
		a.recordOutcome(ctx, 1, delegatedBashCall("call_0dcb2d6851734402866add2b", relocated), a4bSuccessResult(), pre)
		assertA4bPair(t, attemptsForProject(t, brain, a), ws)
	})

	t.Run("delegated", func(t *testing.T) {
		root, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = ws; c.MemoryProjectKey = "p" })
		root.prepareBindingContext(context.Background(), a4bGoal)
		// The worker stands where the frozen command cd's to, exactly as the live
		// run did: a worker whose ground differed would fail the pairing CLOSED.
		worker := spawnTaskWorker(t, root, ws)
		ctx := context.Background()
		pre := worker.captureSourceSnapshot(ctx).Identity
		worker.recordOutcome(ctx, 0, delegatedBashCall("call_d70e5c82b7634f4fb5bec3d3", a4bFailed), a4bFailureResult(), pre)
		worker.recordOutcome(ctx, 0, delegatedBashCall("call_0dcb2d6851734402866add2b", relocated), a4bSuccessResult(), pre)
		assertA4bPair(t, attemptsForProject(t, brain, root), ws)
	})

	t.Run("noise", func(t *testing.T) {
		a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = ws; c.MemoryProjectKey = "p" })
		ctx := context.Background()
		a.prepareBindingContext(ctx, a4bGoal)
		pre := a.captureSourceSnapshot(ctx).Identity
		a.recordOutcome(ctx, 1, delegatedBashCall("call_d70e5c82b7634f4fb5bec3d3", a4bFailed), a4bFailureResult(), pre)

		// A LONG ECHO THAT ONLY PRINTS THE NAME. Its only mention of the
		// goal-named file sits inside the printed text, past the 240-rune
		// preview, so it names no operand and the slot stays open.
		printOnly := "cd " + ws + ` && echo "` + strings.Repeat("independent grand total ", 12) + `week.csv"` +
			" && .venv/bin/python -c '\nfrom decimal import Decimal\nprint(Decimal(1))\n'"
		if len([]rune(printOnly)) <= 240 {
			t.Fatalf("the print-only probe was not long enough to clear the preview: %d runes", len([]rune(printOnly)))
		}
		if alternativeEligible(delegatedBashCall("c", printOnly), "bash", "bash: "+a4bFailed, a4bGoal, ws) {
			t.Fatal("a name that appears only inside printed text was eligible as the alternative")
		}
		a.recordOutcome(ctx, 1, delegatedBashCall("call_print_only", printOnly), toolResult{text: "independent grand total week.csv\n12.35"}, pre)

		// A GENUINE CALCULATION PAST THE EXISTING CEILING IS REFUSED WHOLE. The
		// body exceeds contextualFileBytes, so no identity is manufactured by
		// truncation and the over-limit action cannot claim the slot either.
		over := "cd " + ws + " && .venv/bin/python -c '\n" + strings.Repeat("# padding\n", contextualFileBytes/10) +
			"import csv\nwith open(\"week.csv\") as f:\n    print(sum(float(r[1]) for r in list(csv.reader(f))[1:]))\n'"
		if len(over) <= contextualFileBytes {
			t.Fatalf("the over-bound probe did not exceed contextualFileBytes: %d bytes", len(over))
		}
		if alternativeActionBody(delegatedBashCall("c", over)) != "" {
			t.Fatal("an over-bound action body was not refused whole")
		}
		if alternativeEligible(delegatedBashCall("c", over), "bash", "bash: "+a4bFailed, a4bGoal, ws) {
			t.Fatal("an over-bound action was eligible as the alternative")
		}
		a.recordOutcome(ctx, 1, delegatedBashCall("call_over_bound", over), toolResult{text: "12.35"}, pre)

		rows := attemptsForProject(t, brain, a)
		if len(rows) != 1 || rows[0].Status != store.AttemptFailed {
			t.Fatalf("a print-only echo or an over-bound body claimed the alternative slot: %+v", rows)
		}
	})
}

func assertA4bPair(t *testing.T, rows []store.ContextualAttempt, ws string) {
	t.Helper()
	if len(rows) != 2 {
		t.Fatalf("the live standalone failure and its one long-compound success should be two rows, got %+v", rows)
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
		t.Fatalf("the real pair was not written: %+v", rows)
	}
	if failed.Action != "bash: "+a4bFailed {
		t.Fatalf("the stored failure was not the exact standalone command: %q", failed.Action)
	}
	if succeeded.AlternativeOf != failed.SourceKey {
		t.Fatalf("the long-compound success did not name the standalone failure: alt=%q failure=%q", succeeded.AlternativeOf, failed.SourceKey)
	}
	if !strings.Contains(succeeded.SourceKey, "call_0dcb2d6851734402866add2b") {
		t.Fatalf("the alternative was not the real compound call: %q", succeeded.SourceKey)
	}
	if !strings.Contains(succeeded.Action, "bash: cd "+ws) {
		t.Fatalf("the stored alternative was not the relocated compound command: %q", succeeded.Action)
	}
	if !strings.Contains(succeeded.Observation, "12.35") {
		t.Fatalf("the alternative's own receipt was not kept: %q", succeeded.Observation)
	}
	if succeeded.Goal != failed.Goal || succeeded.TurnID != failed.TurnID {
		t.Fatalf("the alternative lost the frozen goal/turn: alt=%+v failure=%+v", succeeded, failed)
	}
}
