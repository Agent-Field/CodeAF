package schema

import (
	"encoding/json"
	"regexp"
	"strings"
)

const (
	CodeTriggerInput = "trigger_input"
	TriggerRecordID  = "$input.record.id"
)

// RunEntry describes the inputs available to an automatic invocation. A
// record source is usable only if every automatic entry supplies its type.
type RunEntry struct {
	Automatic bool
	Record    string
}

func (a App) RunEntry(id string) RunEntry {
	var entry RunEntry
	for _, trigger := range a.Triggers {
		if trigger.Run != id {
			continue
		}
		switch trigger.On {
		case "record.changed", "record.before", "intake.arrived":
			if trigger.Type == "" || entry.Automatic && entry.Record != trigger.Type {
				return RunEntry{Automatic: true}
			}
			entry.Record = trigger.Type
		default:
			return RunEntry{Automatic: true}
		}
		entry.Automatic = true
	}
	return entry
}

// FromTriggerRecord marks a reference input that an arrival can supply. A
// manual invocation still supplies its own record ID in the usual parameter.
func (p RunParam) FromTriggerRecord() bool {
	return p.Type == "ref" && p.Default == TriggerRecordID
}

var automaticParamReference = regexp.MustCompile(`(?:\$|\{\{\s*|^)params\.([a-zA-Z0-9_-]+)\b`)

// BindRunTrigger is the compiler's lowering of one clearly typed arrival
// record. Preserve parameters already supplied by a query or literal default.
func BindRunTrigger(app App, raw []byte) ([]byte, bool, error) {
	run, err := ParseRun(raw)
	if err != nil {
		return raw, false, err
	}
	entry := app.RunEntry(run.Run)
	if entry.Record == "" {
		return raw, false, nil
	}
	name := ""
	for key, param := range run.Params {
		if param.Type == "ref" && param.To == entry.Record && param.Default == nil {
			if name != "" {
				return raw, false, nil
			}
			name = key
		}
	}
	if name == "" {
		return raw, false, nil
	}
	encoded, err := RunJSON(raw)
	if err != nil {
		return raw, false, err
	}
	doc := object(encoded)
	params, _ := doc["params"].(map[string]any)
	param, _ := params[name].(map[string]any)
	param["default"] = TriggerRecordID
	result, err := json.MarshalIndent(doc, "", "  ")
	return result, err == nil, err
}

func (c *checker) checkAutomaticInputs(file string, run Run, raw any) {
	entry := c.app.RunEntry(run.Run)
	for _, name := range sortedKeys(run.Params) {
		param := run.Params[name]
		if param.FromTriggerRecord() && (entry.Record == "" || entry.Record != param.To) {
			c.add(file, "/params/"+pointerToken.Replace(name)+"/default", CodeTriggerInput,
				"The triggering record must be a %s on every automatic entry to use this default.", param.To)
		}
	}
	if entry.Record == "" {
		return
	}
	walkStrings(raw, "", func(path, value string) {
		if !strings.HasPrefix(path, "/steps/") {
			return
		}
		found := map[string]bool{}
		for _, ref := range automaticParamReference.FindAllStringSubmatch(value, -1) {
			name := ref[1]
			if found[name] || run.Params[name].Default != nil {
				continue
			}
			found[name] = true
			param := run.Params[name]
			if param.Type == "ref" && param.To == entry.Record {
				c.add(file, path, CodeTriggerInput, "The %s trigger supplies its record through $input.record. Read that record here or set the %s reference default to %s for both automatic and manual use.", entry.Record, name, TriggerRecordID)
			} else {
				c.add(file, path, CodeTriggerInput, "The %s trigger does not supply the manual input %s. Give that input a default or read a value from $input.record.", entry.Record, name)
			}
		}
	})
}
