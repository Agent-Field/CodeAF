package session

import (
	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
	"strings"
)

// Only named internal controls and reads inherit Auto. Shell commands,
// arbitrary tools, external account settings and explicit denials retain
// their existing permission policy and critical-action floors.
//
// `factory_act` and `factory_task` are on the list because Auto is the
// person's standing yes to the job's own public acts and tasks, and the
// factory, not this policy, holds what each may do (merging stays the
// person's: internal/factory's manager_authority.go).
func automaticJobPolicy(base *approval.Policy) *approval.Policy {
	if base == nil || base.Default == approval.ActionDeny {
		return base
	}
	p := *base
	p.Tools = make(map[string]approval.Action, len(base.Tools))
	for tool, action := range base.Tools {
		p.Tools[tool] = action
	}
	for _, tool := range []string{"factory_start", "factory_answer", "factory_status", "factory_run", "factory_act", "factory_task", "team_read", "team_status", "manual", "read", "ls", "grep", "find"} {
		if p.Tools[tool] != approval.ActionDeny {
			p.Tools[tool] = approval.ActionAllow
		}
	}
	return &p
}

const jobAutoInstruction = "Factory job Auto mode is enabled. Decide routine engineering and implementation choices within the issue. Do not ask for routine plan, continuation or publication approvals: the factory records those decisions within its budget and retry limits. Ask only when a critical unresolved decision, missing access or credentials, spending authority, destructive external action or conflicting explicit instructions requires the person. Explicit permission denials and critical-action floors still apply. You are the first to hear of every stop: settle what you can (another round, a rerun, a task for a delegate with factory_task, accepting a check's finding with a reason) and hand the rest to the person with one plain sentence. Public acts on the pull request (factory_act: a push, an edit, a reply, marking it ready) are carried out in Auto and logged. Never merge on your own judgment; when the person asks to merge, show them the Merge card again (factory_answer action=offer_merge) and press Merge only when they tell you to in this conversation. Claim progress or publication only after the engine confirms it."

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
