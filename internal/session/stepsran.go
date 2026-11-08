package session

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ranStep is one tool call a model's own program already ran — Claude Code's
// Bash or Edit — as internal/provider's StreamStepsRan hands it up, already
// spelled in codeaf's tool names (internal/claudemodel's steps.go).
type ranStep struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	Output    string          `json:"output"`
	IsError   bool            `json:"is_error"`
	TookMS    int64           `json:"took_ms"`
}

// recordStepsRan writes down steps the model's own program ran, exactly as a
// call codeaf ran is written down: the call, its result, how long it took, and
// a row on the page. It is [Agent.runUserBash]'s recording half without the
// running, because the work is already done.
//
// NOTHING HERE RUNS ANYTHING. A step is a fact about the folder, not an
// instruction, so it never reaches the executor, the approval chain or the
// hooks: Claude Code asked its own permissions before it ran it.
//
// A TURN THAT WAS ABANDONED WRITES NOTHING MORE, for the reason the batch
// boundary in loop.go gives: a newer turn may already own the history.
func (a *Agent) recordStepsRan(ctx context.Context, hub *eventHub, raw string) {
	var steps []ranStep
	if json.Unmarshal([]byte(raw), &steps) != nil {
		return
	}
	for _, step := range steps {
		if abandoned(ctx) {
			return
		}
		if step.ID == "" || step.Name == "" {
			continue
		}
		args := string(step.Arguments)
		if args == "" {
			args = "{}"
		}
		call := ai.ToolCall{ID: step.ID, Type: "function", Function: ai.ToolCallFunction{Name: step.Name, Arguments: args}}
		a.record(ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{call}})
		event := Event{Kind: EventToolBegin, Tool: step.Name, Hint: gloss(call), Args: argsText(call), CallID: call.ID}
		hub.send(event)
		result := textMessage("tool", step.Output)
		result.ToolCallID = call.ID
		a.record(result)
		took := time.Duration(step.TookMS) * time.Millisecond
		a.file.appendTook(call.ID, took)
		hub.send(Event{Kind: EventToolFinished, Tool: step.Name, CallID: call.ID, Args: event.Args, Took: took})
		event.Kind, event.Hint, event.Output = EventToolEnd, "", displayToolOutput(call.ID, step.Output)
		if step.IsError {
			event.Kind = EventToolFailed
		}
		hub.send(event)
	}
}
