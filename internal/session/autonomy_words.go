package session

import (
	"strings"
	"time"
)

// AutonomyUsage is the shared spelling of the rule editor's accepted forms.
const AutonomyUsage = "/autonomy <kind> ask · recommend [duration] · decide"

// AutonomyKinds returns the question kinds in the terminal's display order.
func AutonomyKinds() []AskKind { return append([]AskKind{}, askKinds...) }

// ParseAutonomyLine reads the terminal and wire editor through one parser.
func ParseAutonomyLine(words string) (AskKind, Policy, string) {
	parts := strings.Fields(words)
	if len(parts) < 2 {
		return "", Policy{}, AutonomyUsage
	}
	kind := AutonomyKindNamed(parts[0])
	if kind == "" {
		return "", Policy{}, "there is no question kind called " + parts[0] + " · " + AutonomyUsage
	}
	// THE TWO REFUSALS ARE THE ENGINE'S AND ARE NOT RE-SPELLED HERE. Its door
	// turns down a rule over a confirmation and over a clarification, in its own
	// words, and this command prints them; a copy of the test on this side would
	// be a second definition of a law that has one (ONE SOURCE OF TRUTH).
	rule := Policy{Kind: PolicyAsk}
	switch parts[1] {
	case "ask":
	case "decide":
		rule.Kind = PolicyDecide
	case "recommend":
		// A LENGTH IS OPTIONAL. `recommend` on its own takes the engine's own
		// default, which is the one derivation of that figure (session's
		// [autonomyClock]); naming a duration is how somebody who wants a
		// different one says so.
		rule.Kind = PolicyRecommendThenAuto
		if len(parts) == 3 {
			after, err := time.ParseDuration(parts[2])
			if err != nil || after <= 0 {
				return "", Policy{}, "that duration is not understood: " + parts[2]
			}
			rule.After = after
		} else if len(parts) != 2 {
			return "", Policy{}, AutonomyUsage
		}
	default:
		return "", Policy{}, "choose ask, recommend [duration], or decide"
	}
	return kind, rule, ""
}

// AutonomyKindNamed resolves a kind by the spelling displayed in the terminal.
func AutonomyKindNamed(word string) AskKind {
	word = strings.TrimSpace(strings.ToLower(word))
	for _, kind := range AutonomyKinds() {
		if word == string(kind) {
			return kind
		}
	}
	return ""
}
