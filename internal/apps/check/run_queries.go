package check

import (
	"fmt"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// Literal queries can be checked even when an earlier gate pauses execution.
func runQueries(file, path string, steps []*spec.Step, app *spec.App) []Issue {
	var issues []Issue
	for i, step := range steps {
		at := fmt.Sprintf("%s/%d", path, i)
		if rec := step.RecordsQuery; rec != nil {
			found := queryFields(file, at+"/records.query/query", rec.Type, rec.Query, app.Records[rec.Type])
			for i := range found {
				found[i].Rule = "schema/run-query"
			}
			issues = append(issues, found...)
		}
		issues = append(issues, runQueries(file, at+"/steps", step.Steps, app)...)
	}
	return issues
}
