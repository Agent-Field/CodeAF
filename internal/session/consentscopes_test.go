package session

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/config"
)

// A receipt needs an explicit matched rule, never merely an allowing default.
func TestOnlyExplicitApprovalRulesAnnounceAutomaticConsent(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		name := "default"
		if explicit {
			name = "saved tool"
		}
		t.Run(name, func(t *testing.T) {
			policy := &approval.Policy{Default: approval.ActionAllow}
			if explicit {
				policy.Tools = map[string]approval.Action{"touch": approval.ActionAllow}
			}
			agent, runs := consentAgent(t, &scriptedCompleter{steps: toolCallTurn("touch")}, policy, true)
			profile := t.TempDir()
			agent.config.ProfileDir = profile
			origin := config.RuleOrigin{Agent: "api-docs", Place: "api", At: time.Now().UTC()}
			if explicit {
				if err := config.RememberToolApproval(profile, "touch", "allow", origin); err != nil {
					t.Fatal(err)
				}
			}
			events := collect(t, mustSubmit(t, agent, "touch the file"))
			if err := config.FlushRuleUsage(profile); err != nil {
				t.Fatal(err)
			}
			e, found := firstOfKind(events, EventConsentAllowed)
			if found != explicit {
				t.Fatalf("automatic receipt = %v, explicit = %v", found, explicit)
			}
			if found && (e.Tool != "touch" || e.Rule != "touch" || e.CallID == "") {
				t.Fatalf("automatic event = %+v", e)
			}
			if found && (e.ApprovalRule == nil || e.ApprovalRule.ID != "tool:touch" || e.ApprovalRule.Origin != origin || e.ApprovalRule.Uses != 1 || e.ApprovalRule.Scope != "every agent") {
				t.Fatalf("receipt data = %+v", e.ApprovalRule)
			}
			metadata, err := config.RuleMetadataAt(profile, "tool:touch")
			if err != nil || metadata.Uses != uint64(countKind(events, EventConsentAllowed)) {
				t.Fatalf("count = %+v, %v", metadata, err)
			}
			if len(runs) != 1 || countKind(events, EventConsentRequest) != 0 {
				t.Fatal("an allowed call asked or did not run")
			}
		})
	}
}

func TestConsentAskCarriesOutsideRule(t *testing.T) {
	for _, tc := range []struct{ name, tool, command, pattern, want string }{
		{"outside", "bash", "git push origin main", "git status*", "git status*"},
		{"unrelated", "bash", "git push origin main", "npm test*", ""},
		{"non-bash", "write", "git push origin main", "git status*", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
			agent.SetApprovalPolicy(&approval.Policy{BashPatterns: []approval.Rule{{Match: tc.pattern, Action: approval.ActionAllow}}})
			args, _ := json.Marshal(map[string]string{"command": tc.command})
			call := ai.ToolCall{ID: "c1", Function: ai.ToolCallFunction{Name: tc.tool, Arguments: string(args)}}
			q := agent.consentAsk(1, call, approval.Decision{Action: approval.ActionPrompt})
			if q.Outside != tc.want {
				t.Fatalf("outside = %q, want %q", q.Outside, tc.want)
			}
			raw, err := json.Marshal(q)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			if _, present := fields["outside"]; present != (tc.want != "") {
				t.Fatalf("outside omission: %s", raw)
			}
		})
	}
}
