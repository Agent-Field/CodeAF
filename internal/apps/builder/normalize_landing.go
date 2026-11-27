package builder

import (
	"fmt"
	"maps"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

func wantsWorkingBrief(ask string) bool {
	ask = strings.ToLower(ask)
	return strings.Contains(ask, "start my day") || strings.Contains(ask, "one place") || strings.Contains(ask, "one desk") ||
		strings.Contains(ask, "daily brief") || strings.Contains(ask, "morning brief") ||
		strings.Contains(ask, "together") && strings.Contains(ask, "brief")
}

// Complete the existing landing through the Kit's brief compiler. The first
// view should collect work records rather than the documents summarizing them.
func normalizeLanding(files map[string]string, app schema.App, types schema.TypeSet) ([]schema.Problem, error) {
	plan, err := schema.ParsePlan([]byte(files[schema.PlanFile]))
	if err != nil || len(plan.Screens) == 0 || !wantsWorkingBrief(plan.Ask) {
		return nil, nil
	}
	id := plan.Screens[0].ID
	path := "screens/" + id + ".json"
	screen, err := schema.ParseScreen([]byte(files[path]))
	if err != nil || screen.Body == nil || len(screen.Params) != 0 {
		return nil, nil
	}
	section := briefSection(screen.Body)
	if section == nil && (screen.Body.Part == "RecordList" || screen.Body.Part == "Table") && len(screen.Body.On) == 0 {
		section = screen.Body
	}
	if section == nil || nodeHasID(section, "brief-overview") {
		return nil, nil
	}
	initial := mainCollection(section)
	if initial == nil {
		return nil, nil
	}
	work := initial.Bind.Records
	var agenda *schema.ScreenAgenda
	for _, line := range plan.Screens {
		other, _ := schema.ParseScreen([]byte(files["screens/"+line.ID+".json"]))
		collection := mainCollection(other.Body)
		if collection == nil {
			continue
		}
		name := collection.Bind.Records
		record := app.Records[name]
		if agenda == nil {
			if start := calendarStart(record); start != "" {
				agenda = &schema.ScreenAgenda{Record: name, Start: start}
				for _, field := range []string{"attendees", "participants", "organizer"} {
					if showableStoryField(record.Fields[field]) {
						agenda.Attendees = field
						break
					}
				}
			}
		}
		if !draftSummaryType(record, name) && app.Capabilities.Records[name] != "" &&
			(draftSummaryType(app.Records[work], work) || queueRelevant(plan.Ask, name) > queueRelevant(plan.Ask, work)) {
			work = name
		}
	}
	label := displayName(app.Records[work].Label, work)
	slots := schema.ScreenSlots{Agenda: agenda,
		Summary: displayName(plan.Screens[0].Line, screen.Title),
		Metrics: []schema.ScreenMetric{{Label: label + " in view", Op: "count"}},
	}
	if len(slots.Summary) > 128 {
		slots.Summary = screen.Title
	}
	record := app.Records[work]
	for _, field := range []string{"status", "stage", "state", "priority"} {
		shape := record.Fields[field]
		if shape.Type == "enum" && showableStoryField(shape) {
			if value := firstOpenState(shape.Values); value != "" {
				slots.Metrics = append(slots.Metrics, schema.ScreenMetric{Label: value, Op: "count",
					Where: []schema.QueryPredicate{{Field: field, Op: "eq", Value: value}}})
				break
			}
		}
	}
	if agenda != nil {
		slots.Metrics = append(slots.Metrics, schema.ScreenMetric{Label: displayName(app.Records[agenda.Record].Label, agenda.Record) + " scheduled", Op: "count", Record: agenda.Record})
	}
	for _, field := range []string{"rank", "priority", "score", "received_at", "due_on", "due_at"} {
		if shape := record.Fields[field]; showableStoryField(shape) && shape.Type != "enum" {
			slots.Sort = field
			break
		}
	}
	if work == initial.Bind.Records && initial.Bind.View != "" {
		query := screen.Views[initial.Bind.View]
		slots.Filter = query.Filter
		if query.Window != nil && len(query.Window.OrderBy) != 0 {
			slots.Sort = query.Window.OrderBy[0]
		}
	}
	base := maps.Clone(files)
	delete(base, path)
	compiled, err := schema.CompileRecipe(definition(base), types, schema.Recipe{
		Kind: "add-screen", ID: id, Title: screen.Title, Record: work, As: "brief", Slots: &slots,
	})
	if err != nil {
		return nil, fmt.Errorf("the landing could not be compiled as a brief: %w", err)
	}
	layout, err := schema.ParseScreen(compiled.Screen)
	if err != nil {
		return nil, err
	}
	*section = *layout.Body
	if screen.Views == nil {
		screen.Views = map[string]schema.QueryIR{}
	}
	maps.Copy(screen.Views, layout.Views)
	if len(compiled.App) != 0 {
		files[schema.AppFile] = string(compiled.App)
	}
	if err := saveJSON(files, path, screen); err != nil {
		return nil, err
	}
	return []schema.Problem{{File: path, Code: normalizationNote, Message: "Added a working brief with a priority queue and overview."}}, nil
}

func briefSection(node *schema.Node) *schema.Node {
	if node == nil {
		return nil
	}
	if node.ID == "brief" && node.Part == "Stack" {
		return node
	}
	for _, child := range node.Children {
		if found := briefSection(child); found != nil {
			return found
		}
	}
	return nil
}

func mainCollection(node *schema.Node) *schema.Node {
	if node == nil {
		return nil
	}
	if node.Bind != nil && node.Bind.Records != "" && (node.Part == "RecordList" || node.Part == "Table" || node.Part == "Board") {
		return node
	}
	for _, child := range node.Children {
		if found := mainCollection(child); found != nil {
			return found
		}
	}
	return nil
}

func showableStoryField(field schema.Field) bool {
	return field.Type != "" && !field.Masked && !field.Class.Restricted()
}

func calendarStart(record schema.RecordType) string {
	for _, name := range []string{"starts_at", "starts_on", "start_at", "start", "scheduled_at", "meeting_date", "event_date"} {
		if field := record.Fields[name]; showableStoryField(field) && (field.Type == "date" || field.Type == "datetime") {
			return name
		}
	}
	return ""
}

func draftSummaryType(record schema.RecordType, name string) bool {
	label := strings.ToLower(name + " " + record.Label)
	return strings.Contains(label, "brief") || strings.Contains(label, "digest") || strings.Contains(label, "summary") || strings.Contains(label, "draft")
}

func queueRelevant(ask, name string) int {
	name = strings.ReplaceAll(name, "_", " ")
	if strings.Contains(strings.ToLower(ask), name) {
		return 1
	}
	return 0
}

func firstOpenState(values []string) string {
	for _, state := range values {
		switch strings.ToLower(state) {
		case "new", "open", "pending", "needs review", "needs attention", "unread", "overdue", "to do":
			return state
		}
	}
	return ""
}
