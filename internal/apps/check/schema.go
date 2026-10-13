package check

import (
	"fmt"
	"path"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

func validateFiles(files spec.Files) []Issue {
	issues := []Issue{}
	for _, issue := range spec.Validate(files) {
		issues = append(issues, Issue{Issue: issue, FixHint: "match the file schema in internal/apps/spec/schema"})
	}
	for _, name := range files.Paths() {
		if definitionPath(name) {
			continue
		}
		issues = append(issues, finding(name, "", "schema/file", "unsupported definition file", "write app.json, plan.json, agent.md, screens/<id>.json or runs/<id>.yaml"))
	}
	if len(issues) > 0 {
		return issues
	}
	b, err := spec.Decode(files)
	if err != nil {
		return append(issues, finding("", "", "schema/decode", err.Error(), "write valid definition files"))
	}
	issues = append(issues, planTargets(b)...)
	issues = append(issues, recordTargets(b)...)
	for _, id := range b.RunIDs() {
		issues = append(issues, runActions("runs/"+id+".yaml", "/steps", b.Runs[id].Steps)...)
	}
	return issues
}

func definitionPath(name string) bool {
	if name == spec.AppFile || name == spec.PlanFile || name == spec.AgentFile {
		return true
	}
	dir, file := path.Split(name)
	return dir == "screens/" && strings.HasSuffix(file, ".json") || dir == "runs/" && strings.HasSuffix(file, ".yaml")
}

func planTargets(b *spec.Bundle) []Issue {
	var issues []Issue
	sections := map[string][]spec.PlanLine{"data": b.Plan.Data, "automation": b.Plan.Automation, "screens": b.Plan.Screens}
	for _, section := range keys(sections) {
		lines := sections[section]
		seen := map[string]bool{}
		for i, line := range lines {
			exists := false
			switch section {
			case "data":
				exists = b.App.Records[line.ID] != nil
			case "automation":
				exists = b.Runs[line.ID] != nil
			case "screens":
				exists = b.Screens[line.ID] != nil
			}
			at := fmt.Sprintf("/%s/%d/id", section, i)
			if !exists {
				issues = append(issues, finding(spec.PlanFile, at, "schema/plan", "planned "+section+" target "+line.ID+" is missing", "write the record type or file named by this plan line"))
			}
			if seen[line.ID] {
				issues = append(issues, finding(spec.PlanFile, at, "schema/plan", "duplicate plan id "+line.ID, "give each line a distinct target id"))
			}
			seen[line.ID] = true
		}
	}
	return issues
}

func recordTargets(b *spec.Bundle) []Issue {
	var issues []Issue
	for _, typ := range keys(b.App.Records) {
		record := b.App.Records[typ]
		base := ptr("/records", typ)
		if record.Fields[record.Title] == nil {
			issues = append(issues, finding(spec.AppFile, base+"/title", "schema/record", "title names an unknown field", "name a field declared on this record type"))
		}
		for _, name := range keys(record.Fields) {
			field := record.Fields[name]
			if (field.Type == spec.FieldRef || field.Type == spec.FieldRefs) && b.App.Records[field.To] == nil {
				issues = append(issues, finding(spec.AppFile, ptr(base+"/fields", name)+"/to", "schema/record", "unknown relation type "+field.To, "name a record type declared in app.json"))
			}
		}
	}
	return issues
}

func runActions(file, path string, steps []*spec.Step) []Issue {
	var issues []Issue
	for i, step := range steps {
		at := fmt.Sprintf("%s/%d", path, i)
		actions := []bool{step.Agent != "", step.Tool != "", step.RecordsQuery != nil, step.RecordsCreate != nil, step.RecordsUpdate != nil, step.RecordsDelete != nil, step.Wait != nil, step.Steps != nil, step.Shell != ""}
		count := 0
		for _, present := range actions {
			if present {
				count++
			}
		}
		if count != 1 {
			issues = append(issues, finding(file, at, "schema/run-action", fmt.Sprintf("run step has %d actions; exactly one is required besides gate", count), "split multiple actions into separate steps"))
		}
		issues = append(issues, runActions(file, at+"/steps", step.Steps)...)
	}
	return issues
}
