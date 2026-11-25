package schema

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ViewRecipeDescriptor maps a collection view's intent to its desktop part.
// The manifest supplies the projection and its configurable field.
type ViewRecipeDescriptor struct {
	Part   string         `json:"part"`
	Props  map[string]any `json:"props"`
	By     string         `json:"by,omitempty"`
	Fields string         `json:"fields,omitempty"`
}

// Recipe is an intent the app command can compile into a checked change. ID is
// the screen or process declaration name. Screen optionally gives a screen
// recipe its own ID when that differs from the declaration name.
type Recipe struct {
	Kind       string
	ID         string
	Screen     string
	Title      string
	Record     string
	As         string
	By         string
	Group      string
	Aggregate  string
	Fields     []string
	States     []string
	Transition *Transition
	Form       *ProcessForm
	Checklist  *Policy
	SignOff    *Policy
	Timer      *ProcessTimer
	WorkItem   *WorkItem
	Template   *Template
}

// RecipeResult contains the bytes for a proposed app.json and/or screen. The
// compiler has no filesystem or publication authority.
type RecipeResult struct {
	App    json.RawMessage
	Screen json.RawMessage
}

// CompileRecipe emits a checked change against the draft's type catalog.
func CompileRecipe(def Definition, existing TypeSet, r Recipe) (RecipeResult, error) {
	c, err := compilerFor(def, existing)
	if err != nil {
		return RecipeResult{}, err
	}
	if !fileIDPattern.MatchString(r.ID) {
		return RecipeResult{}, fmt.Errorf("recipe id %q must be a slug", r.ID)
	}
	var screen *Screen
	switch r.Kind {
	case "add-view", "add-detail", "add-chart", "add-queue":
		screen, err = c.compileViewRecipe(r)
	case "add-form":
		if r.Form != nil {
			err = c.changeProcess(r)
			if err != nil {
				break
			}
		}
		screen, err = c.compileViewRecipe(r)
	case "add-lifecycle", "add-transition", "add-checklist", "add-signoff", "add-timer":
		err = c.changeProcess(r)
	default:
		err = fmt.Errorf("unknown app recipe %q", r.Kind)
	}
	if err != nil {
		return RecipeResult{}, err
	}

	if screen != nil {
		if c.screens[screen.Screen] {
			return RecipeResult{}, fmt.Errorf("screen %q already exists", screen.Screen)
		}
		c.screens[screen.Screen] = true
	}
	var result RecipeResult
	if c.def.Files[AppFile] != nil && string(c.def.Files[AppFile]) != string(def.Files[AppFile]) {
		c.checkApp()
		if !c.report.OK() {
			return RecipeResult{}, fmt.Errorf("%s", c.report)
		}
		result.App = c.def.Files[AppFile]
	}
	if screen != nil {
		result.Screen, err = c.compileScreen(*screen)
		if err != nil {
			return RecipeResult{}, err
		}
	}
	return result, nil
}

func compilerFor(def Definition, existing TypeSet) (*checker, error) {
	compiled := schemas()
	if compiled.err != nil {
		return nil, compiled.err
	}
	if kitVersion(def.Files[AppFile]) != 2 {
		return nil, fmt.Errorf("screen recipes need a kit 2 app")
	}
	copyDef := Definition{Files: map[string][]byte{}, Links: def.Links}
	for name, data := range def.Files {
		copyDef.Files[name] = data
	}
	c := &checker{def: copyDef, kit: compiled.kits[2], existing: existing, screens: map[string]bool{}, runs: map[string]bool{}, commands: map[string]Command{}}
	if !c.schemaCheck(AppFile, def.Files[AppFile], compiled.resources["app-2.json"]) {
		return nil, fmt.Errorf("%s", c.report)
	}
	app, err := ParseApp(def.Files[AppFile])
	if err != nil {
		return nil, err
	}
	c.app, c.appOK = app, true
	for _, name := range def.ScreenFiles() {
		c.screens[FileID(name)] = true
	}
	for _, name := range def.RunFiles() {
		c.runs[FileID(name)] = true
	}
	for _, command := range app.Commands {
		c.commands[command.ID] = command
	}
	c.loadCustomParts()
	c.indexSample()
	return c, nil
}

func (c *checker) compileScreen(s Screen) ([]byte, error) {
	file := ScreensDir + "/" + s.Screen + ".json"
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	raw = append(raw, '\n')
	c.def.Files[file] = raw
	c.screens[s.Screen] = true
	if c.schemaCheck(file, raw, schemas().resources["screen-2.json"]) {
		// Use the decoded properties so Check sees the same value types as it
		// does when the store reads a screen.
		value, err := ParseScreen(raw)
		if err != nil {
			return nil, err
		}
		c.checkScreen(file, value)
	}
	if !c.report.OK() {
		return nil, fmt.Errorf("%s", c.report)
	}
	return raw, nil
}

func (c *checker) compileViewRecipe(r Recipe) (*Screen, error) {
	s := &Screen{Screen: r.ID, Title: r.Title, Views: map[string]QueryIR{}}
	if r.Screen != "" {
		s.Screen = r.Screen
	}
	if s.Title == "" {
		s.Title = r.ID
	}
	n := &Node{ID: nodeID("recipe/" + s.Screen + "/" + r.Kind + "/" + r.Record), Label: s.Title, Props: map[string]any{}}
	s.Body = n
	q := QueryIR{Type: r.Record, Select: r.Fields}
	fieldProps := func(name string) {
		if len(r.Fields) > 0 {
			n.Props[name] = r.Fields
		}
	}
	switch r.Kind {
	case "add-view":
		entry, ok := c.kit.Registries.Views[r.As]
		if !ok || entry.Recipe == nil {
			return nil, fmt.Errorf("there is no collection recipe for %q", r.As)
		}
		desc := entry.Recipe
		n.Part = desc.Part
		for key, val := range desc.Props {
			n.Props[key] = val
		}
		fieldProps(desc.Fields)
		if desc.By != "" && r.By == "" {
			return nil, fmt.Errorf("the %s view needs --by", r.As)
		}
		if desc.By != "" {
			switch r.As {
			case "board":
				n.Props[desc.By] = r.By
				q.Group = []string{r.By}
				if life, managed := c.lifecycle(r.Record); managed && life.Field == r.By {
					n.On = map[string][]Action{"move": {{"lifecycle.transition": map[string]any{"type": r.Record, "id": "$row.id", "to": "$input.to"}}}}
				}
			default:
				n.Props["viewOptions"] = map[string]any{r.As: map[string]any{desc.By: r.By}}
				q.Window = &QueryWindow{OrderBy: []string{r.By}}
			}
		}
		if desc.Fields != "" && len(r.Fields) == 0 && n.Props[desc.Fields] == nil {
			if t, ok := c.app.Records[r.Record]; ok && t.Title != "" {
				n.Props[desc.Fields] = []string{t.Title}
			}
		}
	case "add-detail":
		n.Part = "RecordPage"
		fieldProps("fields")
		s.Params = []string{"id"}
		n.Bind = &Bind{Record: "$params.id", Type: r.Record}
		return s, nil
	case "add-form":
		n.Part = "Form"
		n.Props["type"] = r.Record
		fieldProps("fields")
		if r.Form != nil {
			n.Props["form"] = r.ID
		}
		return s, nil
	case "add-chart":
		if r.Group == "" {
			return nil, fmt.Errorf("add-chart needs --group")
		}
		op, field, hasField := strings.Cut(r.Aggregate, ":")
		if op == "" || op != "count" && (!hasField || field == "") {
			return nil, fmt.Errorf("use --agg op:field, or --agg count")
		}
		q.Group = []string{r.Group}
		q.Aggregate = []QueryAggregate{{Op: op, Field: field, As: "result"}}
		n.Part = "Chart"
		n.Props = map[string]any{"kind": "bar", "x": r.Group, "y": "result"}
	case "add-queue":
		n.Part = "TriageQueue"
		fieldProps("columns")
	}
	n.Bind = &Bind{Records: r.Record, View: n.ID}
	s.Views[n.ID] = q
	return s, nil
}

func (c *checker) changeProcess(r Recipe) error {
	process := c.app.Process
	if process == nil {
		process = &Process{}
		c.app.Process = process
	}
	pol := func(p *Policy) error {
		if p == nil {
			return fmt.Errorf("%s needs a policy declaration", r.Kind)
		}
		for _, existing := range c.app.Policies {
			if existing.ID == r.ID {
				return fmt.Errorf("policy %q already exists", r.ID)
			}
		}
		entry := *p
		entry.ID = r.ID
		c.app.Policies = append(c.app.Policies, entry)
		return nil
	}
	switch r.Kind {
	case "add-lifecycle":
		if _, ok := c.lifecycle(r.Record); ok {
			return fmt.Errorf("%s already has a lifecycle", r.Record)
		}
		typ, ok := c.app.Records[r.Record]
		if !ok || typ.Uses || len(r.States) == 0 {
			return fmt.Errorf("add-lifecycle needs a locally defined record type and at least one state")
		}
		field := r.By
		if field == "" {
			field = "state"
		}
		if _, exists := typ.Fields[field]; !exists {
			for name, manifest := range c.kit.Registries.Fields {
				if contains(manifest.Tags, "groupable") {
					typ.Fields[field] = Field{Type: name, Values: r.States, Default: r.States[0], Index: true}
					break
				}
			}
		}
		setRecipeEntry(&process.Lifecycles, r.Record, Lifecycle{Field: field, Initial: r.States[0], States: r.States})
	case "add-transition":
		lifecycle, ok := c.lifecycle(r.Record)
		if !ok || r.Transition == nil {
			return fmt.Errorf("add-transition needs a lifecycle and a transition declaration")
		}
		if _, exists := lifecycle.Transitions[r.ID]; exists {
			return fmt.Errorf("transition %q already exists", r.ID)
		}
		if record := c.app.Records[r.Record]; record.Lifecycle != nil {
			native := RecordTransition{ID: r.ID, Transition: *r.Transition}
			native.Actors = nil
			for _, actor := range r.Transition.Actors {
				if actor == "agent" {
					actor = "ai"
				}
				native.By = append(native.By, actor)
			}
			record.Lifecycle.Transitions = append(record.Lifecycle.Transitions, native)
		} else {
			setRecipeEntry(&lifecycle.Transitions, r.ID, *r.Transition)
			process.Lifecycles[r.Record] = lifecycle
		}
	case "add-form":
		if r.Form == nil || r.Form.Record != r.Record {
			return fmt.Errorf("the form needs the same record type as the recipe")
		}
		if _, exists := process.Forms[r.ID]; exists {
			return fmt.Errorf("form %q already exists", r.ID)
		}
		setRecipeEntry(&process.Forms, r.ID, *r.Form)
		if r.WorkItem != nil {
			setRecipeEntry(&process.WorkItems, r.ID, *r.WorkItem)
		}
	case "add-checklist":
		if r.Checklist != nil {
			if err := pol(r.Checklist); err != nil {
				return err
			}
		} else if r.Template != nil {
			setRecipeEntry(&process.Templates, r.ID, *r.Template)
		} else {
			return fmt.Errorf("add-checklist needs a checklist policy or process template")
		}
	case "add-signoff":
		if err := pol(r.SignOff); err != nil {
			return err
		}
	case "add-timer":
		if r.Timer == nil {
			return fmt.Errorf("add-timer needs a timer declaration")
		}
		if _, exists := process.Timers[r.ID]; exists {
			return fmt.Errorf("timer %q already exists", r.ID)
		}
		setRecipeEntry(&process.Timers, r.ID, *r.Timer)
	}
	return c.writeRecipeApp()
}

func setRecipeEntry[T any](m *map[string]T, key string, value T) {
	if *m == nil {
		*m = map[string]T{}
	}
	(*m)[key] = value
}

func (c *checker) writeRecipeApp() error {
	// Preserve every unedited envelope key, including source arguments added by
	// a newer connector or screen caller.
	app := map[string]json.RawMessage{}
	if err := json.Unmarshal(c.def.Files[AppFile], &app); err != nil {
		return err
	}
	for key, value := range map[string]any{"records": c.app.Records, "process": c.app.Process, "policies": c.app.Policies} {
		if key == "policies" && len(c.app.Policies) == 0 {
			continue
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		app[key] = raw
	}
	raw, err := json.MarshalIndent(app, "", "  ")
	if err != nil {
		return err
	}
	if !c.schemaCheck(AppFile, raw, schemas().resources["app-2.json"]) {
		return fmt.Errorf("%s", c.report)
	}
	c.def.Files[AppFile] = append(raw, '\n')
	return nil
}
