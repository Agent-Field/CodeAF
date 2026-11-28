package session

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// chatScriptConfig is a conversation with the chat's script switch on.
func chatScriptConfig(config *Config) { config.chatScript = true }

// THE CHAT'S SCRIPT SEES WHAT A WORKER'S SCRIPT SEES. The conversation's own
// bash keeps the last lines that fit the window; a script reading through it
// would filter a tail it was told was whole.
func TestChatScriptSeesBashOutputPastTheModelsCap(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, chatScriptConfig)
	if caps := agent.resultCaps(); caps.MaxLines >= 20000 && caps.MaxBytes >= 108894 {
		t.Skipf("the window caps (%d lines, %d bytes) do not bind on seq 1 20000", caps.MaxLines, caps.MaxBytes)
	}
	result := runScriptCall(t, agent, `
const run = await tools.bash({command: "seq 1 20000"});
const lines = run.output.split("\n").filter(Boolean);
return lines.length + " " + lines[0] + " " + run.truncated;
`)
	if !strings.Contains(result.text, "20000 1 false") {
		t.Errorf("the chat's script did not see the whole output:\n%s", result.text)
	}
}

// A SCRIPT IS TOLD WHEN ITS BASH OUTPUT WAS CUT, on either belt, and the cut
// keeps less than the command printed.
func TestScriptBashSaysWhenItsOutputWasCut(t *testing.T) {
	for name, configure := range map[string]func(*Config){"worker": scriptWorkerConfig(t), "chat": chatScriptConfig} {
		t.Run(name, func(t *testing.T) {
			agent, _ := newTestAgent(t, &scriptedCompleter{}, configure)
			result := runScriptCall(t, agent, `
const run = await tools.bash({command: "head -c 1500000 /dev/zero | tr '\\0' a"});
return run.truncated + " " + (run.output.length < 1500000);
`)
			if !strings.Contains(result.text, "true true") {
				t.Errorf("a cut output was not reported as cut:\n%s", result.text)
			}
		})
	}
}

// A SCRIPT THAT READS IS A STEP THAT LEARNED SOMETHING, and the same answer
// again is a spin even though the script's own clock moved between the two.
func TestAScriptThatReadsIsProgressAndTheSameAnswerAgainIsNot(t *testing.T) {
	seen := newProgressLedger()
	step := func(took string) bool {
		return taughtSomething(Event{
			Kind: EventToolEnd, Tool: ScriptToolName, Args: `{"source":"return 1"}`,
			Output: "script finished in " + took + " after 2 calls\nparse.go: func parseDate",
		}, seen)
	}
	if !step("12ms") {
		t.Fatal("a script's first answer read as no progress")
	}
	if step("15ms") {
		t.Fatal("the same answer from the same script read as new because its duration changed")
	}
}

// A REOPENED CONVERSATION KEEPS A SCRIPT'S CALLS UNDER IT, as the live window
// drew them: the journal holds them, because no message does.
func TestAReopenedConversationKeepsAScriptsCallsUnderIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-s", ScriptToolName, `{"source":"await tools.bash({command: \"echo one\"}); await tools.bash({command: \"exit 3\"}); return 'ok'"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		chatScriptConfig(config)
		config.SessionFile = path
	})
	collect(t, mustSubmit(t, agent, "run the script"))
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	for name, entries := range map[string][]DisplayEntry{
		"reopened": reopen(t, path).Transcript(),
		"detached": ReadTranscript(path).Entries,
	} {
		at := -1
		for i, e := range entries {
			if e.Role == "tool" && e.CallID == "call-s" {
				at = i
			}
		}
		if at < 0 || at+2 >= len(entries) {
			t.Fatalf("%s: the script row or its calls are missing: %+v", name, entries)
		}
		first, second := entries[at+1], entries[at+2]
		if !first.ToolResult.Nested() || !second.ToolResult.Nested() {
			t.Fatalf("%s: the rows after the script are not its nested calls: %+v %+v", name, first, second)
		}
		if first.Tool != "bash" || !strings.Contains(first.Args, "echo one") || !strings.Contains(first.Output, "one") {
			t.Errorf("%s: the first nested call reads %+v", name, first)
		}
		if !strings.Contains(second.Args, "exit 3") {
			t.Errorf("%s: the second nested call reads %+v", name, second)
		}
	}
}

// THE APP'S STEP LIST NAMES EACH SCRIPT CALL'S PARENT, so a surface can hang it
// under its script instead of drawing it as a step of its own.
func TestTaskLiveStepsNameTheScriptTheyCameFrom(t *testing.T) {
	live := &taskLive{}
	live.record(Event{Kind: EventToolBegin, Tool: ScriptToolName, CallID: "s"})
	live.record(Event{Kind: EventToolBegin, Tool: "bash", CallID: "s-script-1", Parent: "s"})
	live.record(Event{Kind: EventToolEnd, Tool: "bash", CallID: "s-script-1", Parent: "s"})
	live.record(Event{Kind: EventToolEnd, Tool: ScriptToolName, CallID: "s"})
	parents := map[string]string{}
	for _, step := range live.liveSteps(10) {
		parents[step.Call] = step.Parent
	}
	if parents["s"] != "" || parents["s-script-1"] != "s" {
		t.Errorf("step parents = %v; want the script's call under s and the script under nothing", parents)
	}
}
