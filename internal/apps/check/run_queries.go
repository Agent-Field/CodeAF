package check

import (
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/query"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Literal queries can be checked even when an earlier gate pauses execution.
func runQueries(file, path string, steps []*spec.Step, app *spec.App) []Issue {
	var issues []Issue
	for i, step := range steps {
		at := fmt.Sprintf("%s/%d", path, i)
		if rec := step.RecordsQuery; rec != nil && !strings.Contains(rec.Query, "{{") && !strings.HasPrefix(rec.Query, "$") {
			if typ := app.Records[rec.Type]; typ != nil {
				if _, err := query.Parse(typ, rec.Query, svc.Owner); err != nil {
					issues = append(issues, finding(file, at+"/records.query/query", "schema/run-query", fmt.Sprintf("invalid query %q: %v", rec.Query, err), spec.RunQueryRule))
				}
			}
		}
		issues = append(issues, runQueries(file, at+"/steps", step.Steps, app)...)
	}
	return issues
}
