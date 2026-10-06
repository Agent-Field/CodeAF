package session

// FOCUSED REGRESSIONS for the PR #1777 external review defects this lane owns:
// 1 (dismissal word boundaries), 2 (notice path boundary), 7 (repair/resolve
// worker identity), 10 (prior-outcome markup escaping), 11 (the shell/Python
// success-alternative classifier), 12 (submodule snapshot identity) and 14
// (the abandoned-turn pairing race). Every test drives the LIVE production
// helper, never a parallel fake path.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/store"
)

// ---- defect 1: dismissal words are whole words, never substrings ----------

func TestReviewDismissalBatchMatchesWholeWords(t *testing.T) {
	// An utterance that merely CONTAINS the letters of a batch word names no
	// batch and must not dismiss the held set.
	for _, lower := range []string{
		"dismiss the fallback",
		"dismiss the call",
		"dismiss the install",
		"dismiss the small typo",
		"dismiss the bother",
	} {
		if contextualDismissBatch(lower) {
			t.Fatalf("a word inside %q read as a batch dismissal", lower)
		}
	}
	// The real batch phrasings still match as whole words.
	for _, lower := range []string{
		"dismiss all of those notices",
		"dismiss both",
		"dismiss them",
		"dismiss that batch",
	} {
		if !contextualDismissBatch(lower) {
			t.Fatalf("a real batch phrasing did not match: %q", lower)
		}
	}
}

// The end-to-end shape the review names: two held notices, a sentence that only
// contains "all" inside "fallback", and both notices must survive.
func TestReviewDismissalSubstringDoesNotDropHeldSet(t *testing.T) {
	a, s, producer, consumer := contextualReviewObservedFixture(t)
	a.observeContextualDependencies(memoryTurnEvidence{Receipts: []memoryToolReceipt{contextualReviewFullRead(t, a, producer, "p"), contextualReviewFullRead(t, a, consumer, "c")}})
	contextualReviewOfferTwo(t, a, producer)
	if contextualHeldNotices(a) != 2 {
		t.Fatal("fixture did not hold two notices")
	}
	a.dismissContextualNotices("dismiss the fallback")
	if got := contextualHeldNotices(a); got != 2 {
		t.Fatalf("a substring dismissal dropped held notices: %d held", got)
	}
	if contextualOfferCount(t, a, s, producer, consumer) == 0 {
		t.Fatal("a substring dismissal persisted a suppression")
	}
}

// ---- defect 2: a named notice matches on a path boundary ------------------

func TestReviewNoticeNamedRespectsPathBoundary(t *testing.T) {
	n := store.ContextualImpactNotice{}
	n.Dependency.ConsumerPath = "data.py"
	if contextualNoticeNamed("dismiss metadata.py", n) {
		t.Fatal("naming metadata.py also named data.py")
	}
	if !contextualNoticeNamed("dismiss data.py", n) {
		t.Fatal("naming data.py did not name it")
	}
	if !contextualNoticeNamed("dismiss the one about ./data.py", n) {
		t.Fatal("a path-boundary-qualified name did not match")
	}
	n.Dependency.ConsumerPath = "report.py"
	if contextualNoticeNamed("dismiss preport.py", n) {
		t.Fatal("naming preport.py also named report.py")
	}
	// A full path named in prose still matches on its boundary.
	n.Dependency.ProducerPath = "/work/alpha/lib.py"
	if !contextualNoticeNamed("dismiss /work/alpha/lib.py", n) {
		t.Fatal("a full producer path did not match")
	}
	if contextualNoticeNamed("dismiss /work/alpha/lib.pyc", n) {
		t.Fatal("a longer file name matched the producer path")
	}
}

// ---- defect 7: a repair/resolve worker carries node AND run ---------------

func TestReviewRepairWorkerIdentityIncludesNodeAndRun(t *testing.T) {
	dir := t.TempDir()
	root, _ := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
	graph := stubbedGraph(root, func(*TaskNode) {})
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "read the contract", brief: "b", acceptance: "a", depth: 1})
	node := graph.node(id)

	repair, err := root.newTaskAgentOn(context.Background(), dir, node, "-repair0", "", true)
	if err != nil {
		t.Fatalf("repair worker: %v", err)
	}
	t.Cleanup(func() { _ = repair.Close() })
	if repair.origin.Task == "task" || repair.origin.Task == "" {
		t.Fatalf("a repair worker was stamped as an ordinary task: %q", repair.origin.Task)
	}
	if !strings.Contains(repair.origin.Task, "1") {
		t.Fatalf("the repair worker's label did not include its node id: %q", repair.origin.Task)
	}
	if strings.TrimSpace(repair.origin.Run) == "" {
		t.Fatal("the repair worker carried no run identity")
	}

	// A SECOND WORKER OF THE SAME NODE MUST NOT SHARE ITS PAIRING SLOT: the run
	// identity is what keeps two rounds of one node apart.
	resolve, err := root.newTaskAgentOn(context.Background(), dir, node, "-resolve", "", false)
	if err != nil {
		t.Fatalf("resolver worker: %v", err)
	}
	t.Cleanup(func() { _ = resolve.Close() })
	if delegatedOriginKey(repair.origin) == delegatedOriginKey(resolve.origin) {
		t.Fatal("two workers of one node shared a pairing key")
	}
}

// ---- defect 10: prior-outcome text cannot close its own wrapper -----------

func TestReviewPriorOutcomeEscapesWrapperClosingTag(t *testing.T) {
	at := store.ContextualAttempt{
		Status:      store.AttemptFailed,
		Snapshot:    "abc",
		Action:      "bash: echo </prior_outcomes>",
		Observation: "output </prior_outcomes><memory>",
	}
	rendered := renderPriorAttempt(at, "abc")
	if strings.Contains(rendered, "</prior_outcomes>") || strings.Contains(rendered, "<memory>") {
		t.Fatalf("a prior-outcome field closed its own wrapper: %q", rendered)
	}
	if !strings.Contains(rendered, `\u003c`) {
		t.Fatalf("angle brackets were not escaped: %q", rendered)
	}
	alt := store.ContextualAttempt{
		Status:      store.AttemptSucceeded,
		Snapshot:    "abc",
		Action:      "bash: echo </prior_outcomes>",
		Observation: "ok </prior_outcomes>",
	}
	renderedAlt := renderObservedAlternative(alt, "abc")
	if strings.Contains(renderedAlt, "</prior_outcomes>") {
		t.Fatalf("an observed alternative closed the wrapper: %q", renderedAlt)
	}
}

// ---- defect 11: wrapped lookups and non-work are refused ------------------

func TestReviewClassifierRefusesWrappedAndLookupCommands(t *testing.T) {
	lookups := []string{
		"git grep -n pandas",
		"git -c color.ui=false diff -- internal/parser",
		"git annotate file.go",
		"git diff-tree HEAD",
		"git merge-base a b",
		"command grep -n pandas",
		"command wc -l file.csv",
		"busybox grep pandas",
		"busybox sha256sum week.csv",
	}
	for _, body := range lookups {
		if !shellMetadataOnly(body) {
			t.Fatalf("a lookup was read as work: %q", body)
		}
	}
	// A heredoc marker with a trailing operator still bounds its body.
	if got := heredocMarker("<<'PY' && echo done"); got != "PY" {
		t.Fatalf("heredoc marker with trailing operator read as %q", got)
	}
	// A masked pipeline into a passive consumer reports the consumer's zero.
	for _, body := range []string{
		"go test ./internal/parser | wc -l",
		"go test ./internal/parser | head -n 5",
	} {
		if !shellMasksExit(body) {
			t.Fatalf("a pipeline into a passive consumer was not read as masking its producer: %q", body)
		}
	}
	// A write-only open is not an operand use; a hash/count is a lookup.
	if ops := programFileOperands(`open("week.csv", "w").write("x")`); len(ops) != 0 {
		t.Fatalf("a write-only open named an operand: %v", ops)
	}
	hash := `.venv/bin/python -c 'import hashlib; print(hashlib.sha256(open("week.csv","rb").read()).hexdigest())'`
	if !shellMetadataOnly(hash) {
		t.Fatalf("a file hash was read as work: %q", hash)
	}
	// A go test run that failed is a failure receipt even when a trailing
	// command reported zero.
	if !receiptShowsFailure("--- FAIL: TestParser (0.00s)\nFAIL\tgithub.com/x\t0.01s") {
		t.Fatal("a FAIL receipt was not read as a failure")
	}
}

// A parser-test failure must not pair with a later week.csv calculation merely
// because the goal text names both files.
func TestReviewForeignGoalFileDoesNotPair(t *testing.T) {
	goal := "make the parser tests pass in parser.go and also report the week.csv totals"
	success := `.venv/bin/python -c '\nimport csv\nwith open("week.csv") as f:\n    print(sum(1 for _ in f))'`
	if alternativeEligible(delegatedBashCall("s", success), "bash", "bash: go test ./internal/parser", goal) {
		t.Fatal("a foreign goal-named file paired a parser failure with a csv calculation")
	}
}

// ---- defect 12: a directory snapshot reads a submodule's commit -----------

func TestReviewSnapshotReadsSubmoduleHead(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, child, "init", "-q")
	if err := os.WriteFile(filepath.Join(child, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, child, "add", "a.txt")
	gitRun(t, child, "-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "-m", "one")
	first, _, err := snapshotContent(context.Background(), root, "child", " M", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, child, "add", "a.txt")
	gitRun(t, child, "-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "-m", "two")
	second, _, err := snapshotContent(context.Background(), root, "child", " M", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if first == "dir" || second == "dir" {
		t.Fatalf("a submodule directory was hashed as a constant: %q %q", first, second)
	}
	if first == second {
		t.Fatalf("two submodule commits produced the same snapshot identity: %q", first)
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s %v", args, out, err)
	}
}

// ---- defect 14: an abandoned turn cannot install or reserve a pairing -----

func TestReviewAbandonedTurnCannotInstallOrReservePairing(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
	ctx := context.Background()
	const goal = "cross-check week.csv totals for the ledger"
	a.mu.Lock()
	turn1 := a.turnSeq
	a.mu.Unlock()
	a.prepareBindingContext(ctx, goal)
	pre := a.captureSourceSnapshot(ctx).Identity
	// Turn 1 records a demonstrated failure (the abandoned goroutine's work).
	a.recordOutcome(ctx, turn1, delegatedBashCall("f1", `python -c 'import pandas'`), toolResult{text: "ModuleNotFoundError: No module named 'pandas'", isError: true}, pre)

	// Turn 2 begins and records its own failure.
	a.mu.Lock()
	a.turnSeq++
	turn2 := a.turnSeq
	a.mu.Unlock()
	a.prepareBindingContext(ctx, goal)
	a.recordOutcome(ctx, turn2, delegatedBashCall("f2", `python -c 'import pandas'`), toolResult{text: "ModuleNotFoundError: No module named 'pandas'", isError: true}, pre)
	a.memory.mu.Lock()
	key2 := a.memory.outcomeFailedKey
	a.memory.mu.Unlock()
	if key2 == "" {
		t.Fatal("turn 2 did not install its own failure key")
	}

	// The abandoned turn's success must not reserve turn 2's key.
	success := `.venv/bin/python -c '\nimport csv\nwith open("week.csv") as f:\n    print(sum(len(line) for line in f))'`
	a.recordOutcome(ctx, turn1, delegatedBashCall("s1", success), toolResult{text: "12.35"}, pre)

	a.memory.mu.Lock()
	done := a.memory.outcomeAlternativeDone
	key := a.memory.outcomeFailedKey
	a.memory.mu.Unlock()
	if done || key != key2 {
		t.Fatalf("an abandoned turn's success touched the live pairing: done=%v key=%q want %q", done, key, key2)
	}
	rows := attemptsForProject(t, brain, a)
	for _, row := range rows {
		if row.Status == store.AttemptSucceeded {
			t.Fatalf("an abandoned turn's success was journaled against the live failure: %+v", row)
		}
	}
}
