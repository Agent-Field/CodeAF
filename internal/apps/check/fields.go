package check

import (
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/query"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func fieldHint(typ string, record *spec.RecordType) string {
	return fmt.Sprintf("use fields declared on %s: %s (record metadata: id, version, created_at, updated_at, created_by, updated_by)", typ, strings.Join(keys(record.Fields), ", "))
}

func queryIssue(file, path, typ, text string, record *spec.RecordType) []Issue {
	if record == nil {
		return nil
	}
	if _, err := query.Parse(record, text, svc.Owner); err != nil {
		return []Issue{finding(file, path, "schema/query", fmt.Sprintf("%s query %q: %v", file, text, err), fieldHint(typ, record)+"; "+spec.RunQueryRule)}
	}
	return nil
}

// Templates may depend on a paused step. Field names still belong to the
// record schema, so check those without needing the step's output or value type.
func queryFields(file, path, typ, text string, record *spec.RecordType) []Issue {
	if record == nil || strings.HasPrefix(text, "$") {
		return nil
	}
	stub, stubText := query.Stub(record, text)
	return queryIssue(file, path, typ, stubText, stub)
}
