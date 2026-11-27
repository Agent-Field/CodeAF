package builder

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

// reconcileRunLinks removes entries for files the author never wrote. A
// missing automation has no behavior to infer safely from its label.
func reconcileRunLinks(files map[string]string) ([]schema.Problem, error) {
	def := definition(files)
	app, appErr := schema.ParseApp(def.Files[schema.AppFile])
	plan, planErr := schema.ParsePlan(def.Files[schema.PlanFile])
	if appErr != nil || planErr != nil || app.Kit != 2 || plan.Ask == "" {
		return nil, nil
	}
	have := map[string]bool{}
	var added []schema.PlanLine
	for _, path := range def.RunFiles() {
		id := schema.FileID(path)
		have[id] = true
		if !hasPlanLine(plan.Automation, id) {
			if run, err := schema.ParseRun(def.Files[path]); err == nil && run.Run == id {
				added = append(added, schema.PlanLine{ID: id, Line: displayName(run.Title, id)})
			}
		}
	}
	missing := map[string]bool{}
	dangling := func(id string) bool {
		if id == "" || have[id] {
			return false
		}
		missing[id] = true
		return true
	}
	var manifest, outline map[string]json.RawMessage
	if err := json.Unmarshal(def.Files[schema.AppFile], &manifest); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(def.Files[schema.PlanFile], &outline); err != nil {
		return nil, err
	}
	// Filter raw entries so an unrelated malformed declaration still goes
	// through Check with all of its original settings.
	if err := filterDraftList(manifest, "triggers", "run", dangling); err != nil {
		return nil, err
	}
	removedCommands := map[string]bool{}
	for _, cmd := range app.Commands {
		if dangling(cmd.Run) {
			removedCommands[cmd.ID] = true
		}
	}
	if err := filterDraftList(manifest, "commands", "id", func(id string) bool { return removedCommands[id] }); err != nil {
		return nil, err
	}
	if err := filterDraftList(outline, "automation", "id", dangling); err != nil {
		return nil, err
	}
	if len(added) > 0 {
		lines := slices.DeleteFunc(plan.Automation, func(line schema.PlanLine) bool { return missing[line.ID] })
		outline["automation"], _ = json.Marshal(append(lines, added...))
	}
	if len(missing) == 0 && len(added) == 0 {
		return nil, nil
	}

	orphan := func(id string) bool {
		if !missing[id] && !removedCommands[id] {
			return false
		}
		if _, ok := app.Outputs[id]; ok {
			return false
		}
		return !slices.ContainsFunc(app.Commands, func(cmd schema.Command) bool {
			return cmd.ID == id && !removedCommands[id]
		}) && !slices.ContainsFunc(plan.Builtins, func(b schema.PlanBuiltin) bool {
			return b.ID == id && !missing[b.Run]
		})
	}
	if err := filterDraftList(outline, "builtins", "run", func(id string) bool { return missing[id] }); err != nil {
		return nil, err
	}
	if err := filterDraftList(outline, "actions", "id", func(id string) bool {
		return orphan(id) || slices.ContainsFunc(plan.Builtins, func(b schema.PlanBuiltin) bool { return b.ID == id && missing[b.Run] })
	}); err != nil {
		return nil, err
	}
	if plan.Agent != nil {
		actions := slices.DeleteFunc(plan.Agent.Actions, orphan)
		if len(actions) != len(plan.Agent.Actions) {
			var agent map[string]json.RawMessage
			if err := json.Unmarshal(outline["agent"], &agent); err != nil {
				return nil, err
			}
			agent["actions"], _ = json.Marshal(actions)
			outline["agent"], _ = json.Marshal(agent)
		}
	}
	if len(missing) != 0 {
		if err := saveJSON(files, schema.AppFile, manifest); err != nil {
			return nil, err
		}
	}
	if err := saveJSON(files, schema.PlanFile, outline); err != nil {
		return nil, err
	}
	for _, path := range def.ScreenFiles() {
		if len(missing) == 0 {
			break
		}
		var screen map[string]any
		if json.Unmarshal(def.Files[path], &screen) == nil && removeMissingRunActions(screen, missing) {
			if err := saveJSON(files, path, screen); err != nil {
				return nil, err
			}
		}
	}

	var notes []schema.Problem
	for _, id := range slices.Sorted(maps.Keys(missing)) {
		notes = append(notes, schema.Problem{File: "runs/" + id + ".yaml", Code: normalizationNote,
			Message: fmt.Sprintf("Left %s off the plan because the automation was never written.", displayName(id, "the automation"))})
	}
	return notes, nil
}

func filterDraftList(doc map[string]json.RawMessage, key, field string, remove func(string) bool) error {
	var rows []json.RawMessage
	if len(doc[key]) == 0 {
		return nil
	}
	if err := json.Unmarshal(doc[key], &rows); err != nil {
		return err
	}
	kept := slices.DeleteFunc(slices.Clone(rows), func(raw json.RawMessage) bool {
		var row map[string]json.RawMessage
		var id string
		return json.Unmarshal(raw, &row) == nil && json.Unmarshal(row[field], &id) == nil && remove(id)
	})
	if len(kept) == 0 && len(rows) != 0 {
		delete(doc, key)
	} else if len(kept) < len(rows) {
		doc[key], _ = json.Marshal(kept)
	}
	return nil
}

func removeMissingRunActions(value map[string]any, missing map[string]bool) bool {
	changed := false
	for key, child := range value {
		switch child := child.(type) {
		case map[string]any:
			changed = removeMissingRunActions(child, missing) || changed
		case []any:
			kept := child[:0]
			for _, entry := range child {
				if object, ok := entry.(map[string]any); ok {
					if id, _ := object["runs.start"].(string); missing[id] {
						changed = true
						continue
					}
					nodeChanged := removeMissingRunActions(object, missing)
					changed = changed || nodeChanged
					if nodeChanged && object["part"] == "Button" {
						if on, ok := object["on"].(map[string]any); ok && len(on) == 0 {
							changed = true
							continue
						}
					}
				}
				kept = append(kept, entry)
			}
			if len(kept) == 0 && len(child) != 0 {
				delete(value, key)
			} else {
				value[key] = kept
			}
		}
	}
	return changed
}
