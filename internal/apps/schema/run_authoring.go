package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// CheckRunStepShapes checks each written step against the same Kit registry
// as Check. Capabilities and references can be authored in any order.
func CheckRunStepShapes(file string, raw []byte) error {
	run, err := RunJSON(raw)
	if err != nil {
		return err
	}
	c := checker{kit: schemas().kits[2]}
	entries := c.kit.Registries.Steps
	steps, _ := object(run)["steps"].([]any)
	return walkAuthoredSteps(steps, "/steps", func(step map[string]any, at string) error {
		c.checkEffect(actionCtx{file: file}, at, "step", step, stepModifiers, entries, false, false)
		if len(c.report.Problems) == 0 {
			return nil
		}
		problem := c.report.Problems[0]
		kind := "agent"
		for _, key := range sortedKeys(step) {
			switch {
			case len(entries[key].Example) != 0:
				kind = key
			case strings.HasPrefix(key, "records."):
				kind = "loop"
			}
			if kind != "agent" || key == "agent" {
				break
			}
		}
		var example bytes.Buffer
		if kind == "loop" {
			// This Kit example shows a scoped record query as a refs input.
			return fmt.Errorf("%s%s: %s Kit example: %s", file, problem.Path, problem.Message,
				`{"loop":{"over":"$params.deals","as":"deal","max":25}} (declare deals as a refs input with a query default)`)
		}
		_ = json.Compact(&example, entries[kind].Example)
		return fmt.Errorf("%s%s: %s Kit example: %s", file, problem.Path, problem.Message, example.String())
	})
}

// RewriteRunStepAliases preserves the operation and payload when a familiar
// name has one equivalent in the Kit. Unknown operations remain errors.
func RewriteRunStepAliases(raw []byte) ([]byte, error) {
	run, err := RunJSON(raw)
	if err != nil {
		return nil, err
	}
	doc := object(run)
	steps, _ := doc["steps"].([]any)
	changed := false
	_ = walkAuthoredSteps(steps, "/steps", func(step map[string]any, _ string) error {
		for _, alias := range []struct{ from, to string }{
			{"records.insert", "records.create"},
			{"records.patch", "records.update"},
		} {
			if write, ok := step[alias.from].(map[string]any); ok && step[alias.to] == nil && write["set"] != nil {
				step[alias.to] = write
				delete(step, alias.from)
				changed = true
			}
		}
		if write, ok := step["records.update"].(map[string]any); ok && write["id"] == nil && write["ids"] == nil {
			if id, ok := write["record"].(string); ok && id != "" {
				write["id"] = id
				delete(write, "record")
				changed = true
			}
		}
		return nil
	})
	if !changed {
		return raw, nil
	}
	return json.MarshalIndent(doc, "", "  ")
}

func walkAuthoredSteps(steps []any, at string, visit func(map[string]any, string) error) error {
	for i, child := range steps {
		path := fmt.Sprintf("%s/%d", at, i)
		step, _ := child.(map[string]any)
		if err := visit(step, path); err != nil {
			return err
		}
		children, _ := step["steps"].([]any)
		if err := walkAuthoredSteps(children, path+"/steps", visit); err != nil {
			return err
		}
	}
	return nil
}
