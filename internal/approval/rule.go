package approval

import (
	"encoding/json"
	"strings"
)

// ToolRemember is the memory tool, named here because one shape of its call is
// a floor of its own ([KeepsARule]). It is a wire token the session's belt
// registers under this exact name; internal/session's tool is built from it.
const ToolRemember = "remember"

// KeepsARuleReason is what the person reads on the card a rule-keeping call
// raises, and what the model is told when nobody was there to answer it. It is
// one sentence spelled once, for [Decision.Rule]'s reason: every surface says the
// same words about the same floor rather than each deriving its own.
const KeepsARuleReason = "an always rule is put in front of every conversation and task here"

// KeepsARule reports whether a call would keep a RULE: `remember` with `always`
// set, which puts its line in front of every later turn and task at its scope
// rather than recalling it when it seems relevant.
//
// IT IS A FLOOR UNDER EVERY ALLOW, INCLUDING ONE THAT NAMES THE TOOL. The
// shipped rows allow `remember` by name because writing itself a note is the
// agent's own bookkeeping, which nobody should be asked about. A rule is not
// bookkeeping: it is an instruction to every conversation the person will have
// here, and the person is the only one who can give that. So the floor does not
// yield to a named allow the way [ActsInThePersonsName]'s does — the named allow
// was written about notes, and this is not a note. A deny still refuses: a rule
// that refuses outright is a refusal, and a floor only ever turns a yes into a
// question.
//
// AN UNREADABLE CALL IS NOT A RULE. The arguments say whether `always` is set;
// if they cannot be read the call cannot run either, and the tool's own refusal
// is the honest answer rather than a question about nothing.
func KeepsARule(tool string, args json.RawMessage) bool {
	if strings.TrimSpace(tool) != ToolRemember {
		return false
	}
	var fields struct {
		Always bool `json:"always"`
	}
	if err := json.Unmarshal(args, &fields); err != nil {
		return false
	}
	return fields.Always
}

// keepsARuleFloor turns an allow into a question for a call that keeps a rule,
// and leaves every other answer exactly as it came.
func keepsARuleFloor(tool string, args json.RawMessage, decision Decision) Decision {
	if decision.Action != ActionAllow || !KeepsARule(tool, args) {
		return decision
	}
	return Decision{Action: ActionPrompt, Rule: KeepsARuleReason}
}
