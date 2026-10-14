package check

import (
	"regexp"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// runHint maps one cause of a failed sample run to its repair.
type runHint struct {
	match *regexp.Regexp
	hint  func(m []string) string
}

// runHints is ordered; the first match wins and the reference/query rules are
// the fallback only when no cause is recognised.
var runHints = []runHint{
	{regexp.MustCompile(`model reply missing (\S+)`), func(m []string) string {
		return "the agent step's output requires " + m[1] + "; name " + m[1] + " in that step's prompt as a JSON key to return, or drop it from output.required"
	}},
	{regexp.MustCompile(`model reply was not JSON`), func([]string) string {
		return "the agent step has an output schema; its prompt must ask for one JSON object with exactly those keys"
	}},
	{regexp.MustCompile(`missing run reference "([^"]+)"`), func(m []string) string {
		return "declare " + m[1] + " (a run param, an earlier step id or a loop variable) before using it; " + spec.RunReferenceRule
	}},
	{regexp.MustCompile(`no record type (\S+)`), func(m []string) string {
		return "use a record type declared in app.json instead of " + m[1]
	}},
	{regexp.MustCompile(`step (\S+) has no action`), func(m []string) string {
		return "give step " + m[1] + " exactly one action (agent, tool, records.*, wait, steps)"
	}},
	{regexp.MustCompile(`goto target is missing`), func([]string) string {
		return "point goto at an existing step id in this run"
	}},
	{regexp.MustCompile(`outbound needs approval`), func([]string) string {
		return "put gate: {approval: send} on the step that sends, in the same step as the action"
	}},
	{regexp.MustCompile(`too many steps`), func([]string) string {
		return "bound every loop with max and remove steps that repeat"
	}},
	{regexp.MustCompile(`shell steps are not available`), func([]string) string {
		return "replace the shell step with an agent, tool or records step"
	}},
	{regexp.MustCompile(`wait until must be a time`), func([]string) string {
		return "give wait.until a time expression such as now + days(1)"
	}},
	{regexp.MustCompile(`(?i)query`), func([]string) string { return spec.RunQueryRule }},
}

// runFailureHint is the repair for one sample-run failure message.
func runFailureHint(message string) string {
	for _, h := range runHints {
		if m := h.match.FindStringSubmatch(message); m != nil {
			return h.hint(m)
		}
	}
	return spec.RunReferenceRule + "; " + spec.RunQueryRule
}
