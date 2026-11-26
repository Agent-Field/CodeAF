package main

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/outputs"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/home"
)

func (b *appBackend) RunAppContent(ctx context.Context, p appwire.AppContentScope, command appwire.AppContentCommand) (json.RawMessage, error) {
	app, def, err := b.appContentDefinition(p.App)
	if err != nil {
		return nil, err
	}
	if def.Meta.Version != p.Version {
		return nil, appwire.Errorf(appwire.CodeConflict, "The app changed. Ask again using its current views and actions.")
	}
	views := map[string]schema.QueryIR{}
	for _, id := range def.Screens {
		raw, _ := def.Screen(id)
		screen, err := schema.ParseScreen(raw)
		if err != nil {
			return nil, err
		}
		for id, view := range screen.Views {
			if appAssistantViewSupported(view) {
				views[screen.Screen+"/"+id] = view
			}
		}
	}
	sc := data.Scope{App: p.App, Principal: p.Principal, Action: command.Action.Action, Cause: command.Action.Cause}
	switch command.Verb {
	case appwire.AppContentShow:
		actions, err := appAssistantActions(app, def)
		if err != nil {
			return nil, err
		}
		harnesses := b.appAIHarnesses(runs.Files(home.Join("apps")))
		actions = slices.DeleteFunc(actions, func(a appAssistantAction) bool {
			i := slices.IndexFunc(app.Commands, func(cmd schema.Command) bool { return cmd.ID == a.ID })
			if i < 0 || app.Commands[i].Run == "" {
				return false
			}
			raw, ok := def.Run(app.Commands[i].Run)
			if !ok {
				return true
			}
			flow, err := schema.ParseRun(raw)
			return err != nil || !appAIRunAvailable(harnesses, flow.Steps)
		})
		return json.Marshal(map[string]any{"views": views, "actions": actions})
	case appwire.AppContentQuery:
		declared, ok := views[command.Query.View]
		if !ok {
			return nil, appwire.Errorf(appwire.CodeNotFound, "That view is unavailable to the app assistant.")
		}
		if !b.appAgentAllowed(p.App, p.Principal, "data", command.Query.View) {
			return nil, appwire.Errorf(appwire.CodeForbidden, "The app agent needs a data grant for %q.", command.Query.View)
		}
		return b.queryAppAssistant(sc, declared, command.Query)
	case appwire.AppContentInvoke:
		return b.invokeAppAssistant(ctx, sc, app, def, command.Action)
	case appwire.AppContentPropose:
		if !b.appAgentAllowed(p.App, p.Principal, "actions", "propose_change") {
			return nil, appwire.Errorf(appwire.CodeForbidden, "This agent cannot propose changes.")
		}
		jobs, err := b.AppBuilds()
		if err != nil {
			return nil, err
		}
		draft, err := jobs.ProposeAssistant(p.App, command.Request, command.Cause)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]string{"draft": draft})
	}
	return nil, appwire.Errorf(appwire.CodeBadParams, "Unknown app command.")
}

type appAssistantAction struct {
	ID       string                     `json:"id"`
	Label    string                     `json:"label"`
	Record   string                     `json:"record_type,omitempty"`
	Args     map[string]schema.RunParam `json:"args"`
	Revision bool                       `json:"requires_expected_rev,omitempty"`
}

func appAssistantActions(app schema.App, def store.Definition) ([]appAssistantAction, error) {
	actions := make([]appAssistantAction, 0, len(app.Commands))
	recordAction := func(id, label, typ string) appAssistantAction {
		return appAssistantAction{ID: id, Label: label, Record: typ, Revision: true,
			Args: map[string]schema.RunParam{"record": {Type: "ref", To: typ}}}
	}
	for _, cmd := range app.Commands {
		if raw, ok := def.Run(cmd.Run); ok && cmd.Run != "" {
			flow, err := schema.ParseRun(raw)
			if err != nil {
				return nil, err
			}
			for name, fixed := range cmd.Params {
				if !appAssistantInput(fixed) {
					delete(flow.Params, name)
				}
			}
			actions = append(actions, appAssistantAction{ID: cmd.ID, Label: cmd.Label, Record: cmd.For, Args: flow.Params})
		} else if _, ok := appAssistantRecordUpdate(cmd); ok {
			actions = append(actions, recordAction(cmd.ID, cmd.Label, cmd.For))
		}
	}
	for _, id := range slices.Sorted(maps.Keys(app.Outputs)) {
		t := app.Outputs[id]
		args := map[string]schema.RunParam{}
		if t.Record != "" {
			args["record"] = schema.RunParam{Type: "ref", To: t.Record}
		}
		actions = append(actions, appAssistantAction{ID: "render/" + id, Label: t.Label, Record: t.Record, Args: args})
	}
	for _, typ := range slices.Sorted(maps.Keys(app.Records)) {
		if record := app.Records[typ]; record.Lifecycle != nil {
			for _, tr := range record.Lifecycle.Transitions {
				actions = append(actions, recordAction("transition/"+typ+"/"+tr.ID, tr.Label, typ))
			}
		}
	}
	return actions, nil
}

func appAssistantInput(value any) bool {
	text, ok := value.(string)
	return ok && (strings.HasPrefix(text, "$selection.") || strings.HasPrefix(text, "$input."))
}

func appAssistantRecordUpdate(command schema.Command) (map[string]any, bool) {
	if command.For == "" || len(command.Do) != 1 {
		return nil, false
	}
	update, ok := command.Do[0]["records.update"].(map[string]any)
	set, valid := update["set"].(map[string]any)
	if !ok || !valid || update["ids"] != "$selection.ids" {
		return nil, false
	}
	for _, value := range set {
		if text, ok := value.(string); ok && strings.HasPrefix(text, "$") {
			return nil, false
		}
	}
	return set, true
}

func (b *appBackend) queryAppAssistant(sc data.Scope, view schema.QueryIR, request appwire.AppViewQuery) (json.RawMessage, error) {
	ir := data.QueryIR{Type: view.Type, Group: view.Group, Select: view.Select}
	args := map[string]any{}
	for key, raw := range request.Args {
		var value any
		if json.Unmarshal(raw, &value) != nil || !appViewArgument(view.Args[key], value) {
			return nil, appwire.Errorf(appwire.CodeBadParams, "Invalid argument %q.", key)
		}
		args[key] = value
	}
	for _, filter := range view.Filter {
		value := filter.Value
		if ref, ok := value.(string); ok {
			if key, ref := strings.CutPrefix(ref, "$args."); ref {
				value, ok = args[key]
				if !ok {
					return nil, appwire.Errorf(appwire.CodeBadParams, "Missing argument %q.", key)
				}
			}
		}
		op := filter.Op
		neg := op == "!="
		switch op {
		case "=", "==", "!=":
			op = ":"
		}
		ir.Where = append(ir.Where, data.Cond{Field: filter.Field, Op: op, Value: fmt.Sprint(value), Neg: neg, Literal: true})
	}
	for _, agg := range view.Aggregate {
		ir.Aggregate = append(ir.Aggregate, data.Agg{Fn: agg.Op, Field: agg.Field, As: agg.As})
	}
	s, err := b.dataStore("")
	if err != nil {
		return nil, err
	}
	if len(ir.Aggregate) != 0 {
		result, err := s.Aggregate(sc, &ir)
		return marshalAppContent(result, appDataError(err))
	}
	limit := 0
	if view.Window != nil {
		limit = view.Window.Limit
		if len(view.Window.OrderBy) == 1 {
			ir.Sort = view.Window.OrderBy[0]
		}
	}
	result, err := s.QueryRows(sc, &ir, request.Cursor, limit)
	if err != nil {
		return nil, appDataError(err)
	}
	return json.Marshal(map[string]any{"rows": result.Rows, "next": result.Next, "total": result.Total})
}

func appAssistantViewSupported(view schema.QueryIR) bool {
	if view.Relate != nil || (view.Window != nil && len(view.Window.OrderBy) > 1) {
		return false
	}
	for _, kind := range view.Args {
		if appViewArgumentType(kind) == nil {
			return false
		}
	}
	return true
}

func appViewArgumentType(kind string) reflect.Type {
	switch kind {
	case "text", "longtext", "enum", "date", "datetime", "ref", "email", "url", "phone", "person":
		return reflect.TypeFor[string]()
	case "number", "money":
		return reflect.TypeFor[float64]()
	case "bool":
		return reflect.TypeFor[bool]()
	}
	return nil
}

func appViewArgument(kind string, value any) bool {
	return value != nil && reflect.TypeOf(value) == appViewArgumentType(kind)
}

func (b *appBackend) invokeAppAssistant(ctx context.Context, sc data.Scope, app schema.App, def store.Definition, intent appwire.AppActionIntent) (json.RawMessage, error) {
	if !b.appAgentAllowed(sc.App, sc.Principal, "actions", intent.Action) {
		return nil, appwire.Errorf(appwire.CodeForbidden, "The app agent needs an action grant for %q.", intent.Action)
	}
	if id, isOutput := strings.CutPrefix(intent.Action, "render/"); isOutput {
		template, ok := app.Outputs[id]
		if !ok {
			return nil, appwire.Errorf(appwire.CodeNotFound, "That output is not declared in this app.")
		}
		action := schema.RenderAction{Output: id}
		if template.Record != "" {
			if len(intent.Args) != 1 || json.Unmarshal(intent.Args["record"], &action.Record) != nil || action.Record == "" {
				return nil, appwire.Errorf(appwire.CodeBadParams, "Choose one record ID for the output.")
			}
		} else if len(intent.Args) != 0 {
			return nil, appwire.Errorf(appwire.CodeBadParams, "A query output takes its input from its declaration.")
		}
		file, err := b.invokeKeptOutput(ctx, app, outputs.Invocation{Scope: sc, Render: action})
		return marshalAppContent(file, appDataError(err))
	}
	if name, isTransition := strings.CutPrefix(intent.Action, "transition/"); isTransition {
		typ, transition, ok := strings.Cut(name, "/")
		record := app.Records[typ]
		if !ok || record.Lifecycle == nil || !slices.ContainsFunc(record.Lifecycle.Transitions, func(t schema.RecordTransition) bool { return t.ID == transition }) {
			return nil, appwire.Errorf(appwire.CodeNotFound, "That transition is not declared in this app.")
		}
		var recordID string
		if len(intent.Args) != 1 || json.Unmarshal(intent.Args["record"], &recordID) != nil || recordID == "" || intent.ExpectedRev == nil || *intent.ExpectedRev == 0 || *intent.ExpectedRev > 1<<63-1 {
			return nil, appwire.Errorf(appwire.CodeBadParams, "A transition needs one record and its displayed revision.")
		}
		db, err := b.dataStore("")
		if err != nil {
			return nil, err
		}
		result, err := db.TransitionBatch(sc, transition, []data.TransitionRecord{{RecordRef: data.RecordRef{Type: typ, ID: recordID}, Rev: int64(*intent.ExpectedRev)}}, data.TransitionOptions{})
		return marshalAppContent(result, appDataError(err))
	}
	at := slices.IndexFunc(app.Commands, func(c schema.Command) bool { return c.ID == intent.Action })
	if at < 0 {
		return nil, appwire.Errorf(appwire.CodeNotFound, "That action is not declared in this app.")
	}
	command := app.Commands[at]
	args := map[string]any{}
	for key, raw := range intent.Args {
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		args[key] = value
	}
	if raw, ok := def.Run(command.Run); ok && command.Run != "" {
		flow, err := schema.ParseRun(raw)
		if err != nil {
			return nil, err
		}
		for name, value := range args {
			parameter, declared := flow.Params[name]
			if !declared || !appRunArgument(parameter.Type, value) {
				return nil, appwire.Errorf(appwire.CodeBadParams, "Invalid automation argument %q.", name)
			}
		}
		for name := range args {
			if fixed, ok := command.Params[name]; ok && !appAssistantInput(fixed) {
				return nil, appwire.Errorf(appwire.CodeBadParams, "The app declares a fixed value for %q.", name)
			}
		}
		for name, value := range command.Params {
			if text, ok := value.(string); !ok || !strings.HasPrefix(text, "$") {
				args[name] = value
			}
		}
		if command.Run == appAgentAskRun || strings.HasPrefix(command.Run, appAgentDutyPrefix) {
			return nil, appwire.Errorf(appwire.CodeForbidden, "Use the app agent entry point.")
		}
		if b.startAppRunsOnce != nil {
			b.startAppRunsOnce()
		}
		engine := b.appRuns.Load()
		if engine == nil {
			return nil, appwire.Errorf(appwire.CodeEngine, "App actions are unavailable.")
		}
		return marshalAppContent(engine.StartAgentAction(sc.App, command.Run, args, runs.AgentActionOrigin{Principal: sc.Principal, Action: intent.Action, Version: def.Meta.Version, Cause: sc.Cause}))
	}
	if set, ok := appAssistantRecordUpdate(command); ok {
		id, _ := args["record"].(string)
		if len(args) != 1 || id == "" || intent.ExpectedRev == nil || *intent.ExpectedRev == 0 || *intent.ExpectedRev > 1<<63-1 {
			return nil, appwire.Errorf(appwire.CodeBadParams, "This action takes args.record with one record ID and expected_rev with that row's _rev.")
		}
		db, err := b.dataStore("")
		if err != nil {
			return nil, err
		}
		result, err := db.Update(sc, command.For, []string{id}, set, data.UpdateOptions{Rev: int64(*intent.ExpectedRev)})
		return marshalAppContent(result, appDataError(err))
	}
	return nil, appwire.Errorf(appwire.CodeBadParams, "This action needs a supported app process or automation.")
}

func appRunArgument(typ string, value any) bool {
	if typ == "refs" {
		ids, ok := value.([]any)
		if !ok {
			return false
		}
		for _, id := range ids {
			if _, ok := id.(string); !ok {
				return false
			}
		}
		return true
	}
	return appViewArgument(typ, value)
}

func marshalAppContent(result any, err error) (json.RawMessage, error) {
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

func appAIRunAvailable(harnesses runs.Harnesses, steps []schema.Step) bool {
	for _, step := range steps {
		if step.AI != nil && harnesses.Check(step.AI) != nil {
			return false
		}
		if !appAIRunAvailable(harnesses, step.Steps) {
			return false
		}
	}
	return true
}
