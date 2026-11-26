package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/Agent-Field/codeaf/internal/apps/connections"
	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

func (b *appBackend) appAssistantTools(principal appwire.AppContentScope) *session.AppScope {
	scope := &session.AppScope{ID: principal.App, AuthorizeModel: b.appModelAuthorizer(principal.App)}
	scope.Tools = []bare.Tool{{
		Name:        "app",
		Description: "For a requested change to the app itself, call propose_change without parameters before querying records. Use show to find view IDs and action arguments. Query takes params.view and optional params.args. Invoke takes params.action and params.args. For a custom tool listed by show, call custom_tool with params.tool and params.args. If the action requires a revision, give params.expected_rev from the record row's _rev and the record ID as params.args.record.",
		Schema:      json.RawMessage(`{"type":"object","properties":{"verb":{"type":"string","enum":["show","query","invoke","custom_tool","propose_change"]},"params":{"type":"object"}},"required":["verb"],"additionalProperties":false}`),
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
	if tool, ok := b.appGoogleContentTool(principal); ok {
		scope.Tools = append(scope.Tools, tool)
	}
	return scope
}

// This tool belongs to the app's own codeaf conversation. The app scope's
// model authorizer checks the destination before every request that can carry
// its results. Outside harnesses have no tool entry for message bodies.
func (b *appBackend) appGoogleContentTool(principal appwire.AppContentScope) (bare.Tool, bool) {
	if principal.Principal.ID != principal.App {
		return bare.Tool{}, false
	}
	app, err := runs.Files(home.Join("apps")).App(principal.App)
	if err != nil || !slices.Contains(app.Capabilities.Tools, "google_content") {
		return bare.Tool{}, false
	}
	sources := b.appConnections()
	list, err := sources.List(principal.App)
	if err != nil || !slices.ContainsFunc(list, func(s connections.Summary) bool { return s.Kind == connections.Google }) {
		return bare.Tool{}, false
	}
	return bare.Tool{
		Name:        "google_content",
		Description: "Read one selected Google message body or export one Google Doc as text. Use its app connection, read name and canonical record id.",
		Schema:      json.RawMessage(`{"type":"object","properties":{"connection":{"type":"string"},"read":{"type":"string"},"id":{"type":"string"}},"required":["connection","read","id"],"additionalProperties":false}`),
		Execute: func(ctx context.Context, raw json.RawMessage) (string, bool, error) {
			var p struct {
				Connection string `json:"connection"`
				Read       string `json:"read"`
				ID         string `json:"id"`
			}
			if err := decodeAppConnectionJSON(bytes.NewReader(raw), &p); err != nil {
				return "", true, err
			}
			content, err := sources.ReadGoogleContent(ctx, connections.Query{
				App: principal.App, Connection: p.Connection, Read: p.Read,
			}, p.ID, connections.GoogleOwnModel)
			if err != nil {
				return "", true, err
			}
			session.MarkAppContent(ctx)
			return content.Text, false, nil
		},
	}, true
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
	case appwire.AppContentShow, appwire.AppContentQuery, appwire.AppContentInvoke, appwire.AppContentTool, appwire.AppContentPropose:
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
		load := b.appContentDefinition
		if verb == appwire.AppContentPropose {
			load = b.appAssistantDefinition
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
