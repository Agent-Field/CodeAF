package main

import (
	"encoding/json"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	runengine "github.com/Agent-Field/codeaf/internal/run"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A receipt must identify the working directory and account for every paid
// call even when work stopped before it could finish.
func TestDoRunOutcomeKeepsSpendTokensAndWorkspaceOnEveryEnding(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "project")
	for _, ending := range []runengine.Outcome{
		runengine.OutcomeDone, runengine.OutcomeIncomplete,
		runengine.OutcomeLimit, runengine.OutcomeCannotRun,
	} {
		t.Run(string(ending), func(t *testing.T) {
			summary := runengine.Summary{
				Outcome: ending, USD: 0.125, TokensIn: 1234, TokensOut: 56,
				Nodes: 3, Seconds: 4.5, Result: "  hello  ", Failure: "a call failed",
			}
			outcome := runErrandOutcome(summary, workspace, runSpend{})
			encoded, err := json.Marshal(errandEnvelope(outcome))
			if err != nil {
				t.Fatal(err)
			}
			fields := errandJSONFields(t, string(encoded))
			if fields["spend"] != summary.USD || fields["spend_usd"] != summary.USD {
				t.Fatalf("receipt total differs from the run: %v", fields)
			}
			if fields["spend_work"] != summary.USD || fields["spend_overhead"] != float64(0) {
				t.Fatalf("node-only run's spend halves do not account for its calls: %v", fields)
			}
			if fields["workspace"] != workspace {
				t.Errorf("workspace = %v; want %q", fields["workspace"], workspace)
			}
			tokens := fields["tokens"].(map[string]any)
			if tokens["in"] != float64(summary.TokensIn) || tokens["out"] != float64(summary.TokensOut) {
				t.Errorf("tokens = %v; want %d in and %d out", tokens, summary.TokensIn, summary.TokensOut)
			}
		})
	}
}

// This drives the real run engine through the do door, so a complete builder
// that was never wired into the default road cannot satisfy the contract.
func TestDoRunJSONAccountsForPaidCallsInItsProject(t *testing.T) {
	beltRunEnv(t)
	t.Setenv("CODEAF_PLANDB_BIN", beltPlandbDoor(t))
	workspace := beltRepoWorkspace(t)
	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task: "write out.txt and say what you did", workspace: workspace, asJSON: true,
		timeout: time.Minute, slots: bound(1), stdout: &stdout, stderr: &stderr,
		newBeltCompleter: func(string) session.Completer { return finishingSeat(0.001) },
	})
	if err != nil {
		t.Fatalf("paid run did not finish: %v\n%s", err, stderr.String())
	}
	fields := doEnvelopeFields(t, stdout.String())
	spend := fields["spend"].(float64)
	if spend <= 0 || fields["spend_work"].(float64)+fields["spend_overhead"].(float64) != spend {
		t.Fatalf("spend halves do not sum to the paid total: %v", fields)
	}
	if fields["workspace"] != workspace {
		t.Errorf("workspace = %v; want %q", fields["workspace"], workspace)
	}
	// Every scripted response costs the same amount and reports 100 input
	// tokens and 20 output tokens, so the bill determines exact call totals.
	calls := math.Round(spend / 0.001)
	tokens := fields["tokens"].(map[string]any)
	if tokens["in"] != calls*100 || tokens["out"] != calls*20 {
		t.Errorf("tokens = %v; want totals for %v paid calls", tokens, calls)
	}
}
