package sample

import (
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// Rows are generated field by field; cohere then makes the fields of one row
// agree with each other the way a real record would.
var coherenceRules = []func(schema *spec.RecordType, fields map[string]any, i int, now time.Time){
	endsAfterStarts,
	outcomeDatesFollowStatus,
	lateStatusIsPastDue,
}

func cohere(schema *spec.RecordType, fields map[string]any, i int, now time.Time) {
	for _, rule := range coherenceRules {
		rule(schema, fields, i, now)
	}
}

var (
	beginWords  = []string{"issue", "issued", "start", "starts", "started", "begin", "begins", "opened", "requested", "created", "sent", "invoiced"}
	finishWords = []string{"due", "end", "ends", "deadline", "closes", "expires", "expiry", "delivery"}
	lateWords   = []string{"late", "overdue"}
)

// dateFields lists the generated, non-unique date fields whose words match.
func dateFields(schema *spec.RecordType, words []string) []string {
	var result []string
	for _, name := range keys(schema.Fields) {
		f := schema.Fields[name]
		dated := f.Type == spec.FieldDate || f.Type == spec.FieldDatetime
		if dated && f.Computed == "" && !f.Unique && hasWord(name+" "+f.Label, words...) {
			result = append(result, name)
		}
	}
	return result
}

func readDate(f *spec.Field, value any) (time.Time, bool) {
	text, ok := value.(string)
	if !ok {
		return time.Time{}, false
	}
	layout := time.RFC3339Nano
	if f.Type == spec.FieldDate {
		layout = "2006-01-02"
	}
	at, err := time.Parse(layout, text)
	return at, err == nil
}

// endsAfterStarts puts a due or end date after the row's issue or start date.
func endsAfterStarts(schema *spec.RecordType, fields map[string]any, i int, _ time.Time) {
	begins := dateFields(schema, beginWords)
	if len(begins) == 0 {
		return
	}
	begin, ok := readDate(schema.Fields[begins[0]], fields[begins[0]])
	if !ok {
		return
	}
	for _, name := range dateFields(schema, finishWords) {
		f := schema.Fields[name]
		if end, ok := readDate(f, fields[name]); ok && !end.After(begin) && name != begins[0] {
			fields[name] = formatDate(f, begin.AddDate(0, 0, 14+i%3*7), i)
		}
	}
}

// outcomeDatesFollowStatus clears an outcome date (paid on, delivered at) on
// rows whose status comes before that outcome in the enum's order; rows at or
// past it keep their past date ("requested" stays on an approved request).
func outcomeDatesFollowStatus(schema *spec.RecordType, fields map[string]any, _ int, _ time.Time) {
	status, values := statusOf(schema, fields)
	if status == "" {
		return
	}
	for _, name := range dateFields(schema, pastDateWords) {
		f := schema.Fields[name]
		outcome := slices.Index(values, outcomeValue(name+" "+f.Label, values))
		if outcome > 0 && slices.Index(values, status) < outcome && !f.Required {
			delete(fields, name)
		}
	}
}

// lateStatusIsPastDue moves the due date of a late or overdue row behind now
// and clears its outcome dates: a late invoice is not paid.
func lateStatusIsPastDue(schema *spec.RecordType, fields map[string]any, i int, now time.Time) {
	status, values := statusOf(schema, fields)
	if !hasWord(status, lateWords...) {
		return
	}
	for _, name := range dateFields(schema, pastDateWords) {
		f := schema.Fields[name]
		if outcomeValue(name+" "+f.Label, values) != "" && !f.Required {
			delete(fields, name)
		}
	}
	for _, name := range dateFields(schema, finishWords) {
		f := schema.Fields[name]
		fields[name] = formatDate(f, now.AddDate(0, 0, -3-i%10), i)
	}
}

// statusOf is the row's pipeline enum value and that enum's values.
func statusOf(schema *spec.RecordType, fields map[string]any) (string, []string) {
	for _, name := range keys(schema.Fields) {
		f := schema.Fields[name]
		if f.Type == spec.FieldEnum && hasWord(name+" "+f.Label, "status", "state", "stage", "phase") {
			value, _ := fields[name].(string)
			return value, f.Values
		}
	}
	return "", nil
}

// outcomeValue is the status value a date field records ("paid_on" → "Paid"),
// matched by word stem, or "" when the status has no such value.
func outcomeValue(semantic string, values []string) string {
	for _, value := range values {
		stem := strings.ToLower(value)
		if len(stem) < 4 {
			continue
		}
		for _, token := range strings.Fields(label("", strings.ToLower(semantic))) {
			if len(token) >= 4 && strings.HasPrefix(token, stem[:4]) && strings.HasPrefix(stem, token[:4]) {
				return value
			}
		}
	}
	return ""
}
