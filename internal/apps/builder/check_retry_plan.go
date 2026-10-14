package builder

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// reportNames reports whether a Check report has an issue in file.
func reportNames(report, file string) bool {
	for _, line := range strings.Split(report, "\n") {
		if strings.HasPrefix(line, file) {
			return true
		}
	}
	return false
}

// keepPlanFix accepts a plan.json repair only when Check named plan.json and
// the new plan still parses; the plan card then shows the repaired lines.
func keepPlanFix(b *build, changed map[string][]byte, named bool) {
	raw, ok := changed[spec.PlanFile]
	if !ok {
		return
	}
	plan, err := decodePlan(string(raw))
	if !named || err != nil {
		delete(changed, spec.PlanFile)
		return
	}
	b.setPlan(plan)
}
