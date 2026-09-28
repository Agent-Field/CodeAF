package session

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"github.com/Agent-Field/codeaf/internal/approval"
	"strings"
	"time"
	"unicode/utf8"

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

// SubmitBash is the explicit human command door. Generic submissions, including
// scheduled work and model text, never gain shell authority from a leading !.
func (a *Agent) SubmitBash(ctx context.Context, text string) (<-chan Event, error) {
	command, ok := BashCommand(text)
	if !ok || command == "" {
		return nil, errors.New(BashEmptyWord)
	}
	if a.config.InTask {
		return nil, errors.New("run ! commands from the parent conversation")
	}
	user := userText(text)
	user.bash = command
	return a.submitUser(ctx, user)
}

// approveUserBash treats the entered command as consent while retaining hard
// policy denies and disabled capabilities. It never asks a model for permission.
func (a *Agent) approveUserBash(call ai.ToolCall) (toolResult, bool) {
	if off := a.capabilityRefusal(call.Function.Name, json.RawMessage(call.Function.Arguments)); off != "" {
		return refusal(off), false
	}
	if decision, governed := a.decide(call); governed && decision.Action == approval.ActionDeny {
		return refusal("denied by approval rule: " + decision.Rule), false
	}
	return toolResult{}, true
}

// displayToolOutput preserves the runner's bounded shell result for a person
// who explicitly requested it; model tool rows retain their smaller preview.
func displayToolOutput(id, output string) string {
	if IsUserBashCall(id) {
		return output
	}
	return capOutput(output)
}

// shellOutput publishes a bounded literal prefix while the process runs. The
// runner still owns final truncation and the full spill file. Incomplete UTF-8
// is retained across writes so remote JSON frames cannot corrupt split runes.
type shellOutput struct {
	hub          *eventHub
	id           string
	bytes, lines int
	pending      []byte
	cut          bool
}

func (w *shellOutput) Write(p []byte) (int, error) {
	n := len(p)
	if w.cut {
		return n, nil
	}
	w.pending = append(w.pending, p...)
	end := 0
	for end < len(w.pending) && utf8.FullRune(w.pending[end:]) {
		r, size := utf8.DecodeRune(w.pending[end:])
		if size > w.bytes || r == '\n' && w.lines <= 0 {
			w.cut = true
			break
		}
		end += size
		w.bytes -= size
		if r == '\n' {
			w.lines--
		}
	}
	if end > 0 {
		w.hub.send(Event{Kind: EventToolOutput, Tool: "bash", CallID: w.id, Text: strings.ToValidUTF8(string(w.pending[:end]), "�")})
	}
	w.pending = append(w.pending[:0], w.pending[end:]...)
	if w.cut {
		w.pending = nil
		w.hub.send(Event{Kind: EventToolOutput, Tool: "bash", CallID: w.id, Text: "\n[Live output limit reached; the final result includes the saved output path.]\n"})
	}
	return n, nil
}

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
	// The same pre-action chain protects active work and repository ownership.
	// Only the consent hook distinguishes a command the person entered directly.
	ep := a.newEpisode()
	ep.userBash = true
	checked, refused, allowed := ep.preAction(ctx, hub, call)
	var output string
	var failed bool
	if !allowed {
		output, failed = refused.text, true
	} else if ctx.Err() != nil {
		output, failed = "Command cancelled", true
	} else {
		caps := a.resultCaps()
		writer := &shellOutput{hub: hub, id: call.ID, bytes: caps.MaxBytes, lines: caps.MaxLines}
		var err error
		output, failed, err = bare.RunBash(ctx, a.config.Workspace, json.RawMessage(checked.Function.Arguments), caps, writer)
		if err != nil {
			output, failed = err.Error(), true
		}
	}
	result := textMessage("tool", output)
	result.ToolCallID = call.ID
	a.record(result)
	took := time.Since(started)
	a.file.appendTook(call.ID, took)
	hub.send(Event{Kind: EventToolFinished, Tool: "bash", CallID: call.ID, Args: event.Args, Took: took})
	event.Kind, event.Hint, event.Output = EventToolEnd, "", displayToolOutput(call.ID, output)
	if failed {
		event.Kind = EventToolFailed
	}
	hub.send(event)
	hub.send(Event{Kind: EventTurnDone, Usage: a.sealTurn(Usage{}, started, a.Model())})
	return ctx.Err() == nil
}
