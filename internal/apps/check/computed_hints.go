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

// reservedName matches the compile error of a field whose id is a word the
// expression language keeps for itself (type, string, int, list, map, bool).
var reservedName = regexp.MustCompile(`overlapping identifier for name '([^']+)'`)

const computedFallbackHint = "use declared record fields and duration literals, for example now - days(7)"

// computedHint is the repair for one computed-field compile error.
func computedHint(typ string, record *spec.RecordType, err error) string {
	if m := reservedName.FindStringSubmatch(err.Error()); m != nil {
		return "the expression is fine; the field id \"" + m[1] + "\" on " + typ + " is a reserved word in expressions: rename that field to " + typ + "_" + m[1] + " in app.json and in every screen, query and run that names it"
	}
	if !beyondOneRecord.MatchString(err.Error()) {
		return computedFallbackHint
	}
	return "a computed field can read only this " + typ + " record's own fields (" + strings.Join(keys(record.Fields), ", ") +
		"), with operators && || ! and no aggregates (no sum, count, where, and, or, ??); totals across records belong in a Chart or Stat on a screen: remove this computed field or make it a plain number field"
}
