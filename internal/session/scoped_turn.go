package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

// ScopedCommandTurn runs the ordinary turn loop with a command executor
// in place of the process executor. A scoped turn has no ambient tools,
// filesystem, credentials or network. The caller binds scope to execute.
func ScopedCommandTurn(ctx context.Context, workspace, model, system, request string, client Completer, execute func(context.Context, string) (string, error)) (string, error) {
	if strings.TrimSpace(system) == "" || execute == nil {
		return "", errors.New("a scoped turn needs a prompt and a command executor")
	}
	a, err := newAgent(Config{
		Workspace: workspace, Model: model, System: system, Errand: true,
		OneModel: true, PromptProfile: "full", DeterministicNames: true,
		scopedCommand: execute,
	}, client)
	if err != nil {
		return "", err
	}
	defer a.Close()
	events, err := a.Submit(ctx, request)
	if err != nil {
		return "", err
	}
	complete := false
	for event := range events {
		switch event.Kind {
		case EventError:
			if event.Err != nil {
				return "", event.Err
			}
			return "", errors.New("the app turn stopped")
		case EventTurnDone:
			complete = true
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !complete {
		return "", errors.New("the app turn stopped before it finished")
	}
	return checkpointLastSaid(a.snapshot()), nil
}

// scopedCommandTool uses the engine's ordinary command slot, routed to the
// command parser directly. The former process handler is never reachable.
func (a *Agent) scopedCommandTool() bare.Tool {
	return bare.Tool{
		Name:        "bash",
		Description: "Run the one command allowed in this session. Pass it verbatim, without shell operators, pipes, or flags.",
		Schema:      json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}},"required":["command"],"additionalProperties":false}`),
		Execute: func(ctx context.Context, raw json.RawMessage) (string, bool, error) {
			var args struct{ Command string }
			dec := json.NewDecoder(strings.NewReader(string(raw)))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&args); err != nil {
				return "Send one command.", true, nil
			}
			out, err := a.config.scopedCommand(ctx, args.Command)
			if err != nil {
				return err.Error(), true, nil
			}
			return out, false, nil
		},
	}
}
