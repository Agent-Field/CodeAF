package session

// COVERAGE for the pathlib byte-reader fix. The failed acceptance this answers:
// the live acquisition (trace ee16d52197737e4a, session 1c3b8e94db79d8ee) ran
// `.venv/bin/python ledger.py vendor.csv` (a real UnicodeDecodeError) and then
// independently computed the totals with `.venv/bin/python -c "...pathlib.Path(
// 'vendor.csv').read_bytes()..."`. The eligibility reader recognised only a bare
// `open(...)` inside an inline program, so the genuine calculation was misread as
// a check-only probe (no operand) and no observed alternative could be stored
// beside the failure. These tests pin the exact live goal, failure and success
// and drive the real boundary -- [Agent.recordOutcome] for a session and a task
// worker -- plus the reader unit. They prove the mechanism, not model behaviour.

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
	vendorGoal = "The supplier exports UTF-16 CSV. Create a vendor.csv development fixture by converting the current week.csv data to UTF-16. Do not change the utility yet. Run only .venv/bin/python ledger.py vendor.csv as its own first bash command after conversion, without a status echo or compound wrapper. Then independently calculate category and grand totals with the application interpreter. Keep release source unchanged."

	vendorFailed = ".venv/bin/python ledger.py vendor.csv"

	vendorReadBytes = `.venv/bin/python -c "
import csv, io, pathlib
from decimal import Decimal
raw = pathlib.Path('vendor.csv').read_bytes()
print('first bytes:', raw[:4].hex())
text = raw.decode('utf-16')
rows = list(csv.DictReader(io.StringIO(text)))
tot = {}
for r in rows:
    tot[r['category']] = tot.get(r['category'], Decimal('0')) + Decimal(r['amount'])
grand = sum(tot.values(), Decimal('0'))
for c in sorted(tot): print(c, tot[c].quantize(Decimal('0.01')))
print('grand total', grand.quantize(Decimal('0.01')))
"`

	// A metadata-only preview of the goal path: the path is built and printed,
	// never read, so it names no operand and is not work.
	vendorPreview = `.venv/bin/python -c "from pathlib import Path
print(Path('vendor.csv'))"`

	// A stat() preview of the goal path: it inspects metadata, it does not READ
	// the file, so it must stay a check-only probe.
	vendorStat = `.venv/bin/python -c "from pathlib import Path
print(Path('vendor.csv').stat().st_size)"`
)

func vendorFailureReceipt() toolResult {
	return toolResult{text: "Traceback (most recent call last):\n  File \"/home/santosh/src/contextual-acceptance-work-20261006/ledger/ledger.py\", line 53, in <module>\n    sys.exit(main(sys.argv))\n  File \"/home/santosh/src/contextual-acceptance-work-20261006/ledger/ledger.py\", line 16, in totals\n    for row in csv.DictReader(source):\n  File \"/usr/lib/python3.12/encodings/utf_8_sig.py\", line 69, in _buffer_decode\n    return codecs.utf_8_decode(input, errors, final)\n           ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^\nUnicodeDecodeError: 'utf-8' codec can't decode byte 0xff in position 0: invalid start byte\n\n\nCommand exited with code 1", isError: true}
}

// THE READER RECOGNISES THE PATHLIB READ AND REFUSES ITS PREVIEWS. A path
// constructor read by `.read_bytes()`/`.read_text()`/`.open()` is real work on a
// file; a constructor that is only built, printed or stat()ed reads nothing.
func TestVendorPathlibReadRecognition(t *testing.T) {
	if !alternativeEligible(delegatedBashCall("s", vendorReadBytes), "bash", "bash: "+vendorFailed, vendorGoal, "/w") {
		t.Fatal("the live pathlib read_bytes calculation was not eligible as the alternative")
	}
	// The goal-file link also sees the actual operand, not just the token link.
	if !sharesGoalNamedFileOperand(vendorReadBytes, vendorGoal, "/w") {
		t.Fatal("the pathlib read_bytes operand did not ground the goal-file pairing")
	}
	if got := shellSegmentOperands(vendorReadBytes); len(got) != 1 || got[0] != "vendor.csv" {
		t.Fatalf("the reader did not extract exactly the read operand: %v", got)
	}
	// EQUIVALENT ACTUAL READ CONSTRUCTORS, only as justified: read_text and
	// open() on a path object are the same read; a bare path object is not.
	readText := `.venv/bin/python -c "from pathlib import Path
raw = pathlib.Path('vendor.csv').read_text(encoding='utf-16')"`
	if !alternativeEligible(delegatedBashCall("rt", readText), "bash", "bash: "+vendorFailed, vendorGoal, "/w") {
		t.Fatal("pathlib read_text on the goal file was not eligible")
	}
	pathOpen := `.venv/bin/python -c "from pathlib import Path
f = Path('vendor.csv').open()"`
	if !alternativeEligible(delegatedBashCall("po", pathOpen), "bash", "bash: "+vendorFailed, vendorGoal, "/w") {
		t.Fatal("Path.open on the goal file was not eligible")
	}

	// NEGATIVE CONTROLS: a metadata-only preview, a stat() preview and a trivial
	// command name no operand and must never be the alternative.
	for name, body := range map[string]string{
		"metadata-only preview": vendorPreview,
		"stat() preview":        vendorStat,
		"trivial echo":          "echo vendor.csv",
	} {
		if alternativeEligible(delegatedBashCall("n", body), "bash", "bash: "+vendorFailed, vendorGoal, "/w") {
			t.Errorf("%s was eligible as the alternative: %q", name, body)
		}
	}

	// A READ OF A DIFFERENT FILE WITH THE SAME BASE NAME names a different
	// lexical identity and must not ground the pairing.
	wrong := `.venv/bin/python -c "from pathlib import Path
raw = Path('/other/project/vendor.csv').read_bytes()"`
	if sharesGoalNamedFileOperand(wrong, vendorGoal, "/w") {
		t.Fatal("a same-basename file under another directory grounded the goal pairing")
	}
	if alternativeEligible(delegatedBashCall("w", wrong), "bash", "bash: "+vendorFailed, vendorGoal, "/w") {
		t.Fatal("a read of a different file stole eligibility from the genuine calculation")
	}
}

// THE EXACT LIVE FAILURE AND SUCCESS FORM ONE PAIR AT BOTH BOUNDARIES, and the
// metadata/stat previews and the wrong-path read recorded first do not take the
// one alternative slot.
func TestVendorReadBytesPairsActualFailureAtBoundary(t *testing.T) {
	wrongDir := t.TempDir()
	wrong := `.venv/bin/python -c "from pathlib import Path
raw = Path('` + filepath.Join(wrongDir, "vendor.csv") + `').read_bytes()"`

	run := func(t *testing.T, delegated bool) {
		dir := t.TempDir()
		initRepo(t, dir)
		root, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
		ctx := context.Background()
		root.prepareBindingContext(ctx, vendorGoal)
		if delegated {
			worker := spawnTaskWorker(t, root, dir)
			pre := worker.captureSourceSnapshot(ctx).Identity
			worker.recordOutcome(ctx, 0, delegatedBashCall("f1", vendorFailed), vendorFailureReceipt(), pre)
			worker.recordOutcome(ctx, 0, delegatedBashCall("n1", vendorPreview), toolResult{text: "vendor.csv"}, pre)
			worker.recordOutcome(ctx, 0, delegatedBashCall("n2", vendorStat), toolResult{text: "12"}, pre)
			worker.recordOutcome(ctx, 0, delegatedBashCall("n3", "echo vendor.csv"), toolResult{text: "vendor.csv"}, pre)
			worker.recordOutcome(ctx, 0, delegatedBashCall("n4", wrong), toolResult{text: "ff fe"}, pre)
			worker.recordOutcome(ctx, 0, delegatedBashCall("s1", vendorReadBytes), toolResult{text: "first bytes: fffe6300\nfood 0.30\ntravel 37.05\ngrand total 37.35"}, pre)
		} else {
			pre := root.captureSourceSnapshot(ctx).Identity
			root.recordOutcome(ctx, 1, delegatedBashCall("f1", vendorFailed), vendorFailureReceipt(), pre)
			root.recordOutcome(ctx, 1, delegatedBashCall("n1", vendorPreview), toolResult{text: "vendor.csv"}, pre)
			root.recordOutcome(ctx, 1, delegatedBashCall("n2", vendorStat), toolResult{text: "12"}, pre)
			root.recordOutcome(ctx, 1, delegatedBashCall("n3", "echo vendor.csv"), toolResult{text: "vendor.csv"}, pre)
			root.recordOutcome(ctx, 1, delegatedBashCall("n4", wrong), toolResult{text: "ff fe"}, pre)
			root.recordOutcome(ctx, 1, delegatedBashCall("s1", vendorReadBytes), toolResult{text: "first bytes: fffe6300\nfood 0.30\ntravel 37.05\ngrand total 37.35"}, pre)
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
		if failed.Action != "bash: "+vendorFailed {
			t.Fatalf("the stored failure was not the exact live command: %q", failed.Action)
		}
		if succeeded.AlternativeOf != failed.SourceKey {
			t.Fatalf("the pathlib read_bytes success named no failure: alt=%q failure=%q", succeeded.AlternativeOf, failed.SourceKey)
		}
		if !strings.Contains(succeeded.Action, "pathlib.Path('vendor.csv').read_bytes()") {
			t.Fatalf("the stored alternative was not the live pathlib calculation: %q", succeeded.Action)
		}
	}

	t.Run("root", func(t *testing.T) { run(t, false) })
	t.Run("delegated", func(t *testing.T) { run(t, true) })
}

// ONE FAILURE KEEPS ONE ALTERNATIVE: the genuine pathlib calculation wins the
// single slot under a concurrent batch that also carries metadata previews and
// reads of a different file.
func TestVendorReadBytesKeepsOneAlternativeUnderRace(t *testing.T) {
	wrongDir := t.TempDir()
	wrong := `.venv/bin/python -c "from pathlib import Path
raw = Path('` + filepath.Join(wrongDir, "vendor.csv") + `').read_bytes()"`
	noise := []string{vendorPreview, vendorStat, wrong, "echo vendor.csv"}

	run := func(t *testing.T, delegated bool) {
		dir := t.TempDir()
		initRepo(t, dir)
		root, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
		ctx := context.Background()
		root.prepareBindingContext(ctx, vendorGoal)
		if delegated {
			worker := spawnTaskWorker(t, root, dir)
			pre := worker.captureSourceSnapshot(ctx).Identity
			worker.recordOutcome(ctx, 0, delegatedBashCall("f1", vendorFailed), vendorFailureReceipt(), pre)
			parallelOutcomes(8, func(i int) {
				if i == 3 {
					worker.recordOutcome(ctx, 0, delegatedBashCall("s", vendorReadBytes), toolResult{text: "grand total 37.35"}, pre)
					return
				}
				worker.recordOutcome(ctx, 0, delegatedBashCall(fmt.Sprintf("n%d", i), noise[i%len(noise)]), toolResult{text: "noise"}, pre)
			})
		} else {
			pre := root.captureSourceSnapshot(ctx).Identity
			root.recordOutcome(ctx, 1, delegatedBashCall("f1", vendorFailed), vendorFailureReceipt(), pre)
			parallelOutcomes(8, func(i int) {
				if i == 3 {
					root.recordOutcome(ctx, 1, delegatedBashCall("s", vendorReadBytes), toolResult{text: "grand total 37.35"}, pre)
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
			if !strings.Contains(row.Action, "pathlib.Path('vendor.csv').read_bytes()") {
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
