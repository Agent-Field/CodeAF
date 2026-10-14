package check

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// agentRules are the checks agent.md must pass, in report order.
var agentRules = []func(*spec.Bundle) []Issue{agentTools, agentAnswerMark}

func agentCapabilities(b *spec.Bundle) []Issue {
	var issues []Issue
	for _, rule := range agentRules {
		issues = append(issues, rule(b)...)
	}
	return issues
}

// agentTools holds agent.md tools to least privilege: every tool is granted by
// app.json capabilities and every permitted record operation is present.
func agentTools(b *spec.Bundle) []Issue {
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
			issues = append(issues, finding(spec.AgentFile, fmt.Sprintf("/tools/%d", i), "agent/capability", "tool "+tool+" is not allowed by app capabilities", "remove "+tool+" from agent.md tools"+grantHint(tool, b.App.Capabilities.Connectors)))
		}
	}
	var missing []string
	for _, tool := range keys(required) {
		if !slices.Contains(b.Agent.Tools, tool) {
			missing = append(missing, tool)
		}
	}
	if len(missing) > 0 {
		issues = append(issues, finding(spec.AgentFile, "/tools", "agent/records", "agent cannot perform permitted record operations: missing "+strings.Join(missing, ", "), "add "+strings.Join(missing, ", ")+" to agent.md tools (records.query/get for readable types, records.create/update/delete for rw types; the bridge checks each call's record type)"))
	}
	return issues
}

// grantHint names the one app.json change that would allow tool. A tool no
// capability can grant gets the reason instead.
func grantHint(tool string, connectors map[string]spec.Connector) string {
	family, verb, _ := strings.Cut(tool, ".")
	_, declared := connectors[family]
	switch {
	case family == "records" && (verb == "query" || verb == "get"):
		return ", or give a record type r or rw in app.json capabilities.records"
	case family == "records" && slices.Contains([]string{"create", "update", "delete"}, verb):
		return ", or make the record type rw in app.json capabilities.records if people may change it"
	case tool == "runs.start":
		return ", or write the runs/<id>.yaml the agent starts"
	case declared:
		return ", or add \"" + verb + "\" to app.json capabilities.connectors." + family + ".can if the app uses it"
	}
	return "; agent tools come only from records, runs and connectors in app.json capabilities, never from tools that change the app"
}

// aiMark is rule 9's label suffix. It belongs on labels of ai fields and
// actions; the chat panel already shows every answer as AI.
const aiMark = "· AI"

var negations = []string{"never", "not", "don't", "no "}

// agentAnswerMark finds body lines that tell the agent to put the AI mark on
// its answers, which then open every chat paragraph with "· AI —".
func agentAnswerMark(b *spec.Bundle) []Issue {
	for i, line := range strings.Split(b.Agent.Body, "\n") {
		lower := strings.ToLower(line)
		if !strings.Contains(line, aiMark) || slices.ContainsFunc(negations, func(n string) bool { return strings.Contains(lower, n) }) {
			continue
		}
		return []Issue{finding(spec.AgentFile, "/body", "agent/answer-mark", fmt.Sprintf("agent.md body line %d tells the agent to mark answers %q", i+1, aiMark), fmt.Sprintf("delete that line from agent.md: the chat panel already shows answers as AI; %q goes only on labels of ai fields and actions in app files", aiMark))}
	}
	return nil
}
