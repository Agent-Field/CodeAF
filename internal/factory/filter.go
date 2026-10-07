package factory

import "strings"

// Match is the floor's filter: the words a person types into the rail's `/`
// box, read as a test an item passes or fails. Every word must hold. A word
// that is one of the floor's own words is a question about the item rather
// than a substring; anything else is a plain substring of the title, the repo,
// the area, the triage type and the author, without regard to case.
//
// The words are:
//
//	risky      the risk is high, or the size is L
//	cheap      the estimate is at most 2.5
//	strangers  the author is a stranger
//	spend      the area or the title mentions spend, billing, ledger or cap
//	ui         the area is tui, pages or render
//	prs        the item is a pull request
//	bugs       the triage type is bug
//	thin       the readiness is under 55
//	mine       the author is santosh
//
// THE MATCHER LIVES HERE AND NOT IN THE MOCK because the surface must not
// import the mock (it is built only under the factorymock tag), and a filter
// is a fact about the floor's vocabulary, not about one fake floor. The empty
// query matches everything.
func Match(q string) func(Item) bool {
	terms := strings.Fields(strings.ToLower(q))
	if len(terms) == 0 {
		return func(Item) bool { return true }
	}
	return func(it Item) bool {
		for _, t := range terms {
			if !matchTerm(it, t) {
				return false
			}
		}
		return true
	}
}

// ThinReadiness is the readiness under which an item is thin and wants
// questions before it is worth running.
const ThinReadiness = 55

// CheapEst is the largest estimate the word cheap still covers.
const CheapEst = 2.5

func matchTerm(it Item, t string) bool {
	area := strings.ToLower(it.Triage.Area)
	title := strings.ToLower(it.Title)
	switch t {
	case "risky":
		return it.Triage.Risk == "high" || it.Triage.Size == "L"
	case "cheap":
		return it.Triage.Est > 0 && it.Triage.Est <= CheapEst
	case "strangers":
		return it.Tier == TierStranger
	case "spend":
		for _, w := range []string{"spend", "billing", "ledger", "cap"} {
			if strings.Contains(area, w) || strings.Contains(title, w) {
				return true
			}
		}
		return false
	case "ui":
		switch area {
		case "tui", "pages", "render":
			return true
		}
		return false
	case "prs":
		return it.Kind == KindPR
	case "bugs":
		return it.Triage.Type == "bug"
	case "thin":
		return it.Triage.Readiness < ThinReadiness
	case "mine":
		return it.Author == "santosh"
	}
	hay := title + " " + strings.ToLower(it.Repo) + " " + area + " " +
		strings.ToLower(it.Triage.Type) + " " + strings.ToLower(it.Author)
	return strings.Contains(hay, t)
}
