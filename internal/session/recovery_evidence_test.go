package session

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// CRITICAL: archived evidence is useful only while its retrieval tool remains
// available. A finishing worker still needs the original bytes to save an
// accurate deliverable; a pointer and permission to write cannot replace them.
func TestFinishingCanRetrieveArchivedEvidence(t *testing.T) {
	evidence := strings.Repeat("Original observation: preserve the measured value.\n", 60)
	agent, _ := newTestAgent(t, &refusingCompleter{t: t}, func(config *Config) {
		config.ContextWindow = 2_000_000
	})
	agent.mu.Lock()
	agent.messages = append(agent.messages, exchanges(6, map[int]string{1: evidence})...)
	agent.mu.Unlock()
	changed, err := agent.compact(context.Background(), nil)
	if !changed || err != nil {
		t.Fatalf("archive evidence: changed=%v err=%v", changed, err)
	}
	agent.mu.Lock()
	messages := append([]ai.Message(nil), agent.messages...)
	agent.mu.Unlock()
	stub := messageText(messages[3])
	if !strings.HasPrefix(stub, "[tool: read · ") {
		t.Fatalf("expected archived evidence, got %q", stub)
	}
	path := stubPathIn(t, stub)
	call := func(id string) ai.ToolCall {
		return withdrawnCall(id, "read", fmt.Sprintf(`{"path":%q}`, path))
	}
	before := agent.executeTool(context.Background(), nil, nil, call("before"), "")
	if before.isError || before.text != evidence {
		t.Fatalf("archive was not readable before finishing: %+v", before)
	}
	defer agent.withdrawTools(landingBelt, landingWithdrawal)()
	after := agent.executeTool(context.Background(), nil, nil, call("finishing"), "")
	if after.isError || after.text != evidence {
		t.Fatalf("finishing lost access to its archived evidence: %+v", after)
	}
}
