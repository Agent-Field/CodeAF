package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/manual"
)

type orgSetupDoorStub struct {
	got json.RawMessage
	err error
}

func (d *orgSetupDoorStub) ProposeOrgSetup(_ context.Context, args json.RawMessage) (string, error) {
	d.got = args
	return "Shown as a card.", d.err
}

// THE SET-UP CONVERSATION CARRIES ITS ONE VERB AND NO OTHER: no shell, no
// file, no workstream tool, and the manual names the verb.
func TestTheOrgSetupConversationCarriesOnlyItsVerb(t *testing.T) {
	door := &orgSetupDoorStub{}
	agent := &Agent{config: Config{Workspace: t.TempDir(), ProfileDir: t.TempDir(), OrgSetup: door}}
	agent.tools = agent.belt()
	tools := agent.offeredTools()
	if len(tools) != 1 || tools[0].Name != OrgProposeSetupTool {
		var names []string
		for _, tool := range tools {
			names = append(names, tool.Name)
		}
		t.Fatalf("the set-up belt is %v; want only %s", names, OrgProposeSetupTool)
	}
	if !manual.Chat().Mentions(OrgProposeSetupTool) {
		t.Errorf("no chat manual page mentions the %s tool — add it to internal/manual/chat/", OrgProposeSetupTool)
	}
	if !strings.Contains(OrgSetupRule, OrgProposeSetupTool) {
		t.Errorf("Afi's set-up rule does not name its verb")
	}

	tool := agent.orgSetupTool()
	out, failed, err := tool.Execute(context.Background(), json.RawMessage(`{"workstreams":[{"name":"Company"}]}`))
	if err != nil || failed || out != "Shown as a card." || !strings.Contains(string(door.got), "Company") {
		t.Fatalf("propose: %q failed=%v err=%v got=%s", out, failed, err, door.got)
	}
	door.err = errors.New("two workstreams are called Company")
	out, failed, _ = tool.Execute(context.Background(), json.RawMessage(`{"workstreams":[]}`))
	if !failed || out != "Nothing changed: two workstreams are called Company" {
		t.Fatalf("a refused card: %q failed=%v", out, failed)
	}
}

// A CONVERSATION WITH NO SET-UP DOOR HAS NO SET-UP VERB.
func TestNoSetupDoorNoSetupVerb(t *testing.T) {
	agent := &Agent{config: Config{Workspace: t.TempDir(), ProfileDir: t.TempDir()}}
	agent.tools = agent.belt()
	for _, tool := range agent.offeredTools() {
		if tool.Name == OrgProposeSetupTool {
			t.Fatal("a plain conversation carries org_propose_setup")
		}
	}
}
