package builder

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"gopkg.in/yaml.v3"
)

type planCapability struct {
	kind  string
	match *regexp.Regexp
	names string
}

var planCapabilities = []planCapability{
	{schema.BuiltinCSV, regexp.MustCompile(`(?i)\b(?:import|upload)(?:s|ed|ing)?\b[^.!?;]{0,45}\bcsv\b|\bcsv[ -]+import\b`), `(?:csv[ -]+imports?|import(?:ing)?[ -]+csv)`},
	{schema.BuiltinLifecycle, regexp.MustCompile(`(?i)\b(?:lifecycle(?:[ -]+transitions?)?|(?:state|stage)[ -]+transitions?)\b`), `(?:(?:record[ -]+)?lifecycle|state[ -]+transitions?)`},
	{schema.BuiltinApproval, regexp.MustCompile(`(?i)\b(?:approve|approval)\b[^.!?;]{0,45}\bbefore\b[^.!?;]{0,30}\b(?:sav\w*|writ\w*|updat\w*)\b`), `(?:local[ -]+)?approval`},
}

var lifecycleMove = regexp.MustCompile(`(?i)\b(?:move|mark|transition|change|advance)\b[^.!?;]{0,60}\b(?:to|into)\b`)
var transitionID = regexp.MustCompile("(?i)\\b(?:lifecycle |state |stage )?transition\\s+(?:with\\s+)?id\\s*:?\\s*(" + runWord + ")")
var independentWork = regexp.MustCompile(`(?i)\b(?:connector|scheduled?|daily|hourly|notify|email|send|draft|summari[sz]e|review)\b`)

// Native record capabilities run through the data API. Approval configures an
// existing run. Preserving the plan line gives the author its original settings.
func (w buildPlan) compileCapabilities(plan *schema.Plan, files map[string]string) error {
	ask := w.ask
	if ask == "" {
		ask = plan.Ask
	}
	settings := plan.Builtins
	plan.Builtins = slices.DeleteFunc(slices.Clone(w.builtins), func(b schema.PlanBuiltin) bool {
		return slices.Contains(w.disabled, b.Kind) || !slices.ContainsFunc(planCapabilities, func(cap planCapability) bool { return cap.kind == b.Kind })
	})
	use := func(next schema.PlanBuiltin) {
		if slices.Contains(w.disabled, next.Kind) {
			return
		}
		if authored, ok := builtinSettings(next, settings); ok {
			if next.Line == "" {
				next.Line = authored.Line
			}
			if authored.Record != "" {
				next.Record = authored.Record
			}
			if next.Transition == "" {
				next.Transition = authored.Transition
			}
			if authored.Run != "" {
				next.Run = authored.Run
			}
		}
		i := slices.IndexFunc(plan.Builtins, func(old schema.PlanBuiltin) bool { return old.ID == next.ID && old.Kind == next.Kind })
		if i == -1 {
			plan.Builtins = append(plan.Builtins, next)
		} else {
			plan.Builtins[i] = next
		}
	}

	selectRecordCapability := func(line schema.PlanLine) bool {
		if hasPlanLine(w.runs, line.ID) || independentWork.MatchString(line.Line) {
			return false
		}
		words := strings.ReplaceAll(line.ID, "-", " ") + " " + line.Line
		for _, capability := range planCapabilities {
			if capability.kind == schema.BuiltinApproval || slices.Contains(w.disabled, capability.kind) {
				continue
			}
			matches := capability.match.MatchString(words)
			if capability.kind == schema.BuiltinLifecycle && capability.match.MatchString(ask) {
				requested := plannedTransition(capability.kind, "", ask)
				matches = matches || lifecycleMove.MatchString(words) || requested != "" && requested == line.ID
			}
			matches = matches || slices.ContainsFunc(settings, func(b schema.PlanBuiltin) bool {
				return b.ID == line.ID && b.Kind == capability.kind
			})
			if !matches {
				continue
			}
			use(schema.PlanBuiltin{
				ID: line.ID, Kind: capability.kind, Line: line.Line,
				Record: builtinRecord(plan.Data, words), Transition: plannedTransition(capability.kind, words, ask),
			})
			removeRunFiles(files, line.ID)
			return true
		}
		return false
	}
	plan.Automation = slices.DeleteFunc(plan.Automation, selectRecordCapability)
	plan.Actions = slices.DeleteFunc(plan.Actions, selectRecordCapability)

	// Existing selections are kept even when the writer replaces the plan.
	for _, builtin := range slices.Clone(plan.Builtins) {
		use(builtin)
	}
	for _, capability := range planCapabilities {
		if slices.Contains(w.disabled, capability.kind) {
			continue
		}
		if capability.kind == schema.BuiltinApproval {
			for _, line := range plan.Automation {
				if capability.match.MatchString(line.Line) {
					if !slices.ContainsFunc(plan.Builtins, func(b schema.PlanBuiltin) bool {
						return b.Kind == schema.BuiltinApproval && b.Run == line.ID
					}) {
						use(schema.PlanBuiltin{ID: "approve-" + line.ID, Kind: schema.BuiltinApproval, Line: line.Line, Run: line.ID})
					}
				}
			}
		}
		if slices.ContainsFunc(plan.Builtins, func(b schema.PlanBuiltin) bool { return b.Kind == capability.kind }) {
			continue
		}
		if !capability.match.MatchString(ask) {
			continue
		}
		next := schema.PlanBuiltin{ID: capability.kind, Kind: capability.kind}
		if capability.kind != schema.BuiltinApproval {
			next.Record = builtinRecord(plan.Data, ask)
			next.Transition = plannedTransition(next.Kind, ask, "")
		}
		use(next)
	}

	for i := range plan.Builtins {
		builtin := &plan.Builtins[i]
		switch builtin.Kind {
		case schema.BuiltinCSV, schema.BuiltinLifecycle:
			if builtin.Record == "" && len(plan.Data) == 1 {
				builtin.Record = plan.Data[0].ID
			}
			if err := compileRecordCapability(ask, *builtin, files); err != nil {
				return err
			}
		case schema.BuiltinApproval:
			if builtin.Run == "" && len(plan.Automation) == 1 {
				builtin.Run = plan.Automation[0].ID
			}
			if err := compileApproval(builtin.Run, files); err != nil {
				return err
			}
		}
		if line := builtinPlanLine(*builtin, plan.Automation); line != "" && !hasPlanLine(plan.Actions, builtin.ID) {
			plan.Actions = append(plan.Actions, schema.PlanLine{ID: builtin.ID, Line: line})
		}
	}
	return nil
}

func removedCapabilities(request string) []string {
	var removed []string
	for _, capability := range planCapabilities {
		pattern := removalPrefix + "(?:" + removalVerb + `|(?:no|without)\s+)` + capability.names + `(?:[\s,.!?;]|$)`
		if regexp.MustCompile(pattern).MatchString(request) {
			removed = append(removed, capability.kind)
		}
	}
	return removed
}

func builtinSettings(next schema.PlanBuiltin, authored []schema.PlanBuiltin) (schema.PlanBuiltin, bool) {
	if i := slices.IndexFunc(authored, func(value schema.PlanBuiltin) bool { return value.Kind == next.Kind && value.ID == next.ID }); i >= 0 {
		return authored[i], true
	}
	var match schema.PlanBuiltin
	for _, value := range authored {
		if value.Kind != next.Kind || !(value.ID == next.Kind || next.ID == next.Kind && next.Record == "" && next.Run == "") {
			continue
		}
		if match.ID != "" {
			return schema.PlanBuiltin{}, false
		}
		match = value
	}
	return match, match.ID != ""
}

func builtinPlanLine(builtin schema.PlanBuiltin, runs []schema.PlanLine) string {
	if builtin.Line != "" {
		return builtin.Line
	}
	switch builtin.Kind {
	case schema.BuiltinCSV:
		return "Import a CSV into " + displayName(builtin.Record, "your records")
	case schema.BuiltinLifecycle:
		return "Move " + displayName(builtin.Record, "records") + " between states"
	case schema.BuiltinApproval:
		if !hasPlanLine(runs, builtin.ID) {
			return "Approve changes before they save"
		}
	}
	return ""
}

func plannedTransition(kind, line, ask string) string {
	if kind == schema.BuiltinLifecycle {
		if match := transitionID.FindStringSubmatch(line + "\n" + ask); len(match) != 0 {
			return strings.ToLower(strings.Trim(match[1], "`'\""))
		}
	}
	return ""
}

func builtinRecord(data []schema.PlanLine, words string) string {
	if len(data) == 1 {
		return data[0].ID
	}
	var target string
	for _, line := range data {
		term := strings.NewReplacer("-", " ", "_", " ").Replace(line.ID)
		if !regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(term) + `s?\b`).MatchString(words) {
			continue
		}
		if target != "" {
			return ""
		}
		target = line.ID
	}
	return target
}

func removeRunFiles(files map[string]string, id string) {
	delete(files, "runs/"+id+".yaml")
	delete(files, "runs/"+id+".yml")
}

func compileRecordCapability(ask string, builtin schema.PlanBuiltin, files map[string]string) error {
	if builtin.Record == "" {
		return nil
	}
	var app map[string]json.RawMessage
	var records map[string]json.RawMessage
	var record schema.RecordType
	if json.Unmarshal([]byte(files[schema.AppFile]), &app) != nil ||
		json.Unmarshal(app["records"], &records) != nil ||
		json.Unmarshal(records[builtin.Record], &record) != nil {
		return nil
	}
	changed := false
	if builtin.Kind == schema.BuiltinLifecycle && builtin.Transition != "" && !record.Uses {
		var process schema.Process
		if json.Unmarshal(app["process"], &process) != nil || process.Lifecycles[builtin.Record].Field == "" {
			changed = compileLifecycle(ask, builtin, &record)
		}
	}
	if changed {
		var err error
		records[builtin.Record], err = json.Marshal(record)
		if err != nil {
			return err
		}
		app["records"], err = json.Marshal(records)
		if err != nil {
			return err
		}
	}
	manifest, err := schema.ParseApp([]byte(files[schema.AppFile]))
	if err == nil && manifest.Capabilities.Records[builtin.Record] != "rw" {
		if err := declareRecordAccess(app, builtin.Record, "rw"); err != nil {
			return err
		}
		changed = true
	}
	if changed {
		return saveJSON(files, schema.AppFile, app)
	}
	return nil
}

// Preserve the author's lifecycle while assigning the requested transition
// address. The person's explicit source and destination can fill a missing
// setting even when the writer has already declared other transitions.
func compileLifecycle(ask string, builtin schema.PlanBuiltin, record *schema.RecordType) bool {
	example := lifecycleFromRequest(*record, builtin, ask)
	if record.Lifecycle == nil {
		record.Lifecycle = example
		return example != nil
	}
	transitions := record.Lifecycle.Transitions
	if slices.ContainsFunc(transitions, func(tr schema.RecordTransition) bool { return tr.ID == builtin.Transition }) {
		return false
	}
	if example != nil {
		requested := example.Transitions[0]
		index := slices.IndexFunc(transitions, func(tr schema.RecordTransition) bool {
			return tr.To == requested.To && slices.Equal(tr.From, requested.From)
		})
		if index >= 0 {
			transitions[index].ID = builtin.Transition
		} else {
			record.Lifecycle.Transitions = append(transitions, requested)
		}
		return true
	}
	if len(transitions) == 1 {
		transitions[0].ID = builtin.Transition
		return true
	}
	return false
}

// A request with an explicit transition ID and states is already enough to
// construct a native lifecycle once the author supplies the record fields.
func lifecycleFromRequest(record schema.RecordType, builtin schema.PlanBuiltin, ask string) *schema.RecordLifecycle {
	fields := []string{"status", "state", "stage"}
	if record.Lifecycle != nil {
		fields = []string{record.Lifecycle.Field}
	}
	for _, field := range fields {
		shape := record.Fields[field]
		if shape.Type != "enum" {
			continue
		}
		initial, _ := shape.Default.(string)
		if record.Lifecycle != nil && record.Lifecycle.Initial != "" {
			initial = record.Lifecycle.Initial
		}
		var to string
		for _, value := range shape.Values {
			token := regexp.QuoteMeta(value)
			if regexp.MustCompile(`(?i)\bas\s+` + token + `\b`).MatchString(ask) {
				initial = value
			}
			if regexp.MustCompile(`(?i)\b(?:to|into)\s+` + token + `\b`).MatchString(builtin.Line + "\n" + ask) {
				to = value
			}
		}
		if initial != "" && to != "" && initial != to {
			return &schema.RecordLifecycle{
				Field: field, Initial: initial,
				Transitions: []schema.RecordTransition{{ID: builtin.Transition, Transition: schema.Transition{From: []string{initial}, To: to}}},
			}
		}
	}
	return nil
}

func compileApproval(id string, files map[string]string) error {
	if id == "" {
		return nil
	}
	for _, ext := range []string{".yaml", ".yml"} {
		path := "runs/" + id + ext
		var root yaml.Node
		if err := yaml.Unmarshal([]byte(files[path]), &root); err != nil || len(root.Content) == 0 {
			continue
		}
		if !holdLocalWrites(yamlValue(root.Content[0], "steps")) {
			continue
		}
		raw, err := yaml.Marshal(&root)
		if err != nil {
			return err
		}
		files[path] = string(raw)
	}
	return nil
}

func yamlValue(node *yaml.Node, key string) *yaml.Node {
	if node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value == key {
				return node.Content[i+1]
			}
		}
	}
	return nil
}

func holdLocalWrites(steps *yaml.Node) bool {
	if steps == nil || steps.Kind != yaml.SequenceNode {
		return false
	}
	changed := false
	for _, step := range steps.Content {
		if yamlValue(step, "gate") == nil && (yamlValue(step, "records.create") != nil || yamlValue(step, "records.update") != nil) {
			gate := &yaml.Node{Kind: yaml.MappingNode, Style: yaml.FlowStyle, Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: "approval"}, {Kind: yaml.ScalarNode, Value: "proof"},
			}}
			step.Content = append(step.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "gate"}, gate)
			changed = true
		}
		changed = holdLocalWrites(yamlValue(step, "steps")) || changed
	}
	return changed
}
