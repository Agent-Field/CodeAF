package approval

import (
	"encoding/json"
	"testing"
)

// A RULE IS THE PERSON'S TO GIVE. `remember` with `always` set asks under
// every allow — the blanket one, and the shipped row that names the tool for
// the agent's own notes — and a deny still refuses; the same tool writing an
// ordinary note is untouched.
func TestARuleAsksUnderEveryAllowAndANoteDoesNot(t *testing.T) {
	rule := json.RawMessage(`{"text":"always use tabs here","always":true}`)
	note := json.RawMessage(`{"text":"prefers tabs"}`)
	for name, policy := range map[string]Policy{
		"blanket allow":        {Default: ActionAllow},
		"the row names it":     {Default: ActionPrompt, Tools: map[string]Action{ToolRemember: ActionAllow}},
		"allow and named, too": {Default: ActionAllow, Tools: map[string]Action{ToolRemember: ActionAllow}},
	} {
		got := policy.Check(ToolRemember, rule)
		if got.Action != ActionPrompt || got.Rule != KeepsARuleReason {
			t.Errorf("%s: a rule = %v, want a question saying %q", name, got, KeepsARuleReason)
		}
		if got := policy.Check(ToolRemember, note); got.Action != ActionAllow {
			t.Errorf("%s: an ordinary note = %v, want it allowed", name, got)
		}
	}
	denied := Policy{Default: ActionAllow, Tools: map[string]Action{ToolRemember: ActionDeny}}
	if got := denied.Check(ToolRemember, rule); got.Action != ActionDeny {
		t.Errorf("a rule under a deny = %v, want the deny to stand", got)
	}
}

// ONLY THE MEMORY TOOL'S OWN FIELD MAKES A RULE. Another tool carrying the same
// word, an `always` that is false and arguments that cannot be read are none of
// them a rule.
func TestOnlyRememberWithAlwaysKeepsARule(t *testing.T) {
	for _, call := range []struct {
		tool string
		args string
		want bool
	}{
		{ToolRemember, `{"text":"x","always":true}`, true},
		{" " + ToolRemember + " ", `{"text":"x","always":true}`, true},
		{ToolRemember, `{"text":"x","always":false}`, false},
		{ToolRemember, `{"text":"x"}`, false},
		{ToolRemember, `not json`, false},
		{"write", `{"always":true}`, false},
	} {
		if got := KeepsARule(call.tool, json.RawMessage(call.args)); got != call.want {
			t.Errorf("KeepsARule(%q, %s) = %v, want %v", call.tool, call.args, got, call.want)
		}
	}
}
