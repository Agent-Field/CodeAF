package subharness

import (
	"fmt"
	"regexp"
	"strings"
)

// CONDITIONS: the tiny language a branch and a loop are steered by.
//
// It is deliberately not an expression language. The control layer of a
// sub-harness is DATA — the same law craft's workflows live under — and the
// moment a file can compute, a file can be wrong in ways nobody reading the card
// would see. So there are seven words, they all ask about ONE thing (what the
// step before this one produced, and whether it succeeded), and anything a
// person would want to express beyond them belongs in an agent.loop node's
// brief, where it is visible as a question somebody asked rather than as syntax.
//
//	always            take this arm
//	never             never take it — a case kept in the file but turned off
//	ok                the step before this one succeeded
//	failed            it did not
//	empty             it produced nothing
//	nonempty          it produced something
//	contains <text>   its output holds that text, case-insensitively
//	equals <text>     its output IS that text, trimmed and case-insensitively
//	matches <regexp>  its output matches
//
// The two-word forms take the REST of the line verbatim, quotes and all: a
// condition is written by a model as often as by a person, and a grammar with
// escaping in it is a grammar with a bug report in it.

// State is what a condition is asked about: the last step's output, and whether
// that step succeeded. It is one struct rather than two arguments because the
// runner threads it through every arm and a positional (string, bool) pair reads
// identically whichever way round it is wrong.
type State struct {
	Last string
	OK   bool
}

// conditionVerbs are the two-word forms, and the whole of them.
var conditionVerbs = []string{"contains", "equals", "matches"}

// ValidCondition parses one condition without evaluating it, in the words a
// builder can act on. It is called at parse time so that a program with a broken
// regular expression in it fails on the card rather than in the fourth round of
// a loop.
func ValidCondition(condition string) error {
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return fmt.Errorf("a condition is required (%s)", conditionWords())
	}
	verb, rest := splitCondition(condition)
	switch verb {
	case "always", "never", "ok", "failed", "empty", "nonempty":
		if rest != "" {
			return fmt.Errorf("%q takes no argument", verb)
		}
		return nil
	case "contains", "equals":
		if rest == "" {
			return fmt.Errorf("%q needs the text to look for", verb)
		}
		return nil
	case "matches":
		if rest == "" {
			return fmt.Errorf("matches needs a regular expression")
		}
		if _, err := regexp.Compile(rest); err != nil {
			return fmt.Errorf("matches: %s is not a regular expression: %w", rest, err)
		}
		return nil
	default:
		return fmt.Errorf("%q is not a condition (%s)", verb, conditionWords())
	}
}

// Match evaluates one condition against the state. A condition that does not
// parse is FALSE and an error — never a quiet true — because the arm it guards
// is the one thing a person approved on the card, and taking it on a typo would
// be running something nobody read.
func Match(condition string, state State) (bool, error) {
	if err := ValidCondition(condition); err != nil {
		return false, err
	}
	verb, rest := splitCondition(strings.TrimSpace(condition))
	last := strings.TrimSpace(state.Last)
	switch verb {
	case "always":
		return true, nil
	case "never":
		return false, nil
	case "ok":
		return state.OK, nil
	case "failed":
		return !state.OK, nil
	case "empty":
		return last == "", nil
	case "nonempty":
		return last != "", nil
	case "contains":
		return strings.Contains(strings.ToLower(state.Last), strings.ToLower(rest)), nil
	case "equals":
		return strings.EqualFold(last, strings.TrimSpace(rest)), nil
	case "matches":
		expression, err := regexp.Compile(rest)
		if err != nil {
			return false, err
		}
		return expression.MatchString(state.Last), nil
	}
	return false, fmt.Errorf("%q is not a condition", verb)
}

// splitCondition takes the first word and everything after it, with the rest
// left exactly as written.
func splitCondition(condition string) (string, string) {
	if at := strings.IndexAny(condition, " \t"); at >= 0 {
		return strings.ToLower(condition[:at]), strings.TrimSpace(condition[at+1:])
	}
	return strings.ToLower(condition), ""
}

func conditionWords() string {
	return "always, never, ok, failed, empty, nonempty, " +
		strings.Join(conditionVerbs, " <arg>, ") + " <arg>"
}
