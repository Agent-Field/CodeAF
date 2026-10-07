package session

// REGRESSIONS for the independent review's D1: [priorFailureMethodKey] must not
// collapse two DIFFERENT invocations into one "method". The old key was a
// semantic summary (program family + file operands) and so read
// `ledger.py vendor.csv` and `ledger.py --strict vendor.csv` as the same
// evidence, dropping one of two real, distinct failures from the second slot;
// two different inline programs with no file operands collapsed the same way.
//
// The identity is now the exact stored action, so the same command repeated for
// a second failure still dedups, while every different invocation stays
// distinct. Each test drives the production read seam
// [Agent.priorOutcomeContext] on rows written through the production store
// seam, so the identity under test is the one the note actually renders with.

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/store"
)

const recallIdentityGoal = "Calculate vendor.csv category totals and grand total under the application interpreter. Do not modify files."

func recallIdentityAgent(t *testing.T) (*Agent, *store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	initRepo(t, dir)
	a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
	return a, brain, store.OwnerProject("p")
}

// 8. THE SAME PROGRAM AND FILE, A DIFFERENT FLAG, ARE TWO COMMANDS. Both are
// genuinely grounded failures on the goal artifact and neither may be
// suppressed as a duplicate of the other.
func TestPriorOutcomeSameFileDifferentFlagsAreDistinct(t *testing.T) {
	a, brain, owner := recallIdentityAgent(t)
	seedFailure(t, brain, owner, recallIdentityGoal, "bash: "+recallLedgerFail,
		"PLAIN-FAILURE UnicodeDecodeError byte 0xff", "turn:1:plain", "h1", recallSnapshot)
	seedFailure(t, brain, owner, recallIdentityGoal, "bash: .venv/bin/python ledger.py --strict vendor.csv",
		"STRICT-FAILURE schema check rejected the file", "turn:2:strict", "h2", recallSnapshot)

	block := a.priorOutcomeContext(recallIdentityGoal, recallSnapshot)
	t.Logf("same-file different-flags note:\n%s", block)
	if !strings.Contains(block, "PLAIN-FAILURE") {
		t.Fatalf("the plain ledger.py vendor.csv failure was dropped: %q", block)
	}
	if !strings.Contains(block, "STRICT-FAILURE") {
		t.Fatalf("the ledger.py --strict vendor.csv failure was suppressed as a duplicate: %q", block)
	}
	if got := strings.Count(block, "- Prior observed attempt"); got != priorOutcomeLimit {
		t.Fatalf("expected %d distinct invocations, got %d: %q", priorOutcomeLimit, got, block)
	}
	if utf8.RuneCountInString(block) > memoryBlockRunes {
		t.Fatalf("the note exceeded the shared ceiling: %d", utf8.RuneCountInString(block))
	}
}

// 9. TWO DIFFERENT INLINE PROGRAMS WITH NO FILE OPERANDS ARE TWO COMMANDS. The
// old program-family key (`p:python`) merged them; the exact stored action does
// not.
func TestPriorOutcomeDifferentInlineProgramsAreDistinct(t *testing.T) {
	a, brain, owner := recallIdentityAgent(t)
	seedFailure(t, brain, owner, recallIdentityGoal,
		`bash: .venv/bin/python -c "print('totals one')"`,
		"INLINE-ONE failed on its own branch", "turn:1:inline", "h1", recallSnapshot)
	seedFailure(t, brain, owner, recallIdentityGoal,
		`bash: .venv/bin/python -c "print('totals two')"`,
		"INLINE-TWO failed on its own branch", "turn:2:inline", "h2", recallSnapshot)

	block := a.priorOutcomeContext(recallIdentityGoal, recallSnapshot)
	t.Logf("different inline programs note:\n%s", block)
	if !strings.Contains(block, "INLINE-ONE") {
		t.Fatalf("the first inline program was dropped: %q", block)
	}
	if !strings.Contains(block, "INLINE-TWO") {
		t.Fatalf("the second inline program was suppressed as a duplicate: %q", block)
	}
	if got := strings.Count(block, "- Prior observed attempt"); got != priorOutcomeLimit {
		t.Fatalf("expected %d distinct inline programs, got %d: %q", priorOutcomeLimit, got, block)
	}
}

// 10. AN IDENTICAL COMMAND RECORDED FOR TWO DIFFERENT FAILURES STILL DEDUPS.
// The identity is the action, not the source key, so the repeated invocation
// renders once rather than as two rows of the same evidence.
func TestPriorOutcomeIdenticalActionDifferentSourceKeysDedup(t *testing.T) {
	a, brain, owner := recallIdentityAgent(t)
	seedFailure(t, brain, owner, recallIdentityGoal, "bash: "+recallLedgerFail,
		"FIRST-RECORD failure", "turn:1:first", "h1", recallSnapshot)
	seedFailure(t, brain, owner, recallIdentityGoal, "bash: "+recallLedgerFail,
		"SECOND-RECORD failure", "turn:2:second", "h2", recallSnapshot)

	block := a.priorOutcomeContext(recallIdentityGoal, recallSnapshot)
	t.Logf("identical-action dedup note:\n%s", block)
	if got := strings.Count(block, "- Prior observed attempt"); got != 1 {
		t.Fatalf("an identical command was rendered as %d rows: %q", got, block)
	}
	both := strings.Contains(block, "FIRST-RECORD") && strings.Contains(block, "SECOND-RECORD")
	if both {
		t.Fatalf("two records of one identical command were both rendered: %q", block)
	}
}

// 11. THE PAIRED ALTERNATIVE SURVIVES THE DEDUP. When the repeated method's
// remaining row carries an observed success, that success still rides its own
// failure line instead of the slot staying empty.
func TestPriorOutcomeCoupledAlternativeRetainedBesideDuplicate(t *testing.T) {
	a, brain, owner := recallIdentityAgent(t)
	seedFailure(t, brain, owner, recallIdentityGoal, "bash: "+recallLedgerFail,
		"DUPLICATE-WITHOUT-ALT", "turn:1:dup", "h1", recallSnapshot)
	row := seedFailure(t, brain, owner, recallIdentityGoal, "bash: "+recallLedgerFail,
		"DUPLICATE-WITH-ALT", "turn:2:dup", "h2", recallSnapshot)
	seedAlternative(t, brain, owner, recallIdentityGoal, "bash: "+recallVendorCalc,
		"grand total: 37.35", "turn:2:dup:alt", "h2a", recallSnapshot, row.SourceKey)

	block := a.priorOutcomeContext(recallIdentityGoal, recallSnapshot)
	t.Logf("coupled alternative note:\n%s", block)
	if !strings.Contains(block, "Observed successful alternative") {
		t.Fatalf("the paired observed success was dropped from the repeated method: %q", block)
	}
	if !strings.Contains(block, "DUPLICATE-WITH-ALT") {
		t.Fatalf("the alternative-bearing row of the repeated method was lost: %q", block)
	}
	if strings.Contains(block, "DUPLICATE-WITHOUT-ALT") {
		t.Fatalf("the bare duplicate of the method was rendered as well: %q", block)
	}
	if got := strings.Count(block, "- Prior observed attempt"); got != 1 {
		t.Fatalf("expected one deduped failure, got %d: %q", got, block)
	}
	if utf8.RuneCountInString(block) > memoryBlockRunes {
		t.Fatalf("the note exceeded the shared ceiling: %d", utf8.RuneCountInString(block))
	}
}
