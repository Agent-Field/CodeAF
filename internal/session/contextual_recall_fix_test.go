package session

// RECALL-FIX COVERAGE for the fresh31ab evidence-selection defect.
//
// The preserved cold run's goal named vendor.csv. Its bounded two-slot
// <prior_outcomes> note rendered the NEWEST relevant failure - a mixed probe
// that read vendor.csv as utf-16 (fine) and then read week.csv with the default
// codec (Traceback) - and the newest remaining failure, so the demonstrated
// `ledger.py vendor.csv` failure the goal was actually about never reached the
// model. Recency alone decided the pair; a duplicate/mixed method crowded out a
// distinct, goal-file-grounded one.
//
// [priorOutcomeLines] now ranks deterministic goal-file evidence ahead of
// recency and gives the second slot a DISTINCT method or input, while keeping a
// genuine observed alternative attached to its own failure.
//
// The rows below are the REPRESENTATIVE pre-failure rows captured in
// learning-v3-cold-fresh/first-user-note.txt and learning-v3-cold-causal-failure.txt
// (the mixed pair mem_0muwggi8... / mem_0muwggjb..., the never-presented
// ledger failure mem_0muwfklc..., and the team_read row that took the second
// slot), with the captured `cd /home/...` prefix normalized to the test
// workspace so the lexical identities resolve hermetically. No native history is
// copied or edited to force a result; the same rows are run through the
// preserved selector and the fixed one, and only the fixed render is asserted.

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/store"
)

// recallFixGoal is the exact live goal of the preserved run.
const recallFixGoal = "Calculate vendor.csv category totals and grand total under the application interpreter. Do not modify files."

const (
	// The never-presented goal-relevant failed METHOD: it really USES vendor.csv.
	recallLedgerFail = ".venv/bin/python ledger.py vendor.csv"
	// The mixed probe the preserved note rendered: it USES vendor.csv (utf-16) and
	// then week.csv, a file the goal never names. Two of these share one
	// method/input identity.
	recallMixedProbe  = `.venv/bin/python --version && echo "---vendor---" && .venv/bin/python -c "print(open('vendor.csv',encoding='utf-16').read())" && echo "---week---" && .venv/bin/python -c "print(open('week.csv').read())"`
	recallMixedProbe2 = `.venv/bin/python --version && echo "---vendor again---" && .venv/bin/python -c "print(open('vendor.csv',encoding='utf-16').read())" && echo "---week again---" && .venv/bin/python -c "print(open('week.csv').read())"`
	// The observed successful alternative: the utf-16 read of the SAME goal file.
	recallVendorCalc = `.venv/bin/python -c "import csv; t={}; [t.__setitem__(r['category'], t.get(r['category'],0)+float(r['amount'])) for r in csv.DictReader(open('vendor.csv', newline='', encoding='utf-16'))]; print(t)"`
)

const recallSnapshot = "clean:captured"

// seedRecallFixFixture lays the captured representative rows in their captured
// order: the never-presented ledger failure oldest, the mixed failure, the
// success that answers it, and the team_read failure newest (the row that took
// the preserved note's second slot).
func seedRecallFixFixture(t *testing.T, brain *store.Store, owner string) {
	t.Helper()
	seedFailure(t, brain, owner, recallFixGoal, "bash: "+recallLedgerFail,
		"== python3 ledger.py vendor.csv ==\nUnicodeDecodeError: 'utf-8' codec can't decode byte 0xff in position 0: invalid start byte\nexit=1", "turn:old:ledger", "h-ledger", recallSnapshot)
	row := seedFailure(t, brain, owner, recallFixGoal, "bash: "+recallMixedProbe,
		"Python 3.12.3\n---vendor---\ncategory,amount\nfood,0.1\nfood,0.2\ntravel,12.05\ntravel,25.00\n\n---week---\nTraceback (most recent call last):\n  File \"<string>\", line 1, in <module>\nUnicodeDecodeError: 'utf-8' codec can't decode byte 0xff", "turn:mid:mixed", "h-mixed", recallSnapshot)
	seedAlternative(t, brain, owner, recallFixGoal, "bash: "+recallVendorCalc,
		"food: 0.30\ntravel: 37.05\ngrand total: 37.35\nrows: 4\nexit=0", "turn:mid:mixed:alt", "h-alt", recallSnapshot, row.SourceKey)
	seedFailure(t, brain, owner, recallFixGoal, "team_read",
		"No member of \"Supplier framework check\" has the handle \"fwcalc\".", "turn:new:team", "h-team", recallSnapshot)
}

// oldPriorOutcomeSelection reproduces the preserved (31ab) choice byte for byte:
// the newest relevant failure whose own source key carries an alternative first,
// then the newest remaining failure. It exists only to show the regression is
// real on the SAME rows.
func oldPriorOutcomeSelection(relevant []store.ContextualAttempt, alternatives map[string][]store.ContextualAttempt) []store.ContextualAttempt {
	out := []store.ContextualAttempt{}
	rendered := make([]bool, len(relevant))
	for i, at := range relevant {
		if len(alternatives[at.SourceKey]) == 0 {
			continue
		}
		out = append(out, at)
		rendered[i] = true
		break
	}
	for i, at := range relevant {
		if len(out) >= priorOutcomeLimit {
			break
		}
		if rendered[i] {
			continue
		}
		out = append(out, at)
	}
	return out
}

func priorActions(rows []store.ContextualAttempt) []string {
	out := make([]string, 0, len(rows))
	for _, at := range rows {
		out = append(out, at.Action)
	}
	return out
}

// 1. SELECTION DIFFERENCE ON THE SAME PRESERVED ROWS. The preserved selector
// (fails31ab) crowds the ledger failure out of both slots; the fixed selector
// (passesfixed) presents it AND keeps the useful successful method, inside the
// same two-slot, 4800-rune caps.
func TestPriorOutcomeCapturedRowsSelectionRegression(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
	owner := store.OwnerProject("p")
	seedRecallFixFixture(t, brain, owner)

	rows := attemptsForProject(t, brain, a)
	relevant := relevantFailures(rows, outcomeTerms(recallFixGoal))
	alternatives := indexPriorAlternatives(rows)

	t.Run("fails31ab", func(t *testing.T) {
		old := oldPriorOutcomeSelection(relevant, alternatives)
		if len(old) != priorOutcomeLimit {
			t.Fatalf("the preserved selector did not fill two slots: %+v", priorActions(old))
		}
		t.Logf("31ab selected: %q", priorActions(old))
		if strings.Contains(strings.Join(priorActions(old), " "), "ledger.py") {
			t.Fatalf("the preserved selector already showed the ledger failure; fixture is not representative: %q", priorActions(old))
		}
	})

	t.Run("passesfixed", func(t *testing.T) {
		block := a.priorOutcomeContext(recallFixGoal, recallSnapshot)
		t.Logf("fixed note:\n%s", block)
		if !strings.Contains(block, "ledger.py vendor.csv") {
			t.Fatalf("the fixed selector crowded out the demonstrated ledger.py vendor.csv failure: %q", block)
		}
		if !strings.Contains(block, "Observed successful alternative") {
			t.Fatalf("the useful successful utf-16 method was not shown: %q", block)
		}
		if got := strings.Count(block, "- Prior observed attempt"); got != priorOutcomeLimit {
			t.Fatalf("expected exactly %d rendered failures, got %d: %q", priorOutcomeLimit, got, block)
		}
		if strings.Contains(block, "Supplier framework check") {
			t.Fatalf("the duplicate/recent team_read row was still rendered: %q", block)
		}
		if utf8.RuneCountInString(block) > memoryBlockRunes {
			t.Fatalf("the note exceeded the shared ceiling: %d > %d", utf8.RuneCountInString(block), memoryBlockRunes)
		}
	})
}

// 2. DIVERSITY WITHOUT LOSING THE SUCCESS. Two mixed probes repeat one
// method/input; the bare duplicate is dropped and the one carrying an observed
// success keeps its alternative, beside the grounded ledger failure.
func TestPriorOutcomeKeepsAlternativeAndDropsDuplicateMethod(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
	owner := store.OwnerProject("p")
	seedFailure(t, brain, owner, recallFixGoal, "bash: "+recallLedgerFail,
		"UnicodeDecodeError byte 0xff", "turn:1:ledger", "h1", recallSnapshot)
	seedFailure(t, brain, owner, recallFixGoal, "bash: "+recallMixedProbe,
		"BARE-DUPLICATE-METHOD-DROPPED", "turn:2:mixed", "h2", recallSnapshot)
	row := seedFailure(t, brain, owner, recallFixGoal, "bash: "+recallMixedProbe2,
		"MIXED-WITH-SUCCESS", "turn:3:mixed", "h3", recallSnapshot)
	seedAlternative(t, brain, owner, recallFixGoal, "bash: "+recallVendorCalc,
		"grand total: 37.35", "turn:3:mixed:alt", "h3a", recallSnapshot, row.SourceKey)

	block := a.priorOutcomeContext(recallFixGoal, recallSnapshot)
	if !strings.Contains(block, "ledger.py vendor.csv") {
		t.Fatalf("the grounded ledger failure was crowded out: %q", block)
	}
	if !strings.Contains(block, "Observed successful alternative") {
		t.Fatalf("the observed success was dropped for diversity: %q", block)
	}
	if strings.Contains(block, "BARE-DUPLICATE-METHOD-DROPPED") {
		t.Fatalf("a duplicate method/input row was rendered twice: %q", block)
	}
	if !strings.Contains(block, "MIXED-WITH-SUCCESS") {
		t.Fatalf("the alternative-bearing row of the repeated method was lost: %q", block)
	}
	if got := strings.Count(block, "- Prior observed attempt"); got != priorOutcomeLimit {
		t.Fatalf("expected %d rendered failures, got %d: %q", priorOutcomeLimit, got, block)
	}
	if utf8.RuneCountInString(block) > memoryBlockRunes {
		t.Fatalf("the note exceeded the shared ceiling: %d", utf8.RuneCountInString(block))
	}
}

// 3. THE FULL RECORD -> QUERY -> RENDER PATH: a failure and the success that
// answers it are written by the production recorder, then read back by the
// production read seam and rendered with the grounded failure and its success.
func TestPriorOutcomeFullRecordQueryRenderPath(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
	ctx := context.Background()
	a.prepareBindingContext(ctx, recallFixGoal)
	pre := a.captureSourceSnapshot(ctx).Identity
	a.recordOutcome(ctx, 1, delegatedBashCall("call_ledger_fail", recallLedgerFail),
		toolResult{text: "Traceback (most recent call last):\n  File \"ledger.py\"\nUnicodeDecodeError: 'utf-8' codec can't decode byte 0xff\nCommand exited with code 1", isError: true}, pre)
	a.recordOutcome(ctx, 1, delegatedBashCall("call_vendor_calc", recallVendorCalc),
		toolResult{text: "rows: 3"}, pre)

	rows := attemptsForProject(t, brain, a)
	if len(rows) != 2 {
		t.Fatalf("the failure and its one alternative should be two rows, got %+v", rows)
	}
	block := a.priorOutcomeContext(recallFixGoal, pre)
	if !strings.Contains(block, "ledger.py vendor.csv") {
		t.Fatalf("the recorded grounded failure did not render: %q", block)
	}
	if !strings.Contains(block, "Observed successful alternative") || !strings.Contains(block, "rows: 3") {
		t.Fatalf("the recorded alternative did not render with its failure: %q", block)
	}
}

// 4. OWNERSHIP AND SUPPRESSION STILL GATE THE ROWS: another project's row never
// reaches this project's note, and a suppressed source retires its row.
func TestPriorOutcomeOwnerAndSuppressionEligibility(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
	owner := store.OwnerProject("p")
	seedFailure(t, brain, owner, recallFixGoal, "bash: "+recallLedgerFail,
		"UnicodeDecodeError byte 0xff", "turn:p:ledger", "h-p", recallSnapshot)
	seedFailure(t, brain, store.OwnerProject("other"), recallFixGoal, "bash: "+recallLedgerFail+" foreign",
		"foreign project failure", "turn:o:ledger", "h-o", recallSnapshot)

	block := a.priorOutcomeContext(recallFixGoal, recallSnapshot)
	if !strings.Contains(block, "ledger.py vendor.csv") {
		t.Fatalf("this project's own row was withheld: %q", block)
	}
	if strings.Contains(block, "foreign") {
		t.Fatalf("another owner's row reached this project's note: %q", block)
	}
	if err := brain.SuppressContextualSource(owner, "turn:p:ledger", "h-p", "forgotten"); err != nil {
		t.Fatalf("suppress source: %v", err)
	}
	if after := a.priorOutcomeContext(recallFixGoal, recallSnapshot); strings.Contains(after, "ledger.py vendor.csv") {
		t.Fatalf("a suppressed source still rendered: %q", after)
	}
}

// 5. A CHANGED CIRCUMSTANCE IS LABELLED HONESTLY by the per-record renderer,
// and the grounded ordering holds under it too.
func TestPriorOutcomeGroundedFailureUnderChangedSnapshot(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
	seedRecallFixFixture(t, brain, store.OwnerProject("p"))

	block := a.priorOutcomeContext(recallFixGoal, "dirty:def")
	if !strings.Contains(block, "ledger.py vendor.csv") {
		t.Fatalf("the grounded failure was lost under a changed snapshot: %q", block)
	}
	if !strings.Contains(block, "different source snapshot") {
		t.Fatalf("a changed circumstance was not labelled: %q", block)
	}
	if utf8.RuneCountInString(block) > memoryBlockRunes {
		t.Fatalf("the note exceeded the shared ceiling: %d", utf8.RuneCountInString(block))
	}
}

// 6. SEVERAL NEWER PAIRED VARIANTS CANNOT CROWD THE SPECIFIC FAILURE. Each
// mixed variant reads vendor.csv as utf-16 and dies on a different extra file,
// and each carries a success; the demonstrated, goal-specific ledger failure
// still ranks first and one useful success still rides the second slot.
func TestPriorOutcomeSeveralNewerPairsDoNotCrowdSpecificFailure(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
	owner := store.OwnerProject("p")
	seedFailure(t, brain, owner, recallFixGoal, "bash: "+recallLedgerFail,
		"UnicodeDecodeError byte 0xff", "turn:0:ledger", "h0", recallSnapshot)
	for _, extra := range []string{"week.csv", "month.csv", "quarter.csv"} {
		probe := `.venv/bin/python -c "print(open('vendor.csv',encoding='utf-16').read()); print(open('` + extra + `').read())"`
		row := seedFailure(t, brain, owner, recallFixGoal, "bash: "+probe,
			"vendor ok then failed on "+extra, "turn:"+extra+":probe", "h-"+extra, recallSnapshot)
		seedAlternative(t, brain, owner, recallFixGoal, "bash: "+recallVendorCalc,
			"grand total: 37.35", "turn:"+extra+":alt", "ha-"+extra, recallSnapshot, row.SourceKey)
	}
	seedFailure(t, brain, owner, recallFixGoal, "team_read", "no such member", "turn:new:team", "h-team", recallSnapshot)

	block := a.priorOutcomeContext(recallFixGoal, recallSnapshot)
	t.Logf("several-pairs note:\n%s", block)
	if !strings.Contains(block, "ledger.py vendor.csv") {
		t.Fatalf("the specific ledger failure was crowded out by newer pairs: %q", block)
	}
	if !strings.Contains(block, "Observed successful alternative") {
		t.Fatalf("a useful successful method was not shown: %q", block)
	}
	if got := strings.Count(block, "- Prior observed attempt"); got != priorOutcomeLimit {
		t.Fatalf("expected %d rendered failures, got %d: %q", priorOutcomeLimit, got, block)
	}
	if strings.Contains(block, "no such member") {
		t.Fatalf("the unrelated team_read row was rendered: %q", block)
	}
	if utf8.RuneCountInString(block) > memoryBlockRunes {
		t.Fatalf("the note exceeded the shared ceiling: %d", utf8.RuneCountInString(block))
	}
}

// 7. DISTINCT NON-SHELL FAILED TOOLS ARE NOT MERGED. Two different tools with no
// shell structure share no method key, so neither is suppressed as a duplicate
// of the other.
func TestPriorOutcomeDistinctNonShellToolsAreNotMerged(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
	owner := store.OwnerProject("p")
	seedFailure(t, brain, owner, recallFixGoal, "team_read", "no member of the team has that handle", "turn:1:team", "h1", recallSnapshot)
	seedFailure(t, brain, owner, recallFixGoal, "glob: *.csv", "no files matched", "turn:2:glob", "h2", recallSnapshot)

	block := a.priorOutcomeContext(recallFixGoal, recallSnapshot)
	if !strings.Contains(block, "team_read") || !strings.Contains(block, "glob") {
		t.Fatalf("a distinct non-shell failed tool was suppressed as a duplicate: %q", block)
	}
	if got := strings.Count(block, "- Prior observed attempt"); got != priorOutcomeLimit {
		t.Fatalf("expected %d rendered failures, got %d: %q", priorOutcomeLimit, got, block)
	}
}
