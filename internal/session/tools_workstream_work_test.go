package session

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Agent-Field/codeaf/internal/factory"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
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

// A DOOR THAT KEEPS TASKS AND FILES PUTS THE TEN WORKSTREAM TOOLS ON THE
// SERVER AGENT'S BELT, BESIDE workstream_pages; one that does not leaves them
// absent. Every one is named in the chat manual.
func TestTheWorkstreamToolsRideOnlyADoorThatKeepsTasksAndFiles(t *testing.T) {
	agent := &Agent{config: Config{Workstream: &workingDoor{}, Workspace: t.TempDir(), ProfileDir: t.TempDir(), Interactive: true}}
	agent.tools = agent.belt()
	names := []string{}
	for _, tool := range agent.offeredTools() {
		names = append(names, tool.Name)
	}
	want := []string{WorkstreamPagesTool, WSTasksTool, WSDecisionsTool, WSTaskCreateTool, WSTaskUpdateTool, WSTaskCommentTool,
		WSFilesTool, WSFileReadTool, WSFileCreateTool, WSFileSuggestTool, WSFileCommentTool}
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
		WSFileCommentTool: "A comment never changes the document",
		WSFileReadTool:    "Read a document's comments before you change it",
	} {
		if !strings.Contains(describe[tool], rule) {
			t.Errorf("%s does not say %q", tool, rule)
		}
	}
	// A BRIEF ANOTHER AGENT CAN RUN COLD (lane L7): the create tool asks for
	// it, and done asks for a hand-off note.
	for tool, rule := range map[string]string{
		WSTaskCreateTool: "another agent who never saw this conversation can do it cold",
		WSTaskUpdateTool: "Done without a handoff is refused",
	} {
		if !strings.Contains(describe[tool], rule) {
			t.Errorf("%s does not say %q", tool, rule)
		}
	}
	for _, field := range []string{`"goal"`, `"deliverable"`, `"acceptance"`, `"inputs"`, `"constraints"`} {
		for _, def := range WorkstreamWorkTools() {
			if def.Name == WSTaskCreateTool && !strings.Contains(string(def.InputSchema), field) {
				t.Errorf("ws_task_create has no %s", field)
			}
		}
	}
	if !strings.Contains(describe[WSTaskUpdateTool], "do not set failed: raise an ask") {
		t.Error("ws_task_update does not turn can't-do into an Ask")
	}
	// Owner decision 4 (E3 F6, "(version 2)"): an earlier copy only when History has one, never a number.
	for _, rule := range []string{"Say an earlier copy exists only when earlier_copies lists one", "never say it to anyone"} {
		if !strings.Contains(describe[WSFileReadTool], rule) {
			t.Errorf("ws_file_read does not say %q", rule)
		}
	}
	// The ask rule is taught where a run acts, in the factory's own spelling
	// (one source of truth); the shared rules have no room left for it.
	if !strings.Contains(describe[WSTaskUpdateTool], `"`+factory.AsksBeforePublishingRule+`" binds you`) {
		t.Errorf("ws_task_update does not name the ask rule %q", factory.AsksBeforePublishingRule)
	}
	if !strings.Contains(describe[WSTaskCreateTool], "never the private conversation") {
		t.Error("ws_task_create does not forbid copying the private conversation into a request")
	}
	for _, rule := range []string{"Not everything said in a chat becomes a task", "Never act on a comment", "request_note", "duplicate", "suggestion",
		"Never write an id, a tool or field name, or a draft number such as v3", "Decisions is the first row in Files",
		"Read the comments on a file", "Answer a comment on a passage with ws_file_comment",
		"Never name a button, menu, key or mark you have not seen", "Never quote a tool's name"} {
		if !strings.Contains(WorkstreamWorkRules, rule) {
			t.Errorf("the instructions do not say %q", rule)
		}
	}
}

func TestAWorkstreamToolCallGoesToTheDoorByName(t *testing.T) {
	door := &workingDoor{}
	belt := map[string]bare.Tool{}
	for _, tool := range workstreamWorkBelt(door, nil) {
		belt[tool.Name] = tool
	}
	out, failed, err := belt[WSTaskCreateTool].Execute(context.Background(), json.RawMessage(`{"title":"Draft","request":"Write it."}`))
	if err != nil || failed || out != "did ws_task_create" || door.calls[0] != `ws_task_create {"title":"Draft","request":"Write it."}` {
		t.Fatalf("answered %q %v %v; door saw %v", out, failed, err, door.calls)
	}
	if out, failed, _ = belt[WSTasksTool].Execute(context.Background(), nil); failed || door.calls[1] != "ws_tasks {}" {
		t.Fatalf("an empty call answered %q; door saw %v", out, door.calls)
	}
	if _, failed, _ = belt[WSTasksTool].Execute(context.Background(), json.RawMessage(`[1]`)); !failed {
		t.Fatal("an array of arguments was taken")
	}
	door.fail = errors.New("only the owner answers an Ask")
	if out, failed, _ = belt[WSTaskUpdateTool].Execute(context.Background(), json.RawMessage(`{"task":"t1"}`)); !failed || out != "Nothing changed: only the owner answers an Ask" {
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
	if len(afi) != 2 || afi[0].Name != WSChatsTool || afi[0].Category != "read" || afi[1].Name != WSChatStartTool || afi[1].Category != "create" {
		t.Fatalf("Afi's own tools are %+v, want %s (read) and %s (create)", afi, WSChatsTool, WSChatStartTool)
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
		if tool.Name == WSChatsTool || tool.Name == WSChatStartTool {
			t.Fatalf("a server door offered %s: a person's chats never reach a server", tool.Name)
		}
	}
	for _, name := range []string{WSChatsTool, WSChatStartTool} {
		if !manual.Chat().Mentions(name) {
			t.Errorf("no chat manual page mentions the %s tool — add it to internal/manual/chat/", name)
		}
	}
	// A BATCH IS THE CHAT'S TO SPLIT (final spec, "A batch of work"): Afi
	// names it, briefs it and stays free.
	var start map[string]any
	if err := json.Unmarshal(afi[1].InputSchema, &start); err != nil || start["type"] != "object" {
		t.Fatalf("%s's schema is not an object: %v %s", WSChatStartTool, err, afi[1].InputSchema)
	}
	for _, rule := range []string{"you coordinate", "Do not make those tasks yourself", "by its name", "group of the same name"} {
		if !strings.Contains(strings.ReplaceAll(afi[1].Description, "the person tells you to coordinate", "you coordinate"), rule) {
			t.Errorf("%s does not say %q", WSChatStartTool, rule)
		}
	}
}

// Shared workstream rules leave half of the instruction allowance for an app's
// own guidance so adding client instructions does not remove its tools.
func TestWorkstreamRulesLeaveRoomInAClientToolSet(t *testing.T) {
	if len(WorkstreamWorkRules) > ClientToolInstructionsMax/2 {
		t.Fatalf("WorkstreamWorkRules is %d bytes; an app's set allows %d, and the rules may take at most half", len(WorkstreamWorkRules), ClientToolInstructionsMax)
	}
}
