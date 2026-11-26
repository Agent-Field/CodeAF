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

// runAppCLICommand binds an explicit app chosen by the local CLI caller. The
// content decoder and execution path are the same ones used by the assistant.
func (b *appBackend) runAppCLICommand(ctx context.Context, verb string, raw json.RawMessage) (any, error) {
	switch verb {
	case "agent_ask":
		var p appwire.AppAgentAsk
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		r, err := b.AskAppAgent(p)
		return appwire.RunStarted{ID: r.ID}, err
	case "agent_query":
		var p appwire.AppAgentQuery
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return b.QueryAppAgent(p)
	case "agent_grant":
		var p appwire.AppAgentGrant
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return appwire.Empty{}, b.GrantAppAgent(p)
	case "agent_duty_add":
		var p appwire.AppAgentDuty
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		id, err := b.AddAppAgentDuty(p)
		return appwire.RunStarted{ID: id}, err
	case appwire.AppContentShow, appwire.AppContentQuery, appwire.AppContentInvoke, appwire.AppContentPropose:
		var p struct {
			App     string          `json:"app"`
			Params  json.RawMessage `json:"params"`
			Request string          `json:"request,omitempty"`
		}
		d := json.NewDecoder(bytes.NewReader(raw))
		d.DisallowUnknownFields()
		if err := d.Decode(&p); err != nil {
			return nil, err
		}
		if verb != appwire.AppContentPropose && p.Request != "" {
			return nil, appwire.Errorf(appwire.CodeBadParams, "Request text is only for an app proposal.")
		}
		_, def, err := b.appAssistantDefinition(p.App)
		if err != nil {
			return nil, err
		}
		scope := appwire.AppContentScope{App: p.App, Version: def.Meta.Version, Actor: def.Meta.Name + " agent", Profile: b.localProfile(), Conversation: "app-cli"}
		return appwire.RunScopedAppCommand(ctx, b, scope, verb, p.Params, session.NewSessionID(), func(context.Context) (string, bool) { return p.Request, true },
			func(card appwire.AppChangeProposed) { b.emit.Emit(appwire.EventAppChangeProposed, card) })
	default:
		return appwire.RunAppCommand(b, verb, raw)
	}
}
