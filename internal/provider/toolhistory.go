package provider

import (
	"fmt"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const toolHistoryArgumentLimit = 512

// readableToolHistory carries completed calls as ordinary conversation text
// when an endpoint cannot accept the tool protocol. It leaves every original
// content part in place and only clips long arguments, which are a description
// of an earlier call rather than the result of that call.
func readableToolHistory(messages []ai.Message) []ai.Message {
	var rewritten []ai.Message
	names := make(map[string]string)
	for index, message := range messages {
		if len(message.ToolCalls) == 0 && message.Role != "tool" {
			continue
		}
		if rewritten == nil {
			rewritten = append([]ai.Message(nil), messages...)
		}
		message.Content = append([]ai.ContentPart(nil), message.Content...)
		if message.Role == "tool" {
			name := names[message.ToolCallID]
			if name == "" {
				name = "earlier call"
			}
			message.Role = "user"
			message.Content = append([]ai.ContentPart{{Type: "text", Text: fmt.Sprintf("Result from %s (%s):\n", name, message.ToolCallID)}}, message.Content...)
			message.ToolCallID = ""
		} else {
			var lines strings.Builder
			for _, call := range message.ToolCalls {
				names[call.ID] = call.Function.Name
				arguments := call.Function.Arguments
				if len(arguments) > toolHistoryArgumentLimit {
					runes := []rune(arguments)
					if len(runes) > toolHistoryArgumentLimit {
						arguments = string(runes[:toolHistoryArgumentLimit]) + "…"
					}
				}
				fmt.Fprintf(&lines, "\nCalled %s (%s) with %s", call.Function.Name, call.ID, arguments)
			}
			message.Content = append(message.Content, ai.ContentPart{Type: "text", Text: lines.String()})
			message.ToolCalls = nil
		}
		rewritten[index] = message
	}
	if rewritten == nil {
		return messages
	}
	return rewritten
}
