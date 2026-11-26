package runs

import (
	"maps"
	"slices"
)

// StepDetail exposes the steps without their internal prompts or definitions.
type StepDetail struct {
	ID    string       `json:"id"`
	Label string       `json:"label,omitempty"`
	Kind  string       `json:"kind"`
	State string       `json:"state"`
	Steps []StepDetail `json:"steps,omitempty"`
}

// ApprovalRecord identifies a target or a record the run received as input.
type ApprovalRecord struct {
	Subject
	Label string `json:"label,omitempty"`
}

func (c *checkpoint) stepDetails(steps []Step) []StepDetail {
	details := make([]StepDetail, 0, len(steps))
	for _, step := range steps {
		state := "planned"
		if step.ID == c.Step {
			state = c.State
		} else if _, completed := c.Outputs[step.ID]; completed {
			state = Done
		}
		details = append(details, StepDetail{
			ID: step.ID, Label: step.Title, Kind: detailStepKind(step), State: state, Steps: c.stepDetails(step.Steps),
		})
	}
	return details
}

func detailStepKind(step Step) string {
	switch {
	case step.Agent != "":
		return "agent"
	case step.AI != nil:
		return "ai"
	case step.Tool != "":
		return "tool"
	case step.Render != nil:
		return "render"
	case step.RecordsUpdate != nil:
		return "records_update"
	case step.RecordsCreate != nil:
		return "records_create"
	case step.Loop != nil:
		return "loop"
	case step.Wait != nil:
		return "wait"
	default:
		return "gate"
	}
}

// Project the details from the saved run so old checkpoints need no migration.
func (c *checkpoint) projectDetails() {
	c.Run.Steps = c.stepDetails(c.Definition.Steps)
	if c.Approval == nil || c.Action == nil {
		return
	}
	c.Approval.RecordType = c.Action.RecordType
	seen := map[Subject]bool{}
	add := func(record Subject) {
		if record.Type != "" && record.ID != "" && !seen[record] {
			seen[record] = true
			c.Approval.Records = append(c.Approval.Records, ApprovalRecord{Subject: record})
		}
	}
	addRow := func(raw any) {
		if row, ok := raw.(map[string]any); ok {
			typ, _ := row["type"].(string)
			id, _ := row["id"].(string)
			add(Subject{Type: typ, ID: id})
		}
	}
	for _, id := range c.Action.IDs {
		add(Subject{Type: c.Action.RecordType, ID: id})
	}
	if c.Subject != nil {
		add(*c.Subject)
	}
	addRow(c.Input["record"])
	for _, name := range slices.Sorted(maps.Keys(c.Definition.Params)) {
		switch c.Definition.Params[name].Type {
		case "ref":
			addRow(c.Params[name])
		case "refs":
			list, _ := c.Params[name].([]any)
			for _, row := range list {
				addRow(row)
			}
		}
	}
}
