package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image/png"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/builder"
	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

// CallAppMCP accepts calls only through the profile's local socket. The stdio
// server fixes the principal at launch, outside the model's tool arguments.
func (b *appBackend) CallAppMCP(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var p appMCPRequest
	if err := decodeAppMCP(raw, &p); err != nil || p.App == "" || p.Agent == "" {
		return nil, appwire.Errorf(appwire.CodeBadParams, "Choose an app and an agent.")
	}
	_, def, err := b.appContentDefinition(p.App)
	if err != nil {
		return nil, err
	}
	principal := data.AgentPrincipal{ID: p.Agent}
	scope := appwire.AppContentScope{App: p.App, Version: def.Meta.Version, Principal: principal}
	if p.Kind == "register" && p.Agent != p.App {
		db, err := b.dataStore("")
		if err != nil {
			return nil, err
		}
		if err := db.RegisterMCPAgent(p.App, data.MCPAgent{AgentPrincipal: principal, Name: p.Agent, Harness: p.Harness}); err != nil {
			return nil, appDataError(err)
		}
		b.emit.Emit(appwire.EventAppAgentChanged, appwire.AppAgentChanged{App: p.App, Kind: "grants"})
	}
	catalog, err := b.appMCPCatalog(ctx, scope)
	if err != nil {
		return nil, err
	}
	if p.Kind == "list" || p.Kind == "register" {
		return json.Marshal(catalog)
	}
	if !slices.ContainsFunc(catalog, func(tool appMCPTool) bool { return tool.ID == p.Target && tool.Kind == p.Kind }) {
		group, grant := "actions", p.Target
		switch p.Kind {
		case "query":
			group = "data"
		case "look":
			group, grant = "data", "look/"+p.Target
		case "mark":
			grant = "mark/" + p.Target
		}
		if b.appAgentAllowed(p.App, principal, group, grant) {
			return nil, appwire.Errorf(appwire.CodeNotFound, "That app operation is unavailable.")
		}
		return nil, appwire.Errorf(appwire.CodeForbidden, "The agent needs a grant for that app operation.")
	}
	switch p.Kind {
	case "query":
		var q appwire.AppViewQuery
		if err := decodeAppMCP(p.Args, &q); err != nil || q.View != "" {
			return nil, appwire.Errorf(appwire.CodeBadParams, "Supply the arguments for the declared query.")
		}
		q.View = p.Target
		return b.RunAppContent(ctx, scope, appwire.AppContentCommand{Verb: appwire.AppContentQuery, Query: q})
	case "action":
		var action appwire.AppActionIntent
		if err := decodeAppMCP(p.Args, &action); err != nil || action.Action != "" {
			return nil, appwire.Errorf(appwire.CodeBadParams, "Supply the arguments for the declared Action.")
		}
		action.Action, action.Cause = p.Target, "mcp:"+session.NewSessionID()
		return b.RunAppContent(ctx, scope, appwire.AppContentCommand{Verb: appwire.AppContentInvoke, Action: action})
	case "look":
		return b.appMCPLook(ctx, def, p.Target)
	case "mark":
		var mark struct {
			Text   string         `json:"text"`
			Target builder.Target `json:"target"`
		}
		if err := decodeAppMCP(p.Args, &mark); err != nil || mark.Target.Screen != p.Target || strings.TrimSpace(mark.Text) == "" || len(mark.Text) > 8000 {
			return nil, appwire.Errorf(appwire.CodeBadParams, "Choose a target on the declared screen and enter a note.")
		}
		raw, _ := def.Screen(p.Target)
		if !appMCPMarkTarget(raw, mark.Target) {
			return nil, appwire.Errorf(appwire.CodeBadParams, "The mark target is unavailable on this screen.")
		}
		jobs, err := b.AppBuilds()
		if err != nil {
			return nil, err
		}
		draft, err := jobs.Propose(p.App, mark.Text, &mark.Target)
		return marshalAppContent(map[string]string{"draft": draft}, err)
	}
	return nil, appwire.Errorf(appwire.CodeBadParams, "Unknown app MCP operation.")
}

func decodeAppMCP(raw json.RawMessage, value any) error {
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return appwire.Errorf(appwire.CodeBadParams, "Supply one JSON object.")
	}
	return nil
}

func (b *appBackend) appMCPCatalog(ctx context.Context, scope appwire.AppContentScope) ([]appMCPTool, error) {
	doc, err := b.readAppContentCatalog(ctx, scope)
	if err != nil {
		return nil, err
	}
	tools := []appMCPTool{}
	appendTool := func(id, kind, label string, props map[string]any) {
		if props == nil {
			props = map[string]any{}
		}
		shape, _ := json.Marshal(map[string]any{"type": "object", "properties": props, "additionalProperties": false})
		tools = append(tools, appMCPTool{ID: id, Kind: kind, Label: label, Input: shape})
	}
	allowed := func(group, id string) bool { return b.appAgentAllowed(scope.App, scope.Principal, group, id) }
	for _, action := range doc.Actions {
		if !allowed("actions", action.ID) {
			continue
		}
		args := map[string]any{}
		for name, p := range action.Args {
			args[name] = appMCPArgument(p.Type)
		}
		props := map[string]any{"args": map[string]any{"type": "object", "properties": args, "additionalProperties": false}}
		if action.Revision {
			props["expected_rev"] = map[string]any{"type": "integer", "minimum": 1}
		}
		appendTool(action.ID, "action", action.Label, props)
	}
	for _, id := range slices.Sorted(maps.Keys(doc.Views)) {
		if !allowed("data", id) {
			continue
		}
		args := map[string]any{}
		for name, typ := range doc.Views[id].Args {
			args[name] = appMCPArgument(typ)
		}
		appendTool(id, "query", id, map[string]any{
			"args":   map[string]any{"type": "object", "properties": args, "additionalProperties": false},
			"cursor": map[string]any{"type": "string"},
		})
	}
	_, def, err := b.appContentDefinition(scope.App)
	if err != nil {
		return nil, err
	}
	marksAvailable := appMCPMarksAvailable()
	for _, id := range def.Screens {
		if b.builds.look.ready.Load() && allowed("data", "look/"+id) {
			appendTool(id, "look", "Look at "+id+" with sample records", nil)
		}
		if marksAvailable && allowed("actions", "mark/"+id) {
			appendTool(id, "mark", "Suggest a change to "+id, map[string]any{
				"text": map[string]any{"type": "string"}, "target": map[string]any{"type": "object"},
			})
		}
	}
	return tools, nil
}

func appMCPArgument(kind string) map[string]any {
	switch kind {
	case "bool":
		return map[string]any{"type": "boolean"}
	case "number", "money":
		return map[string]any{"type": "number"}
	case "refs":
		return map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	default:
		return map[string]any{"type": "string"}
	}
}

type appContentCatalog struct {
	Actions []appAssistantAction
	Views   map[string]schema.QueryIR
}

func (b *appBackend) readAppContentCatalog(ctx context.Context, scope appwire.AppContentScope) (appContentCatalog, error) {
	var catalog appContentCatalog
	raw, err := b.RunAppContent(ctx, scope, appwire.AppContentCommand{Verb: appwire.AppContentShow})
	if err != nil {
		return catalog, err
	}
	err = json.Unmarshal(raw, &catalog)
	return catalog, err
}

func appMCPMarkTarget(raw json.RawMessage, target builder.Target) bool {
	if target.Checkpoint != "" || len(target.Path) > 128 {
		return false
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	for _, key := range target.Path {
		switch key := key.(type) {
		case string:
			object, ok := value.(map[string]any)
			if !ok {
				return false
			}
			value, ok = object[key]
			if !ok {
				return false
			}
		case float64:
			array, ok := value.([]any)
			if !ok || key < 0 || key >= float64(len(array)) || key != float64(int(key)) {
				return false
			}
			value = array[int(key)]
		default:
			return false
		}
	}
	if target.Node == "" {
		return len(target.Path) > 0
	}
	object, ok := value.(map[string]any)
	return ok && object["id"] == target.Node
}

type appMCPImage struct {
	PNG      []byte
	Problems []string
}

func (b *appBackend) appMCPLook(ctx context.Context, def store.Definition, screen string) (json.RawMessage, error) {
	if !b.builds.look.ready.Load() {
		return nil, appwire.Errorf(appwire.CodeDeskUnavailable, "The desktop renderer isn't connected.")
	}
	jobs, err := b.AppBuilds()
	if err != nil {
		return nil, err
	}
	p, closeScope, err := jobs.PreviewKept(def)
	if err != nil {
		return nil, appEngineError(err)
	}
	defer closeScope()
	capture, err := b.drawAppPreview(ctx, p, appwire.AppLookParams{Screen: screen})
	if err != nil {
		return nil, err
	}
	picture, err := base64.StdEncoding.DecodeString(capture.PNG)
	if capture.Problem != "" || err != nil || len(picture) == 0 || len(picture) > 3<<20 {
		return nil, appwire.Errorf(appwire.CodeEngine, "The desktop preview could not be read.")
	}
	info, err := png.DecodeConfig(bytes.NewReader(picture))
	if err != nil || info.Width > 8192 || info.Height > 8192 {
		return nil, appwire.Errorf(appwire.CodeEngine, "The desktop returned an invalid app preview image.")
	}
	return json.Marshal(appMCPImage{PNG: picture, Problems: capture.Problems})
}

// A text mark starts a Builder turn, which needs a configured model.
func appMCPMarksAvailable() bool {
	_, err := config.Load()
	return err == nil
}
