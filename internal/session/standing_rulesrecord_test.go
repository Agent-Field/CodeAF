package session

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// workRule is about how the work is done, which no report can show.
var workRule = standing.Item{ID: "c3c3c3c3c3c3c3c3", Words: "Never edit the copy itself.", When: standing.When{Kind: standing.WhenHold}, Status: standing.StatusActive}

// EACH RULE IS RECORDED BY ITS ID WITH WHAT WAS ACTUALLY CHECKED, and the
// summary is "kept" only when every rule was. A rule the report cannot show is
// recorded as not-checkable — never folded into a blanket "kept".
func TestEachRuleIsRecordedByIDWithWhatWasChecked(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	model := &scriptedCompleter{steps: []step{saying("<report>\nSCOPE-TRAVEL\n- press kit done\n</report>")}}
	answer := `{"rules": [
		{"rule": 1, "kind": "obligation", "verdict": "kept", "quote": "SCOPE-TRAVEL"},
		{"rule": 2, "kind": "prohibition", "verdict": "kept"},
		{"rule": 3, "kind": "prohibition", "verdict": "not-checkable", "why": "a report cannot show whether the copy was edited"}]}`
	runner, _ := ruledRunner(t, root, model, answer)
	runner.parent.Governing = &Governing{Reader: rulesReader{travelRule, privacyRule, workRule}}
	runDir := admittedRun(t, root)
	outcome, err := runner.Run(context.Background(), reporting(workspace), runDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Kind != "landed" || outcome.Published == nil {
		t.Fatalf("a report keeping every checkable rule was not published: %+v", outcome)
	}
	record, _ := standing.ReadOccurrence(runDir)
	check := record.RuleCheck
	want := []standing.RuleVerdict{
		{ID: travelRule.ID, Kind: standing.RuleObligation, Verdict: standing.RuleKept, Quote: "SCOPE-TRAVEL"},
		{ID: privacyRule.ID, Kind: standing.RuleProhibition, Verdict: standing.RuleKept},
		{ID: workRule.ID, Kind: standing.RuleProhibition, Verdict: standing.RuleNotCheckable, Why: "a report cannot show whether the copy was edited"},
	}
	if check == nil || !reflect.DeepEqual(check.Verdicts, want) {
		t.Fatalf("the per-rule record is %+v", check)
	}
	if check.Verdict != standing.RuleNotCheckable {
		t.Fatalf("a check with a rule it could not check summed up as %q", check.Verdict)
	}
}

// AN OBLIGATION "KEPT" ON WORDS THE REPORT DOES NOT HAVE IS NO ANSWER. That is
// the live checker's blanket "kept" (S11) asked the new question: it must cite
// where the report does what the rule asks, and it cannot. Asked once more and
// answered the same, the report is held — never published as kept.
func TestAnObligationKeptOnWordsTheReportDoesNotHaveIsNoAnswer(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	model := &scriptedCompleter{steps: []step{saying("<report>\n- press kit done\n</report>")}}
	lie := `{"rules": [{"rule": 1, "kind": "obligation", "verdict": "kept", "quote": "SCOPE-TRAVEL"}]}`
	runner, questions := ruledRunner(t, root, model, lie, lie)
	runner.parent.Governing = &Governing{Reader: rulesReader{travelRule}}
	runDir := admittedRun(t, root)
	outcome, err := runner.Run(context.Background(), reporting(workspace), runDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Published != nil || !strings.Contains(outcome.NeedsPerson, "gave no answer") || len(*questions) != 2 {
		t.Fatalf("an uncited kept was taken: %+v (%d checks)", outcome, len(*questions))
	}
	record, _ := standing.ReadOccurrence(runDir)
	if record.RuleCheck == nil || record.RuleCheck.Verdict != "no answer" || len(record.RuleCheck.Verdicts) != 0 {
		t.Fatalf("an unanswered check recorded verdicts: %+v", record.RuleCheck)
	}
}

// The reader's law, case by case: every rule answered once, a known kind and
// verdict, and a quote from the report wherever the kind's question needs one.
func TestTheRuleCheckAnswerIsReadStrictly(t *testing.T) {
	rules := []standing.Item{travelRule, privacyRule}
	report := "SCOPE-TRAVEL\n- **ship** Friday"
	for reply, answered := range map[string]bool{
		`{"rules":[{"rule":1,"kind":"obligation","verdict":"kept","quote":"SCOPE-TRAVEL"},{"rule":2,"kind":"prohibition","verdict":"kept"}]}`:                       true,
		`{"rules":[{"rule":2,"kind":"prohibition","verdict":"broken","quote":"ship Friday"},{"rule":1,"kind":"obligation","verdict":"broken"}]}`:                    true,
		`{"rules":[{"rule":1,"kind":"obligation","verdict":"kept","quote":"SCOPE-TRAVEL"}]}`:                                                                        false,
		`{"rules":[{"rule":1,"kind":"obligation","verdict":"kept","quote":"SCOPE-TRAVEL"},{"rule":1,"kind":"obligation","verdict":"kept","quote":"SCOPE-TRAVEL"}]}`: false,
		`{"rules":[{"rule":1,"kind":"rule","verdict":"kept"},{"rule":2,"kind":"prohibition","verdict":"kept"}]}`:                                                    false,
		`{"rules":[{"rule":1,"kind":"obligation","verdict":"fine","quote":"SCOPE-TRAVEL"},{"rule":2,"kind":"prohibition","verdict":"kept"}]}`:                       false,
		`{"rules":[{"rule":1,"kind":"obligation","verdict":"kept"},{"rule":2,"kind":"prohibition","verdict":"kept"}]}`:                                              false,
		`{"rules":[{"rule":1,"kind":"obligation","verdict":"kept","quote":"SCOPE-TRAVEL"},{"rule":2,"kind":"prohibition","verdict":"broken"}]}`:                     false,
		`{"rules":[{"rule":3,"kind":"obligation","verdict":"kept","quote":"SCOPE-TRAVEL"}]}`:                                                                        false,
		`{"kept": true}`: false,
		`kept`:           false,
	} {
		if got := parseStandingRuleVerdict(reply, report, rules); got.answered != answered {
			t.Errorf("parse(%s).answered = %v (%s), want %v", reply, got.answered, got.trouble, answered)
		}
	}
}

// THE CODES ARE IDENTIFIERS. A front end reads them instead of the lines, so
// this set is pinned exactly and a change to it is a deliberate act.
func TestTheRuleVerdictCodesArePinned(t *testing.T) {
	got := []string{standing.RuleObligation, standing.RuleProhibition, standing.RuleKept, standing.RuleBroken, standing.RuleNotCheckable}
	if want := []string{"obligation", "prohibition", "kept", "broken", "not-checkable"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rule verdict codes %v, want %v", got, want)
	}
}
