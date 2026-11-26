package schema

import (
	"maps"
	"slices"
	"strings"
)

// RegisteredAction is the agent-facing shape and dispatch binding for an app Action.
type RegisteredAction struct {
	ID       string              `json:"id"`
	Label    string              `json:"label"`
	Record   string              `json:"record_type,omitempty"`
	Args     map[string]RunParam `json:"args"`
	Revision bool                `json:"requires_expected_rev,omitempty"`
	Run      *Run                `json:"-"`
	Fixed    map[string]any      `json:"-"`
	Set      map[string]any      `json:"-"`
}

// ActionRegistry uses declarations that also drive the app's controls and runs.
// A named command takes precedence over the bare run it launches.
func ActionRegistry(app App, runs map[string]Run) []RegisteredAction {
	registry := []RegisteredAction{}
	commands, wrapped := map[string]bool{}, map[string]bool{}
	for _, command := range app.Commands {
		commands[command.ID] = true
		id, params := command.Run, command.Params
		effect := commandEffect(command.Do)
		if id == "" {
			id, _ = effect["runs.start"].(string)
			params, _ = effect["params"].(map[string]any)
		}
		if run, ok := runs[id]; ok {
			registered := registerRun(run, command.ID, command.Label, command.For)
			for key, value := range params {
				if text, ok := value.(string); ok && strings.HasPrefix(text, "$") {
					// UI bindings become typed arguments the caller must supply.
					continue
				}
				registered.Fixed[key] = value
				delete(registered.Args, key)
			}
			registry = append(registry, registered)
			wrapped[id] = true
		} else if set, ok := literalRecordUpdate(command.For, effect); ok {
			registered := recordAction(command.ID, command.Label, command.For)
			registered.Set = set
			registry = append(registry, registered)
		}
	}
	for _, id := range slices.Sorted(maps.Keys(runs)) {
		run := runs[id]
		if wrapped[id] || commands[id] || strings.HasPrefix(id, "_") || !writesRecords(run.Steps) {
			continue
		}
		label := run.Title
		if label == "" {
			label = strings.ReplaceAll(id, "-", " ")
		}
		registry = append(registry, registerRun(run, id, label, ""))
	}
	for _, id := range slices.Sorted(maps.Keys(app.Outputs)) {
		template := app.Outputs[id]
		args := map[string]RunParam{}
		if template.Record != "" {
			args["record"] = RunParam{Type: "ref", To: template.Record}
		}
		registry = append(registry, RegisteredAction{ID: "render/" + id, Label: template.Label, Record: template.Record, Args: args})
	}
	for _, typ := range slices.Sorted(maps.Keys(app.Records)) {
		if lifecycle := app.Records[typ].Lifecycle; lifecycle != nil {
			for _, transition := range lifecycle.Transitions {
				registry = append(registry, recordAction("transition/"+typ+"/"+transition.ID, transition.Label, typ))
			}
		}
	}
	return registry
}

func registerRun(run Run, id, label, record string) RegisteredAction {
	args := map[string]RunParam{}
	maps.Copy(args, run.Params)
	if record == "" {
		// A single record input supplies the action's record type.
		for _, parameter := range run.Params {
			if parameter.Type == "ref" {
				if record != "" {
					record = ""
					break
				}
				record = parameter.To
			}
		}
	}
	return RegisteredAction{ID: id, Label: label, Record: record, Args: args, Run: &run, Fixed: map[string]any{}}
}

func recordAction(id, label, typ string) RegisteredAction {
	return RegisteredAction{ID: id, Label: label, Record: typ, Revision: true,
		Args: map[string]RunParam{"record": {Type: "ref", To: typ}}}
}

// UI feedback can accompany a command without changing the effect it grants.
func commandEffect(actions []Action) Action {
	var effect Action
	for _, action := range actions {
		if _, toast := action["toast"]; toast {
			continue
		}
		if effect != nil {
			return nil
		}
		effect = action
	}
	return effect
}

func literalRecordUpdate(record string, effect Action) (map[string]any, bool) {
	if record == "" || effect["ask"] != nil {
		return nil, false
	}
	update, _ := effect["records.update"].(map[string]any)
	set, ok := update["set"].(map[string]any)
	if !ok || update["ids"] != "$selection.ids" || (update["type"] != nil && update["type"] != record) {
		return nil, false
	}
	for _, value := range set {
		if text, ok := value.(string); ok && strings.HasPrefix(text, "$") {
			return nil, false
		}
	}
	return set, true
}

func writesRecords(steps []Step) bool {
	return slices.ContainsFunc(steps, func(step Step) bool {
		return step.RecordsUpdate != nil || step.RecordsCreate != nil || writesRecords(step.Steps)
	})
}
