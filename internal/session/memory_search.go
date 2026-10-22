package session

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

// memorySearchTool retrieves archived-from-context lines without adding a model
// call to prompt assembly. The owner filter is the same as the conversation's.
func (a *Agent) memorySearchTool() bare.Tool {
	return bare.Tool{
		Name:        "search_memories",
		Description: "Search saved memories outside the prompt by topic.",
		Schema:      json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"],"additionalProperties":false}`),
		Execute: func(_ context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Query string `json:"query"`
			}
			if err := decodeToolArguments(args, &parsed); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}
			if strings.TrimSpace(parsed.Query) == "" {
				return "Name the topic to find in memory.", true, nil
			}
			found, err := a.memory.store.SearchMemoriesFor(a.memoryOwners(), parsed.Query, 20)
			if err != nil {
				return "Could not search memory: " + err.Error(), true, nil
			}
			if len(found) == 0 {
				return "No saved memories match that topic.", false, nil
			}
			var out strings.Builder
			for index, memory := range found {
				if index > 0 {
					out.WriteByte('\n')
				}
				out.WriteString(strconv.Itoa(index+1) + ". " + memory.Text + " · " + memoryReach(memory.Owner, false))
				if err := a.memory.store.RecordMemoryEvent(memory.ID, "retrieved", a.memorySourceSession()); err != nil {
					a.journalMemoryFailure("retrieved", err)
				}
			}
			return out.String(), false, nil
		},
	}
}
