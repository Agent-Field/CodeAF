package check

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

// Check the authored controls before exercising data or asking Desk to render.
func missingControls(def schema.Definition, types schema.TypeSet) []schema.Problem {
	app, _ := schema.ParseApp(def.Files[schema.AppFile])
	plan, _ := schema.ParsePlan(def.Files[schema.PlanFile])
	controls := screenControls{
		commands: map[string]bool{}, creates: map[string]bool{}, runs: map[string]bool{},
		app: app, types: types,
	}
	for _, file := range def.ScreenFiles() {
		screen, _ := schema.ParseScreen(def.Files[file])
		controls.scan(screen.Nav, screen.Body)
	}
	var issues []schema.Problem
	exposed := map[string]bool{}
	directRuns := maps.Clone(controls.runs)
	for _, command := range app.Commands {
		visible := controls.commands[command.ID] || (command.Run != "" && directRuns[command.Run])
		// Inline actions expose a command only when they perform its same write or action sequence.
		for _, actions := range controls.actions {
			if len(command.Do) > 0 && reflect.DeepEqual(command.Do, actions) {
				visible = true
				break
			}
		}
		if visible {
			exposed[command.ID] = true
			controls.collect(command.Do)
			if command.Run != "" {
				controls.runs[command.Run] = true
			}
			continue
		}
		issues = append(issues, problem(schema.AppFile, "command_control_missing",
			fmt.Sprintf("Add a control for %q (command %s) on a screen.", command.Label, command.ID)))
	}
	for _, action := range plan.Actions {
		if !exposed[action.ID] && !controls.runs[action.ID] {
			issues = append(issues, problem(schema.PlanFile, "action_control_missing",
				fmt.Sprintf("Add a control for %q (action %s) on a screen.", action.Line, action.ID)))
		}
	}
	for _, line := range plan.Data {
		if !line.Create || controls.creates[line.ID] {
			continue
		}
		label := app.Records[line.ID].Label
		if label == "" {
			label = strings.NewReplacer("_", " ", "-", " ").Replace(line.ID)
		}
		issues = append(issues, problem(schema.PlanFile, "create_control_missing",
			fmt.Sprintf("Add an %q button or a create form for %s on a screen.", "Add "+strings.ToLower(label), line.ID)))
	}
	return issues
}

type screenControls struct {
	commands, creates, runs map[string]bool
	actions                 [][]schema.Action
	app                     schema.App
	types                   schema.TypeSet
}

func (c *screenControls) scan(roots ...*schema.Node) {
	// A Sheet starts closed. Its controls count once an exposed control can open it.
	sheets := map[string]*schema.Node{}
	opened := map[string]bool{}
	var visit func(*schema.Node)
	visit = func(node *schema.Node) {
		if node == nil || strings.TrimSpace(node.When) == "false" {
			return
		}
		if node.Part == "Sheet" && !opened[node.ID] {
			sheets[node.ID] = node
			return
		}
		switch node.Part {
		case "RecordPage":
			if node.Bind != nil && node.Bind.Record != "" {
				ids, _ := node.Props["actions"].([]any)
				for _, id := range ids {
					if id, ok := id.(string); ok {
						c.commands[id] = true
					}
				}
			}
		case "RecordForm":
			if node.Bind == nil || node.Bind.Record == "" {
				typ, _ := node.Props["type"].(string)
				if typ == "" && node.Bind != nil {
					typ = node.Bind.Type
					if typ == "" {
						typ = node.Bind.Records
					}
				}
				fields, selected := node.Props["fields"].([]any)
				if c.canCreate(typ, func(field string) bool { return !selected || slices.Contains(fields, any(field)) }) {
					c.creates[typ] = true
				}
			}
		}
		for _, actions := range node.On {
			c.collect(actions)
			c.actions = append(c.actions, actions)
			for _, a := range actions {
				if id, ok := a["open"].(string); ok {
					opened[id] = true
				}
			}
		}
		for _, child := range node.Children {
			visit(child)
		}
	}
	for _, root := range roots {
		visit(root)
	}
	for {
		visited := false
		for id, sheet := range sheets {
			if opened[id] {
				delete(sheets, id)
				visit(sheet)
				visited = true
			}
		}
		if !visited {
			return
		}
	}
}

func (c screenControls) collect(actions []schema.Action) {
	for _, action := range actions {
		if run, ok := action["runs.start"].(string); ok {
			c.runs[run] = true
		}
		if set, ok := action["records.create"].(map[string]any); ok {
			typ, _ := set["type"].(string)
			values, _ := set["set"].(map[string]any)
			if c.canCreate(typ, func(field string) bool {
				value := values[field]
				return value != nil && value != ""
			}) {
				c.creates[typ] = true
			}
		}
	}
}

// A click that omits required fields cannot create even when its verb is valid.
func (c screenControls) canCreate(typ string, supplies func(string) bool) bool {
	if c.app.Capabilities.Records[typ] != "rw" {
		return false
	}
	record := c.app.Records[typ]
	fields := maps.Clone(record.Fields)
	if record.Uses {
		if owner, ok := c.types.ForRecord(typ, record); ok {
			fields = maps.Clone(owner.Fields)
			if fields == nil {
				fields = map[string]schema.Field{}
			}
			maps.Copy(fields, record.Fields)
		}
	}
	var managed string
	if c.app.Process != nil {
		managed = c.app.Process.Lifecycles[typ].Field
	}
	if record.Lifecycle != nil {
		managed = record.Lifecycle.Field
	}
	for name, field := range fields {
		if name != managed && field.Required && field.Default == nil && field.Computed == "" && field.AI == "" && !supplies(name) {
			return false
		}
	}
	return true
}
