package check

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

func missingActions(def schema.Definition) []schema.Problem {
	app, _ := schema.ParseApp(def.Files[schema.AppFile])
	plan, _ := schema.ParsePlan(def.Files[schema.PlanFile])
	flows := map[string]schema.Run{}
	for _, file := range def.RunFiles() {
		if flow, err := schema.ParseRun(def.Files[file]); err == nil {
			flows[flow.Run] = flow
		}
	}
	registered := map[string]bool{}
	for _, action := range schema.ActionRegistry(app, flows) {
		registered[action.ID] = true
	}
	var issues []schema.Problem
	for _, command := range app.Commands {
		if registered[command.ID] {
			continue
		}
		if slices.ContainsFunc(command.Do, commandWritesRecords) {
			issues = append(issues, problem(schema.AppFile, "command_action_missing",
				fmt.Sprintf("Make %q grantable. Bind its command to a run with typed parameters and record steps.", command.Label)))
		}
	}
	if plan.Agent != nil {
		for _, id := range plan.Agent.Actions {
			if app.Agent == nil || !registered[id] {
				issues = append(issues, problem(schema.PlanFile, "agent_action_missing",
					fmt.Sprintf("The assistant promises %q. Add a matching grantable Action and an app assistant.", id)))
			}
		}
	}
	return issues
}

func commandWritesRecords(action schema.Action) bool {
	for key := range action {
		if strings.HasPrefix(key, "records.") || key == "runs.start" {
			return true
		}
	}
	return false
}
