package session

// A NO TO A TASK IS NOT AN INVITATION TO DO IT HERE. The model may explain or
// ask what should happen next, but the same request cannot pay for a different
// road to the work the person just declined. This is an authority check before
// approval, not advice a model can choose to ignore.

import (
	"encoding/json"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const declinedProposalRefusal = "the person declined a task proposal, so no work was started; do not change files or start replacement work until a new message from the person; explain that and ask or offer in words"

// taskWorkDeclined survives automatic wakes because only a new human message
// can authorize work again. Its caller never keeps the lock while running a
// tool or waiting for an answer.
func (a *Agent) taskWorkDeclined() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.proposalDeclined
}

// declinedProposalCall leaves only observation and conversation available.
// SHELL TEXT CANNOT PROVE AN ABSENCE OF WRITES, so every bash call is held, as
// are unknown tools: a new writing hand must not silently bypass the person's
// no. Tools with both reading and acting verbs are checked by their arguments.
func (a *Agent) declinedProposalCall(call ai.ToolCall) (toolResult, bool) {
	if !a.taskWorkDeclined() || proposalObservation(call) {
		return toolResult{}, false
	}
	return toolResult{text: declinedProposalRefusal, isError: true}, true
}

func proposalObservation(call ai.ToolCall) bool {
	switch call.Function.Name {
	case "read", "ls", "find", "grep", "read_document", "manual", "view_image",
		"web_search", "web_fetch", "search_conversations", "settings", "services",
		"list_harnesses", "list_subharnesses", "use_skill", loadCapabilityToolName, "ask":
		return true
	case "tasks":
		var args tasksArguments
		return json.Unmarshal([]byte(call.Function.Arguments), &args) == nil &&
			!args.Stop && !args.Forward && !args.Continue && strings.TrimSpace(args.Resolve) == "" &&
			strings.TrimSpace(args.Say) == "" && strings.TrimSpace(args.Note) == ""
	case "jobs":
		args, err := parseJobsArguments(json.RawMessage(call.Function.Arguments))
		return err == nil && (args.Action == "list" || args.Action == "output")
	}
	return false
}
