package builder

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/builder/prompt"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// Need kinds, as the plan card groups them.
const (
	needRecords   = "records"
	needConnector = "connector"
	needAI        = "ai"
	needBudget    = "budget"
	needApproval  = "approval"
)

// planNeeds estimates Needs before app.json exists, from the plan lines and
// what the owner chose in the New app sheet.
func planNeeds(p prompt.Plan, b *build) []bridge.NeedLine {
	ids := make([]string, len(p.Data))
	for i, l := range p.Data {
		ids[i] = l.ID
	}
	needs := []bridge.NeedLine{{Kind: needRecords, Line: "Records · " + strings.Join(ids, ", ")}}
	for _, c := range b.connectors {
		needs = append(needs, bridge.NeedLine{Kind: needConnector, Line: c})
	}
	needs = append(needs, bridge.NeedLine{Kind: needAI, Line: "App agent · " + b.agentModel})
	if mentionsApproval(p.Automation) {
		needs = append(needs, bridge.NeedLine{Kind: needApproval, Line: "Approval · you approve before anything leaves"})
	}
	return append(needs, budgetNeed(b.budget))
}

func mentionsApproval(lines []prompt.Line) bool {
	for _, l := range lines {
		if strings.Contains(strings.ToLower(l.Line), "approv") {
			return true
		}
	}
	return false
}

// appNeeds computes Needs from the written app.json, which is the truth once
// Write has run: records, connector verbs, AI actions, approvals and budget.
func appNeeds(app *spec.App) []bridge.NeedLine {
	types := make([]string, 0, len(app.Records))
	for id := range app.Records {
		types = append(types, id)
	}
	sort.Strings(types)
	needs := []bridge.NeedLine{{Kind: needRecords, Line: "Records · " + strings.Join(types, ", ")}}
	needs = append(needs, connectorNeeds(app)...)
	if len(app.Capabilities.AI) > 0 {
		needs = append(needs, bridge.NeedLine{Kind: needAI, Line: "AI · " + strings.Join(app.Capabilities.AI, ", ")})
	}
	return append(needs, budgetNeed(app.Budget.Month))
}

func connectorNeeds(app *spec.App) []bridge.NeedLine {
	names := make([]string, 0, len(app.Capabilities.Connectors))
	for name := range app.Capabilities.Connectors {
		names = append(names, name)
	}
	sort.Strings(names)
	var needs []bridge.NeedLine
	for _, name := range names {
		c := app.Capabilities.Connectors[name]
		needs = append(needs, bridge.NeedLine{Kind: needConnector, Line: name + " · " + strings.Join(c.Can, ", ")})
		for _, verb := range c.Approve {
			needs = append(needs, bridge.NeedLine{Kind: needApproval, Line: fmt.Sprintf("Approval · %s.%s", name, verb)})
		}
	}
	return needs
}

func budgetNeed(month float64) bridge.NeedLine {
	return bridge.NeedLine{Kind: needBudget, Line: fmt.Sprintf("Budget · up to $%.0f a month", month)}
}
