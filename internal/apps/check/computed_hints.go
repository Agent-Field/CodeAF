package check

import (
	"regexp"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// beyondOneRecord matches the compile errors of a computed field that reaches
// past its own record: an aggregate or another type's name is an undeclared
// reference, and SQL words or other languages' operators are syntax errors.
var beyondOneRecord = regexp.MustCompile(`undeclared reference to|Syntax error`)

const computedFallbackHint = "use declared record fields and duration literals, for example now - days(7)"

// computedHint is the repair for one computed-field compile error.
func computedHint(typ string, record *spec.RecordType, err error) string {
	if !beyondOneRecord.MatchString(err.Error()) {
		return computedFallbackHint
	}
	return "a computed field can read only this " + typ + " record's own fields (" + strings.Join(keys(record.Fields), ", ") +
		"), with operators && || ! and no aggregates (no sum, count, where, and, or, ??); totals across records belong in a Chart or Stat on a screen: remove this computed field or make it a plain number field"
}
