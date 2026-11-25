package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
	procexec "github.com/Agent-Field/codeaf/internal/executor"
)

// scriptWorkerConfig is a bash-belt worker with the script switch on.
func scriptWorkerConfig(t *testing.T) func(*Config) {
	t.Helper()
	worker := bashBeltWorkerConfig(t)
	return func(config *Config) {
		worker(config)
		config.script = true
	}
}

// runScriptCall sends one script call through the real batch and answers its result.
func runScriptCall(t *testing.T, agent *Agent, source string) toolResult {
	t.Helper()
	body, err := json.Marshal(map[string]string{"source": source})
	if err != nil {
		t.Fatal(err)
	}
	results := agent.runTools(context.Background(), []ai.ToolCall{{
		ID: "call-1", Type: "function",
		Function: ai.ToolCallFunction{Name: ScriptToolName, Arguments: string(body)},
	}}, newEventHub())
	if len(results) != 1 {
		t.Fatalf("the batch answered %d results for one call", len(results))
	}
	return results[0]
}

func TestScriptToolIsOnTheWorkerBeltOnlyWithTheSwitch(t *testing.T) {
	on, _ := newTestAgent(t, &scriptedCompleter{}, scriptWorkerConfig(t))
	if !beltNameSet(on.beltTools())[ScriptToolName] {
		t.Error("a bash-belt worker with the switch on does not carry script")
	}
	off, _ := newTestAgent(t, &scriptedCompleter{}, bashBeltWorkerConfig(t))
	if beltNameSet(off.beltTools())[ScriptToolName] {
		t.Error("a bash-belt worker with the switch off carries script")
	}
	conversation, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.script = true })
	if beltNameSet(conversation.beltTools())[ScriptToolName] {
		t.Error("a conversation carries script; the switch is for task workers only")
	}
}

// THE STATUS LINE COMES OFF WHEREVER THE FENCE PUT ITS OWN AFTER IT: the exit
// code is the script's as data, and the output is the command's own.
func TestScriptBashOutputLosesTheStatusLineBeforeAFenceLine(t *testing.T) {
	text := procexec.WithFenceLine("found nothing\n\n" + bare.ExitStatusLine(2))
	got := withoutExitStatus(text, 2)
	if strings.Contains(got, "Command exited") || !strings.Contains(got, "found nothing") {
		t.Errorf("withoutExitStatus(%q) = %q", text, got)
	}
}

func TestScriptWorkerPageNamesTheToolOnlyWhenTheBeltHasIt(t *testing.T) {
	on, _ := newTestAgent(t, &scriptedCompleter{}, scriptWorkerConfig(t))
	if !strings.Contains(bashWorkerDoctrine(on.config), scriptWorkerRule) {
		t.Error("a worker carrying script reads a page that does not say when to use it")
	}
	off, _ := newTestAgent(t, &scriptedCompleter{}, bashBeltWorkerConfig(t))
	if strings.Contains(bashWorkerDoctrine(off.config), "`script`") {
		t.Error("a worker without script reads a page that names it")
	}
}

func TestScriptBashResolvesStructuredWhateverTheExit(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, scriptWorkerConfig(t))
	result := runScriptCall(t, agent, `
const run = await tools.bash({command: "printf 'hi\\n'; exit 3"});
return {output: run.output, exit: run.exit_code, truncated: run.truncated};
`)
	if result.isError {
		t.Fatalf("a non-zero exit failed the script:\n%s", result.text)
	}
	if !strings.Contains(result.text, `{"exit":3,"output":"hi\n","truncated":false}`) {
		t.Errorf("the script did not see bash's structure with the status line removed:\n%s", result.text)
	}
}

func TestScriptSeesBashOutputPastTheModelsCap(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, scriptWorkerConfig(t))
	if caps := agent.resultCaps(); caps.MaxBytes >= 108894 {
		t.Skipf("the window cap (%d bytes) does not bind on seq 1 20000", caps.MaxBytes)
	}
	result := runScriptCall(t, agent, `
const run = await tools.bash({command: "seq 1 20000"});
return run.output.split("\n").filter(Boolean).length + " " + run.truncated;
`)
	if !strings.Contains(result.text, "20000 false") {
		t.Errorf("the script did not see the whole output:\n%s", result.text)
	}
}

// TestScriptBashCallsRunAtTheSameTime proves concurrency through the real gate
// without a clock: each command succeeds only if it sees the other's file while
// it is still running, so calls made one after another both exit 1.
func TestScriptBashCallsRunAtTheSameTime(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, scriptWorkerConfig(t))
	wait := func(mine, theirs string) string {
		return "touch " + mine + "; for i in $(seq 200); do [ -f " + theirs + " ] && exit 0; sleep 0.05; done; exit 1"
	}
	result := runScriptCall(t, agent, `
const runs = await Promise.all([tools.bash({command: `+strconv.Quote(wait("a", "b"))+`}), tools.bash({command: `+strconv.Quote(wait("b", "a"))+`})]);
return runs.map(r => r.exit_code).join(",");
`)
	if !strings.Contains(result.text, "\n0,0") {
		t.Errorf("the two calls did not run at the same time:\n%s", result.text)
	}
}

// A SCRIPT'S CALLS ARE ANNOUNCED AS ITS OWN. Each nested call begins and ends
// on the turn's hub naming the script call as its parent, so a reader that
// counts the model's steps can leave it out and a page can hang it under its
// script (internal/run's bashworker.go reads exactly this).
func TestScriptNestedCallsAreAnnouncedWithTheirParent(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, scriptWorkerConfig(t))
	hub := newEventHub()
	events := hub.subscribe()
	go func() {
		body, _ := json.Marshal(map[string]string{"source": `await tools.bash({command: "echo one"}); await tools.bash({command: "exit 2"})`})
		agent.runTools(context.Background(), []ai.ToolCall{{
			ID: "call-1", Type: "function",
			Function: ai.ToolCallFunction{Name: ScriptToolName, Arguments: string(body)},
		}}, hub)
		hub.close()
	}()
	var begins, ends, fails int
	for _, ev := range collect(t, events) {
		if ev.Parent == "" {
			continue
		}
		if ev.Parent != "call-1" || !strings.HasPrefix(ev.CallID, "call-1-script-") {
			t.Errorf("a nested event names parent %q and id %q", ev.Parent, ev.CallID)
		}
		switch ev.Kind {
		case EventToolBegin:
			begins++
		case EventToolEnd:
			ends++
		case EventToolFailed:
			fails++
		}
	}
	if begins != 2 || ends != 1 || fails != 1 {
		t.Errorf("nested events: %d begins, %d ends, %d failures; want 2, 1, 1", begins, ends, fails)
	}
}

func TestScriptNestedCallMeetsTheSameGate(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, scriptWorkerConfig(t))
	agent.SetApprovalPolicy(&approval.Policy{Default: approval.ActionAllow, Tools: map[string]approval.Action{"bash": approval.ActionDeny}})
	result := runScriptCall(t, agent, `
try { await tools.bash({command: "touch ran"}) } catch (e) { return "refused: " + e.message }
return "it ran";
`)
	if !strings.Contains(result.text, "refused: ") {
		t.Errorf("a denied nested call did not reject:\n%s", result.text)
	}
	if _, err := os.Stat(filepath.Join(workspace, "ran")); err == nil {
		t.Error("the denied command ran")
	}
}

func TestScriptCannotCallScript(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, scriptWorkerConfig(t))
	result := runScriptCall(t, agent, `
try { await tools.script({source: "return 1"}) } catch (e) { return e.message }
`)
	if !strings.Contains(result.text, "script cannot be called from a script") {
		t.Errorf("a script reached script:\n%s", result.text)
	}
}

func TestScriptOutputIsCutAtTheWindowCap(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, scriptWorkerConfig(t))
	limit := agent.resultCaps().MaxBytes
	result := runScriptCall(t, agent, `return "x".repeat(`+strconv.Itoa(limit*2)+`)`)
	if len(result.text) > limit+512 {
		t.Errorf("the script's output reached the model at %d bytes past a %d-byte cap", len(result.text), limit)
	}
	if !strings.Contains(result.text, "[output truncated") {
		t.Errorf("a cut output does not say it was cut:\n%.300s", result.text)
	}
}

func TestScriptFailureReportsTheError(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, scriptWorkerConfig(t))
	result := runScriptCall(t, agent, `text("partial"); throw new Error("boom")`)
	if !result.isError {
		t.Error("an uncaught error did not fail the call")
	}
	for _, want := range []string{"script failed", "partial", "script error: Error: boom"} {
		if !strings.Contains(result.text, want) {
			t.Errorf("the result is missing %q:\n%s", want, result.text)
		}
	}
}
