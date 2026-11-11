package approval

import (
	"encoding/json"
	"testing"
)

func TestApprovalDecisionCarriesOnlyTheMatchedScope(t *testing.T) {
	policy := Policy{Default: ActionPrompt, Tools: map[string]Action{"write": ActionAllow}, BashPatterns: []Rule{{Match: "git status*", Action: ActionAllow}, {Match: "rm *", Action: ActionAllow}}}
	for _, tc := range []struct {
		tool, command, scope string
		action               Action
	}{
		{"bash", "git status --porcelain", "git status*", ActionAllow},
		{"bash", "git push", "", ActionPrompt},
		{"bash", "rm -rf /", "", ActionPrompt},
		{"write", "", "write", ActionAllow},
		{"edit", "", "", ActionPrompt},
	} {
		t.Run(tc.tool+tc.command, func(t *testing.T) {
			args, _ := json.Marshal(map[string]string{"command": tc.command})
			d := policy.Check(tc.tool, args)
			if d.Scope != tc.scope || d.Action != tc.action {
				t.Fatalf("decision = %+v, want %q %s", d, tc.scope, tc.action)
			}
		})
	}
}

func TestOutsideRuleIsLongestNonCoveringSameProgram(t *testing.T) {
	for _, tc := range []struct {
		name, tool, command, want string
		rules                     []Rule
	}{
		{"outside", "bash", "git push origin main", "git status*", []Rule{{Match: "git status*", Action: ActionAllow}}},
		{"longest", "bash", "git push origin main", "git status --short", []Rule{{Match: "git log*", Action: ActionAllow}, {Match: "git status --short", Action: ActionAllow}, {Match: "git diff*", Action: ActionAllow}}},
		{"covering excluded", "bash", "git push origin main", "git status*", []Rule{{Match: "git push origin*", Action: ActionAllow}, {Match: "git status*", Action: ActionAllow}}},
		{"unrelated", "bash", "git push origin main", "", []Rule{{Match: "npm test*", Action: ActionAllow}}},
		{"no rules", "bash", "git push origin main", "", nil},
		{"non bash", "write", "git push origin main", "", []Rule{{Match: "git status*", Action: ActionAllow}}},
		{"program boundary", "bash", "github status", "", []Rule{{Match: "git status*", Action: ActionAllow}}},
		{"matching only", "bash", "git status --short", "", []Rule{{Match: "git status*", Action: ActionAllow}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args, _ := json.Marshal(map[string]string{"command": tc.command})
			if got := (Policy{BashPatterns: tc.rules}).OutsideRule(tc.tool, args); got != tc.want {
				t.Fatalf("outside = %q, want %q", got, tc.want)
			}
		})
	}
}
