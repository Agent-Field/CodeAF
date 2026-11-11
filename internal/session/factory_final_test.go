package session

import (
	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"testing"
)

func TestFinalStageReportNeedsNoAdditionalModelResponse(t *testing.T) {
	a := stageAgent(t, &fakeStageDoor{})
	for _, tt := range []struct {
		args       string
		bad, final bool
	}{
		{`{"done":true}`, false, true}, {`{"done":true,"final":false}`, false, false}, {`{"done":false}`, false, false}, {`{"done":false,"final":true}`, false, true}, {`{"done":true}`, true, false},
	} {
		calls := []ai.ToolCall{{Function: ai.ToolCallFunction{Name: stageResultToolName, Arguments: tt.args}}}
		if got := a.stageBatchFinal(calls, []toolResult{{isError: tt.bad}}); got != tt.final {
			t.Fatalf("%s bad=%v got %v", tt.args, tt.bad, got)
		}
	}
}
