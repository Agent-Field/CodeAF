package factory

import (
	"path"
	"strings"
)

// Fits says whether a stage's [Stage.When] fits an item, which is how a
// banked recipe adapts to the item in front of it without anybody touching the
// recipe. It reads [Stage.When] alone; an approve step's when is the same
// knob, so `approve when large` holds only a large item. A stage
// that does not fit is skipped, and the surface draws it dim
// with its reason rather than leaving it out, so a person can see what the
// recipe would have done.
//
// Conditions read the item's snapshot without touching disk. File conditions
// use the measured change, then forge files, then an explicit triage area.
// Unknown files and area fit conservatively; the runner decides file conditions
// when their step comes up. Empty, always and unrecognized conditions fit too.
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
		return filesIn(it, "auth", "billing", "crypto")
	case "has ui":
		return filesIn(it, "tui", "pages", "render", "ui")
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

// FileCondition says whether a stage waits for the files known at its turn.
// These phases stay in the pending tail even if today's snapshot does not fit.
func FileCondition(st Stage) bool {
	switch strings.ToLower(strings.TrimSpace(st.When)) {
	case "touches auth", "has ui":
		return true
	}
	return false
}

func filesIn(it Item, names ...string) bool {
	if it.Change != nil && len(it.Change.Files) > 0 {
		for _, f := range it.Change.Files {
			if pathIn(f.Path, names...) {
				return true
			}
		}
		return false
	}
	if len(it.Files) > 0 {
		for _, f := range it.Files {
			if pathIn(f.Path, names...) {
				return true
			}
		}
		return false
	}
	return strings.TrimSpace(it.Triage.Area) == "" || areaIn(it.Triage.Area, names...)
}

func pathIn(file string, names ...string) bool {
	for _, segment := range strings.Split(strings.ToLower(file), "/") {
		if areaIn(segment, names...) || areaIn(strings.TrimSuffix(segment, path.Ext(segment)), names...) {
			return true
		}
	}
	return false
}
