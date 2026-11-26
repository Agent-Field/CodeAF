package check

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

// ExampleDepth measures the checked definition and its fictitious example data.
// The same findings warn during editing and block a first-party example export.
func ExampleDepth(def schema.Definition, types schema.TypeSet, rows map[string][]map[string]any) schema.DepthReport {
	const minScreens, minRecords = 3, 30
	app, _ := schema.ParseApp(def.Files[schema.AppFile])
	report := schema.DepthReport{Triggers: len(app.Triggers)}
	for _, records := range rows {
		report.Records += len(records)
	}
	for _, file := range def.ScreenFiles() {
		screen, _ := schema.ParseScreen(def.Files[file])
		lists := map[string]string{}
		walkDepthNodes(screen.Body, func(node *schema.Node) {
			if node.Bind != nil && node.Bind.Records != "" {
				lists[node.ID] = node.Bind.Records
			}
		})
		structured := false
		walkDepthNodes(screen.Body, func(node *schema.Node) {
			switch node.Part {
			case "Chart":
				if node.Bind != nil && (node.Bind.Run != "" || node.Bind.Records != "") {
					structured = true
					report.Charts++
				}
			case "RecordPage":
				if node.Bind != nil && node.Bind.Record != "" {
					structured = true
					if relatedExamples(node, lists, app, types, rows) {
						report.RelatedRecordPages++
					}
				}
			case "RecordList", "Board", "Table", "List", "Timeline", "Map", "Graph", "Grid":
				structured = structured || node.Bind != nil
			}
		})
		if structured {
			report.Screens++
		}
		report.SavedViews += len(screen.Views)
	}
	flows := map[string]schema.Run{}
	for _, file := range def.RunFiles() {
		run, _ := schema.ParseRun(def.Files[file])
		flows[run.Run] = run
	}
	for _, action := range schema.ActionRegistry(app, flows) {
		if action.Run != nil && approvedWrite(action.Run.Steps, false) {
			report.WriteBackActions++
		}
	}
	warn := func(ok bool, code, message string) {
		if !ok {
			report.Warnings = append(report.Warnings, problem(schema.AppFile, code, message))
		}
	}
	warn(report.Screens >= minScreens, "depth_screens", fmt.Sprintf("Add %d structured screens for the queue, record detail and overview.", minScreens))
	warn(report.RelatedRecordPages > 0, "depth_related_records", "Show a populated reference field on a record page.")
	warn(report.Charts > 0, "depth_chart", "Add a chart bound to records or a run.")
	warn(report.Records >= minRecords, "depth_records", fmt.Sprintf("Include at least %d example records. Check found %d.", minRecords, report.Records))
	warn(report.SavedViews > 0, "depth_saved_views", "Include at least one saved view.")
	warn(report.Triggers > 0, "depth_trigger", "Add a trigger that starts an automation.")
	warn(report.WriteBackActions > 0, "depth_write_back", "Register an Action that saves a draft or writes back after approval.")
	return report
}

func walkDepthNodes(node *schema.Node, visit func(*schema.Node)) {
	if node == nil || strings.TrimSpace(node.When) == "false" {
		return
	}
	visit(node)
	for _, child := range node.Children {
		walkDepthNodes(child, visit)
	}
}

func relatedExamples(node *schema.Node, lists map[string]string, app schema.App, types schema.TypeSet, rows map[string][]map[string]any) bool {
	typ := node.Bind.Type
	if typ == "" {
		selection, _ := strings.CutPrefix(node.Bind.Record, "$selection.")
		list, _, _ := strings.Cut(selection, ".")
		typ = lists[list]
	}
	record := app.Records[typ]
	shape, _ := types.ForRecord(typ, record)
	shown, restricted := node.Props["fields"].([]any)
	for _, row := range rows[typ] {
		for field, value := range row {
			if restricted && !slices.Contains(shown, any(field)) {
				continue
			}
			definition, local := record.Fields[field]
			if !local && record.Uses {
				definition = shape.Fields[field]
			}
			if definition.Type == "ref" || definition.Type == "refs" {
				switch ref := value.(type) {
				case string:
					if strings.TrimSpace(ref) != "" {
						return true
					}
				case []any:
					if len(ref) != 0 {
						return true
					}
				}
			}
		}
	}
	return false
}

func approvedWrite(steps []schema.Step, approved bool) bool {
	for _, step := range steps {
		_, _, source := schema.ParseSourceTool(step.Tool)
		gate := step.Gate != nil
		if (approved || gate) && (step.IsLocalWrite() || source) {
			return true
		}
		if approvedWrite(step.Steps, approved) {
			return true
		}
		if gate && step.Tool == "" && step.If == "" && step.OnError == "" {
			approved = true
		}
	}
	return false
}
