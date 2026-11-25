package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/jsrun"
)

// A SCRIPT IS ONE STEP TO EVERY COUNTER THE MODEL IS SHOWN. The hub's count of
// finished calls feeds the worker's "step N/M" frame, so a script that made two
// bash calls must leave it at one, the same unit the runner caps.
func TestScriptNestedCallsAreNotCountedAsFinishedCalls(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, scriptWorkerConfig(t))
	body, err := json.Marshal(map[string]string{"source": `await tools.bash({command: "true"}); await tools.bash({command: "true"}); return "ok"`})
	if err != nil {
		t.Fatal(err)
	}
	hub := newEventHub()
	agent.runTools(context.Background(), []ai.ToolCall{{
		ID: "call-1", Type: "function",
		Function: ai.ToolCallFunction{Name: ScriptToolName, Arguments: string(body)},
	}}, hub)
	if hub.finishedCalls != 1 {
		t.Errorf("finishedCalls = %d after one script that made two calls, want 1", hub.finishedCalls)
	}
}

// THE ROOM'S STEP COUNT AND ITS CALL IN FLIGHT ARE THE SCRIPT'S. A nested end
// neither counts a step nor clears the script from "what is it doing".
func TestTaskLiveCountsAScriptOnceAndKeepsItInFlight(t *testing.T) {
	live := &taskLive{}
	live.record(Event{Kind: EventToolBegin, Tool: ScriptToolName, CallID: "s"})
	for _, kind := range []EventKind{EventToolBegin, EventToolEnd, EventToolBegin, EventToolFailed} {
		live.record(Event{Kind: kind, Tool: "bash", CallID: "s-script-1", Parent: "s"})
	}
	if got := live.state(0); got.Steps != 0 || got.Call == "" {
		t.Errorf("mid-script the room reads steps=%d call=%q; want 0 and the script", got.Steps, got.Call)
	}
	live.record(Event{Kind: EventToolEnd, Tool: ScriptToolName, CallID: "s"})
	if got := live.state(0).Steps; got != 1 {
		t.Errorf("steps = %d after one script, want 1", got)
	}
}

// ONLY BASH ANSWERS A SHAPE. Every other tool answers text, and a text that
// happens to start with a bracket stays the text the tool wrote.
func TestScriptNonBashResultStaysText(t *testing.T) {
	for _, text := range []string{"[INFO] started", `{"a":1}`, "[1, 2]"} {
		got, err := scriptValue(toolResult{text: text})
		if err != nil || got != text {
			t.Errorf("scriptValue(%q) = %#v, %v; want the text unchanged", text, got, err)
		}
	}
	if _, err := scriptValue(toolResult{text: "no such file", isError: true}); err == nil || err.Error() != "no such file" {
		t.Errorf("a failed call answered %v, want its own text as the error", err)
	}
}

// THE DESCRIPTION NAMES EVERY TOOL A SCRIPT MAY CALL, read off the list.
func TestScriptDescriptionNamesEveryCallableTool(t *testing.T) {
	description := scriptDescription
	for _, name := range scriptCallable {
		if !strings.Contains(description, name) {
			t.Errorf("the description does not say a script can call %s", name)
		}
	}
}

// THE SANDBOX IS SAID FIRST, AND SHOWN. A worker that read "JavaScript" as Node
// wrote require('fs') and failed before its first call; the description now
// says what is missing before anything else, and carries a script that works.
func TestScriptDescriptionSaysItIsNotNodeAndShowsAWorkingScript(t *testing.T) {
	if at := strings.Index(scriptDescription, "Not Node.js: no require"); at < 0 || at > 120 {
		t.Errorf("the description does not open by saying it is not Node (at %d): %q", at, scriptDescription)
	}
	if !strings.Contains(scriptDescription, "await tools.bash({command:") {
		t.Error("the description shows no call through tools.bash")
	}
	if !strings.Contains(scriptWorkerRule, "not Node.js") {
		t.Error("the worker page does not say a script is not Node")
	}
}

// THE MANUAL STATES THE LIMITS THE CODE ENFORCES. A changed constant fails here
// until the page says the new number.
func TestScriptManualStatesTheEnforcedLimits(t *testing.T) {
	raw, err := os.ReadFile("../manual/chat/how-tasks-run.md")
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)
	facts := []string{
		fmt.Sprintf("%d tool calls", jsrun.DefaultScriptCalls),
		fmt.Sprintf("%d minutes", int(jsrun.DefaultScriptWall/time.Minute)),
		fmt.Sprintf("%d MiB of output", jsrun.DefaultScriptOutput>>20),
		fmt.Sprintf("up to %d MiB of one command's", scriptBashBytes>>20),
		fmt.Sprintf("Up to %d calls run at the same time", jsrun.DefaultScriptParallel),
	}
	for _, name := range scriptCallable {
		facts = append(facts, "`"+name+"`")
	}
	for _, want := range facts {
		if !strings.Contains(page, want) {
			t.Errorf("internal/manual/chat/how-tasks-run.md does not say %q", want)
		}
	}
}

// A TASK CHAT STARTS IS A TASK WORKER TOO. A node worker built on the bash belt
// with the switch on carries script, as a plan run's worker does; without the
// switch it does not.
func TestChatTaskWorkerOnTheBashBeltCarriesScriptWithTheSwitch(t *testing.T) {
	for _, on := range []bool{true, false} {
		t.Setenv(home.EnvVar, t.TempDir())
		t.Setenv("CODEAF_TASK_BELT", "bash")
		t.Setenv(scriptSwitch, map[bool]string{true: "on", false: ""}[on])
		agent := openIdentityAgent(t, identityPlace(t, "7777777777777777"), nil)
		graph := stubbedGraph(agent, func(*TaskNode) {})
		id := graph.reserve()
		graph.admit(id, taskSpec{title: "Fix", brief: "Fix the bug", acceptance: "fixed", model: "test/model", depth: 1})
		worker, err := agent.newTaskAgent(context.Background(), t.TempDir(), graph.node(id), "")
		if err != nil {
			t.Fatalf("worker construction: %v", err)
		}
		if !worker.config.mayBashBelt() {
			worker.Close()
			t.Fatal("the worker is not on the bash belt, so this proves nothing")
		}
		if got := beltNameSet(worker.beltTools())[ScriptToolName]; got != on {
			t.Errorf("switch on=%v: the chat task's worker carries script = %v", on, got)
		}
		worker.Close()
	}
}

// THE CHAT'S ARM IS ITS OWN SWITCH. A conversation built with it carries
// script; a task never does, whatever the field says. The chat's page adds no
// line for it: the tool's description says all of it, and a page line would be
// paid on every request of every conversation, armed or not.
func TestChatBeltCarriesScriptOnlyWithTheChatSwitch(t *testing.T) {
	on, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.chatScript = true })
	if !beltNameSet(on.beltTools())[ScriptToolName] {
		t.Error("a conversation with the chat switch does not carry script")
	}
	off, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if beltNameSet(off.beltTools())[ScriptToolName] {
		t.Error("a conversation without the chat switch carries script")
	}
	if (Config{chatScript: true, InTask: true}).mayChatScript() {
		t.Error("a task is allowed the chat's script")
	}
}

// THE SWITCH IS READ AT THE DOOR THAT BUILDS A CONVERSATION.
func TestChatScriptSwitchReadsOnlyOn(t *testing.T) {
	for value, want := range map[string]bool{"on": true, " ON ": true, "": false, "1": false, "off": false} {
		t.Setenv(chatScriptSwitch, value)
		if got := chatScriptAsked(); got != want {
			t.Errorf("CODEAF_CHAT_SCRIPT=%q armed = %v, want %v", value, got, want)
		}
	}
}
