package check

import (
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

const ExamplesPublisher = "codeaf examples"

var plainNumber = regexp.MustCompile(`^[+-]?[0-9][0-9,]*(\.[0-9]+)?%?$`)

// LookFindings checks what a screen promises before any preview or model call.
func LookFindings(def schema.Definition, types schema.TypeSet) []schema.Problem {
	app, err := schema.ParseApp(def.Files[schema.AppFile])
	if err != nil || app.Kit != 2 {
		return nil
	}
	var sample schema.Sample
	_ = json.Unmarshal(def.Files[schema.SampleFile], &sample)
	var findings []schema.Problem
	var sectionTitles []string
	hasVisual, navReported := false, false
	for _, file := range def.ScreenFiles() {
		screen, err := schema.ParseScreen(def.Files[file])
		if err != nil || screen.Body == nil {
			continue
		}
		sectionTitles = append(sectionTitles, screen.Title)
		add := func(code, text string) { findings = append(findings, problem(file, code, text)) }
		var fullTable, detail bool
		searches := map[string]int{}
		var walk func(*schema.Node, bool)
		walk = func(n *schema.Node, narrow bool) {
			if n == nil {
				return
			}
			table := n.Part == "Table" || n.Part == "RecordList" && slices.Contains(words(n.Props["views"]), "table")
			fullTable = fullTable || table && !narrow
			switch n.Part {
			case "SectionTabs":
				var labels []string
				for _, raw := range array(n.Props["items"]) {
					item, _ := raw.(map[string]any)
					labels = append(labels, text(item["label"]))
				}
				if !navReported && typeTabs(labels, app.Records) {
					add("look_type_tabs", "Name the sections for jobs or saved views, such as Needs review and Done.")
					navReported = true
				}
			case "RecordList":
				views := words(n.Props["views"])
				hasVisual = hasVisual || n.Bind != nil && (slices.Contains(views, "timeline") || slices.Contains(views, "calendar") || slices.Contains(views, "agenda"))
				if search, _ := n.Props["search"].(bool); n.Props["search"] == nil || search {
					searches[n.ID]++
				}
			case "QueryField":
				searches[text(n.Props["for"])]++
			case "RecordPage", "Thread":
				detail = true
			case "Panel":
				detail = detail || n.Bind != nil && n.Bind.Selection != ""
			case "Map", "Timeline", "Graph", "FileViewer", "Canvas":
				hasVisual = hasVisual || n.Bind != nil
			case "Chart":
				if n.Props["kind"] == "metric" {
					if text(n.Props["title"]) == "" && n.Label == "" {
						add("look_unlabeled_number", "Give the metric a label that says what it measures.")
					}
					if n.Bind != nil && zeroMetric(screen.Views[n.Bind.View], sample) {
						add("look_zero_metric", "This metric has no value in the example data. Show a populated measure or omit the empty tile.")
					}
				} else {
					hasVisual = hasVisual || n.Bind != nil
				}
			case "Text":
				if plainNumber.MatchString(strings.TrimSpace(text(n.Props["text"]))) && n.Label == "" {
					add("look_unlabeled_number", "Name what this number measures.")
				}
			}
			if table {
				cols := words(n.Props["columns"])
				if n.Bind != nil {
					view := screen.Views[n.Bind.View]
					if len(cols) == 0 {
						cols = view.Select
					}
					record := app.Records[n.Bind.Records]
					base, _ := types.ForRecord(n.Bind.Records, record)
					for _, name := range cols {
						field, ok := record.Fields[name]
						if !ok {
							field = base.Fields[name]
						}
						if n.Part == "Table" && (field.Type == "url" || slices.ContainsFunc(sample.Records[n.Bind.Records], func(row map[string]any) bool {
							value := text(row[name])
							return strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://")
						})) {
							add("look_raw_url", "Show "+name+" as a link control so the address does not fill the table cell.")
							break
						}
					}
				}
				if narrow && len(cols) > 6 {
					add("look_narrow_region", "Keep six columns or fewer in a narrow region. Put secondary facts in the detail.")
				}
			}
			for _, child := range n.Children {
				walk(child, narrow || n.Part == "Row" && len(n.Children) > 1)
			}
		}
		walk(screen.Nav, false)
		walk(screen.Body, false)
		if fullTable && !detail {
			add("look_table_without_detail", "Put a compact queue and the selected record on the same work screen.")
		}
		for _, count := range searches {
			if count > 1 {
				add("look_duplicate_search", "Keep one search field for each list.")
				break
			}
		}
	}
	if !navReported && typeTabs(sectionTitles, app.Records) {
		findings = append(findings, problem(schema.PlanFile, "look_type_tabs", "Give the app job-based views instead of a section per type."))
	}
	if !hasVisual {
		findings = append(findings, problem(schema.PlanFile, "look_signature_missing", "Add a visual tied to the job, such as evidence, a schedule, a map, or a trend."))
	}
	return findings
}

func typeTabs(labels []string, records map[string]schema.RecordType) bool {
	forms := map[string]string{}
	normalize := func(s string) string {
		return strings.ToLower(strings.NewReplacer(" ", "", "-", "", "_", "").Replace(s))
	}
	for _, key := range slices.Sorted(maps.Keys(records)) {
		t := records[key]
		for _, label := range []string{key, t.Label} {
			if label == "" {
				continue
			}
			name := normalize(label)
			forms[name], forms[name+"s"], forms[name+"es"] = key, key, key
			if strings.HasSuffix(name, "y") {
				forms[strings.TrimSuffix(name, "y")+"ies"] = key
			}
		}
	}
	matched := map[string]bool{}
	for _, label := range labels {
		if key := forms[normalize(label)]; key != "" {
			matched[key] = true
		}
	}
	return len(matched) >= 2 && len(matched)*2 >= len(labels)
}

func zeroMetric(view schema.QueryIR, sample schema.Sample) bool {
	rows, sampled := sample.Records[view.Type]
	if !sampled || len(view.Aggregate) != 1 || len(view.Args) != 0 || view.Relate != nil {
		return false
	}
	for _, filter := range view.Filter {
		if filter.Op != "eq" && filter.Op != "ne" {
			return false
		}
	}
	agg := view.Aggregate[0]
	if agg.Op != "count" && agg.Op != "sum" && agg.Op != "avg" {
		return false
	}
	total, known := float64(0), agg.Op == "count"
	for _, row := range rows {
		match := true
		for _, f := range view.Filter {
			equal := fmt.Sprint(row[f.Field]) == fmt.Sprint(f.Value)
			match = match && equal == (f.Op == "eq")
		}
		if !match {
			continue
		}
		if agg.Op == "count" {
			return false
		}
		if v, err := strconv.ParseFloat(fmt.Sprint(row[agg.Field]), 64); err == nil {
			total += v
			known = true
		}
	}
	return known && total == 0
}

func array(value any) []any {
	items, _ := value.([]any)
	return items
}

func text(value any) string {
	s, _ := value.(string)
	return s
}

func words(value any) []string {
	var out []string
	for _, item := range array(value) {
		if word := text(item); word != "" {
			out = append(out, word)
		}
	}
	return out
}
