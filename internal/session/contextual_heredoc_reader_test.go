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
