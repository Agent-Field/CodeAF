package main

// STAGE 3'S HALF: what a node MEANS when the thing behind it is a model.
//
// internal/subharness owns the shape of a run and hands each node to an Env
// (exec.go): Loop, Tool, Gate, Check, Cond. This file is one Env, over one
// OpenRouter model and three toy tools, and it exists so a designed harness can
// be RUN rather than only read — a topology nobody executed is a drawing.
//
// TODO-consolidate: internal/subharness/exec_model.go with a ModelExec is where
// this belongs the moment a second caller wants it. It is here, unexported, for
// exactly the reason a dev rig is a dev rig: the prompts below are being tuned
// by the run they are being judged by, and a package is the wrong place to tune
// anything.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/subharness"
)

// toolSpec is one tool this rig actually has: what it is called, what it is for
// in the words the designer and the worker are both shown, and what it does.
type toolSpec struct {
	name  string
	about string
	run   func(args string) (string, error)
}

// The whole tool belt. Three tools that cannot fail interestingly, on purpose:
// this rig is measuring architecture, and a flaky tool would be a second
// variable in every trace.
var availableTools = []toolSpec{
	{
		name:  "echo",
		about: "returns its argument verbatim. args: the text ({{input}} means the previous step's output).",
		run:   func(args string) (string, error) { return args, nil },
	},
	{
		name:  "date",
		about: "returns the current UTC time, RFC3339. args: none.",
		run:   func(string) (string, error) { return time.Now().UTC().Format(time.RFC3339), nil },
	},
	{
		name:  "upper",
		about: "upper-cases its argument. args: the text ({{input}} means the previous step's output).",
		run:   func(args string) (string, error) { return strings.ToUpper(args), nil },
	},
}

func lookupTool(name string) (toolSpec, bool) {
	for _, tool := range availableTools {
		if tool.name == name {
			return tool, true
		}
	}
	return toolSpec{}, false
}

func toolNames(tools []toolSpec) string {
	names := make([]string, len(tools))
	for at, tool := range tools {
		names[at] = tool.name
	}
	return strings.Join(names, ", ")
}

// execModel is the Env. It carries the run's GOAL because a node's brief is
// written against the goal and handed only the step before it — a worker told
// "name the strongest counterargument" with no idea what the argument was about
// would answer about nothing.
type execModel struct {
	chat *chatClient
	goal string
	// gate is what a person would have said, since nobody is here to say it. It
	// is a flag rather than a default so a declined run can be produced on
	// purpose — the status is not a failure and the trace should show that.
	gate string
	// maxTurns clamps what a page asked for. A page may declare up to
	// subharness.MaxTurns; this rig pays for them.
	maxTurns int
	log      func(format string, a ...any)
}

// Loop runs one agent.loop: a worker's turn with the node's slice of the
// whitelist in front of it, bounded by the node's own max_turns.
func (e *execModel) Loop(ctx context.Context, node subharness.Node, input string) (string, error) {
	brief := node.Fields.Get("brief")
	turns := node.Fields.Int("max_turns", 3)
	if turns < 1 {
		turns = 1
	}
	if turns > e.maxTurns {
		turns = e.maxTurns
	}

	var granted []toolSpec
	for _, name := range splitCommas(node.Fields.Get("tools")) {
		if tool, found := lookupTool(name); found {
			granted = append(granted, tool)
		}
	}

	system := strings.Join([]string{
		"You are ONE NODE inside a sub-harness that is working on a person's goal.",
		"",
		"THE RUN'S GOAL: " + e.goal,
		"",
		"YOUR BRIEF, and the whole of what you are responsible for:",
		brief,
		"",
		"Do your brief's job and nothing else — another node does the next part, and",
		"work you do out of turn is work that gets thrown away or contradicted.",
		"Answer with the WORK ITSELF: no preamble, no restating of the brief, no",
		"promises about what you are about to do. What you write becomes the input the",
		"next node reads, so write it for that reader.",
	}, "\n")

	history := []message{
		{Role: "system", Content: system},
		{Role: "user", Content: "What the previous step produced (your input):\n\n" + firstOr(input, "(nothing yet — you are the first step; work from the goal)")},
	}
	for turn := 0; turn < turns; turn++ {
		request := chatRequest{Messages: history, MaxTokens: 4000, Temperature: 0.4}
		if len(granted) > 0 && turn < turns-1 {
			// The LAST turn carries no tools, so a worker out of rounds answers
			// with words instead of asking for a round it cannot have.
			request.Tools = toolDefs(granted)
			request.ToolChoice = "auto"
		}
		out, err := e.chat.complete(ctx, request)
		if err != nil {
			return "", err
		}
		if len(out.ToolCalls) == 0 {
			return out.Text, nil
		}
		history = append(history, message{Role: "assistant", Content: out.Text, ToolCalls: out.ToolCalls})
		for _, call := range out.ToolCalls {
			result := e.dispatch(call)
			e.log("      · %s tool %s → %s", node.Id, call.Function.Name, clip(oneLine(result), 90))
			history = append(history, message{Role: "tool", ToolCallID: call.ID, Name: call.Function.Name, Content: result})
		}
	}
	// Out of turns: one more call with no tools, which is the same ending the
	// provider's own loop gives a worker that ran long.
	out, err := e.chat.complete(ctx, chatRequest{Messages: history, MaxTokens: 4000, Temperature: 0.4})
	if err != nil {
		return "", err
	}
	return out.Text, nil
}

// dispatch answers one tool call. A tool that does not exist or arguments that
// will not parse come back AS THE ANSWER rather than as an error: the worker is
// the one who can act on either, and killing the run over a mistyped tool name
// would tell us nothing about the architecture.
func (e *execModel) dispatch(call toolCall) string {
	tool, found := lookupTool(call.Function.Name)
	if !found {
		return fmt.Sprintf("error: %q is not a tool here (the tools are %s)", call.Function.Name, toolNames(availableTools))
	}
	var arguments struct {
		Text string `json:"text"`
	}
	if raw := strings.TrimSpace(call.Function.Arguments); raw != "" && raw != "{}" {
		if err := json.Unmarshal([]byte(raw), &arguments); err != nil {
			return fmt.Sprintf("error: your arguments did not parse as JSON: %v", err)
		}
	}
	out, err := tool.run(arguments.Text)
	if err != nil {
		return "error: " + err.Error()
	}
	return out
}

// Tool calls one tool with the node's fixed arguments.
func (e *execModel) Tool(ctx context.Context, node subharness.Node, input string) (string, error) {
	name := node.Fields.Get("tool")
	tool, found := lookupTool(name)
	if !found {
		return "", fmt.Errorf("%q is not a tool this rig has (%s)", name, toolNames(availableTools))
	}
	args := strings.ReplaceAll(node.Fields.Get("args"), "{{input}}", input)
	return tool.run(args)
}

// Gate answers for the person who is not here. It never blocks: an Env with
// nobody to ask must answer (exec.go), and the answer it gives is the flag's.
func (e *execModel) Gate(ctx context.Context, node subharness.Node, state subharness.State) (subharness.GateAnswer, error) {
	ask := node.Fields.Get("ask")
	e.log("      · gate asked: %s", oneLine(ask))
	switch e.gate {
	case "decline":
		return subharness.GateAnswer{Approved: false, Note: "the rig is headless and was told to decline"}, nil
	case "intervene":
		return subharness.GateAnswer{Intervene: true, Note: "the rig is headless and was told to intervene"}, nil
	default:
		return subharness.GateAnswer{Approved: true}, nil
	}
}

// Check runs one verify node. The bool is the verdict; the string is what the
// check said, which becomes the state the next condition reads — so a FAILING
// check says what is wrong, and a passing one says nothing, leaving the material
// it approved standing (exec.go).
func (e *execModel) Check(ctx context.Context, node subharness.Node, state subharness.State) (bool, string, error) {
	rung := node.Fields.Get("ladder")
	if rung == "" {
		rung = "the harness's own rung"
	}
	check := firstOr(node.Fields.Get("check"), "the material is a sound answer to the goal")
	system := strings.Join([]string{
		"You are a VERIFIER inside a sub-harness. You are at the " + rung + " rung of a",
		"verification ladder, and your judgement decides whether the run believes its own",
		"output.",
		"",
		"THE RUN'S GOAL: " + e.goal,
		"",
		"WHAT YOU ARE CHECKING: " + check,
		"",
		"Be a real check. Passing something that does not hold makes the whole ladder",
		"decorative; failing something for style makes the run loop forever.",
		"",
		"Answer in exactly this shape, and nothing else:",
		"PASS",
		"or",
		"FAIL — <what is wrong, in one sentence, specific enough to fix>",
	}, "\n")
	answer, err := e.chat.ask(ctx, system, "The material:\n\n"+firstOr(state.Last, "(nothing)"), 1500, 0.1)
	if err != nil {
		return false, "", err
	}
	verdict := strings.ToUpper(strings.TrimSpace(oneLine(answer)))
	passed := strings.HasPrefix(verdict, "PASS")
	if passed {
		// A silent pass on purpose: the material it approved stays the state.
		return true, "", nil
	}
	return false, strings.TrimSpace(answer), nil
}

// Cond judges a condition the small language refused — a sentence. It is asked
// only for those: everything `ValidCondition` accepts was already decided,
// deterministically, inside the package (exec.go's Runner.cond).
func (e *execModel) Cond(ctx context.Context, node subharness.Node, condition string, state subharness.State) (bool, error) {
	system := strings.Join([]string{
		"You are judging ONE CONDITION inside a sub-harness run. The condition was written",
		"as a sentence, which is why a person's judgement is wanted rather than a match.",
		"",
		"THE RUN'S GOAL: " + e.goal,
		"THE CONDITION: " + condition,
		"THE LAST STEP SUCCEEDED: " + fmt.Sprint(state.OK),
		"",
		"Answer with exactly one word: YES if the condition holds, NO if it does not.",
	}, "\n")
	answer, err := e.chat.ask(ctx, system, "What the last step produced:\n\n"+firstOr(state.Last, "(nothing)"), 1200, 0.0)
	if err != nil {
		return false, err
	}
	word := strings.ToUpper(strings.TrimSpace(oneLine(answer)))
	return strings.HasPrefix(word, "YES"), nil
}

// toolDefs is the belt as the wire declares it. Every tool takes one string,
// because every tool here does.
func toolDefs(tools []toolSpec) []toolDef {
	out := make([]toolDef, 0, len(tools))
	for _, tool := range tools {
		var def toolDef
		def.Type = "function"
		def.Function.Name = tool.name
		def.Function.Description = tool.about
		def.Function.Parameters = map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{"type": "string", "description": "the tool's argument"},
			},
			"required": []string{"text"},
		}
		out = append(out, def)
	}
	return out
}

func splitCommas(value string) []string {
	var out []string
	for _, word := range strings.Split(value, ",") {
		if word = strings.TrimSpace(word); word != "" {
			out = append(out, word)
		}
	}
	return out
}

func firstOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func oneLine(text string) string {
	text = strings.TrimSpace(text)
	if at := strings.IndexByte(text, '\n'); at >= 0 {
		return strings.TrimSpace(text[:at])
	}
	return text
}
