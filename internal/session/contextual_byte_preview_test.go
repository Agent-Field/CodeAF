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

	// THE EXACT LIVE BOUNDED-SIZE PROBE: the historic alternative that build
	// 902b4eb8c still served because `.read(4)` carried a non-empty, literal size
	// argument the reader refused. A small literal count is a bounded pure read
	// and must be projected out exactly like the zero-argument probe.
	byteSizePreview = `.venv/bin/python -c "import sys; d=open('vendor.csv','rb').read(4); print(d)"`

	// The same bounded-size read at a larger literal count: the acceptance is the
	// LITERAL SHAPE, not a specific number.
	byteSize64Preview = `.venv/bin/python -c "import sys; d=open('vendor.csv','rb').read(64); print(d)"`

	// The same diagnostic through an assigned literal alias and an inline read:
	// both are the same class and must be refused too.
	aliasPreview = `.venv/bin/python -c "import sys
p = 'vendor.csv'
d = open(p, 'rb').read()
print(len(d), d[:80])"`

	inlinePreview = `.venv/bin/python -c "print(len(open('vendor.csv','rb').read()), open('vendor.csv','rb').read()[:80])"`

	// THE EXACT e62 SECOND SUCCESSFUL COMMAND: it dumps the WHOLE decoded file to
	// look at it and computes nothing, so it belongs to the same inspection class
	// as the byte probe. It is the command that would still have stolen the one
	// alternative slot had only len/prefix been refused.
	fullDumpPreview = `.venv/bin/python -c "print(open('vendor.csv',encoding='utf-16').read())"`

	// The same full dump through an assigned read buffer and a STRAIGHT known
	// decode, and a decoded PREFIX of that buffer: both inspect, neither computes.
	decodedDumpPreview = `.venv/bin/python -c "d = open('vendor.csv','rb').read()
print(d.decode('utf-16'))"`
	decodedPrefixPreview = `.venv/bin/python -c "d = open('vendor.csv','rb').read()
print(d[:80].decode('utf-16'))"`

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
		"bounded literal size":  "d = open('vendor.csv','rb').read(4)\nprint(d)",
		"bounded size inline":   `print(len(open('vendor.csv','rb').read(64)), open('vendor.csv','rb').read(4)[:4])`,
		"pathlib read_bytes":    "from pathlib import Path\nd = Path('vendor.csv').read_bytes()\nprint(len(d), d[:4].hex())",
		"path prefix hex":       "raw = open('vendor.csv','rb').read()\nprint(raw[:16].hex())",
		// THE e62 SECOND SUCCESS: a full decoded dump is inspection too.
		"full decoded dump": `print(open('vendor.csv',encoding='utf-16').read())`,
		// A raw buffer printed whole, and a straight known-content decode of it,
		// are the same inspection class as a length or a prefix.
		"raw buffer printed": "d = open('vendor.csv','rb').read()\nprint(d)",
		"decoded buffer":     "d = open('vendor.csv','rb').read()\nprint(d.decode('utf-16'))",
		"decoded prefix":     "d = open('vendor.csv','rb').read()\nprint(d[:80].decode('utf-16'))",
	}
	for name, prog := range previews {
		if !programReadPreviewOnly(prog) {
			t.Errorf("%s was not recognised as the pure read-preview diagnostic: %q", name, prog)
		}
	}
	works := map[string]string{
		"decode then parse": "raw = open('vendor.csv','rb').read()\ntext = raw.decode('utf-16')\nrows = list(csv.DictReader(io.StringIO(text)))\nprint(len(rows))",
		"parse and total":   "import csv\nrows = list(csv.DictReader(open('vendor.csv', encoding='utf-16')))\nprint(sum(len(r) for r in rows))",
		"arithmetic print":  "d = open('vendor.csv','rb').read()\nprint(len(d) + 1)",
		"computed decode":   "d = open('vendor.csv','rb').read()\nprint(d.decode(enc))",
		"unknown call":      "d = open('vendor.csv','rb').read()\nprint(d.splitlines())",
		"bare read":         "d = open('vendor.csv','rb').read()",
		// A non-literal SIZE is not a proven bound and must fail closed.
		"symbolic size": "d = open('vendor.csv','rb').read(n)\nprint(d)",
		"computed size": "d = open('vendor.csv','rb').read(2 + 2)\nprint(d)",
		"signed size":   "d = open('vendor.csv','rb').read(-1)\nprint(d)",
		"called size":   "d = open('vendor.csv','rb').read(size())\nprint(d)",
		"string size":   "d = open('vendor.csv','rb').read('4')\nprint(d)",
		"two sizes":     "d = open('vendor.csv','rb').read(4, 8)\nprint(d)",
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
		"bounded literal size":  byteSizePreview,
		"e62 full read dump":    fullDumpPreview,
		"decoded buffer":        decodedDumpPreview,
		"decoded prefix":        decodedPrefixPreview,
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
			worker.recordOutcome(ctx, 0, delegatedBashCall("p3b", byteSizePreview), toolResult{text: "b'\\xff\\xfec\\x00'"}, pre)
			worker.recordOutcome(ctx, 0, delegatedBashCall("p3c", byteSize64Preview), toolResult{text: "b'\\xff\\xfec\\x00'"}, pre)
			worker.recordOutcome(ctx, 0, delegatedBashCall("p4", fullDumpPreview), toolResult{text: "category,amount\nfood,0.30"}, pre)
			worker.recordOutcome(ctx, 0, delegatedBashCall("p5", decodedDumpPreview), toolResult{text: "category,amount\nfood,0.30"}, pre)
			worker.recordOutcome(ctx, 0, delegatedBashCall("g1", previewGenuine), toolResult{text: "food 0.30\ntravel 37.05\ngrand total 37.35"}, pre)
		} else {
			pre := root.captureSourceSnapshot(ctx).Identity
			root.recordOutcome(ctx, 1, delegatedBashCall("f1", previewFailed), vendorFailureReceipt(), pre)
			root.recordOutcome(ctx, 1, delegatedBashCall("p1", bytePreview), toolResult{text: "4096 b'\\xff\\xfe6300'"}, pre)
			root.recordOutcome(ctx, 1, delegatedBashCall("p2", aliasPreview), toolResult{text: "4096 b'\\xff\\xfe6300'"}, pre)
			root.recordOutcome(ctx, 1, delegatedBashCall("p3", inlinePreview), toolResult{text: "4096 b'\\xff\\xfe6300'"}, pre)
			root.recordOutcome(ctx, 1, delegatedBashCall("p3b", byteSizePreview), toolResult{text: "b'\\xff\\xfec\\x00'"}, pre)
			root.recordOutcome(ctx, 1, delegatedBashCall("p3c", byteSize64Preview), toolResult{text: "b'\\xff\\xfec\\x00'"}, pre)
			root.recordOutcome(ctx, 1, delegatedBashCall("p4", fullDumpPreview), toolResult{text: "category,amount\nfood,0.30"}, pre)
			root.recordOutcome(ctx, 1, delegatedBashCall("p5", decodedDumpPreview), toolResult{text: "category,amount\nfood,0.30"}, pre)
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
	noise := []string{bytePreview, aliasPreview, inlinePreview, byteSizePreview, byteSize64Preview, fullDumpPreview, decodedDumpPreview, decodedPrefixPreview}

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

// ── the read-time projection guard ──────────────────────────────────────────
//
// THE OLD BAD ALTERNATIVE IS IMMUTABLE AND THE FIX IS AT THE READ. The live
// session e62 stored the pure byte-count/prefix diagnostic as the observed
// alternative BEFORE the writer refused such a shape, so the journal row cannot
// be altered or deleted. [Agent.priorOutcomeBlock] therefore projects it out of
// the rendered pair while leaving the raw event and the failure history exactly
// where they were. This test seeds that historic shape directly (the way the
// journal really holds it), drives the real read, and pins all five properties
// the steer names: the known complete byte probe is filtered, the failed row is
// left, a later genuine pair is shown, a legacy clipped program is retained
// honestly, and the raw journal is unmodified.

// The exact live diagnostic as attemptAction would STORE it: the "bash: " tool
// prefix plus the raw body, well inside the 240-rune display clip so the reader
// can prove the whole program.
const storedBytePreview = "bash: " + bytePreview

func TestProjectionOmitsHistoricPurePreviewAlternative(t *testing.T) {
	dir := t.TempDir()
	root, brain := brainAgent(t, &reflexScript{}, func(c *Config) {
		c.Workspace = dir
		c.MemoryProjectKey = "p"
	})
	owner := store.OwnerProject("p")

	// 1. The immutable historic failure, and its historic pure-preview
	// alternative as the OLD build stored it.
	appendH := func(id, action, status, alt string, receipts ...string) {
		t.Helper()
		_, err := brain.AppendContextualAttempt(store.ContextualAttempt{
			ID: id, Owner: owner, Tool: "bash", Action: action, Goal: previewGoal,
			Status: status, ReceiptIDs: receipts, Observation: "historic row",
			Snapshot: "snapA", SourceKey: id, AlternativeOf: alt,
		})
		if err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	appendH("hist-fail", "bash: "+previewFailed, store.AttemptFailed, "", "r0")
	appendH("hist-noise", storedBytePreview, store.AttemptSucceeded, "hist-fail", "r1")
	// The exact e62 SECOND successful command, stored the same way: a whole-file
	// read dump is the same inspection class and is projected out too.
	appendH("hist-dump", "bash: "+fullDumpPreview, store.AttemptSucceeded, "hist-fail", "r4")

	// 2. A LATER GENUINE REAL PAIR: the standard-library calculation, stored in
	// its real display-clipped form, plus a LEGACY CLIPPED diagnostic-looking
	// program whose tail the clip hid. Neither is judged from an incomplete
	// source, so both stay.
	genuineStored := "bash: " + contextualClip(previewGenuine, 240)
	appendH("hist-genuine", genuineStored, store.AttemptSucceeded, "hist-fail", "r2")
	legacyClipped := "bash: " + `.venv/bin/python -c "import sys; d=open('vendor.csv','rb').read(); print(len(d), d[:80])` + "\u2026"
	appendH("hist-legacy", legacyClipped, store.AttemptSucceeded, "hist-fail", "r3")

	before := attemptsForProject(t, brain, root)

	block := root.priorOutcomeBlock(brain, "p", previewGoal, "snapA")
	if block == "" {
		t.Fatal("the projection rendered nothing: the failure history vanished")
	}
	// FILTERED: the known complete past byte probe never reaches the model, and
	// neither does the exact e62 full-file dump.
	if strings.Contains(block, bytePreview) {
		t.Fatalf("the historic pure preview was still rendered:\n%s", block)
	}
	if strings.Contains(block, fullDumpPreview) {
		t.Fatalf("the historic full read dump was still rendered:\n%s", block)
	}
	// LEFT: the failure history remains.
	if !strings.Contains(block, previewFailed) {
		t.Fatalf("the failure history was dropped with its alternative:\n%s", block)
	}
	// SHOWN: the later genuine real pair is rendered, honestly clipped (the
	// block quotes the action with Go escaping, so the marker is unescaped).
	if !strings.Contains(block, "raw.decode('utf-16')") {
		t.Fatalf("the genuine later alternative was not shown:\n%s", block)
	}
	// RETAINED: the clipped/unknown program is NOT mislabelled metadata.
	if !strings.Contains(block, "print(len(d), d[:80])") {
		t.Fatalf("a clipped program was silently dropped as metadata:\n%s", block)
	}
	if got := strings.Count(block, "Observed successful alternative"); got != 2 {
		t.Fatalf("expected the two honest alternatives, got %d:\n%s", got, block)
	}

	// UNMODIFIED: the read wrote nothing; every raw row is byte-for-byte here.
	after := attemptsForProject(t, brain, root)
	if len(after) != len(before) {
		t.Fatalf("the projection changed the journal: %d rows before, %d after", len(before), len(after))
	}
	for i := range before {
		if before[i].Action != after[i].Action || before[i].Status != after[i].Status || before[i].AlternativeOf != after[i].AlternativeOf {
			t.Fatalf("row %d changed under the projection: %+v -> %+v", i, before[i], after[i])
		}
	}
	var previewRow *store.ContextualAttempt
	for i := range after {
		if after[i].ID == "hist-noise" {
			previewRow = &after[i]
		}
	}
	if previewRow == nil || previewRow.Action != storedBytePreview {
		t.Fatalf("the raw preview event was altered: %+v", previewRow)
	}
}

// THE GUARD IS PURE-POSITIVE AND FAILS CLOSED. Only the complete known preview
// skeleton is proven metadata; a genuine complete calculation, a clipped
// program of any kind, a non-shell action and an unreadable one are all left as
// work so nothing genuine is ever dropped from history.
func TestProjectionGuardProvesOnlyCompleteKnownPreview(t *testing.T) {
	proven := map[string]string{
		"exact live diagnostic": storedBytePreview,
		"literal alias":         "bash: " + aliasPreview,
		"inline read":           "bash: " + inlinePreview,
		"bounded literal size":  "bash: " + byteSizePreview,
		"e62 full read dump":    "bash: " + fullDumpPreview,
		"decoded buffer":        "bash: " + decodedDumpPreview,
		"decoded prefix":        "bash: " + decodedPrefixPreview,
	}
	for name, action := range proven {
		if !priorAlternativeProvenPurePreview(action) {
			t.Errorf("%s was not proven a pure preview: %q", name, action)
		}
	}
	kept := map[string]string{
		"complete genuine calculation": "bash: " + previewGenuine,
		"decode then parse":            "bash: " + `.venv/bin/python -c "raw = open('vendor.csv','rb').read(); text = raw.decode('utf-16'); rows = list(csv.DictReader(io.StringIO(text))); print(len(rows))"`,
		"unbounded unknown size":       "bash: " + `.venv/bin/python -c "import sys; d=open('vendor.csv','rb').read(n); print(d)"`,
		"clipped genuine calculation":  "bash: " + contextualClip(previewGenuine, 240),
		"legacy clipped program":       "bash: " + `.venv/bin/python -c "import sys; d=open('vendor.csv','rb').read(); print(len(d), d[:80])` + "\u2026",
		"unproven clipping marker":     "bash: " + inlinePreview + "\u2026",
		"unreadable structure":         "bash: " + ".venv/bin/python -c \"d = open('vendor.csv','rb').read()\nprint(len(d)\"",
		"not a shell action":           "read: vendor.csv",
		"no body":                      "bash",
		"blank":                        "   ",
	}
	for name, action := range kept {
		if priorAlternativeProvenPurePreview(action) {
			t.Errorf("%s was misclassified as the pure preview: %q", name, action)
		}
	}
}
