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
	Write    string              `json:"-"`
	Set      map[string]any      `json:"-"`
	Inputs   map[string]string   `json:"-"`
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
		} else if registered, ok := registerRecordCommand(app, command, effect); ok {
			registry = append(registry, registered)
		}
	}
	for _, id := range slices.Sorted(maps.Keys(runs)) {
		run := runs[id]
		if wrapped[id] || strings.HasPrefix(id, "_") || !writesRecords(run.Steps) {
			continue
		}
		label := run.Title
		if label == "" {
			label = strings.ReplaceAll(id, "-", " ")
		}
		if commands[id] {
			id = "run/" + id
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
		count := 0
		for _, parameter := range run.Params {
			if parameter.Type == "ref" {
				record = parameter.To
				count++
			}
		}
		if count != 1 {
			record = ""
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

func registerRecordCommand(app App, command Command, effect Action) (RegisteredAction, bool) {
	for _, verb := range []string{"records.create", "records.update", "records.delete"} {
		write, ok := effect[verb].(map[string]any)
		if !ok {
			continue
		}
		record, _ := write["type"].(string)
		if record == "" {
			record = command.For
		}
		if verb != "records.create" {
			target, _ := write["ids"].(string)
			if target == "" {
				target, _ = write["id"].(string)
			}
			if record == "" || (command.For != "" && command.For != record) || !commandSelectsRecord(target) {
				return RegisteredAction{}, false
			}
		}
		action := recordAction(command.ID, command.Label, record)
		action.Write = verb
		if verb == "records.create" {
			action.Args = map[string]RunParam{}
			action.Revision = false
		}
		action.Set, _ = write["set"].(map[string]any)
		action.Inputs = map[string]string{}
		ask, _ := effect["ask"].(map[string]any)
		for field, value := range action.Set {
			text, _ := value.(string)
			if !strings.HasPrefix(text, "$") {
				continue
			}
			input, bound := strings.CutPrefix(text, "$input.")
			kind, asked := ask[input].(string)
			if !bound || !asked || action.Revision && input == "record" {
				return RegisteredAction{}, false
			}
			shape := app.Records[record].Fields[field]
			param := RunParam{Type: shape.Type, To: shape.To}
			if param.Type == "" {
				param.Type = kind
			}
			if prior, ok := action.Args[input]; ok && (prior.Type != param.Type || prior.To != param.To) {
				return RegisteredAction{}, false
			}
			action.Args[input] = param
			action.Inputs[field] = input
		}
		return action, true
	}
	return RegisteredAction{}, false
}

func commandSelectsRecord(target string) bool {
	return target == "$selection.ids" || target == "$record.id" || target == "$row.id" ||
		strings.HasPrefix(target, "$selection.") && strings.HasSuffix(target, ".id")
}

func writesRecords(steps []Step) bool {
	return slices.ContainsFunc(steps, func(step Step) bool {
		return step.RecordsUpdate != nil || step.RecordsCreate != nil || writesRecords(step.Steps)
	})
}
