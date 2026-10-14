package builder

import (
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/apps/builder/prompt"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// agentLineID is the fixed id plan.json gives the agent line.
const agentLineID = "agent"

// specPlan is the plan as plan.json and the plan card carry it.
func specPlan(p prompt.Plan) spec.Plan {
	return spec.Plan{
		Ask:        p.Ask,
		Name:       p.Name,
		Data:       specLines(p.Data),
		Automation: specLines(p.Automation),
		Screens:    specLines(p.Screens),
		Agent:      spec.PlanLine{ID: agentLineID, Line: p.Agent.Line},
		Needs:      "computed",
		Tint:       p.Tint,
	}
}

func specLines(lines []prompt.Line) []spec.PlanLine {
	out := make([]spec.PlanLine, len(lines))
	for i, l := range lines {
		out[i] = spec.PlanLine{ID: l.ID, Line: l.Line}
	}
	return out
}

// planFromSpec is the inverse, used when a plan is read back from the store.
func planFromSpec(p spec.Plan) prompt.Plan {
	return prompt.Plan{
		Ask:        p.Ask,
		Name:       p.Name,
		Data:       promptLines(p.Data),
		Automation: promptLines(p.Automation),
		Screens:    promptLines(p.Screens),
		Agent:      prompt.AgentLine{ID: agentLineID, Line: p.Agent.Line},
		Needs:      "computed",
		Tint:       p.Tint,
	}
}

func promptLines(lines []spec.PlanLine) []prompt.Line {
	out := make([]prompt.Line, len(lines))
	for i, l := range lines {
		out[i] = prompt.Line{ID: l.ID, Line: l.Line}
	}
	return out
}

func planJSON(p prompt.Plan) ([]byte, error) {
	return json.MarshalIndent(specPlan(p), "", "  ")
}
