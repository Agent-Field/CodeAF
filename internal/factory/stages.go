package factory

import "strings"

// Fits says whether a stage's [Stage.When] fits an item, which is how a
// banked recipe adapts to the item in front of it without anybody touching the
// recipe. It reads [Stage.When] alone; an approve step's when is the same
// knob, so `approve when large` holds only a large item. A stage
// that does not fit is skipped, and the surface draws it dim
// with its reason rather than leaving it out, so a person can see what the
// recipe would have done.
//
// THE CONDITIONS ARE READ OFF THE ITEM'S OWN TRIAGE AND NOTHING ELSE, so a
// frame can answer the question from a snapshot without touching disk. An
// empty condition and `always` fit everything. A condition this function does
// not know fits too: A STAGE IS NEVER SKIPPED ON A WORD NOBODY TAUGHT IT, because
// a silent skip is a stage that quietly never runs.
func Fits(st Stage, it Item) bool {
	return whenFits(st.When, it)
}

// whenFits is the one predicate a condition word is read with.
func whenFits(cond string, it Item) bool {
	switch strings.ToLower(strings.TrimSpace(cond)) {
	case "", "always":
		return true
	case "thin":
		return it.Triage.Thin()
	case "large":
		shown, _ := ShownSize(it)
		return shown == "L"
	case "touches auth":
		return areaIn(it.Triage.Area, "auth", "billing", "crypto")
	case "has ui":
		return areaIn(it.Triage.Area, "tui", "pages", "render")
	}
	return true
}

func areaIn(area string, names ...string) bool {
	area = strings.ToLower(strings.TrimSpace(area))
	for _, n := range names {
		if area == n {
			return true
		}
	}
	return false
}
