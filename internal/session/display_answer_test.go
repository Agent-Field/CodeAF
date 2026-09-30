package session

import (
	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"testing"
)

func TestDisplayAnswerComesFromResponseBoundary(t *testing.T) {
	preamble := textMessage("assistant", "Checking next.")
	preamble.ToolCalls = []ai.ToolCall{{ID: "r", Type: "function"}}
	preamble.ToolCalls[0].Function.Name = "read"
	preamble.ToolCalls[0].Function.Arguments = `{"path":"a"}`
	update := textMessage("assistant", "[update] **First finding is ready.**")
	update.ToolCalls = preamble.ToolCalls
	es := shapeEntries([]ai.Message{textMessage("user", "Check both"), textMessage("assistant", "First is ready."), preamble, update, textMessage("assistant", "Both are ready.")}, nil)
	for _, e := range es {
		want := e.Text == "First is ready." || e.Text == "Both are ready." || e.Text == "**First finding is ready.**"
		if e.Answer != want {
			t.Fatalf("%s %q answer=%v, want %v", e.Role, e.Text, e.Answer, want)
		}
	}
}
