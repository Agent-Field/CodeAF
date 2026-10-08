package tui3

import "github.com/Agent-Field/codeaf/internal/factory"

// ── WHO SET A STAGE, AND WHY ────────────────────────────────────────────────
//
// A stage carries who last set it ([factory.ByRecipe], [factory.ByManager],
// [factory.ByPlan], [factory.ByYou]; "" is the recipe) and the reason they
// gave. These two accessors are the one place the item page reads them.

// stageBy is who last set the stage, and "" when nobody said.
func stageBy(s factory.Stage) string { return s.By }

// stageWhy is the reason given for the stage as it stands, and "" for none.
func stageWhy(s factory.Stage) string { return s.Why }

// stageSetByOther says whether someone other than the recipe set the stage:
// the manager, the plan or you. Such a stage wears the `+` on the item
// page's rail.
func stageSetByOther(s factory.Stage) bool {
	switch stageBy(s) {
	case factory.ByManager, factory.ByPlan, factory.ByYou:
		return true
	}
	return false
}
