package main

// Words become chips. This is the whole bridge between the unstructured half
// (what you say) and the structured half (the work order). It is deliberately
// a small, honest parser: it lifts what it recognizes and leaves the rest as
// the title. A real build would let a cheap model do the lifting; the mock
// shows the interaction, not the NLP.

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	reCap    = regexp.MustCompile(`\$\s?(\d+(?:\.\d+)?)`)
	reRounds = regexp.MustCompile(`(?i)\b(\d+|one|two|three|four)\s+(?:review\s+)?rounds?\b`)
	reModel  = regexp.MustCompile(`(?i)\b(kimi(?:-k3)?|deepseek(?:-v4\.1(?:-flash)?)?|qwen(?:3\.5)?(?:-coder)?|glm(?:-5)?|minimax(?:-m3)?)\b`)
	reGate   = regexp.MustCompile(`(?i)\b(?:gate[:\s]+)?(plan first|plan gate|design first|ship it|self[- ]ship|no gate|auto)\b`)
	reRole   = regexp.MustCompile(`(?i)\b(plan|write|review|reviewer)\s+(?:with|on|using)?\s*(kimi(?:-k3)?|deepseek(?:-v4\.1(?:-flash)?)?|qwen(?:3\.5)?(?:-coder)?|glm(?:-5)?|minimax(?:-m3)?)\b`)
	reDont   = regexp.MustCompile(`(?i)\b(don'?t touch [a-z0-9/_. -]+?|keep [a-z0-9 ]+? compat(?:ible)?|no new deps?(?:endencies)?|stay in [a-z0-9/_.-]+)\b`)
)

var wordNums = map[string]int{"one": 1, "two": 2, "three": 3, "four": 4}

func canonModel(s string) string {
	s = strings.ToLower(s)
	switch {
	case strings.HasPrefix(s, "kimi"):
		return "kimi-k3"
	case strings.HasPrefix(s, "deepseek"):
		return "deepseek-v4.1-flash"
	case strings.HasPrefix(s, "qwen"):
		return "qwen3.5-coder"
	case strings.HasPrefix(s, "glm"):
		return "glm-5"
	case strings.HasPrefix(s, "minimax"):
		return "minimax-m3"
	}
	return s
}

// parseOrder lifts chips out of words. It returns the words with the lifted
// parts removed (the title) and the edited order.
func parseOrder(words string, o WorkOrder) (string, WorkOrder) {
	rest := words
	if m := reCap.FindStringSubmatch(rest); m != nil {
		if f, err := strconv.ParseFloat(m[1], 64); err == nil {
			o.Cap = f
		}
		rest = strings.Replace(rest, m[0], "", 1)
	}
	if m := reRounds.FindStringSubmatch(rest); m != nil {
		if n, ok := wordNums[strings.ToLower(m[1])]; ok {
			o.Rounds = n
		} else if n, err := strconv.Atoi(m[1]); err == nil {
			o.Rounds = n
		}
		rest = strings.Replace(rest, m[0], "", 1)
	}
	for _, m := range reRole.FindAllStringSubmatch(rest, -1) {
		mdl := canonModel(m[2])
		switch strings.ToLower(m[1]) {
		case "plan":
			o.PlanModel = mdl
		case "write":
			o.WriteModel = mdl
		default:
			o.ReviewModel = mdl
		}
		rest = strings.Replace(rest, m[0], "", 1)
	}
	if m := reModel.FindStringSubmatch(rest); m != nil {
		// A bare model name with no role sets the writer, the one that costs.
		o.WriteModel = canonModel(m[1])
		rest = strings.Replace(rest, m[0], "", 1)
	}
	if m := reGate.FindStringSubmatch(rest); m != nil {
		switch strings.ToLower(m[1]) {
		case "plan first", "plan gate", "design first":
			o.Gate = GatePlan
		case "ship it":
			o.Gate = GateShip
		default:
			o.Gate = GateNone
		}
		rest = strings.Replace(rest, m[0], "", 1)
	}
	low := strings.ToLower(rest)
	if strings.Contains(low, "security") {
		o.Security = true
		rest = reSecurity.ReplaceAllString(rest, "")
	}
	for _, m := range reDont.FindAllString(rest, -1) {
		o.Constraints = append(o.Constraints, strings.TrimSpace(m))
		rest = strings.Replace(rest, m, "", 1)
	}
	rest = strings.TrimSpace(strings.Join(strings.Fields(strings.NewReplacer(",", " ", " and ", " ", " with ", " ").Replace(rest)), " "))
	rest = strings.Trim(rest, " ,.;:")
	return rest, o
}

var reSecurity = regexp.MustCompile(`(?i)\b(?:with |and |a |do )?security(?: review| pass| check)?\b`)

// semantic turns a filter sentence into a predicate. Mocked: a handful of
// words a cheap model would obviously map, plus plain substring match.
func semantic(q string) func(*Item) bool {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return func(*Item) bool { return true }
	}
	terms := strings.Fields(q)
	return func(it *Item) bool {
		hay := strings.ToLower(it.Title + " " + it.Repo.Name + " " + it.Triage.Area + " " + it.Triage.Type + " " + it.Author + " " + it.Kind.String() + " " + strings.Join(it.Labels, " "))
		for _, t := range terms {
			switch t {
			case "risky", "risk", "dangerous":
				if it.Triage.Risk != "high" && it.Triage.Size != "L" && !it.Order.Security {
					return false
				}
			case "cheap", "quick", "small", "easy":
				if it.Triage.Est > 2.5 {
					return false
				}
			case "big", "large", "expensive":
				if it.Triage.Size != "L" {
					return false
				}
			case "stranger", "strangers", "external", "outside":
				if it.Tier != TierStranger {
					return false
				}
			case "mine", "own":
				if it.Author != "santosh" {
					return false
				}
			case "ready", "actionable":
				if it.Triage.Readiness < 60 {
					return false
				}
			case "unclear", "vague", "thin":
				if it.Triage.Readiness >= 60 {
					return false
				}
			case "money", "spend", "cost", "billing":
				if !strings.Contains(hay, "spend") && !strings.Contains(hay, "billing") && !strings.Contains(hay, "ledger") && !strings.Contains(hay, "invoice") && !strings.Contains(hay, "cap") {
					return false
				}
			case "ui", "visual", "screen":
				if !strings.Contains(hay, "tui") && !strings.Contains(hay, "ui") && !strings.Contains(hay, "pages") && !strings.Contains(hay, "render") && !strings.Contains(hay, "tree") {
					return false
				}
			case "prs", "pr", "review", "reviews":
				if it.Kind != KindPR {
					return false
				}
			case "bugs", "bug":
				if it.Triage.Type != "bug" {
					return false
				}
			case "dup", "dupes", "duplicates":
				if it.Triage.DupOf == 0 {
					return false
				}
			default:
				if !strings.Contains(hay, t) {
					return false
				}
			}
		}
		return true
	}
}

// parseStep reads "after review, make it neater" into a Step. With no time
// word the step runs after review, which is where most of them belong.
func parseStep(words string) Step {
	st := Step{When: "after review", On: true}
	low := strings.ToLower(strings.TrimSpace(words))
	for _, w := range []string{"after plan", "after write", "after review", "before proof", "after test"} {
		if strings.HasPrefix(low, w) {
			st.When = w
			words = strings.TrimSpace(words[len(w):])
			break
		}
	}
	st.Text = strings.Trim(strings.TrimSpace(words), " ,:;.")
	return st
}
