package main

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/session"
)

func (b *appBackend) appAssistantTools(principal appwire.AppContentScope) *session.AppScope {
	scope := &session.AppScope{ID: principal.App}
	door, ok := any(b).(appwire.AppContentDoor)
	if !ok {
		return scope
	}
	scope.Tools = []bare.Tool{{
		Name:        "app",
		Description: "Use codeaf app for this app. Show its declared views and actions, query a view with typed arguments, invoke a declared action, or propose an app edit the person requested.",
		Schema:      json.RawMessage(`{"type":"object","properties":{"verb":{"type":"string","enum":["show","query","invoke","propose_change"]},"params":{"type":"object"}},"required":["verb"],"additionalProperties":false}`),
		Execute: func(ctx context.Context, raw json.RawMessage) (string, bool, error) {
			var command struct {
				Verb   string          `json:"verb"`
				Params json.RawMessage `json:"params"`
			}
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&command); err != nil {
				return "", true, err
			}
			if command.Verb != appwire.AppContentPropose {
				session.MarkAppContent(ctx)
			}
			result, err := appwire.RunScopedAppCommand(ctx, door, principal, command.Verb, command.Params, session.NewSessionID(),
				session.AppChangeRequest, func(card appwire.AppChangeProposed) {
					select {
					case <-b.left:
						return
					default:
						b.emit.Emit(appwire.EventAppChangeProposed, card)
					}
				})
			return string(result), err != nil, err
		},
	}}
	return scope
}
