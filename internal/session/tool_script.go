package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/jsrun"
)

// THE SCRIPT TOOL: a JavaScript step that calls the belt's other tools and hands
// back only what it prints or returns, on a task worker's belt or a
// conversation's, each behind its own switch
// (internal/manual/chat/how-tasks-run.md states its rules and limits).
//
// EVERY NESTED CALL GOES THROUGH [Agent.executeTool], the one chokepoint every
// call in this program passes — the job budget, pre-action, the approval
// policy, the seat's record — exactly as a saved program's tool() does
// (subharness_env.go). A gate written here would be a second gate, so this file
// decides only what a script may NAME, and the session decides everything else.

// scriptToolName is the tool's one name, read by the belt, the tests and the
// off-limits list.
const scriptToolName = "script"

// scriptOffLimits are the tools a script may not call. A script inside a script
// would be an interpreter nobody bounded. The rest ask the person something or
// start work that outlives the step — a card, a question, a task, a chat, a
// clock — and only the model, in its own turn, should do that: on a chat's belt
// a script that could hand work out would blur the road the conversation chose
// with the step that chose it. The tool's description is written from this
// list ([scriptDescription]).
var scriptOffLimits = []string{
	scriptToolName, "propose_subharness", "ask", RequestSecretToolName,
	"propose_task", quickTaskToolName, "divide_work", teamStartToolName,
	"start_chat", "automation", "factory_add",
}

// scriptSpillName is the file a script's bash files its whole output in when
// the script-wide cut fires. It is not the step's own name (bashSpillName): the
// run's recorder takes the newest action file as the step's whole output, and a
// script's call is not the step.
const scriptSpillName = "script-call-%06d.txt"

// scriptBashBytes is how much of one bash call's output a script sees. It is
// wider than the model's window on purpose: the point of a script is to filter
// what the model would never read whole, and what the script returns is cut to
// the window afterwards.
const scriptBashBytes = 1 << 20

// scriptSwitch is the variable that arms the tool. It is plumbing rather than a
// settings row for CODEAF_TASK_BELT's reason (internal/config's settings.go):
// an experiment's switch must be as easy to stop using as it was to start.
const scriptSwitch = "CODEAF_TASK_SCRIPT"

// chatScriptSwitch arms the tool on a conversation's own belt, measured apart
// from the worker's switch so each arm changes one thing.
const chatScriptSwitch = "CODEAF_CHAT_SCRIPT"

// scriptAsked and chatScriptAsked read the two switches. Only "on" arms one
// while the tool is measured; anything else, unset included, leaves it off.
func scriptAsked() bool     { return switchedOn(scriptSwitch) }
func chatScriptAsked() bool { return switchedOn(chatScriptSwitch) }

func switchedOn(name string) bool {
	return strings.EqualFold(strings.TrimSpace(env.Get(name)), "on")
}

// scriptDescription says in its second sentence what the sandbox is NOT. A
// pilot's worker read "JavaScript" as Node, wrote require('fs') and failed
// before its first call, because the one sentence saying otherwise came last;
// a worked example closes it.
var scriptDescription = "Run JavaScript that calls your other tools; only what it prints or returns comes back. " +
	"Not Node.js: no require, import, fs, process, network or clock. Files and commands go through await tools.bash({command}). " +
	"Use it for several calls in one step, or to filter large output down to what matters. " +
	"The source is an async function body, so top-level await and return work. " +
	"tools.<name>(args) takes the arguments you would pass directly; write a character an identifier cannot hold as _. " +
	"bash resolves to {output, exit_code, truncated, full_output_path} whatever the exit code, and rejects if it timed out or was stopped; " +
	"other tools resolve to their text. A refusal or error rejects with the tool's own text. " +
	"text(value) and console.log add output. Not callable from a script: " + strings.Join(scriptOffLimits, ", ") + ". " +
	"Example: for (const f of (await tools.bash({command: \"rg -l parse_date src\"})).output.trim().split(\"\\n\")) " +
	"text(f + \": \" + (await tools.bash({command: \"rg -n 'def parse_date' \" + f})).output.trim());"

const scriptSchema = `{"type":"object","properties":{"source":{"type":"string","description":"JavaScript: the body of an async function."}},"required":["source"],"additionalProperties":false}`

// scriptWorkerRule is the worker page's one paragraph about the tool. It gives
// bash and script one job each, so the belt does not teach two ways to say one
// thing (bashbelt.go's concern about a belt that carried both).
const scriptWorkerRule = "## Combining tool calls\n\n" +
	"Use `script` to combine several tool calls in one step or to filter a large\n" +
	"result before you read it. A script is not Node.js: it reaches files and\n" +
	"commands only through `await tools.bash({command})`."

// scriptTool is the tool as the belt carries it.
func (a *Agent) scriptTool() bare.Tool {
	return bare.Tool{
		Name:        scriptToolName,
		Description: scriptDescription,
		Schema:      json.RawMessage(scriptSchema),
		Execute:     a.runScript,
	}
}

// runScript runs one script on the turn that called it.
//
// THE TURN RIDES IN ON THE CONTEXT: the episode and the hub the batch planted
// ([Agent.runToolsWarm]) are what a nested call needs to be the same call it
// would have been at the top level, and the script call's own id names its
// nested calls, so a reader can find the parent of each.
func (a *Agent) runScript(ctx context.Context, args json.RawMessage) (string, bool, error) {
	var in struct {
		Source string `json:"source"`
	}
	if err := decodeToolArguments(args, &in); err != nil || strings.TrimSpace(in.Source) == "" {
		return "script needs source: the JavaScript to run", true, nil
	}
	ep, hub, parent := episodeFrom(ctx), eventHubFrom(ctx), callIDFrom(ctx)
	var made atomic.Int64
	caller := func(ctx context.Context, name string, args map[string]any) (any, error) {
		id := fmt.Sprintf("%s-script-%d", parent, made.Add(1))
		return a.scriptCall(ctx, ep, hub, parent, id, name, args)
	}
	started := time.Now()
	result := jsrun.RunScript(ctx, in.Source, a.scriptNames(), caller, jsrun.ScriptLimits{})
	text, isError := scriptReport(result, time.Since(started))
	return a.cutBashResult(text), isError, nil
}

// scriptNames is every belt tool, the off-limits ones included: a script that
// reaches for script is told why it cannot, in a sentence, rather than meeting
// a bare "is not a function".
func (a *Agent) scriptNames() []string {
	belt := a.beltTools()
	names := make([]string, 0, len(belt))
	for _, tool := range belt {
		names = append(names, tool.Name)
	}
	return names
}

// scriptCall runs one nested call and answers what the script receives. A
// name the belt does not carry is answered by dispatch, as it is at the top
// level; the names a script can reach are the belt's own ([Agent.scriptNames]).
func (a *Agent) scriptCall(ctx context.Context, ep *episode, hub *eventHub, parent, id, name string, args map[string]any) (any, error) {
	if slices.Contains(scriptOffLimits, name) {
		return nil, fmt.Errorf("%s cannot be called from a script", name)
	}
	body, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("the arguments for %s cannot be written down: %w", name, err)
	}
	call := ai.ToolCall{ID: id, Type: "function", Function: ai.ToolCallFunction{Name: name, Arguments: string(body)}}
	var probe *scriptBash
	if name == "bash" {
		ctx, probe = withScriptBash(ctx)
	}
	rendered := argsText(call)
	if hub != nil {
		begin := a.toolBeginEvent(call, rendered, "")
		begin.Parent = parent
		hub.send(begin)
	}
	result := a.executeTool(ctx, ep, hub, call, rendered)
	if hub != nil {
		// THE END CARRIES ITS PARENT, so a reader that counts the model's steps
		// leaves it out and a reader that draws them hangs it under its script.
		// It goes out on the hub directly and never as a belt step: the step
		// the run worker acknowledges is the script's own.
		end := a.clientToolEvent(toolEndEvent(call, rendered, result))
		end.Parent = parent
		hub.send(end)
	}
	if probe != nil {
		return probe.answer(result)
	}
	return scriptValue(result)
}

// scriptValue is what the script receives from any tool but bash: the tool's
// text, or its text as the error.
//
// TEXT IS NOT GUESSED INTO A SHAPE. A log line that starts with "[" or a JSON
// file read for its words would come back as an array or object the script did
// not ask for, and its .split would throw; the script parses what it knows is
// JSON itself.
func scriptValue(result toolResult) (any, error) {
	if result.isError {
		return nil, errors.New(result.text)
	}
	return result.text, nil
}

// scriptReport is the script tool's result: one line saying how it ended, the
// output, and the reason when it did not finish.
func scriptReport(result jsrun.ScriptResult, took time.Duration) (string, bool) {
	ending, closing := "finished", ""
	switch {
	case result.Failed != "":
		ending, closing = "failed", "script error: "+result.Failed
	case result.Stopped != "":
		ending, closing = "stopped", "script stopped: "+result.Stopped
	}
	// ONE BUILDER, because the output can be megabytes and is cut to the window
	// right after this: it is copied once here and not once per join.
	var text strings.Builder
	text.Grow(len(result.Output) + len(closing) + 64)
	fmt.Fprintf(&text, "script %s in %s after %d call%s", ending, took.Round(time.Millisecond), result.Calls, plural(result.Calls))
	for _, part := range []string{result.Output, closing} {
		if part != "" {
			text.WriteString("\n")
			text.WriteString(part)
		}
	}
	return text.String(), closing != ""
}

// scriptBash is what one bash call inside a script learns about its own run:
// the exit status bare saw, and whether the script-wide cut fired. It is locked
// because a call moved to the background finishes on another goroutine after
// the script has read it.
type scriptBash struct {
	mu        sync.Mutex
	exit      *int
	truncated bool
	path      string
}

type scriptBashKey struct{}

// withScriptBash marks one nested bash call: bare reports its exit status as
// data ([bare.WithBashExit]) and [Agent.truncatingBash] cuts it at
// [scriptBashBytes] rather than at the window.
func withScriptBash(ctx context.Context) (context.Context, *scriptBash) {
	probe := &scriptBash{}
	ctx = bare.WithBashExit(ctx, func(code int) {
		probe.mu.Lock()
		defer probe.mu.Unlock()
		probe.exit = &code
	})
	return context.WithValue(ctx, scriptBashKey{}, probe), probe
}

func scriptBashFrom(ctx context.Context) *scriptBash {
	probe, _ := ctx.Value(scriptBashKey{}).(*scriptBash)
	return probe
}

// cut records the script-wide cut's outcome.
func (b *scriptBash) cut(truncated bool, path string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.truncated, b.path = truncated, path
}

// answer is what the script receives for its bash call. A command that ran
// resolves whatever its exit; one that never ran or never finished rejects with
// bash's own words; one moved to the background resolves with no exit code.
func (b *scriptBash) answer(result toolResult) (any, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.exit == nil && result.isError {
		return nil, errors.New(result.text)
	}
	output, exit := result.text, any(nil)
	if b.exit != nil {
		output, exit = withoutExitStatus(result.text, *b.exit), *b.exit
	}
	answer := map[string]any{"output": output, "exit_code": exit, "truncated": b.truncated}
	if b.path != "" {
		answer["full_output_path"] = b.path
	}
	return answer, nil
}

// withoutExitStatus removes the status line bare appends to a non-zero exit, so
// the output is the command's own. The code is known, so the line is matched
// exactly, in bare's own spelling ([bare.ExitStatusLine]), and never parsed.
func withoutExitStatus(text string, code int) string {
	if code == 0 {
		return text
	}
	// THE LAST STATUS LINE COMES OFF WHEREVER IT SITS: a line the session adds
	// after the command's — the fence's (procexec.WithFenceLine) — stays.
	status := bare.ExitStatusLine(code)
	at := strings.LastIndex(text, status)
	if at < 0 {
		return text
	}
	return strings.TrimSuffix(text[:at], "\n\n") + text[at+len(status):]
}
