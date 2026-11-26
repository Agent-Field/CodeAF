package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/appcmd"
	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/outputs"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/session"
)

// An app run supplies the principal and kept version before the model sees
// any records or command arguments. The executor has one literal app verb.
func (b *appBackend) completeOutputAgent(ctx context.Context, app schema.App, call runs.AgentCall, model, system, prompt string, client session.Completer, spend *session.SpendGuard) (runs.Reply, error) {
	workspace, err := os.MkdirTemp("", "codeaf-app-output-")
	if err != nil {
		return runs.Reply{}, err
	}
	defer os.RemoveAll(workspace)
	system += "\n\nYou may invoke a granted output from this app using codeaf app render " +
		`{"output":"OUTPUT_ID","record":"RECORD_ID"}` +
		". For a query output, omit the record. Use only record IDs supplied by the run. " +
		"The app and actor come from the run. Declared output IDs: " +
		strings.Join(slices.Sorted(maps.Keys(app.Outputs)), ", ") + "."
	// The session records provider usage. The guard supplies the same
	// turn's accumulated cost to the run without recording a second charge.
	spend.Task = &session.SpendTask{}
	answer, err := session.ScopedCommandTurn(ctx, workspace, model, system, prompt, spend.Wrap(model, client), func(ctx context.Context, command string) (string, error) {
		verb, args, err := appcmd.Parse(command)
		if err != nil {
			return "", err
		}
		if verb != "render" {
			return "", errors.New("this run's app command can only render a declared output")
		}
		var action schema.RenderAction
		decoder := json.NewDecoder(bytes.NewReader(args))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&action); err != nil || action.Output == "" {
			return "", errors.New("render needs an output id and optional record id or inbox flag")
		}
		payload, err := json.Marshal(action)
		if err != nil {
			return "", err
		}
		// An agent can request several distinct files in one step. Repeated
		// requests for the same file reuse the effect's saved receipt.
		key := fmt.Sprintf("%s/render/%x", call.Key, sha256.Sum256(payload))
		file, err := b.invokeKeptOutput(ctx, app, outputs.Invocation{
			Scope:  data.Scope{App: call.App, Principal: data.AgentPrincipal{ID: call.App}, Flow: true},
			Render: action, Run: call.ID, Step: call.Step, Key: key,
		})
		if err != nil {
			return "", err
		}
		receipt, err := json.Marshal(file)
		return string(receipt), err
	})
	return runs.Reply{Output: answer, Cost: spend.Task.Total()}, err
}
