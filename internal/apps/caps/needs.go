package caps

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// Needs derives the plan card's permissions from the definition, never from
// a model's claims about what its app will need.
func Needs(app *spec.App) []bridge.NeedLine {
	lines := []bridge.NeedLine{}
	if app == nil {
		return lines
	}
	for _, name := range keys(app.Capabilities.Records) {
		access := app.Capabilities.Records[name]
		if access != spec.AccessRead && access != spec.AccessReadWrite {
			continue
		}
		verb := "Read"
		if access == spec.AccessReadWrite {
			verb = "Read and write"
		}
		lines = append(lines, bridge.NeedLine{Kind: "records", Line: verb + " " + name + " records"})
	}
	for _, name := range keys(app.Capabilities.Connectors) {
		connector := app.Capabilities.Connectors[name]
		if len(connector.Can) != 0 {
			lines = append(lines, bridge.NeedLine{Kind: "connector", Line: name + ": " + strings.Join(connector.Can, ", ") + " as " + connector.As})
		}
		if len(connector.Approve) != 0 {
			lines = append(lines, bridge.NeedLine{Kind: "approval", Line: name + ": approval needed for " + strings.Join(connector.Approve, ", ")})
		}
	}
	if len(app.Capabilities.AI) != 0 {
		lines = append(lines, bridge.NeedLine{Kind: "ai", Line: "AI: " + strings.Join(app.Capabilities.AI, ", ")})
	}
	for _, tool := range app.Capabilities.Tools {
		lines = append(lines, bridge.NeedLine{Kind: "connector", Line: "Tool: " + tool})
	}
	if app.Budget.Month > 0 {
		lines = append(lines, bridge.NeedLine{Kind: "budget", Line: fmt.Sprintf("$%.2f USD per month", app.Budget.Month)})
	}
	if app.Budget.PerRun > 0 {
		lines = append(lines, bridge.NeedLine{Kind: "budget", Line: fmt.Sprintf("$%.2f USD per run", app.Budget.PerRun)})
	}
	return lines
}

func keys[V any](values map[string]V) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
