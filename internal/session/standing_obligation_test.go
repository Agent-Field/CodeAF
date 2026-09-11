package session

// standing_obligation_test.go is validators S11 and S12b scripted: a rule that
// OBLIGES a report to do something reached the run, the run left it out, and
// the check — which asked only whether the report BROKE a rule, with a quote —
// had nothing to quote and said "kept". The report was published and its
// record said every rule was kept.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// The two obligations of S11, each naming the marker a report must carry.
var (
	travelRule = standing.Item{ID: "a1a1a1a1a1a1a1a1", Words: "Reports for Travel work start with the exact first line: SCOPE-TRAVEL", When: standing.When{Kind: standing.WhenHold}, Status: standing.StatusActive}
	launchRule = standing.Item{ID: "b2b2b2b2b2b2b2b2", Words: "Reports for Launch work end with the exact last line: SCOPE-LAUNCH", When: standing.When{Kind: standing.WhenHold}, Status: standing.StatusActive}
	markerIn   = regexp.MustCompile(`SCOPE-[A-Z]+`)
)

// truthfulChecker answers whichever question the check asks, truthfully, the
// way the live checker did: asked whether the report BREAKS any rule (the
// question before wave 4) there is nothing to quote, so it answers kept; asked
// for each rule by its kind, it says an obligation whose marker is missing is
// broken, and cites the marker where it is there.
func truthfulChecker(asked *int) func([]ai.Message) (*ai.Response, bool) {
	return func(messages []ai.Message) (*ai.Response, bool) {
		if len(messages) == 0 || messageText(messages[0]) != standingRulesPrompt {
			return nil, false
		}
		*asked++
		question := messageText(messages[len(messages)-1])
		report := question[strings.Index(question, "<<<")+3 : strings.LastIndex(question, ">>>")]
		if !strings.Contains(standingRulesPrompt, "Answer EACH rule") {
			return textResponse(`{"kept": true}`), true
		}
		type entry struct {
			Rule    int    `json:"rule"`
			Kind    string `json:"kind"`
			Verdict string `json:"verdict"`
			Quote   string `json:"quote,omitempty"`
			Why     string `json:"why,omitempty"`
		}
		var answer struct {
			Rules []entry `json:"rules"`
		}
		rules := question[:strings.Index(question, "THE REPORT:")]
		for index, line := range strings.Split(strings.TrimSpace(strings.TrimPrefix(rules, "THE RULES:")), "\n") {
			marker := markerIn.FindString(line)
			if strings.Contains(report, marker) {
				answer.Rules = append(answer.Rules, entry{Rule: index + 1, Kind: "obligation", Verdict: "kept", Quote: marker})
			} else {
				answer.Rules = append(answer.Rules, entry{Rule: index + 1, Kind: "obligation", Verdict: "broken", Why: fmt.Sprintf("the report has no %s line", marker)})
			}
		}
		raw, _ := json.Marshal(answer)
		return textResponse(string(raw)), true
	}
}

// AN OMITTED OBLIGATION IS FOUND, SENT BACK, AND NOT RECORDED AS KEPT. The run
// wrote the Launch line and left the Travel line out; the check finds the
// omission, the run is sent back once with it, and the corrected report — both
// lines — is what publishes. Before, the report without SCOPE-TRAVEL was
// published and its record said "kept".
func TestAnOmittedObligationIsFoundAndSentBackNotRecordedAsKept(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	var corrections []string
	model := &scriptedCompleter{steps: []step{
		saying("<report>\n- press kit done\n- legal review pending\nSCOPE-LAUNCH\n</report>"),
		func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
			corrections = append(corrections, messageText(messages[len(messages)-1]))
			return saying("<report>\nSCOPE-TRAVEL\n- press kit done\n- legal review pending\nSCOPE-LAUNCH\n</report>")(ctx, messages)
		},
	}}
	asked := 0
	model.aside = truthfulChecker(&asked)
	runner := standingChildRunner(t, root, model)
	runner.parent.Governing = &Governing{Reader: rulesReader{travelRule, launchRule}}
	runDir := admittedRun(t, root)
	outcome, err := runner.Run(context.Background(), reporting(workspace), runDir, "")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(workspace, "reports", "r.md"))
	if !strings.HasPrefix(string(raw), "SCOPE-TRAVEL\n") {
		t.Fatalf("a report that omitted an obligation was published: %q (outcome %+v)", raw, outcome)
	}
	if len(corrections) != 1 || !strings.Contains(corrections[0], "SCOPE-TRAVEL") {
		t.Fatalf("the run was not sent back with the omission: %q", corrections)
	}
	record, _ := standing.ReadOccurrence(runDir)
	if record.RuleCheck == nil || !record.RuleCheck.Rewrote || !strings.Contains(record.RuleCheck.First, "SCOPE-TRAVEL") {
		t.Fatalf("the omission was not recorded as the finding: %+v", record.RuleCheck)
	}
}

// AN OBLIGATION STILL OMITTED AFTER ITS ONE CORRECTION HOLDS THE REPORT, and the
// record says it is broken rather than kept.
func TestAnObligationStillOmittedAfterItsCorrectionHoldsTheReport(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	omitted := "<report>\n- press kit done\nSCOPE-LAUNCH\n</report>"
	model := &scriptedCompleter{steps: []step{saying(omitted), saying(omitted)}}
	asked := 0
	model.aside = truthfulChecker(&asked)
	runner := standingChildRunner(t, root, model)
	runner.parent.Governing = &Governing{Reader: rulesReader{travelRule, launchRule}}
	runDir := admittedRun(t, root)
	outcome, err := runner.Run(context.Background(), reporting(workspace), runDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "reports", "r.md")); !os.IsNotExist(err) || outcome.Kind != standing.OutcomeNeedsYou || outcome.Withheld != "held-by-rules" {
		t.Fatalf("a report still omitting an obligation was not held: %+v", outcome)
	}
	record, _ := standing.ReadOccurrence(runDir)
	if record.RuleCheck == nil || record.RuleCheck.Verdict != "broken" || record.RuleCheck.Held == "" {
		t.Fatalf("the hold was not recorded as broken: %+v", record.RuleCheck)
	}
}
