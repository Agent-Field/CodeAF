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
		app: app, types: types, collections: map[string]bool{},
	}
	for _, file := range def.ScreenFiles() {
		screen, _ := schema.ParseScreen(def.Files[file])
		controls.scan(file, screen.Nav, screen.Body)
	}
	issues := controls.issues
	exposed := map[string]bool{}
	directRuns := maps.Clone(controls.runs)
	for _, command := range app.Commands {
		visible := controls.commands[command.ID] || (command.Run != "" && directRuns[command.Run]) ||
			(app.Kit == 2 && command.For != "" && controls.collections[command.For])
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
	collections             map[string]bool
	issues                  []schema.Problem
	app                     schema.App
	types                   schema.TypeSet
}

func (c *screenControls) scan(file string, roots ...*schema.Node) {
	// A Sheet starts closed. Its controls count once an exposed control can open it.
	sheets := map[string]*schema.Node{}
	opened := map[string]bool{}
	var visit func(*schema.Node, bool)
	visit = func(node *schema.Node, inRecord bool) {
		if node == nil || strings.TrimSpace(node.When) == "false" {
			return
		}
		if node.Part == "Sheet" && !opened[node.ID] {
			sheets[node.ID] = node
			return
		}
		switch node.Part {
		case "RecordList", "Board", "Table":
			if node.Bind != nil {
				c.collections[node.Bind.Records] = true
			}
		case "RecordPage":
			if node.Bind != nil && node.Bind.Type != "" && node.Bind.Record != "" {
				c.collections[node.Bind.Type] = true
			}
			if node.Bind != nil && node.Bind.Record != "" {
				ids, _ := node.Props["actions"].([]any)
				for _, id := range ids {
					if id, ok := id.(string); ok {
						c.commands[id] = true
					}
				}
			}
		case "Form", "RecordForm":
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
		inRecord = inRecord || node.Part == "RecordPage" || node.Bind != nil && node.Bind.Record != ""
		for _, actions := range node.On {
			c.collect(actions)
			if c.app.Kit == 2 && node.Part == "Button" && !inRecord {
				for _, action := range actions {
					run, ok := action["runs.start"].(string)
					if ok && selectionParameter(action["params"]) {
						c.issues = append(c.issues, schema.Problem{File: file, Path: node.ID, Code: "record_command_placement",
							Message: fmt.Sprintf("Put %q on the record. Declare a command with for set to the record type and bind the run parameter to $record.id.", run)})
					}
				}
			}
			c.actions = append(c.actions, actions)
			for _, a := range actions {
				if id, ok := a["open"].(string); ok {
					opened[id] = true
				}
			}
		}
		for _, child := range node.Children {
			visit(child, inRecord)
		}
	}
	for _, root := range roots {
		visit(root, false)
	}
	for {
		visited := false
		for id, sheet := range sheets {
			if opened[id] {
				delete(sheets, id)
				visit(sheet, false)
				visited = true
			}
		}
		if !visited {
			return
		}
	}
}

func selectionParameter(raw any) bool {
	params, _ := raw.(map[string]any)
	for _, value := range params {
		if text, ok := value.(string); ok && (strings.HasPrefix(text, "$selection.") || strings.HasPrefix(text, "$record.")) {
			return true
		}
	}
	return false
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
