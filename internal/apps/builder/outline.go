package builder

import (
	"encoding/json"
	"slices"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

// The outlined data row belongs to the choice while reuse is pending. Keep
// the authored plan complete for the writer, Check and the saved definition.
func (j *job) projectOutline(app schema.App, plan schema.Plan) {
	for i := range plan.Data {
		line := &plan.Data[i]
		if hasBuilderDetail(line.Line) {
			line.Line = displayName(app.Records[line.ID].Label, line.ID)
		}
	}
	choices := map[string]int{}
	var offers []Offer
	for _, offer := range j.Offers {
		row := outlineRecord(offer, app, plan.Data)
		if row != nil && slices.Contains(j.Using, row.ID) {
			continue
		}
		if row != nil {
			outline := *row
			offer.Outline = &outline
		}
		key := offer.Type
		if row != nil {
			key = row.ID
		} else if offer.Common != "" {
			key = offer.Common
		}
		index, exists := choices[key]
		switch {
		case !exists:
			choices[key] = len(offers)
			offers = append(offers, offer)
		case offers[index].App == "" && offer.App != "":
			offers[index] = offer
		}
	}
	if offers == nil {
		offers = []Offer{}
	}
	j.Offers = offers
	rowsOwnedByOffers := map[string]bool{}
	for i := range offers {
		offer := &offers[i]
		if offer.Outline != nil {
			rowsOwnedByOffers[offer.Outline.ID] = true
			if offer.App == "" && !hasBuilderDetail(offer.Outline.Line) {
				// Keep the original description with a new-list suggestion.
				offer.Line = offer.Outline.Line + " · " + offer.Line
			}
		}
	}
	seen := map[string]bool{}
	plan.Data = slices.DeleteFunc(plan.Data, func(line schema.PlanLine) bool {
		duplicate := seen[line.ID]
		seen[line.ID] = true
		return duplicate || rowsOwnedByOffers[line.ID]
	})
	if plan.Data == nil {
		plan.Data = []schema.PlanLine{}
	}
	for _, section := range [][]schema.PlanLine{plan.Automation, plan.Actions, plan.Screens} {
		for i := range section {
			line := &section[i]
			if !hasBuilderDetail(line.Line) {
				continue
			}
			screen, _ := schema.ParseScreen([]byte(j.Working.Files["screens/"+line.ID+".json"]))
			line.Line = displayName(screen.Title, line.ID)
		}
	}
	if plan.Agent != nil && hasBuilderDetail(plan.Agent.Line) {
		plan.Agent.Line = "Help with this app"
	}
	if plan.Ask == "" || hasBuilderDetail(plan.Ask) {
		plan.Ask = "Outline for " + displayName(app.Name, "your app")
	}
	if raw, err := json.Marshal(plan); err == nil {
		j.Plan = raw
	}
}

func outlineRecord(offer Offer, app schema.App, lines []schema.PlanLine) *schema.PlanLine {
	var match *schema.PlanLine
	count := 0
	for i := range lines {
		line := &lines[i]
		if line.ID == offer.Type {
			return line
		}
		if offer.Common != "" && app.Records[line.ID].Common == offer.Common {
			match = line
			count++
		}
	}
	if count == 1 {
		return match
	}
	return nil
}
