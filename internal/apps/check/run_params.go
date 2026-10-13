package check

import (
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func runParams(run *spec.Run, rows map[string][]bridge.Record) map[string]any {
	params := map[string]any{}
	for name, param := range run.Params {
		params[name] = sampleParam(param, rows)
	}
	return params
}

func sampleParam(param *spec.RunParam, rows map[string][]bridge.Record) any {
	if param.Type == "ref" || param.Type == "refs" {
		ids := []string{}
		for _, row := range rows[param.To] {
			ids = append(ids, row.ID)
		}
		if param.Type == "refs" {
			return ids
		}
		if len(ids) > 0 {
			return ids[0]
		}
		return "sample-" + param.To
	}
	if param.Default != nil {
		value, err := resolveValue(param.Default, map[string]any{"me": svc.Owner, "now": "2026-10-13T12:00:00Z"})
		if err == nil {
			return value
		}
	}
	switch param.Type {
	case "number":
		return float64(1)
	case "bool":
		return true
	case "date":
		return "2026-10-13"
	case "person":
		return svc.Owner
	case "enum":
		if len(param.Values) > 0 {
			return param.Values[0]
		}
	}
	return "sample"
}

// Check invokes runs by hand, so it must recreate the record context normally
// supplied by a record trigger before resolving that trigger's parameter map.
func behaviourParams(b *spec.Bundle, id string, rows map[string][]bridge.Record) (map[string]any, error) {
	params := runParams(b.Runs[id], rows)
	for _, trigger := range b.App.Triggers {
		if trigger.Run != id || !strings.HasPrefix(string(trigger.On), "record.") {
			continue
		}
		if len(rows[trigger.Type]) == 0 {
			return nil, fmt.Errorf("trigger %s needs a sample record of type %s", trigger.ID, trigger.Type)
		}
		row := rows[trigger.Type][0]
		record := map[string]any{"id": row.ID, "type": trigger.Type, "fields": row.Fields}
		env := map[string]any{"me": svc.Owner, "record": record, "id": row.ID, "now": "2026-10-13T12:00:00Z"}
		for field, value := range row.Fields {
			record[field] = value
			env[field] = value
		}
		resolved, err := resolveValue(trigger.Params, env)
		if err != nil {
			return nil, fmt.Errorf("trigger %s params: %w", trigger.ID, err)
		}
		values, _ := resolved.(map[string]any)
		for name, value := range values {
			if param := b.Runs[id].Params[name]; param != nil && param.Type == "refs" {
				if items, ok := value.([]any); ok {
					ids := make([]string, len(items))
					for i, item := range items {
						ids[i] = fmt.Sprint(item)
					}
					value = ids
				}
			}
			params[name] = value
		}
		break
	}
	return params, nil
}
