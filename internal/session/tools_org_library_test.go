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
		if _, failed, _ := tool.Execute(context.Background(), json.RawMessage(`{"write":"x"}`)); !failed {
			t.Fatal("the Library tool took an argument it does not have")
		}
	}
	if len(door.asked) != 1 || door.asked[0] != "refunds|" {
		t.Fatalf("the door was asked %v", door.asked)
	}
	if !manual.Chat().Mentions(OrgLibraryTool) {
		t.Errorf("no chat manual page mentions the %s tool", OrgLibraryTool)
	}
}
