package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// STANDING SHOW PRINTS EACH RULE'S TRUTH, never a blanket "kept": the count of
// what the check found, then one line per rule by id — the words that keep an
// obligation, what is missing from a broken one, why a rule could not be
// checked — and a run that spent past its limit says by how much.
func TestStandingShowPrintsEachRulesTruthAndAnOvershoot(t *testing.T) {
	record := standingRecord{
		Item:  standing.Item{ID: "0123456789abcdef", Words: "keep the launch status current", Status: standing.StatusActive},
		Rules: []standingRule{{ID: "a1a1a1a1a1a1a1a1", Words: "Reports for Travel work start with SCOPE-TRAVEL"}},
		Occurrences: []standingRun{{Occurrence: standing.Occurrence{
			Phase: standing.PhaseFinished, Outcome: "landed", Admitted: time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC),
			USD: 0.072, PerRunUSD: 0.06,
			RuleCheck: &standing.RuleCheck{
				Rules:   []string{"a1a1a1a1a1a1a1a1", "b2b2b2b2b2b2b2b2", "c3c3c3c3c3c3c3c3"},
				Verdict: standing.RuleBroken,
				Verdicts: []standing.RuleVerdict{
					{ID: "a1a1a1a1a1a1a1a1", Kind: standing.RuleObligation, Verdict: standing.RuleBroken, Why: "the report has no SCOPE-TRAVEL line"},
					{ID: "b2b2b2b2b2b2b2b2", Kind: standing.RuleObligation, Verdict: standing.RuleKept, Quote: "SCOPE-LAUNCH"},
					{ID: "c3c3c3c3c3c3c3c3", Kind: standing.RuleProhibition, Verdict: standing.RuleNotCheckable, Why: "a report cannot show whether the copy was edited"},
				},
			},
		}, RunDir: "/tmp/runs/000001"}},
	}
	var out bytes.Buffer
	if err := writeStandingRecord(&out, record); err != nil {
		t.Fatal(err)
	}
	shown := out.String()
	for _, want := range []string{
		"checked against 3 rule(s): 1 kept, 1 broken, 1 not checkable",
		"rule a1a1a1a1a1a1a1a1 “Reports for Travel work start with SCOPE-TRAVEL” (obligation): broken — the report has no SCOPE-TRAVEL line",
		"rule b2b2b2b2b2b2b2b2 (obligation): kept — the report says “SCOPE-LAUNCH”",
		"rule c3c3c3c3c3c3c3c3 (prohibition): not checkable — a report cannot show whether the copy was edited",
		"cost $0.0720 · $0.0120 over its $0.0600 limit",
	} {
		if !strings.Contains(shown, want) {
			t.Errorf("show does not say %q:\n%s", want, shown)
		}
	}
	if strings.Contains(shown, ": kept\n") || strings.Contains(shown, "still breaks") {
		t.Fatalf("show summed per-rule verdicts into one word:\n%s", shown)
	}
}
