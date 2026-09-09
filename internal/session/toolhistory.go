package session

import "github.com/Agent-Field/agentfield/sdk/go/ai"

// CRITICAL: PROVIDER CALL IDS ARE SCOPED TO THEIR ASSISTANT BATCH. Reusing an
// ID in a later round must never replace the earlier call's arguments, result,
// or failure evidence. The replay and restored-history tests pin this boundary.
//
// toolResultCalls pairs each result with its call in the preceding assistant
// batch. Providers may reuse an ID in another batch, so a conversation-wide
// ID map would attach old evidence to a newer command. Result positions belong
// to this snapshot; call pointers retain the existing shallow message identity.
func toolResultCalls(messages []ai.Message) map[int]*ai.ToolCall {
	out := make(map[int]*ai.ToolCall)
	pending := make(map[string]*ai.ToolCall)
	for index, message := range messages {
		if message.Role == "assistant" {
			clear(pending)
			for callIndex := range message.ToolCalls {
				call := &message.ToolCalls[callIndex]
				if call.ID != "" {
					pending[call.ID] = call
				}
			}
		}
		if message.Role == "tool" {
			if call := pending[message.ToolCallID]; call != nil {
				out[index] = call
				delete(pending, message.ToolCallID)
			}
		}
	}
	return out
}
