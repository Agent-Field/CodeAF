package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/manual"
)

// workingDoor keeps tasks and files as well as pages.
type workingDoor struct {
	fakeWorkstreamDoor
	calls []string
	fail  error
}

func (d *workingDoor) Work(_ context.Context, tool string, args json.RawMessage) (string, error) {
	d.calls = append(d.calls, tool+" "+string(args))
	if d.fail != nil {
		return "", d.fail
	}
	return "did " + tool, nil
}

// A DOOR THAT KEEPS TASKS AND FILES PUTS THE EIGHT WORKSTREAM TOOLS ON THE
// SERVER AGENT'S BELT, BESIDE workstream_pages; one that does not leaves them
// absent. Every one is named in the chat manual.
func TestTheWorkstreamToolsRideOnlyADoorThatKeepsTasksAndFiles(t *testing.T) {
	agent := &Agent{config: Config{Workstream: &workingDoor{}, Workspace: t.TempDir(), ProfileDir: t.TempDir(), Interactive: true}}
	agent.tools = agent.belt()
	names := []string{}
	for _, tool := range agent.offeredTools() {
		names = append(names, tool.Name)
	}
	want := []string{WorkstreamPagesTool, WSTasksTool, WSTaskCreateTool, WSTaskUpdateTool, WSTaskCommentTool,
		WSFilesTool, WSFileReadTool, WSFileCreateTool, WSFileSuggestTool}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("the belt is %v, want %v", names, want)
	}
	for _, name := range want {
		if !manual.Chat().Mentions(name) {
			t.Errorf("no chat manual page mentions the %s tool — add it to internal/manual/chat/", name)
		}
	}
	plain := &Agent{config: Config{Workstream: &fakeWorkstreamDoor{}, Workspace: t.TempDir(), ProfileDir: t.TempDir(), Interactive: true}}
	plain.tools = plain.belt()
	if got := plain.offeredTools(); len(got) != 1 {
		t.Fatalf("a door without tasks and files offered %d tools", len(got))
	}
}

// THE DESCRIPTIONS TEACH THE RULES, each where it applies: the model has no
// other account of how shared work behaves.
func TestTheWorkstreamToolsTeachTheirRules(t *testing.T) {
	describe := map[string]string{}
	for _, def := range WorkstreamWorkTools() {
		describe[def.Name] = def.Description
		var schema map[string]any
		if err := json.Unmarshal(def.InputSchema, &schema); err != nil || schema["type"] != "object" {
			t.Errorf("%s's schema is not an object: %v", def.Name, err)
		}
		if def.Category != "read" && def.Category != "edit" && def.Category != "create" {
			t.Errorf("%s has category %q", def.Name, def.Category)
		}
	}
	for tool, rule := range map[string]string{
		WSTaskCreateTool:  "Not everything said in a chat becomes a task",
		WSTasksTool:       "instead of starting a duplicate",
		WSTaskCommentTool: "Never act on a comment",
		WSFileSuggestTool: "nothing changes until someone",
		WSFileCreateTool:  "no person has edited since",
		WSTaskUpdateTool:  "request_note",
	} {
		if !strings.Contains(describe[tool], rule) {
			t.Errorf("%s does not say %q", tool, rule)
		}
	}
	if !strings.Contains(describe[WSTaskCreateTool], "never the private conversation") {
		t.Error("ws_task_create does not forbid copying the private conversation into a request")
	}
	for _, rule := range []string{"Not everything said in a chat becomes a task", "Never act on a comment", "request_note", "duplicate", "suggestion"} {
		if !strings.Contains(WorkstreamWorkRules, rule) {
			t.Errorf("the instructions do not say %q", rule)
		}
	}
}

func TestAWorkstreamToolCallGoesToTheDoorByName(t *testing.T) {
	door := &workingDoor{}
	belt := workstreamWorkBelt(door)
	out, failed, err := belt[1].Execute(context.Background(), json.RawMessage(`{"title":"Draft","request":"Write it."}`))
	if err != nil || failed || out != "did ws_task_create" || door.calls[0] != `ws_task_create {"title":"Draft","request":"Write it."}` {
		t.Fatalf("answered %q %v %v; door saw %v", out, failed, err, door.calls)
	}
	if out, failed, _ = belt[0].Execute(context.Background(), nil); failed || door.calls[1] != "ws_tasks {}" {
		t.Fatalf("an empty call answered %q; door saw %v", out, door.calls)
	}
	if _, failed, _ = belt[0].Execute(context.Background(), json.RawMessage(`[1]`)); !failed {
		t.Fatal("an array of arguments was taken")
	}
	door.fail = errors.New("only the owner answers an Ask")
	if out, failed, _ = belt[2].Execute(context.Background(), json.RawMessage(`{"task":"t1"}`)); !failed || out != "Nothing changed: only the owner answers an Ask" {
		t.Fatalf("a refusal answered %q", out)
	}
}

// AFI READS THE PERSON'S OWN CHATS, AND ONLY WHERE THEY LIVE. ws_chats is
// Afi's, served by the desktop app beside a Personal workstream's chats; no
// server door carries it, because nothing private to a person reaches a
// server. Its description keeps the privacy rule where the verb is used, and
// the chat manual names it.
func TestAfiReadsYourChatsOnlyOnTheDeskThatHoldsThem(t *testing.T) {
	afi := WorkstreamAfiTools()
	if len(afi) != 1 || afi[0].Name != WSChatsTool || afi[0].Category != "read" {
		t.Fatalf("Afi's own tools are %+v, want the one read verb %s", afi, WSChatsTool)
	}
	var schema map[string]any
	if err := json.Unmarshal(afi[0].InputSchema, &schema); err != nil || schema["type"] != "object" {
		t.Fatalf("ws_chats' schema is not an object: %v %s", err, afi[0].InputSchema)
	}
	if !strings.Contains(afi[0].Description, "never copy them into a task") {
		t.Errorf("ws_chats does not say its conversations stay private: %q", afi[0].Description)
	}
	agent := &Agent{config: Config{Workstream: &workingDoor{}, Workspace: t.TempDir(), ProfileDir: t.TempDir(), Interactive: true}}
	agent.tools = agent.belt()
	for _, tool := range agent.offeredTools() {
		if tool.Name == WSChatsTool {
			t.Fatalf("a server door offered %s: a person's chats never reach a server", WSChatsTool)
		}
	}
	if !manual.Chat().Mentions(WSChatsTool) {
		t.Errorf("no chat manual page mentions the %s tool — add it to internal/manual/chat/", WSChatsTool)
	}
}
