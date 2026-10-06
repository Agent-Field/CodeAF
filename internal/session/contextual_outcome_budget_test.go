package session

// OUTCOME-BUDGET PRIORITY COVERAGE.
//
// The captured first request of the selected ledger goal (trace
// c7f62af391ccbcb9 / call 3d1f3e8f) opened with a <memory> note of exactly two
// records: one genuinely approved rule (the dev-only pandas cross-check) and one
// ADVISORY lexical-history record (".venv/bin/python is app interpreter",
// labelled history only). That note measured 3465 runes of the one shared 4800
// ceiling, so the relevant observed failure+success pair the project had
// already journaled for the same goal (f3e702e166c73a33) had only 1335 runes
// left and the 1707-rune <prior_outcomes> block was omitted WHOLE. The advisory
// history was riding in the MANDATORY slice and starved a grounded local
// outcome.
//
// These tests pin the corrected priority at the REAL seams: the genuinely
// approved rules and confirmed decisions are mandatory and reserved FIRST, the
// grounded impacts and relevant prior outcomes come next, and the advisory
// lexical history (with the asynchronous recall) spends only what is left, by
// whole records. A lexical-history record is not a rule and never crowds out a
// grounded local outcome.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/store"
)

// seedAdvisoryHistory writes one plain observation record with its own journal
// row, exactly as the extractor lays an unapproved observation down: the lexical
// fallback can reach it by word match, but its OWN row is not an approved rule
// or a confirmed decision, so it must ride labelled history only.
func seedAdvisoryHistory(t *testing.T, brain *store.Store, id, owner, text, observation string) store.Memory {
	t.Helper()
	m, err := brain.AddMemory(store.Memory{ID: id, Owner: owner, Type: store.MemoryFact, Title: id, Text: text})
	if err != nil {
		t.Fatalf("add advisory memory %s: %v", id, err)
	}
	if _, err := brain.AppendContextualEvidence(store.ContextualEvidence{
		ID: "ev-" + id, MemoryID: m.ID, Owner: owner, SessionID: "s", TurnID: "t",
		Actor: "user", Authority: "observation", Observation: observation,
		Verification: "asserted", SourceKey: "s:t:" + id, SourceHash: "h-" + id,
	}); err != nil {
		t.Fatalf("append advisory evidence %s: %v", id, err)
	}
	return m
}

const ledgerBudgetGoal = "Calculate vendor.csv category totals and the grand total under the application interpreter. Do not modify files."

// seedBudgetPair lays the captured ledger failure+success pair at the same
// shapes the writers used: the failed utility call and its later stdlib
// alternative, with receipts long enough to be clipped to the renderer's own
// 240-rune window exactly as the captured 941-rune pair line was.
func seedBudgetPair(t *testing.T, brain *store.Store, owner, goal string) {
	t.Helper()
	failKey := "turn:1:vendor"
	failObs := "Traceback (most recent call last):\n  File \"ledger.py\", line 16, in totals\n    for row in csv.DictReader(source):\nUnicodeDecodeError: 'utf-8' codec can't decode byte 0xff in position 0: invalid start byte\nCommand exited with code 1 " +
		strings.Repeat("stack frame detail ", 20)
	fail := store.ContextualAttempt{
		ID: store.NewMemoryID(), Owner: owner, SessionID: "s", TurnID: "t", Tool: "bash",
		Action: "bash: .venv/bin/python ledger.py vendor.csv", Goal: goal, Status: store.AttemptFailed,
		ReceiptIDs: []string{"c1"}, Observation: failObs, Snapshot: "dirty:abc",
		SourceKey: failKey, SourceHash: "hf", ValidFrom: time.Now(),
	}
	if _, err := brain.AppendContextualAttempt(fail); err != nil {
		t.Fatalf("seed budget failure: %v", err)
	}
	succAction := "bash: .venv/bin/python - <<'PY' (stdlib csv + Decimal over vendor.csv)" + strings.Repeat(" padding", 30)
	alt := store.ContextualAttempt{
		ID: store.NewMemoryID(), Owner: owner, SessionID: "s", TurnID: "t", Tool: "bash",
		Action: succAction, Goal: goal, Status: store.AttemptSucceeded,
		ReceiptIDs: []string{"c2"}, Observation: "food: 0.30 travel: 37.05 grand total: 37.35", Snapshot: "dirty:abc",
		AlternativeOf: failKey, SourceKey: failKey + ":alt", SourceHash: "ha", ValidFrom: time.Now(),
	}
	if _, err := brain.AppendContextualAttempt(alt); err != nil {
		t.Fatalf("seed budget alternative: %v", err)
	}
}

// 1. AN OVERSIZED ADVISORY-HISTORY RECORD DOES NOT CROWD OUT THE GROUNDED PAIR.
// The captured first-request shape is replayed: one approved rule and one
// advisory history record compete with a real failure+success pair for the same
// goal. The approved rule stays mandatory, the pair still lands, both wrappers
// stay closed, and the note never exceeds the one shared ceiling.
func TestGroundedOutcomePairSurvivesOversizedAdvisoryHistory(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	a, brain := brainAgent(t, &reflexScript{routeErr: errors.New("router down")}, func(c *Config) {
		c.Workspace = dir
		c.MemoryProjectKey = "p"
	})
	owner := store.OwnerProject("p")
	seedApprovedRule(t, brain, "money", owner,
		"Release artifacts must work offline and money must use exact decimal arithmetic.")
	seedBudgetPair(t, brain, owner, ledgerBudgetGoal)
	// The advisory history record is deliberately large: under the old ordering
	// it rides the mandatory <memory> block and leaves too little for the pair.
	seedAdvisoryHistory(t, brain, "vendor-interp", owner,
		"History only: the application interpreter .venv/bin/python runs project scripts such as ledger.py; "+
			"interpreter application vendor totals category history provenance.",
		"Using the application interpreter .venv/bin/python, independently cross-check the grand total. "+
			strings.Repeat("interpreter application vendor totals category history words ", 40))

	a.prepareBindingContext(context.Background(), ledgerBudgetGoal)
	a.mu.Lock()
	block := a.memoryText
	a.mu.Unlock()

	if utf8.RuneCountInString(block) > memoryBlockRunes {
		t.Fatalf("the note exceeded the shared ceiling: %d > %d", utf8.RuneCountInString(block), memoryBlockRunes)
	}
	// The genuinely approved rule is mandatory and reserved first.
	if !strings.Contains(block, "exact decimal arithmetic") {
		t.Fatalf("the approved rule was not reserved: %q", block)
	}
	// The grounded pair must still land BEFORE the model chooses a method.
	if !strings.Contains(block, "<prior_outcomes>") || !strings.Contains(block, "</prior_outcomes>") {
		t.Fatalf("the grounded prior-outcome pair was crowded out by advisory history: %q", block)
	}
	if !strings.Contains(block, "Observed successful alternative") {
		t.Fatalf("the observed successful alternative was absent: %q", block)
	}
	if !strings.Contains(block, "vendor") {
		t.Fatalf("the relevant prior action was absent from the pair: %q", block)
	}
	// No wrapper was left open: the advisory history was omitted WHOLE, not cut.
	if strings.Count(block, "<memory>") != strings.Count(block, "</memory>") ||
		strings.Count(block, "<prior_outcomes>") != strings.Count(block, "</prior_outcomes>") {
		t.Fatalf("a wrapper was left open (a record was clipped): %q", block)
	}
	// The ordered landmarks must hold: authority, then outcomes.
	ruleAt := strings.Index(block, "exact decimal arithmetic")
	outAt := strings.Index(block, "<prior_outcomes>")
	if ruleAt < 0 || outAt < 0 || ruleAt > outAt {
		t.Fatalf("approved rule did not precede the grounded outcomes (rule=%d out=%d): %q", ruleAt, outAt, block)
	}
	// THE CAPTURED BUDGET PRESSURE IS REAL. The OLD combined mandatory slice --
	// the approved rule AND the advisory history in ONE <memory> block, as the
	// 3465-rune captured note carried them -- plus the 1707-rune pair exceeds the
	// one 4800 ceiling, which is exactly why the pair used to be omitted WHOLE.
	combined := renderBindingBlock(a.bindingMemories(brain, ledgerBudgetGoal, ""))
	outcomes := a.priorOutcomeContext(ledgerBudgetGoal, "")
	if utf8.RuneCountInString(combined)+utf8.RuneCountInString(outcomes) <= memoryBlockRunes {
		t.Fatalf("fixture failed to reproduce the captured budget pressure: combined=%d outcomes=%d ceiling=%d",
			utf8.RuneCountInString(combined), utf8.RuneCountInString(outcomes), memoryBlockRunes)
	}
}

// 2. WHEN THERE IS ROOM, THE ADVISORY HISTORY RIDES LAST. The priority is
// visible in the one block: the mandatory approved rule, then the grounded
// prior outcomes, and only then the advisory lexical history. This is the
// ordering contract the shared ceiling exists to keep.
func TestBindingPriorityReservesAuthorityThenOutcomesThenHistory(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	a, brain := brainAgent(t, &reflexScript{}, func(c *Config) {
		c.Workspace = dir
		c.MemoryProjectKey = "p"
	})
	owner := store.OwnerProject("p")
	seedApprovedRule(t, brain, "offline", owner, "Release artifacts must work offline.")
	seedBudgetPair(t, brain, owner, ledgerBudgetGoal)
	seedAdvisoryHistory(t, brain, "interp-note", owner,
		"The application interpreter .venv/bin/python runs project scripts such as ledger.py; "+
			"interpreter application vendor totals history provenance.",
		"Using the application interpreter .venv/bin/python, independently cross-check the grand total.")

	a.prepareBindingContext(context.Background(), ledgerBudgetGoal)
	a.mu.Lock()
	block := a.memoryText
	a.mu.Unlock()

	if utf8.RuneCountInString(block) > memoryBlockRunes {
		t.Fatalf("the note exceeded the shared ceiling: %d", utf8.RuneCountInString(block))
	}
	ruleAt := strings.Index(block, "must work offline")
	outAt := strings.Index(block, "<prior_outcomes>")
	histLabel := "History only"
	histAt := strings.Index(block, histLabel)
	if ruleAt < 0 {
		t.Fatalf("the approved rule was not reserved: %q", block)
	}
	if outAt < 0 {
		t.Fatalf("the grounded outcomes were omitted despite room: %q", block)
	}
	if histAt < 0 {
		t.Fatalf("the advisory history was omitted despite room: %q", block)
	}
	if !(ruleAt < outAt && outAt < histAt) {
		t.Fatalf("priority order wrong: authority@%d outcomes@%d history@%d\n%s", ruleAt, outAt, histAt, block)
	}
	// The pair is whole and the provenance label is intact even though it now
	// rides a separate <memory> block after the outcomes.
	if !strings.Contains(block, "Observed successful alternative") {
		t.Fatalf("the observed alternative was clipped: %q", block)
	}
	if strings.Count(block, "<memory>") != strings.Count(block, "</memory>") {
		t.Fatalf("a memory wrapper was left open: %q", block)
	}
}

// 3. A LENT WORKER KEEPS THE SAME PRIORITY. The worker's read-only binding note
// reserves the approved rule first and still carries the grounded pair even
// when a large advisory history record is present, and the note fits the shared
// ceiling.
func TestWorkerBindingPrioritySurvivesAdvisoryHistory(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	root, brain := brainAgent(t, &reflexScript{}, func(c *Config) {
		c.Workspace = dir
		c.MemoryProjectKey = "wkey"
	})
	owner := store.OwnerProject("wkey")
	seedApprovedRule(t, brain, "wrule", owner, "workers must use the frozen toolchain")
	seedBudgetPair(t, brain, owner, ledgerBudgetGoal)
	seedAdvisoryHistory(t, brain, "winterp", owner,
		"The application interpreter .venv/bin/python runs project scripts such as ledger.py; "+
			"interpreter application vendor totals category history.",
		"Using the application interpreter .venv/bin/python, independently cross-check the grand total. "+
			strings.Repeat("interpreter application vendor totals category history words ", 40))

	worker := spawnTaskWorker(t, root, dir)
	worker.prepareWorkerBinding(context.Background(), ledgerBudgetGoal)
	worker.mu.Lock()
	block := worker.bindingText
	worker.mu.Unlock()

	if worker.remembers() || worker.memoryWritable() || len(worker.memoryTools()) != 0 {
		t.Fatal("the worker gained a brain or a verb from the lent store")
	}
	if utf8.RuneCountInString(block) > memoryBlockRunes {
		t.Fatalf("the worker note exceeded the shared ceiling: %d", utf8.RuneCountInString(block))
	}
	if !strings.Contains(block, "must use the frozen toolchain") {
		t.Fatalf("the worker lost the mandatory approved rule: %q", block)
	}
	if !strings.Contains(block, "<prior_outcomes>") || !strings.Contains(block, "Observed successful alternative") {
		t.Fatalf("the worker lost the grounded pair to advisory history: %q", block)
	}
}
