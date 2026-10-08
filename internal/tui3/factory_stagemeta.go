package tui3

import "github.com/Agent-Field/codeaf/internal/factory"

// ── WHO SET A STAGE, AND WHY ────────────────────────────────────────────────
//
// A stage carries who last set it (`recipe`, `manager`, `plan`, `you`, or
// nothing) and the reason they gave. The fields arrive with factory/runedit;
// until then these two accessors are the one place the item page reads them,
// and they answer nothing.

// The setters other than the recipe, as the engine spells them: a stage one of
// them set wears the `+` on the item page's rail.
const (
	stageByManager = "manager"
	stageByPlan    = "plan"
	stageByYou     = "you"
)

// stageSetByOther says whether someone other than the recipe set the stage.
func stageSetByOther(s factory.Stage) bool {
	switch stageBy(s) {
	case stageByManager, stageByPlan, stageByYou:
		return true
	}
	return false
}

// factoryStageMetaFake stands in for the two fields in tests until they land:
// nil everywhere else.
var factoryStageMetaFake func(s factory.Stage) (by, why string)

// stageBy is who last set the stage, and "" when nobody said.
func stageBy(s factory.Stage) string {
	// until factory/runedit lands: then s.By / s.Why
	if factoryStageMetaFake != nil {
		by, _ := factoryStageMetaFake(s)
		return by
	}
	return ""
}

// stageWhy is the reason given for the stage as it stands, and "" for none.
func stageWhy(s factory.Stage) string {
	// until factory/runedit lands: then s.By / s.Why
	if factoryStageMetaFake != nil {
		_, why := factoryStageMetaFake(s)
		return why
	}
	return ""
}
