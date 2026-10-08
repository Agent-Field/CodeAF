package session

// COVERAGE for the stdin-heredoc reader fix. The failed acceptance this answers:
// the live acquisition (trace f7635df7ba2eb60a, session 7f5a21a70563ff5c) ran
// `.venv/bin/python ledger.py vendor.csv` (a real UnicodeDecodeError) and then
// independently computed the totals with a PYTHON STDIN HEREDOC
// (`.venv/bin/python - <<'PY' ... path = "vendor.csv"; raw = open(path, "rb")...
// PY`). The reader knew only literal call arguments, so the constant the
// heredoc assigned (path = "vendor.csv") and then READ was invisible, the whole
// program was misread as a check-only probe and no observed alternative could be
// stored beside the failure. These tests pin the exact live goal, failure and
// success and drive the real boundary -- [Agent.recordOutcome] for a session and
// a task worker -- plus the bounded reader unit and its negative controls. They
// prove the mechanism, not model behaviour.

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/store"
)

// The EXACT live goal and actions from the owned trace; never a shortened
// command invented to share a token, and never an altered goal.
const (
	heredocGoal = "Check the supplier export vendor.csv against the current ledger utility before we change release code. Run only .venv/bin/python ledger.py vendor.csv as a standalone bash command, without a status echo or compound wrapper. Then independently verify its category and grand totals with a standard-library calculation under the same application interpreter. Do not modify any file."

	heredocFailed = ".venv/bin/python ledger.py vendor.csv"

	heredocRead = `.venv/bin/python - <<'PY'
import csv, json
from decimal import Decimal
path = "vendor.csv"
raw = open(path, "rb").read()
if raw.startswith(b"\xff\xfe"): enc = "utf-16"
elif raw.startswith(b"\xfe\xff"): enc = "utf-16-be"
elif raw.startswith(b"\xef\xbb\xbf"): enc = "utf-8-sig"
else: enc = "utf-8"
print("detected encoding:", enc)
with open(path, newline="", encoding=enc) as f:
    by = {}
    n = 0
    for row in csv.DictReader(f):
        n += 1
        amt = Decimal(row["amount"])
        by[row["category"]] = by.get(row["category"], Decimal(0)) + amt
print("rows:", n)
Q = Decimal("0.01")
for c in sorted(by):
    print(f"{c}: {by[c].quantize(Q)}")
total = sum(by.values(), Decimal(0))
print("grand total:", total.quantize(Q))
print(json.dumps({c: str(by[c].quantize(Q)) for c in sorted(by)}))
PY`

	heredocSuccessReceipt = "detected encoding: utf-16\nrows: 4\nfood: 0.30\ntravel: 37.05\ngrand total: 37.35\n{\"food\": \"0.30\", \"travel\": \"37.05\"}"

	// NEGATIVE CONTROLS. Each is a probe or a non-executed echo, never a read.

	// cat(1) prints the heredoc body as TEXT: the `open("vendor.csv")` inside is
	// literal code that never runs, so it names no operand.
	heredocCatMetadata = `cat <<'PY'
open("vendor.csv")
PY`

	// The literal is assigned, never used by a read.
	heredocLiteralUnused = `.venv/bin/python - <<'PY'
path = "vendor.csv"
print(path)
PY`

	// A DYNAMIC alias (`compute()`), a REASSIGNMENT and a BLOCK-SCOPED literal
	// all fail closed: which value reaches open() is not provable.
	heredocDynamicAlias = `.venv/bin/python - <<'PY'
path = compute()
raw = open(path, "rb").read()
PY`
	heredocReassigned = `.venv/bin/python - <<'PY'
path = "vendor.csv"
path = other.csv
raw = open(path, "rb").read()
PY`
	heredocBlockScoped = `.venv/bin/python - <<'PY'
if flag:
    path = "vendor.csv"
raw = open(path, "rb").read()
PY`

	// A COMMENT and a fake call inside a printed string are not executed code.
	heredocComment = `.venv/bin/python - <<'PY'
# open("vendor.csv")
print("done")
PY`
	heredocFakeCode = `.venv/bin/python -c 'print("open(\"vendor.csv\")")'`
	heredocPrintf   = `printf 'open("vendor.csv")\n'`

	// MULTIPLE and UNTERMINATED stdin delimiters are not provable.
	heredocMultiple = `.venv/bin/python - <<'A' <<'B'
path = "vendor.csv"
raw = open(path, "rb").read()
A
B`
	heredocUnterminated = `.venv/bin/python - <<'PY'
path = "vendor.csv"
raw = open(path, "rb").read()`
)

// heredocOperands reads the whole body the way the boundary does: per quote-aware
// segment, so an unterminated heredoc that the segmenter split into loose lines
// names no operand.
func heredocOperands(body string) []string {
	var out []string
	for _, segment := range shellSegments(body) {
		out = append(out, shellSegmentOperands(segment)...)
	}
	return out
}

func heredocFailureReceipt() toolResult {
	return vendorFailureReceipt()
}

// THE READER RESOLVES THE HEREDOC'S ASSIGNED LITERAL AND REFUSES ITS FAIL-CLOSED
// SHAPES. A single clearly delimited interpreter stdin heredoc whose bare-name
// argument was assigned a simple string constant exactly once reads that file;
// a dynamic, reassigned, block-scoped, commented, printed or cat-fed body does
// not.
func TestVendorHeredocReadRecognition(t *testing.T) {
	if shellCheckOnly(heredocRead) {
		t.Fatal("the live heredoc calculation was misread as a check-only probe")
	}
	if got := heredocOperands(heredocRead); len(got) == 0 || got[0] != "vendor.csv" {
		t.Fatalf("the reader did not resolve the heredoc operand: %v", got)
	}
	if !alternativeEligible(delegatedBashCall("s", heredocRead), "bash", "bash: "+heredocFailed, heredocGoal, "/w") {
		t.Fatal("the live heredoc calculation was not eligible as the alternative")
	}
	if !sharesGoalNamedFileOperand(heredocRead, heredocGoal, "/w") {
		t.Fatal("the heredoc's resolved operand did not ground the goal-file pairing")
	}
	// The same read through Path(...).read_text is the same proven constant.
	pathRead := `.venv/bin/python - <<'PY'
from pathlib import Path
path = "vendor.csv"
raw = Path(path).read_text(encoding="utf-16")
PY`
	if !alternativeEligible(delegatedBashCall("p", pathRead), "bash", "bash: "+heredocFailed, heredocGoal, "/w") {
		t.Fatal("the heredoc Path(path).read_text calculation was not eligible")
	}

	// Each negative control names no operand and is never the alternative.
	for name, body := range map[string]string{
		"cat-fed heredoc":    heredocCatMetadata,
		"unused literal":     heredocLiteralUnused,
		"dynamic alias":      heredocDynamicAlias,
		"reassigned literal": heredocReassigned,
		"block-scoped":       heredocBlockScoped,
		"comment fake code":  heredocComment,
		"printed fake call":  heredocFakeCode,
		"printf text":        heredocPrintf,
		"multiple heredocs":  heredocMultiple,
		"unterminated":       heredocUnterminated,
	} {
		if got := heredocOperands(body); len(got) != 0 {
			t.Errorf("%s produced an operand: %v", name, got)
		}
		if alternativeEligible(delegatedBashCall("n", body), "bash", "bash: "+heredocFailed, heredocGoal, "/w") {
			t.Errorf("%s was eligible as the alternative: %q", name, body)
		}
	}

	// A READ OF A DIFFERENT FILE WITH THE SAME BASE NAME is a different lexical
	// identity and must not ground the pairing or steal the one slot.
	wrong := `.venv/bin/python - <<'PY'
path = "/other/project/vendor.csv"
raw = open(path, "rb").read()
PY`
	if sharesGoalNamedFileOperand(wrong, heredocGoal, "/w") {
		t.Fatal("a same-basename file under another directory grounded the goal pairing")
	}
	if alternativeEligible(delegatedBashCall("w", wrong), "bash", "bash: "+heredocFailed, heredocGoal, "/w") {
		t.Fatal("a read of a different file stole eligibility from the genuine calculation")
	}
}

// THE EXACT LIVE FAILURE AND SUCCESS FORM ONE PAIR AT BOTH BOUNDARIES, and the
// metadata, dynamic and wrong-path noise recorded first does not take the one
// alternative slot.
func TestVendorHeredocPairsActualFailureAtBoundary(t *testing.T) {
	wrongDir := t.TempDir()
	wrong := `.venv/bin/python - <<'PY'
path = "` + filepath.Join(wrongDir, "vendor.csv") + `"
raw = open(path, "rb").read()
PY`

	run := func(t *testing.T, delegated bool) {
		dir := t.TempDir()
		initRepo(t, dir)
		root, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
		ctx := context.Background()
		root.prepareBindingContext(ctx, heredocGoal)
		if delegated {
			worker := spawnTaskWorker(t, root, dir)
			pre := worker.captureSourceSnapshot(ctx).Identity
			worker.recordOutcome(ctx, 0, delegatedBashCall("f1", heredocFailed), heredocFailureReceipt(), pre)
			worker.recordOutcome(ctx, 0, delegatedBashCall("n1", heredocCatMetadata), toolResult{text: "open(\"vendor.csv\")"}, pre)
			worker.recordOutcome(ctx, 0, delegatedBashCall("n2", heredocDynamicAlias), toolResult{text: "ff fe"}, pre)
			worker.recordOutcome(ctx, 0, delegatedBashCall("n3", wrong), toolResult{text: "ff fe"}, pre)
			worker.recordOutcome(ctx, 0, delegatedBashCall("s1", heredocRead), toolResult{text: heredocSuccessReceipt}, pre)
		} else {
			pre := root.captureSourceSnapshot(ctx).Identity
			root.recordOutcome(ctx, 1, delegatedBashCall("f1", heredocFailed), heredocFailureReceipt(), pre)
			root.recordOutcome(ctx, 1, delegatedBashCall("n1", heredocCatMetadata), toolResult{text: "open(\"vendor.csv\")"}, pre)
			root.recordOutcome(ctx, 1, delegatedBashCall("n2", heredocDynamicAlias), toolResult{text: "ff fe"}, pre)
			root.recordOutcome(ctx, 1, delegatedBashCall("n3", wrong), toolResult{text: "ff fe"}, pre)
			root.recordOutcome(ctx, 1, delegatedBashCall("s1", heredocRead), toolResult{text: heredocSuccessReceipt}, pre)
		}

		rows := attemptsForProject(t, brain, root)
		if len(rows) != 2 {
			t.Fatalf("the failure and its one genuine alternative should be two rows, got %+v", rows)
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
			t.Fatalf("the pair was not written: %+v", rows)
		}
		if failed.Action != "bash: "+heredocFailed {
			t.Fatalf("the stored failure was not the exact live command: %q", failed.Action)
		}
		if succeeded.AlternativeOf != failed.SourceKey {
			t.Fatalf("the heredoc success named no failure: alt=%q failure=%q", succeeded.AlternativeOf, failed.SourceKey)
		}
		if !strings.Contains(succeeded.Action, `path = "vendor.csv"`) || !strings.Contains(succeeded.Action, "open(path, \"rb\").read()") {
			t.Fatalf("the stored alternative was not the live heredoc calculation: %q", succeeded.Action)
		}
	}

	t.Run("root", func(t *testing.T) { run(t, false) })
	t.Run("delegated", func(t *testing.T) { run(t, true) })
}

// ONE FAILURE KEEPS ONE ALTERNATIVE: the genuine heredoc calculation wins the
// single slot under a concurrent batch that also carries metadata, a dynamic
// alias and a same-basename read.
func TestVendorHeredocKeepsOneAlternativeUnderRace(t *testing.T) {
	wrongDir := t.TempDir()
	wrong := `.venv/bin/python - <<'PY'
path = "` + filepath.Join(wrongDir, "vendor.csv") + `"
raw = open(path, "rb").read()
PY`
	noise := []string{heredocCatMetadata, heredocDynamicAlias, wrong, "echo vendor.csv"}

	run := func(t *testing.T, delegated bool) {
		dir := t.TempDir()
		initRepo(t, dir)
		root, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
		ctx := context.Background()
		root.prepareBindingContext(ctx, heredocGoal)
		if delegated {
			worker := spawnTaskWorker(t, root, dir)
			pre := worker.captureSourceSnapshot(ctx).Identity
			worker.recordOutcome(ctx, 0, delegatedBashCall("f1", heredocFailed), heredocFailureReceipt(), pre)
			parallelOutcomes(8, func(i int) {
				if i == 3 {
					worker.recordOutcome(ctx, 0, delegatedBashCall("s", heredocRead), toolResult{text: "grand total: 37.35"}, pre)
					return
				}
				worker.recordOutcome(ctx, 0, delegatedBashCall(fmt.Sprintf("n%d", i), noise[i%len(noise)]), toolResult{text: "noise"}, pre)
			})
		} else {
			pre := root.captureSourceSnapshot(ctx).Identity
			root.recordOutcome(ctx, 1, delegatedBashCall("f1", heredocFailed), heredocFailureReceipt(), pre)
			parallelOutcomes(8, func(i int) {
				if i == 3 {
					root.recordOutcome(ctx, 1, delegatedBashCall("s", heredocRead), toolResult{text: "grand total: 37.35"}, pre)
					return
				}
				root.recordOutcome(ctx, 1, delegatedBashCall(fmt.Sprintf("n%d", i), noise[i%len(noise)]), toolResult{text: "noise"}, pre)
			})
		}
		rows := attemptsForProject(t, brain, root)
		succeeded := 0
		for _, row := range rows {
			if row.Status != store.AttemptSucceeded {
				continue
			}
			succeeded++
			if !strings.Contains(row.Action, `path = "vendor.csv"`) {
				t.Fatalf("noise stole the one slot under the race: %q", row.Action)
			}
		}
		if succeeded != 1 {
			t.Fatalf("expected exactly one stored alternative under the race, got %d: %+v", succeeded, rows)
		}
	}

	t.Run("root", func(t *testing.T) { run(t, false) })
	t.Run("delegated", func(t *testing.T) { run(t, true) })
}

// THE DOCUMENTED PARSER FALSE-OPENS ARE CLOSED. Each shape below was a verified
// lexical false-open or a delimiter class that named a stdin SOURCE as if it
// were a read: an explicit EMPTY -c program that must replace the heredoc body,
// an ambiguous/unterminated stdin header, a filename-shaped heredoc or
// here-string delimiter, and a literal the program does not actually execute
// (inside a triple-quoted string or a comment, continued across a backslash,
// used before it is declared, or shadowed in a function body). None may name an
// operand, ground the pairing, or be eligible as the observed alternative.
func TestVendorHeredocClosesParserFalseOpens(t *testing.T) {
	// D1: an EXPLICIT empty -c/-e program REPLACES the heredoc body, so the
	// body's read is not the executed program and names no operand. A bare `-`
	// (no inline program) keeps the body, which the vendored positive covers.
	emptyPrograms := map[string]string{
		"empty -c": `.venv/bin/python -c '' <<'PY'
path = "vendor.csv"
raw = open(path, "rb").read()
PY`,
		"empty -e": `.venv/bin/python -e "" <<'PY'
path = "vendor.csv"
raw = open(path, "rb").read()
PY`,
	}
	for name, body := range emptyPrograms {
		if got := heredocOperands(body); len(got) != 0 {
			t.Errorf("%s named an operand: %v", name, got)
		}
		if alternativeEligible(delegatedBashCall("e", body), "bash", "bash: "+heredocFailed, heredocGoal, "/w") {
			t.Errorf("%s was eligible as the alternative", name)
		}
	}

	// An EXPLICIT non-empty program likewise replaces the body: only the
	// program's own read is a use, never the discarded heredoc body.
	replace := `.venv/bin/python -c 'open("week.csv")' <<'PY'
path = "vendor.csv"
raw = open(path, "rb").read()
PY`
	if got := heredocOperands(replace); len(got) != 1 || got[0] != "week.csv" {
		t.Errorf("an explicit -c program did not replace the heredoc body: %v", got)
	}

	// D2 + P1: multiple or unterminated stdin sources and a filename-shaped
	// delimiter never name a file operand.
	falseOpens := map[string]string{
		"two stdin sources, filename delimiter": `.venv/bin/python - <<A <<x.csv
path = "vendor.csv"
raw = open(path, "rb").read()
A <<x.csv`,
		"unterminated heredoc": `.venv/bin/python - <<'PY'
path = "vendor.csv"
raw = open(path, "rb").read()`,
		"quoted filename delimiter": `.venv/bin/python - <<'vendor.csv'
print("no read")
vendor.csv`,
		"spaced filename delimiter": `.venv/bin/python - << 'vendor.csv'
print("no read")
vendor.csv`,
		"filename here-string":        `.venv/bin/python - <<<'vendor.csv'`,
		"filename delimiter, no body": `.venv/bin/python - <<'week.csv'`,
		// R-L2: an UNTERMINATED heredoc is fed to the interpreter's stdin to
		// EOF, so its body lines are never loose shell commands. A reader line
		// in that body must not falsely name the file.
		"unterminated body, reader one-liner": `.venv/bin/python - <<'PY'
.venv/bin/python -c 'open("vendor.csv")'`,
		"unterminated body, ledger run": `.venv/bin/python - <<'PY'
.venv/bin/python ledger.py vendor.csv`,
		"unterminated body, filename delimiter": `.venv/bin/python - <<x.csv
.venv/bin/python ledger.py vendor.csv`,
	}
	for name, body := range falseOpens {
		if got := heredocOperands(body); len(got) != 0 {
			t.Errorf("%s named an operand: %v", name, got)
		}
		if alternativeEligible(delegatedBashCall("d", body), "bash", "bash: "+heredocFailed, heredocGoal, "/w") {
			t.Errorf("%s was eligible as the alternative", name)
		}
	}

	// D3: a literal the program does not actually execute at top level before
	// the read names no operand.
	unexecuted := map[string]string{
		"assignment inside triple-quoted string": `.venv/bin/python - <<'PY'
doc = """
path = "vendor.csv"
"""
raw = open(path, "rb").read()
PY`,
		"assignment inside a comment": `.venv/bin/python - <<'PY'
# path = "vendor.csv"
raw = open(path, "rb").read()
PY`,
		"assignment across a continuation": `.venv/bin/python - <<'PY'
path = \
    "vendor.csv"
raw = open(path, "rb").read()
PY`,
		"use before declaration": `.venv/bin/python - <<'PY'
raw = open(path, "rb").read()
path = "vendor.csv"
PY`,
		"shadowed by a function parameter": `.venv/bin/python - <<'PY'
path = "vendor.csv"
def f(path):
    return open(path)
PY`,
		"shadowed by a block-local literal": `.venv/bin/python - <<'PY'
path = "vendor.csv"
def f():
    path = "vendor.csv"
    return open(path)
PY`,
		// R-L1: a ONE-LINE compound body is unindented yet still a suite the
		// reader cannot prove runs, so its read names no operand.
		"one-line def parameter shadow": `.venv/bin/python - <<'PY'
path = "vendor.csv"
def f(path): return open(path, "rb").read()
PY`,
		"one-line def body": `.venv/bin/python - <<'PY'
path = "vendor.csv"
def f(): return open(path, "rb").read()
PY`,
		"one-line if branch": `.venv/bin/python - <<'PY'
path = "vendor.csv"
if False: open(path, "rb").read()
PY`,
		"one-line lambda": `.venv/bin/python - <<'PY'
path = "vendor.csv"
x = lambda: open(path, "rb").read()
PY`,
	}
	for name, body := range unexecuted {
		if got := heredocOperands(body); len(got) != 0 {
			t.Errorf("%s named an operand: %v", name, got)
		}
		if alternativeEligible(delegatedBashCall("u", body), "bash", "bash: "+heredocFailed, heredocGoal, "/w") {
			t.Errorf("%s was eligible as the alternative", name)
		}
	}

	// The genuine top-level shape still resolves, and a read INSIDE a block of
	// the real vendor shape (the unindented `with open(path, ...)` header) is
	// still read, so the hardening only closed non-executed and shadowed names.
	stillReads := `.venv/bin/python - <<'PY'
from pathlib import Path
path = "vendor.csv"
with open(path, newline="", encoding="utf-16") as f:
    rows = f.read()
milestone = Path(path).read_bytes()
PY`
	if got := heredocOperands(stillReads); len(got) == 0 || got[0] != "vendor.csv" {
		t.Errorf("the top-level vendor shape stopped resolving: %v", got)
	}

	// R-L4: an EXPLICIT EMPTY inline program executes nothing, so the file
	// arguments left on its command line are the interpreter's argv and name no
	// operand. A non-empty inline program and an ordinary script run keep their
	// ordinary command literal operands.
	emptyInline := map[string]string{
		"empty -c with bare file argument": `.venv/bin/python -c '' vendor.csv`,
		"empty -c, bare arg and heredoc": `.venv/bin/python -c '' vendor.csv <<'PY'
path = "vendor.csv"
raw = open(path, "rb").read()
PY`,
	}
	for name, body := range emptyInline {
		if got := heredocOperands(body); len(got) != 0 {
			t.Errorf("%s named an operand: %v", name, got)
		}
		if alternativeEligible(delegatedBashCall("l", body), "bash", "bash: "+heredocFailed, heredocGoal, "/w") {
			t.Errorf("%s was eligible as the alternative", name)
		}
	}
	for name, body := range map[string]string{
		"ordinary script run":       `.venv/bin/python ledger.py vendor.csv`,
		"real inline -c reader":     `.venv/bin/python -c 'open("vendor.csv")'`,
		"non-empty -c and file arg": `.venv/bin/python -c 'open("week.csv")' vendor.csv`,
	} {
		if got := heredocOperands(body); len(got) == 0 {
			t.Errorf("%s stopped naming its ordinary literal operand: %v", name, got)
		}
	}
}
