package approval

import (
	"encoding/json"
	"strings"
)

// AlwaysAsks reports whether a call is one of the two the gate must put to a
// person EVERY time — the critical shapes in bash.go's table, and the calls
// that leave this machine in somebody's own name (approval.go's
// [ActsInThePersonsName]).
//
// It exists for internal/session's consent gate, which keeps a memo of "stop
// asking me about this tool" and consults it before running anything. A memo
// could otherwise turn one approved `git status` into silence for
// `rm -rf /var/tmp/x` for the rest of the session. Protected deletions can
// still match the critical table here because the policy refuses them before
// the session consults this predicate for a memo or a question.
//
// It is deliberately NOT the whole policy asked again. Everything else the
// policy has to say about a call is a rule somebody wrote, and a memo standing
// in for a person is exactly what the memo is for. These two are the floor —
// the shapes that hold under a blanket allow — and a floor that a keystroke
// elsewhere could lift is not a floor.
func AlwaysAsks(tool string, args json.RawMessage) bool {
	tool = strings.TrimSpace(tool)
	if ActsInThePersonsName(tool, args) {
		return true
	}
	if tool != ToolBash {
		return false
	}
	command, ok := bashCommand(args)
	if !ok {
		// Nothing to match against the table. The policy has already turned an
		// unreadable bash call into a prompt of its own (Check), and inventing a
		// floor for a call whose text never arrived would make every malformed
		// call unanswerable rather than safe.
		return false
	}
	segments, _ := splitSegments(command)
	_, hit := criticalHit(command, segments)
	return hit
}

// AlwaysAsksRule reports whether a decision's rule string is one of the
// floor's own always-ask reasons — a critical shell command or a call that
// acts in the person's name outside this machine.
//
// It exists so a surface building a consent question from an event that
// carries the gate's rule reason grades the same floor calls as irreversible
// that [AlwaysAsks] grades from the raw call.
func AlwaysAsksRule(rule string) bool {
	rule = strings.TrimSpace(rule)
	return strings.HasPrefix(rule, criticalCommandRulePrefix) ||
		(strings.HasSuffix(rule, actsInPersonsNameSuffix) && len(rule) > len(actsInPersonsNameSuffix))
}
