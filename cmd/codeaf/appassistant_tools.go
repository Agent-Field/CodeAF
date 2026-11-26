package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/session"
)

func (b *appBackend) appAssistantTools(principal appwire.AppContentScope) *session.AppScope {
	scope := &session.AppScope{ID: principal.App, AuthorizeModel: b.appModelAuthorizer(principal.App)}
	scope.Tools = []bare.Tool{{
		Name:        "app",
		Description: "For a requested change to the app itself, call propose_change without parameters before querying records. Use show to find view IDs and action arguments. Query takes params.view and optional params.args. Invoke takes params.action and params.args. If the action requires a revision, give params.expected_rev from the record row's _rev and the record ID as params.args.record.",
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
			if command.Verb == appwire.AppContentQuery || command.Verb == appwire.AppContentInvoke {
				session.MarkAppContent(ctx)
			}
			result, err := appwire.RunScopedAppCommand(ctx, b, principal, command.Verb, command.Params, session.NewSessionID(), session.AppChangeRequest)
			var refusal *appwire.Error
			if errors.As(err, &refusal) {
				return refusal.Message, true, nil
			}
			return string(result), err != nil, err
		},
	}}
	return scope
}

// runAppCLICommand binds an explicit app chosen by the local CLI caller. The
// content decoder and execution path are the same ones used by the assistant.
func (b *appBackend) runAppCLICommand(ctx context.Context, verb string, raw json.RawMessage) (any, error) {
	if verb == "_mcp" {
		return b.CallAppMCP(ctx, raw)
	}
	switch verb {
	case "data_query":
		return decodeAppDataCommand(raw, b.DataQuery)
	case "data_aggregate":
		return decodeAppDataCommand(raw, b.DataAggregate)
	case "data_types":
		return decodeAppDataCommand(raw, b.DataTypes)
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
	case "agent_duty_remove":
		var p appwire.AppAgentDutyRef
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return appwire.Empty{}, b.RemoveAppAgentDuty(p)
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
		load := b.appAssistantDefinition
		if verb == appwire.AppContentShow {
			load = b.appContentDefinition
		}
		_, def, err := load(p.App)
		if err != nil {
			return nil, err
		}
		scope := appwire.AppContentScope{App: p.App, Version: def.Meta.Version, Principal: data.AgentPrincipal{ID: p.App}, Profile: b.localProfile(), Conversation: "app-cli"}
		return appwire.RunScopedAppCommand(ctx, b, scope, verb, p.Params, session.NewSessionID(), func(context.Context) (string, bool) { return p.Request, true })
	default:
		return appwire.RunAppCommand(b, verb, raw)
	}
}

func decodeAppDataCommand[P, R any](raw json.RawMessage, run func(P) (*R, error)) (any, error) {
	var p P
	if err := decodeAppConnectionJSON(bytes.NewReader(raw), &p); err != nil {
		return nil, appwire.Errorf(appwire.CodeBadParams, "The data command needs a JSON object with known fields.")
	}
	return run(p)
}
