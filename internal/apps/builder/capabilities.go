package builder

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

const (
	builtinCSV       = "csv-import"
	builtinLifecycle = "lifecycle"
	builtinApproval  = "approval"
)

type planCapability struct {
	kind  string
	match *regexp.Regexp
}

// Each entry selects existing native behavior. The draft supplies record,
// state and run settings through their normal schema.
var planCapabilities = []planCapability{
	{builtinCSV, regexp.MustCompile(`(?i)\b(?:import|upload)(?:s|ed|ing)?\b[^.!?;]{0,45}\bcsv\b|\bcsv[ -]+import\b`)},
	{builtinLifecycle, regexp.MustCompile(`(?i)\b(?:lifecycle(?:[ -]+transitions?)?|(?:state|stage)[ -]+transitions?)\b`)},
	{builtinApproval, regexp.MustCompile(`(?i)\b(?:approve|approval)\b[^.!?;]{0,40}\bbefore\b`)},
}

var lifecycleMove = regexp.MustCompile(`(?i)\b(?:move|mark|transition|change|advance)\b[^.!?;]{0,60}\b(?:to|into)\b`)
var transitionID = regexp.MustCompile("(?i)\\b(?:lifecycle |state |stage )?transition\\s+(?:with\\s+)?id\\s*:?\\s*(" + runWord + ")")
var independentWork = regexp.MustCompile(`(?i)\b(?:connector|scheduled?|daily|hourly|notify|email|send|publish|draft|summari[sz]e|review)\b`)

func compileCapabilities(request string, plan *schema.Plan, files map[string]string) {
	ask := request + "\n" + plan.Ask
	for _, capability := range planCapabilities {
		if capability.match.MatchString(ask) {
			addBuiltin(plan, schema.PlanBuiltin{ID: capability.kind, Kind: capability.kind, Record: builtinRecord(plan.Data, ask)})
		}
	}
	requested := explicitRunIDs(request)
	selectNative := func(line schema.PlanLine) bool {
		if slices.Contains(requested, line.ID) {
			return false
		}
		words := strings.ReplaceAll(line.ID, "-", " ") + " " + line.Line
		for _, cap := range planCapabilities[:2] {
			selected := slices.ContainsFunc(plan.Builtins, func(b schema.PlanBuiltin) bool { return b.Kind == cap.kind })
			match := cap.match.MatchString(words)
			if cap.kind == builtinLifecycle && selected && lifecycleMove.MatchString(words) {
				match = true
			}
			if match && !independentWork.MatchString(line.Line) {
				addBuiltin(plan, schema.PlanBuiltin{ID: line.ID, Kind: cap.kind, Record: builtinRecord(plan.Data, words)})
				removeBuiltinRun(files, line.ID)
				return true
			}
		}
		return slices.ContainsFunc(plan.Builtins, func(b schema.PlanBuiltin) bool {
			return b.ID == line.ID && (b.Kind == builtinCSV || b.Kind == builtinLifecycle)
		})
	}
	plan.Automation = slices.DeleteFunc(plan.Automation, selectNative)
	plan.Actions = slices.DeleteFunc(plan.Actions, selectNative)

	for _, builtin := range plan.Builtins {
		switch builtin.Kind {
		case builtinLifecycle:
			compileLifecycle(ask, builtin.Record, files)
		case builtinApproval:
			compileApproval(ask, plan.Automation, files)
		}
	}
}

func addBuiltin(plan *schema.Plan, next schema.PlanBuiltin) {
	if !slices.ContainsFunc(plan.Builtins, func(old schema.PlanBuiltin) bool { return old.ID == next.ID && old.Kind == next.Kind }) {
		plan.Builtins = append(plan.Builtins, next)
	}
}

func builtinRecord(data []schema.PlanLine, words string) string {
	if len(data) == 1 {
		return data[0].ID
	}
	for _, line := range data {
		term := strings.NewReplacer("-", " ", "_", " ").Replace(line.ID)
		if regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(term) + `s?\b`).MatchString(words) {
			return line.ID
		}
	}
	return ""
}

func removeBuiltinRun(files map[string]string, id string) {
	delete(files, "runs/"+id+".yaml")
	delete(files, "runs/"+id+".yml")
}

func compileLifecycle(ask, recordID string, files map[string]string) {
	match := transitionID.FindStringSubmatch(ask)
	if recordID == "" || len(match) == 0 {
		return
	}
	id := strings.ToLower(strings.Trim(match[1], "`'\""))
	var app map[string]json.RawMessage
	var records map[string]json.RawMessage
	var record schema.RecordType
	if json.Unmarshal([]byte(files[schema.AppFile]), &app) != nil ||
		json.Unmarshal(app["records"], &records) != nil ||
		json.Unmarshal(records[recordID], &record) != nil || record.Uses {
		return
	}
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
		var to string
		for _, value := range shape.Values {
			token := regexp.QuoteMeta(value)
			if regexp.MustCompile(`(?i)\bas\s+` + token + `\b`).MatchString(ask) {
				initial = value
			}
			if regexp.MustCompile(`(?i)\b(?:to|into)\s+` + token + `\b`).MatchString(ask) {
				to = value
			}
		}
		if initial == "" || to == "" || initial == to {
			continue
		}
		if record.Lifecycle == nil {
			record.Lifecycle = &schema.RecordLifecycle{Field: field, Initial: initial}
		}
		found := false
		for i := range record.Lifecycle.Transitions {
			transition := &record.Lifecycle.Transitions[i]
			if transition.ID == id {
				found = true
				break
			}
			if transition.To == to && slices.Equal(transition.From, []string{initial}) {
				transition.ID = id
				found = true
				break
			}
		}
		if !found {
			record.Lifecycle.Transitions = append(record.Lifecycle.Transitions, schema.RecordTransition{
				ID: id, Transition: schema.Transition{From: []string{initial}, To: to},
			})
		}
		records[recordID], _ = json.Marshal(record)
		app["records"], _ = json.Marshal(records)
		if err := declareRecordAccess(app, recordID, "rw"); err != nil {
			return
		}
		raw, _ := json.MarshalIndent(app, "", "  ")
		files[schema.AppFile] = string(raw)
		return
	}
}

var localApproval = regexp.MustCompile(`(?i)\b(?:approve|approval)\b[^.!?;]{0,45}\bbefore\b[^.!?;]{0,30}\b(?:save\w*|writ\w*|updat\w*)\b`)

func compileApproval(ask string, lines []schema.PlanLine, files map[string]string) {
	if len(lines) != 1 || !localApproval.MatchString(ask) {
		return
	}
	id := lines[0].ID
	for _, ext := range []string{".yaml", ".yml"} {
		file := "runs/" + id + ext
		run, err := schema.ParseRun([]byte(files[file]))
		if err != nil || run.Run == "" {
			continue
		}
		if holdLocalWrites(run.Steps) {
			raw, _ := json.MarshalIndent(run, "", "  ")
			files[file] = string(raw)
		}
	}
}

func holdLocalWrites(steps []schema.Step) bool {
	changed := false
	for i := range steps {
		step := &steps[i]
		if step.Gate == nil && (step.RecordsCreate != nil || step.RecordsUpdate != nil) {
			step.Gate = &schema.Gate{Approval: "proof"}
			changed = true
		}
		if holdLocalWrites(step.Steps) {
			changed = true
		}
	}
	return changed
}
