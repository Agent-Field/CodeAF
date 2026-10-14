package check

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/query"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

var conditionParam = regexp.MustCompile(`(?:^|[^a-z0-9_.-])\$?params\.([a-z_][a-z0-9_-]*(?:\.[a-z_][a-z0-9_-]*)*)`)
var conditionString = regexp.MustCompile(`"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'`)

var referenceTemplate = regexp.MustCompile(`\{\{([^{}]*)\}\}`)

func valueReferences(value any, path string, visit func(string, string)) {
	switch v := value.(type) {
	case string:
		if strings.HasSuffix(path, "/if") || strings.HasSuffix(path, "/until") {
			for _, match := range conditionParam.FindAllStringSubmatch(conditionString.ReplaceAllString(v, " "), -1) {
				visit("params."+match[1], path)
			}
		}
		if strings.HasPrefix(v, "$") && !strings.ContainsAny(v, " \t\n") {
			visit(v[1:], path)
		}
		for _, match := range referenceTemplate.FindAllStringSubmatch(v, -1) {
			visit(match[1], path)
		}
	case map[string]any:
		for _, key := range keys(v) {
			valueReferences(v[key], ptr(path, key), visit)
		}
	case []any:
		for i, item := range v {
			valueReferences(item, fmt.Sprintf("%s/%d", path, i), visit)
		}
	}
}

func paramFields(b *spec.Bundle) []Issue {
	var issues []Issue
	for _, id := range b.RunIDs() {
		run := b.Runs[id]
		file := "runs/" + id + ".yaml"
		for _, name := range keys(run.Params) {
			param := run.Params[name]
			if (param.Type == "ref" || param.Type == "refs") && b.App.Records[param.To] == nil {
				issues = append(issues, finding(file, ptr("/params", name)+"/to", "schema/run-param", "unknown record type "+param.To, "use a record type declared in app.json"))
			}
		}
		raw, _ := json.Marshal(run.Steps)
		var steps any
		_ = json.Unmarshal(raw, &steps)
		valueReferences(steps, "/steps", func(ref, path string) {
			parts := strings.Split(ref, ".")
			if len(parts) < 2 || parts[0] != "params" {
				return
			}
			param := run.Params[parts[1]]
			if param == nil {
				issues = append(issues, finding(file, path, "schema/run-param", fmt.Sprintf("run %s references unknown parameter %q", id, parts[1]), "use a declared parameter: "+strings.Join(keys(run.Params), ", ")))
				return
			}
			if len(parts) == 2 {
				return
			}
			hint := "params hold typed values; use $params." + parts[1] + " directly"
			if record := b.App.Records[param.To]; record != nil {
				hint += "; load the record with records.query before reading its fields; " + fieldHint(param.To, record)
			}
			issues = append(issues, finding(file, path, "schema/run-param", fmt.Sprintf("run %s cannot read field %q from parameter %q", id, strings.Join(parts[2:], "."), parts[1]), hint))
		})
	}
	for i, trigger := range b.App.Triggers {
		record := b.App.Records[trigger.Type]
		if record == nil {
			continue
		}
		valueReferences(trigger.Params, fmt.Sprintf("/triggers/%d/params", i), func(ref, path string) {
			parts := strings.Split(ref, ".")
			if len(parts) < 2 || parts[0] != "record" {
				return
			}
			field := parts[1]
			if field == "fields" && len(parts) > 2 {
				field = parts[2]
			}
			if _, err := query.Expression(record, field); err != nil {
				issues = append(issues, finding(spec.AppFile, path, "schema/run-param", fmt.Sprintf("trigger %s: %v", trigger.ID, err), fieldHint(trigger.Type, record)))
			}
		})
	}
	return issues
}
