package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/appwire"
)

func (b *appBackend) RunAppContent(_ context.Context, p appwire.AppContentScope, command appwire.AppContentCommand) (json.RawMessage, error) {
	app, def, err := b.appAssistantDefinition(p.App)
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
			views[screen.Screen+"/"+id] = view
		}
	}
	sc := data.Scope{App: p.App, Agent: true, Actor: data.AgentAssigneePrefix + p.App, Cause: command.Action.Cause}
	switch command.Verb {
	case appwire.AppContentShow:
		actions := append([]schema.Command(nil), app.Commands...)
		for typ, record := range app.Records {
			if record.Lifecycle == nil {
				continue
			}
			for _, transition := range record.Lifecycle.Transitions {
				actions = append(actions, schema.Command{ID: "transition/" + typ + "/" + transition.ID, Label: transition.Label})
			}
		}
		return json.Marshal(map[string]any{"views": views, "actions": actions})
	case appwire.AppContentQuery:
		declared, ok := views[command.Query.View]
		if !ok || !b.appAgentAllowed(p.App, "data", command.Query.View) {
			return nil, appwire.Errorf(appwire.CodeForbidden, "This agent cannot use that view.")
		}
		return b.queryAppAssistant(sc, declared, command.Query)
	case appwire.AppContentInvoke:
		return b.invokeAppAssistant(sc, app, def, command.Action)
	case appwire.AppContentPropose:
		if !b.appAgentAllowed(p.App, "actions", "propose_change") {
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

func (b *appBackend) queryAppAssistant(sc data.Scope, view schema.QueryIR, request appwire.AppViewQuery) (json.RawMessage, error) {
	ir := data.QueryIR{Type: view.Type, Group: view.Group, Select: view.Select}
	if view.Relate != nil {
		return nil, appwire.Errorf(appwire.CodeBadParams, "This view requires a trace projection.")
	}
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
		if len(view.Window.OrderBy) > 1 {
			return nil, appwire.Errorf(appwire.CodeBadParams, "This view needs multiple sort fields.")
		}
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

func appViewArgument(kind string, value any) bool {
	switch kind {
	case "text", "longtext", "enum", "date", "datetime", "ref", "email", "url", "phone", "person":
		_, ok := value.(string)
		return ok
	case "number", "money":
		_, ok := value.(float64)
		return ok
	case "bool":
		_, ok := value.(bool)
		return ok
	}
	return false
}

func (b *appBackend) invokeAppAssistant(sc data.Scope, app schema.App, def store.Definition, intent appwire.AppActionIntent) (json.RawMessage, error) {
	if !b.appAgentAllowed(sc.App, "actions", intent.Action) {
		return nil, appwire.Errorf(appwire.CodeForbidden, "This agent cannot invoke that action.")
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
			if fixed, ok := command.Params[name]; ok && !strings.HasPrefix(fmt.Sprint(fixed), "$selection.") && !strings.HasPrefix(fmt.Sprint(fixed), "$input.") {
				return nil, appwire.Errorf(appwire.CodeBadParams, "The app declares a fixed value for %q.", name)
			}
		}
		for name, value := range command.Params {
			if !strings.HasPrefix(fmt.Sprint(value), "$") {
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
		return marshalAppContent(engine.StartAgentAction(sc.App, command.Run, args, runs.AgentActionOrigin{Action: intent.Action, Version: def.Meta.Version, Cause: sc.Cause}))
	}
	if len(command.Do) == 1 && command.For != "" {
		update, ok := command.Do[0]["records.update"].(map[string]any)
		set, valid := update["set"].(map[string]any)
		id, _ := args["record"].(string)
		if ok && valid && update["ids"] == "$selection.ids" && len(args) == 1 && id != "" && intent.ExpectedRev != nil && *intent.ExpectedRev > 0 && *intent.ExpectedRev <= 1<<63-1 {
			for _, value := range set {
				if strings.HasPrefix(fmt.Sprint(value), "$") {
					return nil, appwire.Errorf(appwire.CodeBadParams, "This action needs additional inputs.")
				}
			}
			db, err := b.dataStore("")
			if err != nil {
				return nil, err
			}
			result, err := db.Update(sc, command.For, []string{id}, set, data.UpdateOptions{Rev: int64(*intent.ExpectedRev)})
			return marshalAppContent(result, appDataError(err))
		}
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
