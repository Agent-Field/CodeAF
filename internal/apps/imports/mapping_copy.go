package imports

import (
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

func sourceLabel(kind string) string {
	return map[string]string{"csv": "CSV", "airtable": "Airtable", "hubspot": "HubSpot"}[kind]
}
func choiceNote(app *spec.App, f *spec.Field, m bridge.Mapping, r *receipt, index int) string {
	values := map[string]bool{}
	for _, row := range r.Rows {
		for _, value := range sourceValues(f.Type, row[index]) {
			values[value] = true
		}
	}
	label := m.Field
	if !strings.HasSuffix(label, "s") {
		label += "s"
	}
	if f.Type == spec.FieldEnum || f.Type == spec.FieldEnums {
		return fmt.Sprintf("%d %s %s → %s", len(values), sourceLabel(r.Source), label, strings.Join(f.Values, " · "))
	}
	matched, unmatched, skipped := 0, 0, 0
	for value := range values {
		id, exists := m.People[value]
		switch {
		case !exists:
			unmatched++
		case id == "":
			skipped++
		default:
			matched++
		}
	}
	space := app.Space
	if space == "" || space == "personal" {
		space = "workspace"
	}
	note := fmt.Sprintf("%d %s match %s members", matched, label, space)
	if unmatched == 1 {
		note += " · 1 does not"
	} else if unmatched > 1 {
		note += fmt.Sprintf(" · %d do not", unmatched)
	}
	if skipped > 0 {
		note += fmt.Sprintf(" · %d skipped", skipped)
	}
	return note
}
