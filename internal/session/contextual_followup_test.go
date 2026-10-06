package session

// REGRESSIONS for the six concrete follow-up findings (A-F) from the read-only
// audit. Each one is driven through the REAL production path -- Agent.recordOutcome
// for pairing, captureSourceSnapshot for the snapshot, dismissContextualNotices for
// the batch dismissal and delegatedSourceKey for the persisted identity -- not
// only through the helper the finding names.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/store"
)

// ---- A: the same interpreter is not the same work -------------------------

// A python failure on parser_test.py must NOT be answered by a python success on
// week.csv merely because the frozen goal names both files. The success shares
// no ACTUAL input operand with the failure, so nothing pairs them.
func TestFollowupSameInterpreterForeignFileDoesNotPair(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
	ctx := context.Background()
	const goal = "make parser_test.py pass and also report the week.csv totals"
	a.prepareBindingContext(ctx, goal)
	pre := a.captureSourceSnapshot(ctx).Identity

	a.recordOutcome(ctx, 1, delegatedBashCall("f1", "python parser_test.py"), toolResult{text: "SyntaxError: invalid syntax", isError: true}, pre)
	a.recordOutcome(ctx, 1, delegatedBashCall("s1", "python calc.py week.csv"), toolResult{text: "grand total 12.35"}, pre)
	if rows := attemptsForProject(t, brain, a); len(rows) != 1 {
		t.Fatalf("a same-interpreter foreign-file success was filed as the alternative: %+v", rows)
	}
	if alternativeEligible(delegatedBashCall("s1", "python calc.py week.csv"), "bash", "bash: python parser_test.py", goal, dir) {
		t.Fatal("same-interpreter different-file success was eligible as the alternative")
	}
}

// THE REAL CSV PAIR SURVIVES: a failed utility read of vendor.csv and a later
// standard-library UTF-16 read of the SAME vendor.csv share an ACTUAL operand,
// so the success is still filed beside the failure.
func TestFollowupSharedActualOperandStillPairs(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
	ctx := context.Background()
	const goal = "Check the supplier export vendor.csv against the ledger utility, then independently total it with the standard library"
	const failed = ".venv/bin/python ledger.py vendor.csv"
	const success = ".venv/bin/python -c \"import csv, io\nraw = open('vendor.csv', 'rb').read()\ntext = raw.decode('utf-16')\nprint(sum(1 for _ in csv.DictReader(io.StringIO(text))))\""
	if !sharesActualInputOperand(failed, success, dir) {
		t.Fatal("the two vendor.csv reads did not share an actual input operand")
	}
	a.prepareBindingContext(ctx, goal)
	pre := a.captureSourceSnapshot(ctx).Identity
	a.recordOutcome(ctx, 1, delegatedBashCall("f1", failed), toolResult{text: "UnicodeDecodeError: 'utf-8' codec can't decode byte 0xff", isError: true}, pre)
	a.recordOutcome(ctx, 1, delegatedBashCall("s1", success), toolResult{text: "3"}, pre)
	rows := attemptsForProject(t, brain, a)
	if len(rows) != 2 {
		t.Fatalf("the real shared-operand pair was not kept: %+v", rows)
	}
	var paired bool
	for _, row := range rows {
		if row.Status == store.AttemptSucceeded && row.AlternativeOf != "" {
			paired = true
		}
	}
	if !paired {
		t.Fatalf("the success did not name the failure: %+v", rows)
	}
}

// ---- B: recognized wrappers and their options are normalized -------------

// A wrapper's own option must be peeled with the wrapper, so the lookup it wraps
// is still read as a lookup and refused as an alternative. An UNKNOWN option is
// refused outright rather than read as the program.
func TestFollowupWrapperOptionsNormalized(t *testing.T) {
	for _, body := range []string{
		"command -p grep -n pandas",
		"command -- grep pandas",
		"command -v grep pandas",
		"busybox -c grep pandas",
		"env -i grep pandas",
	} {
		if !shellMetadataOnly(body) {
			t.Errorf("a wrapped lookup was not read as metadata: %q", body)
		}
		if alternativeEligible(delegatedBashCall("x", body), "bash", "bash: go test ./internal/parser", "make the parser tests pass") {
			t.Errorf("a wrapped lookup was eligible as the alternative: %q", body)
		}
	}
	// `command -v` is itself a lookup, whatever word follows it.
	if !shellMetadataOnly("command -v python") {
		t.Fatal("`command -v python` was not read as the lookup it is")
	}
	// AN UNKNOWN OPTION REFUSES LEARNING instead of falling through to the word.
	if shellMetadataOnly("command -Z grep pandas") {
		t.Fatal("an unknown wrapper option was read as metadata")
	}
	if !shellWrapperUncertain("command -Z grep pandas") {
		t.Fatal("an unknown wrapper option was not flagged as uncertain")
	}
	if alternativeEligible(delegatedBashCall("x", "command -Z grep pandas"), "bash", "bash: grep -n pandas", "find pandas in the export") {
		t.Fatal("an unknown wrapper option was eligible as the alternative")
	}
}

// The production boundary refuses a wrapped lookup as the observed alternative:
// the tool receipt proves nothing about the failed work.
func TestFollowupWrappedLookupIsNotStoredAsAlternative(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
	ctx := context.Background()
	const goal = "find where pandas is imported and make the parser tests pass"
	a.prepareBindingContext(ctx, goal)
	pre := a.captureSourceSnapshot(ctx).Identity
	a.recordOutcome(ctx, 1, delegatedBashCall("f1", "grep -rn pandas internal/parser"), toolResult{text: "nothing", isError: true}, pre)
	a.recordOutcome(ctx, 1, delegatedBashCall("s1", "command -p grep -rn pandas internal/parser"), toolResult{text: "internal/parser/x.py:1:import pandas"}, pre)
	if rows := attemptsForProject(t, brain, a); len(rows) != 1 {
		t.Fatalf("a wrapped lookup was stored as the alternative: %+v", rows)
	}
}

// ---- C: any pipeline may mask its producer ---------------------------------

// A pipe into a consumer that is NOT on any blacklist still reports that
// consumer's zero, so it must be read as masking; only explicit pipefail clears
// it, and a quoted or heredoc `|` is not a pipeline at all.
func TestFollowupPipelinesMaskUnlessPipefail(t *testing.T) {
	for _, body := range []string{
		"go test ./internal/parser | grep -c PASS",
		"go test ./internal/parser | awk '{print $1}'",
		"go test ./internal/parser | wc -l",
	} {
		if !shellMasksExit(body) {
			t.Errorf("a pipeline into a consumer was not read as masking: %q", body)
		}
	}
	if shellMasksExit("set -o pipefail; go test ./internal/parser | grep -c PASS") {
		t.Fatal("an explicit pipefail pipeline was read as masking")
	}
	if shellMasksExit(`python -c "print(1 | 2)"`) {
		t.Fatal("a quoted pipe was read as a pipeline")
	}
	if shellMasksExit("python - <<'PY'\nprint(1 | 2)\nPY") {
		t.Fatal("a heredoc pipe was read as a pipeline")
	}
}

// The production path refuses a piped success as the observed alternative: its
// zero belongs to the consumer, not to the test run.
func TestFollowupPipedSuccessIsNotStoredAsAlternative(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
	ctx := context.Background()
	const goal = "make the parser tests pass"
	a.prepareBindingContext(ctx, goal)
	pre := a.captureSourceSnapshot(ctx).Identity
	a.recordOutcome(ctx, 1, delegatedBashCall("f1", "go test ./internal/parser"), toolResult{text: "--- FAIL: TestParser\nFAIL", isError: true}, pre)
	a.recordOutcome(ctx, 1, delegatedBashCall("s1", "go test ./internal/parser | grep -c PASS"), toolResult{text: "0\n0"}, pre)
	if rows := attemptsForProject(t, brain, a); len(rows) != 1 {
		t.Fatalf("a masked pipeline was stored as the alternative: %+v", rows)
	}
}

// ---- D: the submodule snapshot is content-aware ----------------------------

func addGitlink(t *testing.T, parent, child string) {
	t.Helper()
	sha := strings.TrimSpace(gitRepo(t, child, "rev-parse", "HEAD"))
	gitRun(t, parent, "update-index", "--add", "--cacheinfo", "160000,"+sha+",child")
	gitRun(t, parent, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-qm", "add gitlink")
}

// A submodule's snapshot identity tracks its OWN commit and dirty CONTENT, so a
// second edit of an already-dirty tracked file, a changed untracked file, an
// added nested untracked file and a commit move are all different identities --
// and an unreadable submodule is UNKNOWN rather than a constant two trees share.
func TestFollowupSubmoduleSnapshotIsContentAware(t *testing.T) {
	root := t.TempDir()
	initRepo(t, root)
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, child, "init", "-q")
	if err := os.WriteFile(filepath.Join(child, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, child, "add", "a.txt")
	gitRun(t, child, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-qm", "one")
	addGitlink(t, root, child)

	a, _ := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = root; c.MemoryProjectKey = "p" })
	ctx := context.Background()
	seen := []string{a.captureSourceSnapshot(ctx).Identity}
	next := func(label string) {
		t.Helper()
		got := a.captureSourceSnapshot(ctx).Identity
		for _, prior := range seen {
			if prior == got {
				t.Fatalf("%s left the submodule snapshot identity unchanged: %q", label, got)
			}
		}
		if strings.Contains(got, ":unknown") {
			t.Fatalf("%s collapsed to a constant unknown submodule identity: %q", label, got)
		}
		seen = append(seen, got)
	}
	// A TRACKED FILE EDITED A SECOND TIME, still uncommitted: the porcelain word
	// stays ` M` and only the content changed.
	if err := os.WriteFile(filepath.Join(child, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	next("first tracked edit")
	if err := os.WriteFile(filepath.Join(child, "a.txt"), []byte("three\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	next("second tracked edit")
	// AN UNTRACKED FILE AT THE SAME PATH, CONTENT CHANGED.
	if err := os.WriteFile(filepath.Join(child, "u.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	next("untracked file added")
	if err := os.WriteFile(filepath.Join(child, "u.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	next("untracked file content changed")
	// AN ADDITIONAL NESTED UNTRACKED FILE (the dir collapses to `?? udir/`
	// without --untracked-files=all, so only content-awareness can see it).
	if err := os.MkdirAll(filepath.Join(child, "udir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, "udir", "x.txt"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	next("nested untracked file added")
	if err := os.WriteFile(filepath.Join(child, "udir", "y.txt"), []byte("2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	next("additional nested untracked file")
	// A COMMIT MOVE.
	gitRun(t, child, "add", "-A")
	gitRun(t, child, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-qm", "two")
	next("commit move")

	// AN UNREADABLE SUBMODULE PROPAGATES UNKNOWN, never a stable constant.
	if err := os.RemoveAll(filepath.Join(child, ".git")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, ".git"), []byte("gitdir: /nonexistent/gone\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	broken := a.captureSourceSnapshot(ctx)
	if !broken.Unknown || broken.Identity != "unknown" {
		t.Fatalf("an unreadable submodule did not propagate UNKNOWN: %+v", broken)
	}
}

// The submodule's own untracked directory listing must be read with
// --untracked-files=all, or a directory that gained a file inside it keeps the
// same status bytes.
func TestFollowupSubmoduleUntrackedListingIsExpanded(t *testing.T) {
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-q", "--allow-empty", "-m", "init")
	if err := os.MkdirAll(filepath.Join(dir, "udir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "udir", "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := submoduleIdentity(context.Background(), dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "udir", "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := submoduleIdentity(context.Background(), dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("a nested untracked file was invisible to the submodule identity: %q", first)
	}
}

// ---- E: dismissal boundaries and the batch phrase --------------------------

func TestFollowupDismissalDotBoundaryAndBatchPhrase(t *testing.T) {
	n := store.ContextualImpactNotice{}
	n.Dependency.ConsumerPath = "data.py"
	if contextualNoticeNamed("dismiss item.data.py", n) {
		t.Fatal("naming item.data.py also named the notice about data.py")
	}
	if !contextualNoticeNamed("dismiss data.py", n) {
		t.Fatal("naming data.py did not name the notice")
	}
	if contextualDismissBatch("dismiss the concern, that is all") {
		t.Fatal("`that is all` was read as a batch reference to the notices")
	}
	if !contextualDismissBatch("dismiss all notices") || !contextualDismissBatch("dismiss both of these") {
		t.Fatal("an explicit notice-referent batch phrase did not match")
	}
}

// The production path: two held notices survive a sentence that only contains
// `all` inside an unrelated clause.
func TestFollowupBatchDismissNeedsNoticeReferent(t *testing.T) {
	a, s, producer, consumer := contextualReviewObservedFixture(t)
	a.observeContextualDependencies(memoryTurnEvidence{Receipts: []memoryToolReceipt{contextualReviewFullRead(t, a, producer, "p"), contextualReviewFullRead(t, a, consumer, "c")}})
	contextualReviewOfferTwo(t, a, producer)
	if contextualHeldNotices(a) != 2 {
		t.Fatal("fixture did not hold two notices")
	}
	a.dismissContextualNotices("dismiss the concern, that is all")
	if got := contextualHeldNotices(a); got != 2 {
		t.Fatalf("`that is all` dropped the held set: %d held", got)
	}
	if contextualOfferCount(t, a, s, producer, consumer) == 0 {
		t.Fatal("`that is all` persisted a suppression")
	}
	a.dismissContextualNotices("dismiss all notices")
	if got := contextualOfferCount(t, a, s, producer, consumer); got != 0 {
		t.Fatalf("a real batch dismissal did not persist: %d still offered", got)
	}
}

// ---- F: the persisted delegated source key carries Run ---------------------

func TestFollowupDelegatedSourceKeyIncludesRun(t *testing.T) {
	base := delegatedOrigin{Session: "s", Owner: "project:p", Turn: "t", Task: "node", Run: "run-a"}
	other := base
	other.Run = "run-b"
	first := delegatedSourceKey(base, "call-1")
	second := delegatedSourceKey(other, "call-1")
	if first == second {
		t.Fatalf("two workers of one node shared a source key: %q", first)
	}
	if !strings.Contains(first, "run-a") {
		t.Fatalf("the persisted source key did not carry Run: %q", first)
	}
	// OLD RECORDS STAY READABLE: a pre-Run (opaque) source key round-trips with no
	// migration and no parse-back.
	brain, err := store.Open(filepath.Join(t.TempDir(), "brain.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer brain.Close()
	const oldKey = "delegated:s:t:node:call-old"
	row, err := brain.AppendContextualAttempt(store.ContextualAttempt{
		ID: store.NewMemoryID(), Owner: "project:p", SessionID: "s", TurnID: "t", Tool: "bash",
		Action: "bash: x", Goal: "g", Status: store.AttemptSucceeded, Observation: "ok",
		ReceiptIDs: []string{"c"}, SourceKey: oldKey, AlternativeOf: "delegated:s:t:node:call-fail",
		SourceHash: contextualHash("ok"), ValidFrom: time.Now(),
	})
	if err != nil {
		t.Fatalf("an old-shape source key was not readable: %v", err)
	}
	if row.SourceKey != oldKey {
		t.Fatalf("an old source key was rewritten: %q", row.SourceKey)
	}
}
