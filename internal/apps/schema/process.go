package schema

import (
	"fmt"
	"strings"
)

// Process declares the work people and agents do. Execution state and signed
// record revisions belong to the space database.
type Process struct {
	Lifecycles map[string]Lifecycle    `json:"lifecycles,omitempty"`
	Forms      map[string]ProcessForm  `json:"forms,omitempty"`
	WorkItems  map[string]WorkItem     `json:"work_items,omitempty"`
	Timers     map[string]ProcessTimer `json:"timers,omitempty"`
	Templates  map[string]Template     `json:"templates,omitempty"`
}

type Lifecycle struct {
	Field       string                `json:"field"`
	Initial     string                `json:"initial"`
	States      []string              `json:"states"`
	Transitions map[string]Transition `json:"transitions,omitempty"`
}

// RecordLifecycle is the store's ordered declaration on a record type.
type RecordLifecycle struct {
	Field       string             `json:"field"`
	Initial     string             `json:"initial,omitempty"`
	Transitions []RecordTransition `json:"transitions"`
}

type RecordTransition struct {
	ID string   `json:"id"`
	By []string `json:"by,omitempty"`
	Transition
}

type Transition struct {
	Label   string   `json:"label"`
	From    []string `json:"from"`
	To      string   `json:"to"`
	Actors  []string `json:"actors,omitempty"`
	Guard   string   `json:"guard,omitempty"`
	Effects []Action `json:"effects,omitempty"`
}

type ProcessForm struct {
	Record    string     `json:"record"`
	Title     string     `json:"title"`
	Steps     []FormStep `json:"steps"`
	SaveDraft bool       `json:"saveDraft,omitempty"`
}

type FormStep struct {
	ID     string      `json:"id"`
	Label  string      `json:"label"`
	When   string      `json:"when,omitempty"`
	Fields []FormField `json:"fields"`
}

type FormField struct {
	Field    string `json:"field"`
	When     string `json:"when,omitempty"`
	Required bool   `json:"required,omitempty"`
}

type WorkItem struct {
	Title    string         `json:"title"`
	Subject  RecordEndpoint `json:"subject"`
	Assignee string         `json:"assignee"`
	Due      string         `json:"due,omitempty"`
	SLA      string         `json:"sla,omitempty"`
	Form     string         `json:"form"`
	Status   string         `json:"status,omitempty"`
	Outcome  map[string]any `json:"outcome,omitempty"`
}

type ProcessTimer struct {
	Record  string   `json:"record"`
	After   string   `json:"after,omitempty"`
	At      string   `json:"at,omitempty"`
	Every   string   `json:"every,omitempty"`
	Effects []Action `json:"effects"`
}

type Template struct {
	Record string         `json:"record"`
	Title  string         `json:"title"`
	Items  []TemplateItem `json:"items"`
}

type TemplateItem struct {
	ID       string   `json:"id"`
	WorkItem string   `json:"workItem"`
	After    []string `json:"after,omitempty"`
}

func (c *checker) lifecycle(typ string) (Lifecycle, bool) {
	t := c.app.Records[typ]
	if lc := t.Lifecycle; lc != nil {
		states := t.Fields[lc.Field].Values
		l := Lifecycle{Field: lc.Field, Initial: lc.Initial, States: states, Transitions: map[string]Transition{}}
		if l.Initial == "" && len(states) > 0 {
			l.Initial = states[0]
		}
		for _, tr := range lc.Transitions {
			l.Transitions[tr.ID] = tr.Transition
		}
		return l, true
	}
	if c.app.Process != nil {
		l, ok := c.app.Process.Lifecycles[typ]
		return l, ok
	}
	return Lifecycle{}, false
}

func (c *checker) checkProcess() {
	for _, typ := range sortedKeys(c.app.Records) {
		t := c.app.Records[typ]
		if t.Lifecycle == nil {
			continue
		}
		at := "/records/" + typ + "/lifecycle"
		if c.app.Process != nil {
			if _, duplicate := c.app.Process.Lifecycles[typ]; duplicate {
				c.add(AppFile, at, CodePolicy, "Declare the lifecycle once per record type.")
			}
		}
		seen := map[string]bool{}
		for i, tr := range t.Lifecycle.Transitions {
			if seen[tr.ID] {
				c.add(AppFile, fmt.Sprintf("%s/transitions/%d/id", at, i), CodeDuplicateID, "Duplicate transition %q.", tr.ID)
			}
			seen[tr.ID] = true
		}
		l, _ := c.lifecycle(typ)
		c.checkLifecycle(at, typ, l)
	}
	if c.app.Process == nil {
		return
	}
	process, _ := object(c.def.Files[AppFile])["process"].(map[string]any)
	for _, group := range sortedKeys(process) {
		values, _ := process[group].(map[string]any)
		for _, id := range sortedKeys(values) {
			at := "/process/" + group + "/" + id
			if !c.validateEntry(AppFile, at, CodePolicy, "process/"+group, values[id]) {
				continue
			}
			entry := c.kit.Registries.Processes[group]
			for _, rule := range entry.Rules {
				switch rule.Check {
				case "lifecycle":
					c.checkLifecycle(at, id, c.app.Process.Lifecycles[id])
				case "process-form":
					c.checkForm(at, c.app.Process.Forms[id])
				case "work-item":
					item := c.app.Process.WorkItems[id]
					c.checkRecordAccess(AppFile, at+"/subject/type", item.Subject.Type, "r")
					c.checkProcessRef(AppFile, at+"/form", "forms", item.Form, item.Subject.Type)
				case "process-timer":
					timer := c.app.Process.Timers[id]
					c.checkRecordAccess(AppFile, at+"/record", timer.Record, "r")
					c.checkRecordExpression(AppFile, at+"/at", timer.Record, timer.At)
					c.checkProcessEffects(at+"/effects", timer.Record, timer.Effects)
				case "process-template":
					c.checkTemplate(at, c.app.Process.Templates[id])
				}
			}
		}
	}
}

func (c *checker) checkLifecycle(at, typ string, lifecycle Lifecycle) {
	if !c.checkRecordAccess(AppFile, at, typ, "rw") {
		return
	}
	c.checkTaggedField(AppFile, at+"/field", typ, lifecycle.Field, "groupable")
	fields, _ := c.typeFields(typ)
	field := c.app.Records[typ].Fields[lifecycle.Field]
	if field.Type == "" || field.Computed != "" || field.AI != "" || c.app.Records[typ].Uses {
		c.add(AppFile, at+"/field", CodePolicy, "A lifecycle requires a locally declared state field that the engine writes.")
	}
	choices := fields[lifecycle.Field].Values
	if len(choices) != len(lifecycle.States) {
		c.add(AppFile, at+"/states", CodePolicy, "Lifecycle states must match the state field's choices.")
	}
	for _, state := range lifecycle.States {
		if !contains(choices, state) {
			c.add(AppFile, at+"/states", CodePolicy, "%q is not a choice of the state field.", state)
		}
	}
	if !contains(lifecycle.States, lifecycle.Initial) {
		c.add(AppFile, at+"/initial", CodePolicy, "The initial state must be one of the lifecycle states.")
	}
	for _, name := range sortedKeys(lifecycle.Transitions) {
		tr := lifecycle.Transitions[name]
		tat := at + "/transitions/" + name
		if !contains(lifecycle.States, tr.To) {
			c.add(AppFile, tat+"/to", CodePolicy, "Transition %q refers to unknown state %q.", name, tr.To)
		}
		for _, state := range tr.From {
			if state != "*" && !contains(lifecycle.States, state) {
				c.add(AppFile, tat, CodePolicy, "Transition %q refers to unknown state %q.", name, state)
			}
		}
		c.checkRecordExpression(AppFile, tat+"/guard", typ, tr.Guard)
		c.checkProcessEffects(tat+"/effects", typ, tr.Effects)
	}
}

func (c *checker) checkForm(at string, form ProcessForm) {
	if !c.checkRecordAccess(AppFile, at+"/record", form.Record, "r") {
		return
	}
	seen := map[string]bool{}
	for i, step := range form.Steps {
		path := fmt.Sprintf("%s/steps/%d", at, i)
		if seen[step.ID] {
			c.add(AppFile, path+"/id", CodeDuplicateID, "Two form steps use %q.", step.ID)
		}
		seen[step.ID] = true
		c.checkRecordExpression(AppFile, path+"/when", form.Record, step.When)
		for j, f := range step.Fields {
			fp := fmt.Sprintf("%s/fields/%d", path, j)
			c.checkTaggedField(AppFile, fp+"/field", form.Record, f.Field, "")
			c.checkRecordExpression(AppFile, fp+"/when", form.Record, f.When)
			fields, _ := c.typeFields(form.Record)
			if fields[f.Field].Computed != "" || systemFields[f.Field] {
				c.add(AppFile, fp+"/field", CodeNotAllowed, "The form cannot edit %q.", f.Field)
			}
			if l, ok := c.lifecycle(form.Record); ok && l.Field == f.Field {
				c.add(AppFile, fp+"/field", CodeNotAllowed, "Change %q through a lifecycle transition.", f.Field)
			}
		}
	}
}

func (c *checker) checkTemplate(at string, template Template) {
	c.checkRecordAccess(AppFile, at+"/record", template.Record, "r")
	deps := map[string][]string{}
	for i, item := range template.Items {
		path := fmt.Sprintf("%s/items/%d", at, i)
		if _, ok := deps[item.ID]; ok {
			c.add(AppFile, path+"/id", CodeDuplicateID, "Two process items use %q.", item.ID)
		}
		deps[item.ID] = item.After
		c.checkProcessRef(AppFile, path+"/workItem", "work_items", item.WorkItem, template.Record)
	}
	for _, issue := range dependencyProblems(deps) {
		c.add(AppFile, at+"/items", CodePolicy, "%s", issue)
	}
}

func (c *checker) checkProcessEffects(at, typ string, effects []Action) {
	ctx := actionCtx{file: AppFile, rowType: typ}
	for i, effect := range effects {
		c.checkGalleryAction(ctx, fmt.Sprintf("%s/%d", at, i), effect)
	}
}

func (c *checker) checkProcessRef(file, at, group, id, typ string) {
	if id == "" {
		return
	}
	process, _ := object(c.def.Files[AppFile])["process"].(map[string]any)
	items, _ := process[group].(map[string]any)
	value, ok := items[id].(map[string]any)
	if ok && typ != "" {
		record := asString(value["record"])
		if subject, found := value["subject"].(map[string]any); found {
			record = asString(subject["type"])
		}
		ok = record == "" || record == typ
	}
	if !ok {
		c.add(file, at, CodePolicy, "%q is not a matching declaration in process.%s.", id, group)
	}
}

func (c *checker) checkTransitionEffect(file, at string, value map[string]any) {
	typ := asString(value["type"])
	if !c.checkRecordAccess(file, at, typ, "rw") {
		return
	}
	l, exists := c.lifecycle(typ)
	if !exists {
		c.add(file, at, CodePolicy, "There is no lifecycle for %s.", typ)
		return
	}
	if id := asString(value["transition"]); id != "" {
		if _, ok := l.Transitions[id]; !ok {
			c.add(file, at, CodePolicy, "%q is not a transition of %s.", id, typ)
		}
	}
	if to := asString(value["to"]); to != "" && !strings.HasPrefix(to, "$") {
		for _, t := range l.Transitions {
			if t.To == to {
				return
			}
		}
		c.add(file, at, CodePolicy, "No transition moves %s to %q.", typ, to)
	}
}

func (c *checker) checkLifecycleGroup(ctx actionCtx, at, typ, field, event string, n *Node) {
	life, exists := c.lifecycle(typ)
	if !exists || life.Field != field {
		return
	}
	actions := n.On[event]
	if len(actions) == 1 {
		for kind, value := range actions[0] {
			if c.kit.Registries.Actions[kind].Transition {
				payload, _ := value.(map[string]any)
				if asString(payload["type"]) == typ && asString(payload["id"]) == "$row.id" && asString(payload["to"]) == "$input.to" {
					return
				}
			}
		}
	}
	c.add(ctx.file, at+"/on/"+event, CodePolicy, "Dragging between lifecycle states must request a transition to $input.to.")
}
