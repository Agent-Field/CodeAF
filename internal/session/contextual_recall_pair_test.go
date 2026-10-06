package session

// REGRESSION for the review's pair-preservation defect, found by root after the
// D1 exact-identity fix (20a2a195f). Two grounded failures about ONLY the goal
// file vendor.csv (plain `ledger.py vendor.csv` and `ledger.py --strict
// vendor.csv`, neither with an observed alternative) rank ahead of a third,
// eligible and relevant failure whose command read vendor.csv fine and died on
// week.csv - and which DOES carry the observed working alternative. The two-slot
// note let the two bare rows fill both slots and dropped the demonstrated
// working method entirely.
//
// The second slot is now COMPLEMENTARY: when the leading row carries no
// alternative, the slot is reserved for the highest-ranked alternative-bearing
// failure, its own pair kept whole. These tests drive the real read seam
// [Agent.priorOutcomeContext] on rows written through the production store seam,
// and do not re-implement the selector.

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/store"
)

const recallPairGoal = "Calculate vendor.csv category totals and grand total under the application interpreter. Do not modify files."

const (
	recallPairPlain  = ".venv/bin/python ledger.py vendor.csv"
	recallPairStrict = ".venv/bin/python ledger.py --strict vendor.csv"
	// The mixed probe: vendor.csv read fine as utf-16, then week.csv with the
	// default codec (Traceback). One extra, non-goal input - and it owns the
	// observed working alternative.
	recallPairMixed = `.venv/bin/python --version && echo "---vendor---" && .venv/bin/python -c "print(open('vendor.csv',encoding='utf-16').read())" && echo "---week---" && .venv/bin/python -c "print(open('week.csv').read())"`
	recallPairAlt   = `.venv/bin/python -c "import csv; t={}; [t.__setitem__(r['category'], t.get(r['category'],0)+float(r['amount'])) for r in csv.DictReader(open('vendor.csv', newline='', encoding='utf-16'))]; print(t)"`
)

func recallPairAgent(t *testing.T) (*Agent, *store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	initRepo(t, dir)
	a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
	return a, brain, store.OwnerProject("p")
}

// 1. THE COUNTEREXAMPLE. Two precise, alternative-less failures on the goal file
// plus one mixed failure carrying the working alternative: exactly two failure
// bullets render AND the observed working alternative survives, in the same
// bullet as its own failure.
func TestPriorOutcomeBarePairDoesNotCrowdOutWorkingAlternative(t *testing.T) {
	a, brain, owner := recallPairAgent(t)
	seedFailure(t, brain, owner, recallPairGoal, "bash: "+recallPairPlain,
		"PLAIN-FAILURE UnicodeDecodeError: 'utf-8' codec can't decode byte 0xff", "turn:1:plain", "h1", recallSnapshot)
	seedFailure(t, brain, owner, recallPairGoal, "bash: "+recallPairStrict,
		"STRICT-FAILURE schema check rejected the file", "turn:2:strict", "h2", recallSnapshot)
	row := seedFailure(t, brain, owner, recallPairGoal, "bash: "+recallPairMixed,
		"---vendor---\ncategory,amount\nfood,0.1\n\n---week---\nTraceback (most recent call last):\nUnicodeDecodeError", "turn:3:mixed", "h3", recallSnapshot)
	seedAlternative(t, brain, owner, recallPairGoal, "bash: "+recallPairAlt,
		"grand total: 37.35\nrows: 4\nexit=0", "turn:3:mixed:alt", "h3a", recallSnapshot, row.SourceKey)

	block := a.priorOutcomeContext(recallPairGoal, recallSnapshot)
	t.Logf("complementary-pair note:\n%s", block)
	if got := strings.Count(block, "- Prior observed attempt"); got != priorOutcomeLimit {
		t.Fatalf("expected %d failure bullets, got %d: %q", priorOutcomeLimit, got, block)
	}
	if !strings.Contains(block, "Observed successful alternative") {
		t.Fatalf("the demonstrated working alternative was crowded out by two bare failures: %q", block)
	}
	if !strings.Contains(block, "grand total: 37.35") {
		t.Fatalf("the working alternative's own receipt was dropped: %q", block)
	}
	// The alternative-bearing failure rides its own bullet: never detached from
	// the success it answers.
	if !strings.Contains(block, "---week---") {
		t.Fatalf("the failure that owns the alternative was not rendered with it: %q", block)
	}
	if utf8.RuneCountInString(block) > memoryBlockRunes {
		t.Fatalf("the note exceeded the shared ceiling: %d", utf8.RuneCountInString(block))
	}
}

// 2. THE LEADING ROW ALREADY CARRIES AN ALTERNATIVE: the second slot favors a
// distinct grounded failure, and a CHANGED-snapshot alternative is still carried
// (labelled honestly) rather than triggering any fallback.
func TestPriorOutcomeLeadingAlternativeLeavesSecondSlotDistinct(t *testing.T) {
	a, brain, owner := recallPairAgent(t)
	row := seedFailure(t, brain, owner, recallPairGoal, "bash: "+recallPairPlain,
		"PLAIN-FAILURE UnicodeDecodeError", "turn:1:plain", "h1", recallSnapshot)
	seedAlternative(t, brain, owner, recallPairGoal, "bash: "+recallPairAlt,
		"grand total: 37.35", "turn:1:alt", "h1a", "clean:other", row.SourceKey)
	seedFailure(t, brain, owner, recallPairGoal, "bash: "+recallPairStrict,
		"STRICT-FAILURE schema check rejected the file", "turn:2:strict", "h2", recallSnapshot)

	block := a.priorOutcomeContext(recallPairGoal, recallSnapshot)
	t.Logf("leading-alternative note:\n%s", block)
	if got := strings.Count(block, "- Prior observed attempt"); got != priorOutcomeLimit {
		t.Fatalf("expected %d failure bullets, got %d: %q", priorOutcomeLimit, got, block)
	}
	if !strings.Contains(block, "Observed successful alternative") {
		t.Fatalf("the leading row's own alternative was dropped: %q", block)
	}
	if !strings.Contains(block, "different source snapshot") {
		t.Fatalf("a changed-snapshot alternative was not labelled honestly: %q", block)
	}
	if !strings.Contains(block, "PLAIN-FAILURE") || !strings.Contains(block, "STRICT-FAILURE") {
		t.Fatalf("the second slot did not take the distinct grounded failure: %q", block)
	}
}

// 3. A SUPPRESSED ALTERNATIVE DOES NOT FORCE A FALLBACK. The mixed failure's
// only alternative is a pure read-preview, projected out at read time, so no
// row in the window carries an eligible alternative: the slot still goes to the
// distinct grounded failure and no alternative is invented.
func TestPriorOutcomeSuppressedAlternativeDoesNotForceFallback(t *testing.T) {
	a, brain, owner := recallPairAgent(t)
	seedFailure(t, brain, owner, recallPairGoal, "bash: "+recallPairPlain,
		"PLAIN-FAILURE UnicodeDecodeError", "turn:1:plain", "h1", recallSnapshot)
	seedFailure(t, brain, owner, recallPairGoal, "bash: "+recallPairStrict,
		"STRICT-FAILURE schema check rejected the file", "turn:2:strict", "h2", recallSnapshot)
	preview := "bash: " + `.venv/bin/python -c "d=open('vendor.csv','rb').read(4); print(d)"`
	if !priorAlternativeProvenPurePreview(preview) {
		t.Fatalf("fixture alternative was not classified as a pure read-preview: %q", preview)
	}
	row := seedFailure(t, brain, owner, recallPairGoal, "bash: "+recallPairMixed,
		"---week---\nUnicodeDecodeError", "turn:3:mixed", "h3", recallSnapshot)
	seedAlternative(t, brain, owner, recallPairGoal, preview, "b'\\xff\\xfe'", "turn:3:mixed:alt", "h3a", recallSnapshot, row.SourceKey)

	block := a.priorOutcomeContext(recallPairGoal, recallSnapshot)
	t.Logf("suppressed-alternative note:\n%s", block)
	if got := strings.Count(block, "- Prior observed attempt"); got != priorOutcomeLimit {
		t.Fatalf("expected %d distinct failures, got %d: %q", priorOutcomeLimit, got, block)
	}
	if strings.Contains(block, "Observed successful alternative") {
		t.Fatalf("a projected pure read-preview was rendered as a working alternative: %q", block)
	}
	if !strings.Contains(block, "PLAIN-FAILURE") || !strings.Contains(block, "STRICT-FAILURE") {
		t.Fatalf("the distinct grounded failures were not both shown: %q", block)
	}
}
