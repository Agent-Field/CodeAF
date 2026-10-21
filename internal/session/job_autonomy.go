package session

import (
	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
	"strings"
)

// Only named internal controls and reads inherit Auto. Shell commands,
// arbitrary tools, external account settings and explicit denials retain
// their existing permission policy and critical-action floors.
func automaticJobPolicy(base *approval.Policy) *approval.Policy {
	if base == nil || base.Default == approval.ActionDeny {
		return base
	}
	p := *base
	p.Tools = make(map[string]approval.Action, len(base.Tools))
	for tool, action := range base.Tools {
		p.Tools[tool] = action
	}
	for _, tool := range []string{"factory_start", "factory_answer", "factory_run", "team_read", "team_status", "manual", "read", "ls", "grep", "find"} {
		if p.Tools[tool] != approval.ActionDeny {
			p.Tools[tool] = approval.ActionAllow
		}
	}
	return &p
}

const jobAutoInstruction = "Factory job Auto mode is enabled. Decide routine engineering and implementation choices within the issue. Do not ask for routine plan, continuation or publication approvals: the factory records those decisions within its budget and retry limits. Ask only when a critical unresolved decision, missing access or credentials, spending authority, destructive external action or conflicting explicit instructions requires the person. Explicit permission denials and critical-action floors still apply. Never merge. Claim progress or publication only after the engine confirms it."

// This request's live mode is transient prompt context, not another chat line
// or a permanent change to history. Switching Manual removes it next request.
func (a *Agent) jobAutonomyMessages(messages []ai.Message) []ai.Message {
	source, ok := a.jobBudgetOf().(interface{ FactoryAuto() bool })
	if !ok || !source.FactoryAuto() || len(messages) == 0 || messages[0].Role != "system" {
		return messages
	}
	system := messageContentText(messages[0])
	if strings.Contains(system, jobAutoInstruction) {
		return messages
	}
	copied := append([]ai.Message(nil), messages...)
	copied[0].Content = textMessage("system", system+"\n\n"+jobAutoInstruction).Content
	return copied
}
