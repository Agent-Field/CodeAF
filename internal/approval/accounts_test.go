package approval

import "testing"

func TestAccountCLIsAskUnlessASavedRuleAllowsThem(t *testing.T) {
	allowAll := Policy{Default: ActionAllow}
	saved := Policy{Default: ActionAllow, BashPatterns: []Rule{{Match: "gh api *", Action: ActionAllow}}}
	cases := []struct {
		name    string
		policy  Policy
		command string
		want    Action
	}{
		{"gh read", allowAll, "gh api repos/Agent-Field/CodeAF", ActionPrompt},
		{"gh after cd", allowAll, "cd /tmp && gh repo view Agent-Field/CodeAF --json stargazerCount", ActionPrompt},
		{"gh by path", allowAll, "/opt/homebrew/bin/gh pr list", ActionPrompt},
		{"gh with env", allowAll, "GH_PAGER= gh issue list", ActionPrompt},
		{"gh in a pipe", allowAll, "gh api repos/x/y | jq .stargazers_count", ActionPrompt},
		{"aws", allowAll, "aws s3 ls", ActionPrompt},
		{"saved rule", saved, "gh api repos/Agent-Field/CodeAF", ActionAllow},
		{"anonymous fetch", allowAll, "curl -s https://api.github.com/repos/Agent-Field/CodeAF", ActionAllow},
		{"git is local", allowAll, "git status", ActionAllow},
		{"word inside args", allowAll, "echo gh", ActionAllow},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.policy.CheckBash(c.command).Action; got != c.want {
				t.Fatalf("%q = %v, want %v", c.command, got, c.want)
			}
		})
	}
}
