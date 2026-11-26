package main

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/automation"
	"github.com/Agent-Field/codeaf/internal/session"
)

const appAgentAskRun = "_agent_ask"
const appAgentDutyPrefix = "_agent_duty_"

type savedAppDuty struct {
	appwire.AppAgentDuty
	Since time.Time `json:"since"`
}

type appAgentIdentity struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Harness string `json:"harness"`
}

func (b *appBackend) appAgentAllowed(app, group, id string) bool {
	allowed := false
	_ = b.state.read(func(f *appStateFile) { allowed = f.AppAgentGrants[app+"/"+group+"/"+id] })
	return allowed
}

func (b *appBackend) checkAppAgentAction(app string, origin runs.AgentActionOrigin) error {
	_, def, err := b.appAssistantDefinition(app)
	if err != nil {
		return err
	}
	if def.Meta.Version != origin.Version || !b.appAgentAllowed(app, "actions", origin.Action) {
		return appwire.Errorf(appwire.CodeForbidden, "The app or its agent grant changed.")
	}
	return nil
}

func (b *appBackend) GrantAppAgent(p appwire.AppAgentGrant) error {
	if _, _, err := b.appAssistantDefinition(p.App); err != nil {
		return err
	}
	if p.Agent != p.App || (p.Group != "actions" && p.Group != "data") || strings.TrimSpace(p.ID) == "" {
		return appwire.Errorf(appwire.CodeBadParams, "Choose a grant for this app’s agent.")
	}
	err := b.state.write(func(f *appStateFile) error {
		if f.AppAgentGrants == nil {
			f.AppAgentGrants = map[string]bool{}
		}
		f.AppAgentGrants[p.App+"/"+p.Group+"/"+p.ID] = p.Allowed
		return nil
	})
	if err == nil {
		b.emit.Emit(appwire.EventAppAgentChanged, appwire.AppAgentChanged{App: p.App, Kind: "grants"})
	}
	return err
}

func (b *appBackend) checkAppAgentAsk(p appwire.AppAgentAsk) error {
	_, def, err := b.appAssistantDefinition(p.App)
	if err != nil {
		return err
	}
	if _, ok := def.Screen(p.Screen); !ok || strings.TrimSpace(p.Text) == "" || len(p.Text) > 16000 {
		return appwire.Errorf(appwire.CodeBadParams, "Choose a screen and send a question.")
	}
	if p.Record != nil {
		if strings.TrimSpace(p.Record.Type) == "" || strings.TrimSpace(p.Record.ID) == "" {
			return appwire.Errorf(appwire.CodeBadParams, "The record needs a type and ID.")
		}
	}
	return nil
}

func (b *appBackend) AskAppAgent(p appwire.AppAgentAsk) (runs.Run, error) {
	if err := b.checkAppAgentAsk(p); err != nil {
		return runs.Run{}, err
	}
	if b.startAppRunsOnce != nil {
		b.startAppRunsOnce()
	}
	engine := b.appRuns.Load()
	if engine == nil {
		return runs.Run{}, appwire.Errorf(appwire.CodeEngine, "App actions are unavailable.")
	}
	return engine.Start(p.App, appAgentAskRun, appAgentRunParams(p))
}

func appAgentRunParams(p appwire.AppAgentAsk) map[string]any {
	params := map[string]any{"text": p.Text, "screen": p.Screen, "type": "", "record": ""}
	if p.Record != nil {
		params["type"], params["record"] = p.Record.Type, p.Record.ID
	}
	return params
}

func (b *appBackend) AddAppAgentDuty(p appwire.AppAgentDuty) (string, error) {
	if err := b.checkAppAgentAsk(p.AppAgentAsk); err != nil {
		return "", err
	}
	schedule := automation.Schedule{Every: p.Cron}
	next, err := schedule.Next(b.now())
	if err != nil || next.IsZero() || schedule.Interval() != 0 {
		return "", appwire.Errorf(appwire.CodeBadParams, "A standing duty needs a five-field cron schedule.")
	}
	id := appAgentDutyPrefix + session.NewSessionID()
	err = b.state.write(func(f *appStateFile) error {
		if f.AppAgentDuties == nil {
			f.AppAgentDuties = map[string]savedAppDuty{}
		}
		f.AppAgentDuties[id] = savedAppDuty{AppAgentDuty: p, Since: b.now()}
		return nil
	})
	if err == nil && b.startAppRunsOnce != nil {
		b.startAppRunsOnce()
	}
	if err == nil {
		b.emit.Emit(appwire.EventAppAgentChanged, appwire.AppAgentChanged{App: p.App, Kind: "duties"})
	}
	return id, err
}

func (b *appBackend) RemoveAppAgentDuty(p appwire.AppAgentDutyRef) error {
	if strings.TrimSpace(p.App) == "" || !strings.HasPrefix(p.ID, appAgentDutyPrefix) {
		return appwire.Errorf(appwire.CodeBadParams, "Choose an app and a standing duty.")
	}
	err := b.state.write(func(f *appStateFile) error {
		duty, ok := f.AppAgentDuties[p.ID]
		if !ok || duty.App != p.App {
			return appwire.Errorf(appwire.CodeNotFound, "That standing duty could not be found.")
		}
		delete(f.AppAgentDuties, p.ID)
		return nil
	})
	if err == nil {
		b.emit.Emit(appwire.EventAppAgentChanged, appwire.AppAgentChanged{App: p.App, Kind: "duties"})
	}
	return err
}

func (b *appBackend) loadAppAgentRun(files runs.Files, app, id string) (runs.Definition, error) {
	if id != appAgentAskRun && !strings.HasPrefix(id, appAgentDutyPrefix) {
		return files.Load(app, id)
	}
	if _, _, err := b.appAssistantDefinition(app); err != nil {
		return runs.Definition{}, err
	}
	params := appAgentRunParams(appwire.AppAgentAsk{})
	if id != appAgentAskRun {
		var duty savedAppDuty
		if err := b.state.read(func(f *appStateFile) { duty = f.AppAgentDuties[id] }); err != nil {
			return runs.Definition{}, err
		}
		if duty.App != app {
			return runs.Definition{}, runs.ErrNotFound
		}
		params = appAgentRunParams(duty.AppAgentAsk)
	}
	declared := map[string]runs.Parameter{}
	for key, value := range params {
		declared[key] = runs.Parameter{Type: "text", Default: value}
	}
	return runs.Definition{Run: id, Title: "App agent", Params: declared, Steps: []runs.Step{{ID: "answer", Agent: "app", Prompt: "$params.text", Args: map[string]any{"screen": "$params.screen", "type": "$params.type", "record": "$params.record"}}}}, nil
}

func (b *appBackend) appAgentTriggers(files runs.Files) ([]runs.Trigger, error) {
	triggers, err := files.Triggers()
	if err != nil {
		return nil, err
	}
	var duties map[string]savedAppDuty
	if err := b.state.read(func(f *appStateFile) { duties = maps.Clone(f.AppAgentDuties) }); err != nil {
		return nil, err
	}
	triggers = slices.DeleteFunc(triggers, func(t runs.Trigger) bool {
		return t.Run == appAgentAskRun || strings.HasPrefix(t.Run, appAgentDutyPrefix)
	})
	for id, duty := range duties {
		if _, _, err := b.appAssistantDefinition(duty.App); err == nil {
			triggers = append(triggers, runs.Trigger{App: duty.App, ID: id, Run: id, On: "schedule", Cron: duty.Cron, Since: duty.Since})
		}
	}
	return triggers, nil
}

// QueryAppAgent exposes an app's own agent work with the desk's projection.
func (b *appBackend) QueryAppAgent(p appwire.AppAgentQuery) (any, error) {
	_, def, err := b.appAssistantDefinition(p.App)
	if err != nil {
		return nil, err
	}
	identity := appAgentIdentity{ID: p.App, Name: def.Meta.Name + " agent", Harness: "codeaf"}
	switch p.Kind {
	case "runs":
		rows := []any{}
		engine := b.appRuns.Load()
		if engine == nil {
			return rows, nil
		}
		list, err := engine.List(p.App, "")
		if err != nil {
			return nil, err
		}
		for _, run := range list {
			if run.DefinitionID != appAgentAskRun && !strings.HasPrefix(run.DefinitionID, appAgentDutyPrefix) {
				continue
			}
			params, output, err := engine.AgentProjection(run.ID)
			if err != nil {
				return nil, err
			}
			screen, _ := params["screen"].(string)
			record := appwire.DataRecordRef{}
			record.Type, _ = params["type"].(string)
			record.ID, _ = params["record"].(string)
			if (p.Screen != "" && p.Screen != screen) || (p.Record != nil && *p.Record != record) {
				continue
			}
			row := map[string]any{"id": run.ID, "screen": screen, "agent": identity, "title": "App agent", "state": run.State, "steps": []any{map[string]string{"id": "answer", "label": "Answer", "state": run.State}}, "output": output}
			if record.ID != "" {
				row["record"] = record
			}
			rows = append(rows, row)
		}
		return rows, nil
	case "grants":
		type permission struct {
			ID      string `json:"id"`
			Label   string `json:"label"`
			Allowed bool   `json:"allowed"`
		}
		groups := map[string][]permission{"actions": {}, "data": {}}
		raw, err := b.RunAppContent(context.Background(), appwire.AppContentScope{App: p.App, Version: def.Meta.Version}, appwire.AppContentCommand{Verb: appwire.AppContentShow})
		if err != nil {
			return nil, err
		}
		var shown struct {
			Views   map[string]json.RawMessage
			Actions []struct{ ID, Label string }
		}
		if err := json.Unmarshal(raw, &shown); err != nil {
			return nil, err
		}
		for _, id := range slices.Sorted(maps.Keys(shown.Views)) {
			groups["data"] = append(groups["data"], permission{id, id, b.appAgentAllowed(p.App, "data", id)})
		}
		for _, a := range shown.Actions {
			groups["actions"] = append(groups["actions"], permission{a.ID, a.Label, b.appAgentAllowed(p.App, "actions", a.ID)})
		}
		groups["actions"] = append(groups["actions"], permission{"propose_change", "Propose changes", b.appAgentAllowed(p.App, "actions", "propose_change")})
		return []any{map[string]any{"agent": identity, "actions": groups["actions"], "data": groups["data"]}}, nil
	case "duties":
		type listedDuty struct {
			ID string `json:"id"`
			appwire.AppAgentDuty
		}
		duties := []listedDuty{}
		err := b.state.read(func(f *appStateFile) {
			for _, id := range slices.Sorted(maps.Keys(f.AppAgentDuties)) {
				if duty := f.AppAgentDuties[id]; duty.App == p.App {
					duties = append(duties, listedDuty{ID: id, AppAgentDuty: duty.AppAgentDuty})
				}
			}
		})
		return duties, err
	default:
		return nil, appwire.Errorf(appwire.CodeBadParams, "Unknown agent projection.")
	}
}
