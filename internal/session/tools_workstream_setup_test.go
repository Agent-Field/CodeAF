package session

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/manual"
)

// setupDoor is a door that keeps tasks, files and setup cards.
type setupDoor struct{ workingDoor }

func (*setupDoor) KeepsSetupCards() bool { return true }

// AFI PROPOSES A NEW WORKSTREAM'S SETUP ONLY THROUGH A DOOR THAT KEEPS THE CARD
// (owner decision 2026-11-17 §9). ws_propose_setup rides the belt beside the
// work tools when the door says it keeps cards, and is absent otherwise; the
// manual names it; its description and the rule Afi is told say the numbers
// the owner set (5 to 8 deliverables, at most two questions) from the
// constants, and that nothing is written until Accept.
func TestAfiProposesASetupOnlyThroughADoorThatKeepsTheCard(t *testing.T) {
	door := &setupDoor{}
	agent := &Agent{config: Config{Workstream: door, Workspace: t.TempDir(), ProfileDir: t.TempDir(), Interactive: true}}
	agent.tools = agent.belt()
	var found bool
	for _, tool := range agent.offeredTools() {
		found = found || tool.Name == WSProposeSetupTool
	}
	if !found {
		t.Fatalf("a door that keeps setup cards offered no %s", WSProposeSetupTool)
	}
	out, failed, err := agent.tools[len(agent.tools)-1].Execute(context.Background(), json.RawMessage(`{"name":"x","goal":"y","tasks":[{"title":"a"}]}`))
	if err != nil || failed || out != "did "+WSProposeSetupTool {
		t.Fatalf("the call answered %q %v %v", out, failed, err)
	}

	work := &Agent{config: Config{Workstream: &workingDoor{}, Workspace: t.TempDir(), ProfileDir: t.TempDir(), Interactive: true}}
	work.tools = work.belt()
	for _, tool := range work.offeredTools() {
		if tool.Name == WSProposeSetupTool {
			t.Fatalf("a door that keeps no setup card offered %s", WSProposeSetupTool)
		}
	}
	if !manual.Chat().Mentions(WSProposeSetupTool) {
		t.Errorf("no chat manual page mentions the %s tool — add it to internal/manual/chat/", WSProposeSetupTool)
	}

	def := WorkstreamSetupTools()[0]
	var schema struct {
		Properties map[string]struct {
			MaxItems int `json:"maxItems"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(def.InputSchema, &schema); err != nil {
		t.Fatalf("the schema: %v", err)
	}
	if got := schema.Properties["tasks"].MaxItems; got != WorkstreamSetupMostTasks {
		t.Errorf("the schema allows %d tasks, the rule says %d", got, WorkstreamSetupMostTasks)
	}
	for _, want := range []string{"Nothing is written until the person presses Accept", "deliverables"} {
		if !strings.Contains(def.Description, want) {
			t.Errorf("%s does not say %q", WSProposeSetupTool, want)
		}
	}
	for _, want := range []string{"5 to " + strconv.Itoa(WorkstreamSetupMostTasks), "at most " + []string{"zero", "one", "two", "three"}[WorkstreamSetupMostQuestions] + " short questions", "never make its tasks or files yourself"} {
		if !strings.Contains(WorkstreamSetupRule, want) {
			t.Errorf("Afi's setup rule does not say %q", want)
		}
	}
}
