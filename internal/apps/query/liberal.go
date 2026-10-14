package query

import (
	"regexp"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// LIBERAL IN, STRICT OUT. Writers (people and models) spell a date filter the
// way they would say it: `due < now + 14d`, `status:Part\ paid`. normalize
// folds those spellings into the one token form before parsing, so the AST and
// the SQL stay exactly as strict as before.

var (
	// escapedValue is an unquoted value holding backslash-escaped spaces.
	escapedValue = regexp.MustCompile(`([:<>=~])((?:[^\s"'\\]|\\ )*\\ (?:[^\s"'\\]|\\ )*)`)
	// spacedMoment is `now + 14d` or `today - days(7)` written with spaces.
	spacedMoment = regexp.MustCompile(`\b(now|today|tomorrow|yesterday)\s*([+-])\s*(\d+[dwhm]\b|days\(\s*[0-9.]+\s*\))`)
	innerSpace   = regexp.MustCompile(`\s+`)
)

func normalize(schema *spec.RecordType, text string) string {
	text = escapedValue.ReplaceAllStringFunc(text, func(m string) string {
		return m[:1] + `"` + strings.ReplaceAll(m[1:], `\ `, " ") + `"`
	})
	text = spacedMoment.ReplaceAllStringFunc(text, func(m string) string { return innerSpace.ReplaceAllString(m, "") })
	if spaced := spacedComparison(schema); spaced != nil {
		text = spaced.ReplaceAllString(text, "$1$2$3")
	}
	return text
}

// spacedComparison matches `field < value` only for the record type's own
// date and datetime fields (and the record clocks), so free text is untouched.
func spacedComparison(schema *spec.RecordType) *regexp.Regexp {
	names := []string{"created_at", "updated_at"}
	for name, f := range schema.Fields {
		if f != nil && (f.Type == spec.FieldDate || f.Type == spec.FieldDatetime) {
			names = append(names, regexp.QuoteMeta(name))
		}
	}
	sort.Strings(names)
	re, err := regexp.Compile(`(^|\s)(` + strings.Join(names, "|") + `)\s*(<=|>=|<|>)\s+`)
	if err != nil {
		return nil
	}
	return re
}
