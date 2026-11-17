package session

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/manual"
)

// libraryWorkstreamDoor is a workstream door on an organization's server that
// also keeps a Library.
type libraryWorkstreamDoor struct {
	fakeWorkstreamDoor
	asked []string
}

func (d *libraryWorkstreamDoor) Library(_ context.Context, query, page string, from int) (string, error) {
	d.asked = append(d.asked, query+"|"+page)
	return `{"pages":[]}`, nil
}

// THE ORGANIZATION'S LIBRARY RIDES BESIDE THE WORKSTREAM TOOL ONLY WHERE THE
// DOOR KEEPS ONE (tools_org_library.go), it only reads, and the manual names it.
func TestTheLibraryToolIsOnTheServerBeltOnlyWhereTheDoorKeepsALibrary(t *testing.T) {
	door := &libraryWorkstreamDoor{}
	agent := &Agent{config: Config{Workstream: door, Workspace: t.TempDir(), ProfileDir: t.TempDir(), Interactive: true}}
	agent.tools = agent.belt()
	var names []string
	for _, tool := range agent.offeredTools() {
		names = append(names, tool.Name)
	}
	if len(names) != 2 || names[0] != WorkstreamPagesTool || names[1] != OrgLibraryTool {
		t.Fatalf("the server agent's belt is %v", names)
	}
	for _, tool := range agent.tools {
		if tool.Name != OrgLibraryTool {
			continue
		}
		if _, failed, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"refunds"}`)); failed || err != nil {
			t.Fatalf("a search failed: %v", err)
		}
		// An argument the tool does not have is ignored, as every tool's is
		// (toolargs.go): asked to write, it can only read, so it lists titles.
		if _, failed, err := tool.Execute(context.Background(), json.RawMessage(`{"write":"x"}`)); failed || err != nil {
			t.Fatalf("an unknown argument broke the read: %v", err)
		}
	}
	if len(door.asked) != 2 || door.asked[0] != "refunds|" || door.asked[1] != "|" {
		t.Fatalf("the door was asked %v", door.asked)
	}
	if !manual.Chat().Mentions(OrgLibraryTool) {
		t.Errorf("no chat manual page mentions the %s tool", OrgLibraryTool)
	}
}
