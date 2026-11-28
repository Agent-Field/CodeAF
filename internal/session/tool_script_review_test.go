package session

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/exec"
	"github.com/Agent-Field/codeaf/internal/jsrun"
)

// These tests pin the fixes from the review of #2815. Each fails without its fix.

// A SAVED PROGRAM'S SCRIPT MEETS THE SAME GATE. The program road hands the
// episode and hub to [Agent.executeTool] as parameters rather than on the
// context, and a script reached that way once ran its nested calls with no
// approval rules at all.
func TestASavedProgramsScriptMeetsTheSameGate(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.chatScript = true })
	agent.SetApprovalPolicy(&approval.Policy{Default: approval.ActionAllow, Tools: map[string]approval.Action{"bash": approval.ActionDeny}})
	env := &subharnessEnv{
		agent: agent, manifest: exec.Manifest{Whitelist: []string{ScriptToolName}},
		ep: agent.newEpisode(), hub: newEventHub(),
	}
	result, err := env.Tool(context.Background(), ScriptToolName, map[string]any{"source": `
try { await tools.bash({command: "touch ran"}) } catch (e) { return "refused: " + e.message }
return "it ran";
`})
	if err != nil {
		t.Fatalf("the program's script call failed: %v", err)
	}
	if !strings.Contains(result.Text, "refused: ") {
		t.Errorf("a denied nested call through a saved program did not reject:\n%s", result.Text)
	}
	if _, err := os.Stat(filepath.Join(workspace, "ran")); err == nil {
		t.Error("the denied command ran")
	}
}

// A SCRIPT WITH NO TURN TO CHECK ITS CALLS AGAINST DOES NOT RUN. A nil episode
// skips the whole pre-action chain, so a road that reaches the script without
// one must fail closed rather than run every call unchecked.
func TestAScriptOutsideATurnRefusesToRun(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, scriptWorkerConfig(t))
	call := ai.ToolCall{ID: "call-1", Type: "function", Function: ai.ToolCallFunction{
		Name: ScriptToolName, Arguments: `{"source":"await tools.bash({command: \"touch ran\"}); return \"it ran\""}`,
	}}
	result := agent.executeTool(context.Background(), nil, nil, call, argsText(call))
	if !result.isError || strings.Contains(result.text, "it ran") {
		t.Errorf("a script with no turn ran:\n%s", result.text)
	}
	if _, err := os.Stat(filepath.Join(workspace, "ran")); err == nil {
		t.Error("a script with no turn ran its command")
	}
}

// A QUICK WORKER DOES NOT CARRY SCRIPT. It runs every call under the node's
// quick lock, and a script's nested call would wait on that lock forever.
func TestAQuickWorkerDoesNotCarryScript(t *testing.T) {
	worker := scriptWorkerConfig(t)
	quick, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		worker(config)
		config.quickItems = func(int, []string) string { return "" }
	})
	if beltNameSet(quick.beltTools())[ScriptToolName] {
		t.Error("a quick worker carries script")
	}
	if strings.Contains(bashWorkerDoctrine(quick.config), scriptWorkerRule) {
		t.Error("a quick worker's page names script")
	}
}

// A SCRIPT CALLS ONLY WHAT IS ALLOWED, AND EVERYTHING ELSE ON THE BELT IS
// REFUSED. Every tool the chat's belt carries outside [scriptCallable] is
// tried, so a tool added to the belt later is refused until it is allowed on
// purpose.
func TestChatScriptReachesOnlyTheToolsItIsAllowed(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.chatScript = true })
	plain := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	var refused []string
	for name := range beltNameSet(agent.beltTools()) {
		if !slices.Contains(scriptCallable, name) && plain.MatchString(name) {
			refused = append(refused, name)
		}
	}
	slices.Sort(refused)
	for _, want := range []string{"propose_task", "watch", "edit", ScriptToolName} {
		if !slices.Contains(refused, want) {
			t.Fatalf("the chat's belt lacks %s; the test would prove nothing about it", want)
		}
	}
	names, _ := json.Marshal(refused)
	result := runScriptCall(t, agent, `
const out = [];
for (const name of `+string(names)+`) {
  try { await tools[name]({}); out.push(name + ": reached") } catch (e) { out.push(name + ": " + e.message) }
}
return out.join("\n");
`)
	for _, name := range refused {
		if !strings.Contains(result.text, name+": "+name+" cannot be called from a script") {
			t.Errorf("a chat script's call to %s was not refused", name)
		}
	}
	if strings.Contains(result.text, ": reached") {
		t.Errorf("a script reached a tool it is not allowed:\n%s", result.text)
	}
}

// A SCRIPT'S READ IS THE FILE, NEVER A POINTER. The held-read shortcut answers
// a read whose bytes the transcript already holds with a one-line pointer; a
// script cannot see the transcript, so it would parse the pointer as the file.
func TestAScriptsReadOfAHeldFileReturnsTheFile(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	path := filepath.Join(workspace, "page.md")
	writeFile(t, path, "one\ntwo\nthree\n")
	if first := readHeld(t, agent, "c1", heldArgs(path, 0, 0)); !strings.Contains(first.text, "three") {
		t.Fatalf("the direct read did not run: %.80q", first.text)
	}
	call := ai.ToolCall{ID: "s-script-1", Function: ai.ToolCallFunction{Name: "read", Arguments: heldArgs(path, 0, 0)}}
	nested := agent.executeTool(withScriptParent(context.Background(), "s"), agent.newEpisode(), nil, call, "")
	if strings.HasPrefix(nested.text, "[already read] ") || !strings.Contains(nested.text, "three") {
		t.Errorf("a script's read was answered with a pointer: %.80q", nested.text)
	}
}

// AND WHAT A SCRIPT READ IS NOT HELD. Its bytes never reach the transcript, so
// a later direct read of the same file must read the file.
func TestAScriptsReadDoesNotMakeALaterReadAPointer(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	path := filepath.Join(workspace, "page.md")
	writeFile(t, path, "one\ntwo\nthree\n")
	call := ai.ToolCall{ID: "s-script-1", Function: ai.ToolCallFunction{Name: "read", Arguments: heldArgs(path, 0, 0)}}
	agent.executeTool(withScriptParent(context.Background(), "s"), agent.newEpisode(), nil, call, "")
	if direct := readHeld(t, agent, "c1", heldArgs(path, 0, 0)); strings.HasPrefix(direct.text, "[already read] ") {
		t.Errorf("a direct read pointed at bytes only a script saw: %.80q", direct.text)
	}
}

// A SCRIPT'S RESULT CARRIES NO JOB FOOTER, and does not use up the footer the
// model is owed: the next result the model reads still carries it.
func TestAScriptsCallLeavesTheJobFooterForTheModel(t *testing.T) {
	agent, _ := jobsAgent(t)
	id := startJob(t, agent, "sleep 5")
	defer agent.jobs.kill(context.Background(), id)
	ep := agent.newEpisode()
	call := ai.ToolCall{ID: "s-script-1", Function: ai.ToolCallFunction{Name: "bash", Arguments: "{}"}}
	if nested := agent.finishToolResult(withScriptParent(context.Background(), "s"), ep, call, toolResult{text: "{}"}); nested.text != "{}" {
		t.Errorf("a script's result carried the job footer:\n%q", nested.text)
	}
	direct := agent.finishToolResult(context.Background(), ep, ai.ToolCall{ID: "c1", Function: call.Function}, toolResult{text: "one"})
	if !strings.Contains(direct.text, "running ") {
		t.Errorf("the model's next result lost the job footer:\n%q", direct.text)
	}
}

// THE REPORT SAYS WHAT A FINISHED SCRIPT DID NOT WAIT FOR.
func TestScriptReportNamesTheCallsItDidNotWaitFor(t *testing.T) {
	text, isError := scriptReport(jsrun.ScriptResult{Output: "started", Calls: 1, Unawaited: 1}, 0)
	if isError || !strings.Contains(text, "did not wait for 1 call;") {
		t.Errorf("the report does not name the unawaited call:\n%s", text)
	}
}

// A SCRIPT CALLED WITH NO ID STILL NAMES A PARENT. A provider that sends an
// empty call id once left every nested call looking like the model's own, and
// two such scripts minted the same ids.
func TestAScriptWithNoCallIDStillNamesItsCallsParent(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, scriptWorkerConfig(t))
	hub := newEventHub()
	events := hub.subscribe()
	go func() {
		body, _ := json.Marshal(map[string]string{"source": `await tools.bash({command: "echo one"})`})
		for range 2 {
			agent.runTools(context.Background(), []ai.ToolCall{{
				Type: "function", Function: ai.ToolCallFunction{Name: ScriptToolName, Arguments: string(body)},
			}}, hub)
		}
		hub.close()
	}()
	parents, ids := map[string]bool{}, map[string]bool{}
	for _, ev := range collect(t, events) {
		if ev.Kind != EventToolBegin || ev.Tool != "bash" {
			continue
		}
		if ev.Parent == "" {
			t.Errorf("a nested call of a script with no id names no parent (id %q)", ev.CallID)
		}
		parents[ev.Parent], ids[ev.CallID] = true, true
	}
	if len(parents) != 2 || len(ids) != 2 {
		t.Errorf("two scripts gave %d parents and %d call ids; want 2 and 2", len(parents), len(ids))
	}
}

// EVERY EVENT A NESTED CALL RAISES NAMES ITS SCRIPT, the live output and the
// finish as well as the begin and the end.
func TestEveryEventOfANestedCallNamesItsScript(t *testing.T) {
	worker := scriptWorkerConfig(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		worker(config)
		config.LiveToolOutput = true
	})
	hub := newEventHub()
	events := hub.subscribe()
	go func() {
		body, _ := json.Marshal(map[string]string{"source": `await tools.bash({command: "echo one"})`})
		agent.runTools(context.Background(), []ai.ToolCall{{
			ID: "call-1", Type: "function", Function: ai.ToolCallFunction{Name: ScriptToolName, Arguments: string(body)},
		}}, hub)
		hub.close()
	}()
	seen := map[EventKind]bool{}
	for _, ev := range collect(t, events) {
		if !strings.HasPrefix(ev.CallID, "call-1-script-") {
			continue
		}
		seen[ev.Kind] = true
		if ev.Parent != "call-1" {
			t.Errorf("a nested call's %v event names parent %q", ev.Kind, ev.Parent)
		}
	}
	for _, kind := range []EventKind{EventToolOutput, EventToolFinished} {
		if !seen[kind] {
			t.Errorf("the nested call raised no %v event; the test proves nothing about it", kind)
		}
	}
}

// A FAILED CHECK INSIDE A SCRIPT IS STILL A CHECK THE NODE RAN.
func TestAFailedCheckInsideAScriptIsACheckTheNodeRan(t *testing.T) {
	run := &childRun{log: io.Discard}
	run.observe(Event{Kind: EventToolFailed, Tool: "bash", CallID: "s-script-1", Parent: "s"})
	if !run.ranCheck {
		t.Error("a script's failing test run did not count as a check the node ran")
	}
}

// A NESTED CALL THAT NEEDS APPROVAL ASKS THE PERSON, AND THE ANSWER DECIDES
// IT: the script waits for the answer, runs the call on yes and is refused on no.
func TestAChatScriptsCallThatNeedsApprovalWaitsForTheAnswer(t *testing.T) {
	for _, allow := range []bool{true, false} {
		source := `try { await tools.bash({command: "touch ran"}); return "it ran" } catch (e) { return "refused: " + e.message }`
		body, err := json.Marshal(map[string]string{"source": source})
		if err != nil {
			t.Fatal(err)
		}
		completer := &scriptedCompleter{steps: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponse("call-s", ScriptToolName, string(body)), nil
			},
			func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
		}}
		agent, workspace := newTestAgent(t, completer, func(config *Config) {
			config.chatScript = true
			config.AskConsent = true
			config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow, Tools: map[string]approval.Action{"bash": approval.ActionPrompt}}
		})
		events := drainAnswering(t, mustSubmit(t, agent, "run it"), func(request Event) {
			agent.ResolveConsent(request.ID, allow)
		})
		if asked := countKind(events, EventConsentRequest); asked != 1 {
			t.Fatalf("allow=%v: the nested call asked %d times, want once; events %v", allow, asked, kinds(events))
		}
		_, statErr := os.Stat(filepath.Join(workspace, "ran"))
		if allow && statErr != nil {
			t.Error("an approved nested call did not run")
		}
		if !allow && statErr == nil {
			t.Error("a declined nested call ran")
		}
	}
}
