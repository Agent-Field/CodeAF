package session

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"strings"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

// These refusals are shared with the composer so a late engine refusal gives
// the same instruction as the surface's check before spending the draft.
const (
	BashEmptyWord = "type a command after !"
	BashBusyWord  = "wait for this turn to finish or stop it before running a ! command"
)

// BashCommand recognizes the person's shell prefix, including an empty command
// so the composer can retain it rather than send it to the model.
func BashCommand(text string) (string, bool) {
	text = strings.TrimSpace(text)
	command, ok := strings.CutPrefix(text, "!")
	return strings.TrimSpace(command), ok
}

// IsUserBashCall identifies the durable call IDs minted for commands the person
// ran. Live and reopened views use the same mark to show their output immediately.
func IsUserBashCall(id string) bool { return strings.HasPrefix(id, "user_bash_") }

// runUserBash keeps the ordinary turn's ownership, cancellation and journal, but
// asks no model to act on the command or interpret its result. The user message
// followed by a call/result pair also gives the next model turn valid context.
func (a *Agent) runUserBash(ctx context.Context, hub *eventHub, command string) bool {
	started := time.Now()
	args, _ := json.Marshal(struct {
		Command string `json:"command"`
	}{command})
	call := ai.ToolCall{ID: "user_bash_" + rand.Text(), Type: "function",
		Function: ai.ToolCallFunction{Name: "bash", Arguments: string(args)}}
	a.record(ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{call}})
	event := Event{Kind: EventToolBegin, Tool: "bash", Hint: gloss(call), Args: argsText(call), CallID: call.ID}
	hub.send(event)
	// This is an explicitly requested command, not delegated work. The bare
	// runner supplies the same detached process, deadline and output bounds as
	// the agent's shell without a model choosing tools or promoting a job.
	var output string
	var failed bool
	for _, tool := range bare.AllToolsCapped(a.config.Workspace, a.resultCaps()) {
		if tool.Name == "bash" {
			var err error
			if ctx.Err() != nil {
				output, failed = "Command cancelled", true
			} else {
				output, failed, err = tool.Execute(ctx, args)
			}
			if err != nil {
				output, failed = err.Error(), true
			}
			break
		}
	}
	result := textMessage("tool", output)
	result.ToolCallID = call.ID
	a.record(result)
	took := time.Since(started)
	a.file.appendTook(call.ID, took)
	hub.send(Event{Kind: EventToolFinished, Tool: "bash", CallID: call.ID, Args: event.Args, Took: took})
	event.Kind, event.Hint, event.Output = EventToolEnd, "", capOutput(output)
	if failed {
		event.Kind = EventToolFailed
	}
	hub.send(event)
	hub.send(Event{Kind: EventTurnDone, Usage: a.sealTurn(Usage{}, started, a.Model())})
	return ctx.Err() == nil
}
