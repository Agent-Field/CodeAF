package tui3

import "strings"

// Run-backed pages have no retargeting door. Keep their recorded assignment
// visible independently of the spend ledger, and say where a future choice is
// made instead of presenting a picker that would change another conversation.
func (a *app) roomRunModelRows(width int, row func(string, string) railLine) []railLine {
	var out []railLine
	if p := a.programOf(); p != nil {
		if program := p.page.Program; program != nil {
			for _, model := range program.Models {
				out = append(out, row("Model · "+modelBase(model), ""))
			}
		}
		if len(out) == 0 {
			out = append(out, row("Models · not recorded", ""))
		}
		if program := p.page.Program; program != nil && program.Effort != "" {
			out = append(out, row("Thinking · "+program.Effort, ""))
		}
		return append(out, a.roomModelLimitRows(width, "Models are fixed for this run.", "Choose models when starting it again.")...)
	}
	if plan := a.roomPlan(); plan != nil {
		for _, seat := range plan.page.Models {
			label := seat.Seat
			if label != "" {
				label = strings.ToUpper(label[:1]) + label[1:]
			}
			out = append(out, row(label+" · "+modelBase(seat.Model), ""))
		}
		if len(out) > 0 {
			out = append([]railLine{{text: a.pal.dim(fit("Assigned crew", width)), entry: -1}}, out...)
		}
		if used := plan.page.Row.Model; used != "" {
			out = append(out, row("Used model · "+modelBase(used), ""))
		} else if len(out) == 0 {
			out = append(out, row("Model · not recorded", ""))
		}
	}
	return append(out, a.roomModelLimitRows(width, "Run models cannot be changed here.", "/crew sets the next run's crew.")...)
}

func (a *app) roomModelLimitRows(width int, words ...string) []railLine {
	var out []railLine
	for _, word := range words {
		for _, line := range wrap(word, max(width, 1)) {
			out = append(out, railLine{text: a.pal.dim(line), entry: -1})
		}
	}
	return out
}
