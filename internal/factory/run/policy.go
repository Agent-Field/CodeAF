package run

import (
	"strings"
	"unicode"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// policyShape is a policy sentence this file knows how to apply.
type policyShape int

const (
	shapeUnknown policyShape = iota
	shapeTestsFirst
	shapeStrangerWrite
	shapeNoStranger
	shapePRNeedsGreen
	shapeNeverClose
)

// shapeOf reads one policy line into a shape, ignoring case, punctuation and
// the spacing between words.
func shapeOf(line string) policyShape {
	switch normPolicy(line) {
	case "tests pass before anything posts":
		return shapeTestsFirst
	case "a strangers pr never runs write":
		return shapeStrangerWrite
	case "nothing posts to a stranger":
		return shapeNoStranger
	case "no pull request without a green check":
		return shapePRNeedsGreen
	case "never close an issue":
		return shapeNeverClose
	}
	return shapeUnknown
}

func normPolicy(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r == '\'' || r == '’':
			// an apostrophe vanishes: stranger's is strangers.
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// UnknownPolicy returns the lines no code applies. They are for a model's
// reading elsewhere; reporting them keeps a typo from passing as a rule.
func UnknownPolicy(policy []string) []string {
	var out []string
	for _, l := range policy {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if shapeOf(l) == shapeUnknown {
			out = append(out, l)
		}
	}
	return out
}

// PolicyAllows applies the recipe's policy sentences to one outward action.
//
// POLICY IS APPLIED BY CODE, NEVER BY A MODEL. A line this file does not
// recognise is ignored here, and a line it does recognise refuses with the
// line itself as the reason, so the proof sheet quotes the rule that stopped
// the post.
func PolicyAllows(policy []string, it factory.Item, action factory.Action, prior []factory.StageResult) (bool, string) {
	for _, line := range policy {
		switch shapeOf(line) {
		case shapeTestsFirst:
			if !checksGreen(it, prior) {
				return false, line
			}
		case shapeNoStranger:
			if action.Verb == "comment" && it.Tier == factory.TierStranger {
				return false, line
			}
		case shapePRNeedsGreen:
			if (action.Verb == "pr" || action.Verb == "open-pr") && !someCheckGreen(it, prior) {
				return false, line
			}
		case shapeNeverClose:
			if action.Verb == "close" {
				return false, line
			}
		}
	}
	return true, ""
}

// PolicyAllowsStage is the loop's half: it refuses a stage before it runs.
// Only `a stranger's PR never runs write` speaks here.
func PolicyAllowsStage(policy []string, it factory.Item, stage factory.Stage) (bool, string) {
	for _, line := range policy {
		if shapeOf(line) == shapeStrangerWrite && stage.Name == "write" && it.Tier == factory.TierStranger {
			return false, line
		}
	}
	return true, ""
}

// checkIndexes are the places in prior that came from check stages. A prior
// result lines up with the item's stage of the same place; a result past the
// item's stages has no kind and is not counted as a check.
func checkIndexes(it factory.Item, prior []factory.StageResult) []int {
	var out []int
	for i := range prior {
		if i < len(it.Stages) && it.Stages[i].Kind == factory.StageCheck {
			out = append(out, i)
		}
	}
	return out
}

func testClaimFailed(prior []factory.StageResult) bool {
	for _, r := range prior {
		for _, c := range r.Claims {
			if c.Medium == "test" && !c.OK {
				return true
			}
		}
	}
	return false
}

// checksGreen: every check result exited 0 and no test claim failed.
func checksGreen(it factory.Item, prior []factory.StageResult) bool {
	if testClaimFailed(prior) {
		return false
	}
	for _, i := range checkIndexes(it, prior) {
		if prior[i].Exit != 0 {
			return false
		}
	}
	return true
}

// someCheckGreen: a check ran and passed, and nothing failed.
func someCheckGreen(it factory.Item, prior []factory.StageResult) bool {
	if !checksGreen(it, prior) {
		return false
	}
	if len(checkIndexes(it, prior)) > 0 {
		return true
	}
	for _, r := range prior {
		for _, c := range r.Claims {
			if c.Medium == "test" && c.OK {
				return true
			}
		}
	}
	return false
}
