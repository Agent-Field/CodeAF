package session

// COVERAGE for the pure byte-count/prefix read-preview fix. The live
// acquisition (session e62e4aeb6fc60d70) ran the standalone
// `.venv/bin/python ledger.py vendor.csv` (a real UnicodeDecodeError) and then
// the diagnostic `.venv/bin/python -c "import sys; d=open('vendor.csv','rb').
// read(); print(len(d), d[:80])"`, which was stored as the sole observed
// successful alternative even though it only copies bytes out of the file to
// look at them and computes nothing. These tests pin the exact live goal and
// diagnostic, the aliased and inline-read variants of the same shape, and the
// genuine standard-library calculation that must still pair; they drive the real
// boundary -- [Agent.recordOutcome] for a session and a task worker -- plus the
// reader unit. They prove the mechanism, not model behaviour.

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/store"
)

// The EXACT live goal, failed command and diagnostic from the owned trace; never
// an altered goal or a shortened command invented to share a token.
const (
	previewGoal = "Check the supplier export vendor.csv against the current ledger utility before we change release code. Run only .venv/bin/python ledger.py vendor.csv as a standalone bash command, without a status echo or compound wrapper. Then independently verify its category and grand totals with a standard-library calculation under the same application interpreter. Do not modify any file."

	previewFailed = ".venv/bin/python ledger.py vendor.csv"

	// The exact live byte-count/prefix diagnostic.
	bytePreview = `.venv/bin/python -c "import sys; d=open('vendor.csv','rb').read(); print(len(d), d[:80])"`

	// The same diagnostic through an assigned literal alias and an inline read:
	// both are the same class and must be refused too.
	aliasPreview = `.venv/bin/python -c "import sys
p = 'vendor.csv'
d = open(p, 'rb').read()
print(len(d), d[:80])"`

	inlinePreview = `.venv/bin/python -c "print(len(open('vendor.csv','rb').read()), open('vendor.csv','rb').read()[:80])"`

	// The genuine standard-library calculation over the same goal file: it reads
	// with open(...).read(), DECODES and PARSES it and does Decimal arithmetic,
	// so it is real work and must stay the observed alternative.
	previewGenuine = `.venv/bin/python -c "import csv, io
from decimal import Decimal
raw = open('vendor.csv', 'rb').read()
text = raw.decode('utf-16')
rows = list(csv.DictReader(io.StringIO(text)))
tot = {}
for r in rows:
    tot[r['category']] = tot.get(r['category'], Decimal('0')) + Decimal(r['amount'])
for c in sorted(tot): print(c, tot[c])
print('grand total', sum(tot.values(), Decimal('0')))"`
)

// THE READER REFUSES THE PURE DIAGNOSTIC AND LEAVES REAL WORK. A program that
// reads a buffer and only prints its length and a bounded prefix is metadata; a
// program that decodes, parses or computes is not. An unreadable structure fails
// closed.
func TestReadPreviewOnlyRefusesDiagnosticAndLeavesWork(t *testing.T) {
	previews := map[string]string{
		"exact live diagnostic": `import sys; d=open('vendor.csv','rb').read(); print(len(d), d[:80])`,
		"literal alias":         "p = 'vendor.csv'\nd = open(p, 'rb').read()\nprint(len(d), d[:80])",
		"inline read":           `print(len(open('vendor.csv','rb').read()), open('vendor.csv','rb').read()[:80])`,
		"pathlib read_bytes":    "from pathlib import Path\nd = Path('vendor.csv').read_bytes()\nprint(len(d), d[:4].hex())",
		"path prefix hex":       "raw = open('vendor.csv','rb').read()\nprint(raw[:16].hex())",
	}
	for name, prog := range previews {
		if !programReadPreviewOnly(prog) {
			t.Errorf("%s was not recognised as the pure read-preview diagnostic: %q", name, prog)
		}
	}
	works := map[string]string{
		"decode then print":  "raw = open('vendor.csv','rb').read()\ntext = raw.decode('utf-16')\nprint(text)",
		"parse and total":    "import csv\nrows = list(csv.DictReader(open('vendor.csv', encoding='utf-16')))\nprint(sum(len(r) for r in rows))",
		"arithmetic print":   "d = open('vendor.csv','rb').read()\nprint(len(d) + 1)",
		"buffer printed raw": "d = open('vendor.csv','rb').read()\nprint(d)",
		"bare read":          "d = open('vendor.csv','rb').read()",
	}
	for name, prog := range works {
		if programReadPreviewOnly(prog) {
			t.Errorf("%s does real work but was refused as the pure preview: %q", name, prog)
		}
	}
	// UNKNOWN STRUCTURE FAILS CLOSED: an inline program the reader cannot bound
	// is refused rather than guessed to be work.
	for _, prog := range []string{
		"d = open('vendor.csv','rb').read()\nprint(len(d)",
		"d = open('vendor.csv','rb').read()\nprint([d[:80]",
		"print('unterminated",
	} {
		if !programReadPreviewOnly(prog) {
			t.Errorf("an unreadable program did not fail closed: %q", prog)
		}
	}
}

// THE TAXONOMY AGREES: the diagnostic is metadata and never an eligible
// alternative, while the genuine calculation is neither.
func TestReadPreviewTaxonomyAgrees(t *testing.T) {
	for name, body := range map[string]string{
		"exact live diagnostic": bytePreview,
		"literal alias":         aliasPreview,
		"inline read":           inlinePreview,
	} {
		if !shellMetadataOnly(body) {
			t.Errorf("%s was not metadata: %q", name, body)
		}
		if alternativeEligible(delegatedBashCall("x", body), "bash", "bash: "+previewFailed, previewGoal, "/w") {
			t.Errorf("%s was eligible as the alternative: %q", name, body)
		}
	}
	if shellMetadataOnly(previewGenuine) {
		t.Fatalf("the genuine calculation was mislabelled metadata: %q", previewGenuine)
	}
	if !alternativeEligible(delegatedBashCall("g", previewGenuine), "bash", "bash: "+previewFailed, previewGoal, "/w") {
		t.Fatalf("the genuine calculation was not eligible as the alternative: %q", previewGenuine)
	}
}

// A DIAGNOSTIC SUCCESS RECORDED BEFORE THE GENUINE CALCULATION DOES NOT CONSUME
// THE ONE ALTERNATIVE SLOT. The later genuine calculation is the single stored
// alternative and it names the frozen failure, at BOTH boundaries.
func TestReadPreviewDoesNotConsumeAlternativeAtBoundary(t *testing.T) {
	run := func(t *testing.T, delegated bool) {
		dir := t.TempDir()
		initRepo(t, dir)
		root, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
		ctx := context.Background()
		root.prepareBindingContext(ctx, previewGoal)
		if delegated {
			worker := spawnTaskWorker(t, root, dir)
			pre := worker.captureSourceSnapshot(ctx).Identity
			worker.recordOutcome(ctx, 0, delegatedBashCall("f1", previewFailed), vendorFailureReceipt(), pre)
			worker.recordOutcome(ctx, 0, delegatedBashCall("p1", bytePreview), toolResult{text: "4096 b'\\xff\\xfe6300'"}, pre)
			worker.recordOutcome(ctx, 0, delegatedBashCall("p2", aliasPreview), toolResult{text: "4096 b'\\xff\\xfe6300'"}, pre)
			worker.recordOutcome(ctx, 0, delegatedBashCall("p3", inlinePreview), toolResult{text: "4096 b'\\xff\\xfe6300'"}, pre)
			worker.recordOutcome(ctx, 0, delegatedBashCall("g1", previewGenuine), toolResult{text: "food 0.30\ntravel 37.05\ngrand total 37.35"}, pre)
		} else {
			pre := root.captureSourceSnapshot(ctx).Identity
			root.recordOutcome(ctx, 1, delegatedBashCall("f1", previewFailed), vendorFailureReceipt(), pre)
			root.recordOutcome(ctx, 1, delegatedBashCall("p1", bytePreview), toolResult{text: "4096 b'\\xff\\xfe6300'"}, pre)
			root.recordOutcome(ctx, 1, delegatedBashCall("p2", aliasPreview), toolResult{text: "4096 b'\\xff\\xfe6300'"}, pre)
			root.recordOutcome(ctx, 1, delegatedBashCall("p3", inlinePreview), toolResult{text: "4096 b'\\xff\\xfe6300'"}, pre)
			root.recordOutcome(ctx, 1, delegatedBashCall("g1", previewGenuine), toolResult{text: "food 0.30\ntravel 37.05\ngrand total 37.35"}, pre)
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
		if failed.Action != "bash: "+previewFailed {
			t.Fatalf("the stored failure was not the exact live command: %q", failed.Action)
		}
		if succeeded.AlternativeOf != failed.SourceKey {
			t.Fatalf("the genuine success named no failure: alt=%q failure=%q", succeeded.AlternativeOf, failed.SourceKey)
		}
		if !strings.Contains(succeeded.Action, "raw.decode('utf-16')") {
			t.Fatalf("the stored alternative was not the genuine calculation: %q", succeeded.Action)
		}
	}

	t.Run("root", func(t *testing.T) { run(t, false) })
	t.Run("delegated", func(t *testing.T) { run(t, true) })
}

// ONE FAILURE KEEPS ONE ALTERNATIVE: under a concurrent batch of diagnostics the
// genuine calculation still wins the single slot, at BOTH boundaries.
func TestReadPreviewKeepsOneAlternativeUnderRace(t *testing.T) {
	noise := []string{bytePreview, aliasPreview, inlinePreview}

	run := func(t *testing.T, delegated bool) {
		dir := t.TempDir()
		initRepo(t, dir)
		root, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
		ctx := context.Background()
		root.prepareBindingContext(ctx, previewGoal)
		if delegated {
			worker := spawnTaskWorker(t, root, dir)
			pre := worker.captureSourceSnapshot(ctx).Identity
			worker.recordOutcome(ctx, 0, delegatedBashCall("f1", previewFailed), vendorFailureReceipt(), pre)
			parallelOutcomes(8, func(i int) {
				if i == 3 {
					worker.recordOutcome(ctx, 0, delegatedBashCall("g", previewGenuine), toolResult{text: "grand total 37.35"}, pre)
					return
				}
				worker.recordOutcome(ctx, 0, delegatedBashCall(fmt.Sprintf("n%d", i), noise[i%len(noise)]), toolResult{text: "noise"}, pre)
			})
		} else {
			pre := root.captureSourceSnapshot(ctx).Identity
			root.recordOutcome(ctx, 1, delegatedBashCall("f1", previewFailed), vendorFailureReceipt(), pre)
			parallelOutcomes(8, func(i int) {
				if i == 3 {
					root.recordOutcome(ctx, 1, delegatedBashCall("g", previewGenuine), toolResult{text: "grand total 37.35"}, pre)
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
			if !strings.Contains(row.Action, "raw.decode('utf-16')") {
				t.Fatalf("a diagnostic stole the one slot under the race: %q", row.Action)
			}
		}
		if succeeded != 1 {
			t.Fatalf("expected exactly one stored alternative under the race, got %d: %+v", succeeded, rows)
		}
	}

	t.Run("root", func(t *testing.T) { run(t, false) })
	t.Run("delegated", func(t *testing.T) { run(t, true) })
}

// KNOWN METADATA, NO-OP, PIPELINE AND WRONG-FILE CONTROLS STAY REFUSED, so the
// diagnostic is caught by the same taxonomy that already refuses a lookup.
func TestReadPreviewKeepsKnownRefusals(t *testing.T) {
	dir := t.TempDir()
	refused := map[string]string{
		"metadata echo":  "echo vendor.csv",
		"no-op":          "true",
		"metadata cat":   "cat vendor.csv",
		"lookup wrapper": "cd " + dir + " && ls -la && cat vendor.csv",
		"wrong-file read": `.venv/bin/python -c "from pathlib import Path` +
			"\nprint(len(Path('" + filepath.Join(dir, "other.csv") + "').read_bytes()))\"",
	}
	for name, body := range refused {
		if alternativeEligible(delegatedBashCall("r", body), "bash", "bash: "+previewFailed, previewGoal, dir) {
			t.Errorf("%s was eligible as the alternative: %q", name, body)
		}
	}
}
