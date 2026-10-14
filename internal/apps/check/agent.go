package check

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

func agentCapabilities(b *spec.Bundle) []Issue {
	allowed := map[string]bool{}
	required := map[string]bool{}
	for _, typ := range keys(b.App.Capabilities.Records) {
		access := b.App.Capabilities.Records[typ]
		if b.App.Records[typ] == nil {
			continue
		}
		verbs := []string{"query", "get"}
		if access == spec.AccessReadWrite {
			verbs = append(verbs, "create", "update", "delete")
		}
		for _, verb := range verbs {
			allowed["records."+verb] = true
			required["records."+verb] = true
		}
	}
	if len(b.Runs) > 0 {
		allowed["runs.start"] = true
	}
	for name, connector := range b.App.Capabilities.Connectors {
		for _, verb := range connector.Can {
			allowed[name+"."+verb] = true
		}
	}
	var issues []Issue
	for i, tool := range b.Agent.Tools {
		if !allowed[tool] {
			issues = append(issues, finding(spec.AgentFile, fmt.Sprintf("/tools/%d", i), "agent/capability", "tool "+tool+" is not allowed by app capabilities", "derive tools from readable/writable records, existing runs and granted connector verbs; never grant definition-writing tools"))
		}
	}
	var missing []string
	for _, tool := range keys(required) {
		if !slices.Contains(b.Agent.Tools, tool) {
			missing = append(missing, tool)
		}
	}
	if len(missing) > 0 {
		issues = append(issues, finding(spec.AgentFile, "/tools", "agent/records", "agent cannot perform permitted record operations: missing "+strings.Join(missing, ", "), "include records.query/get for readable types and records.create/update/delete for every rw type; the bridge checks each call's record type"))
	}
	return issues
}
