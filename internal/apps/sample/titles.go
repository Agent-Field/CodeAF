package sample

import (
	"strings"
	"unicode"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// Item titles are noun phrases: a time qualifier (a month or season reads
// naturally before any work noun: "October order", "Year-end deal"), an optional descriptive
// value from the record's own schema, and the record's label as head noun.
// No verbs lead, so a list reads as things rather than chores.
var titleQualifiers = []string{"January", "February", "March", "April", "June", "July", "August", "September", "October", "November", "December", "Year-end", "Mid-year", "Holiday", "Weekend", "Winter"}
var titleSeries = []string{"", "second", "follow-up", "community", "flagship", "starter", "classic", "express"}

func titleValue(typ string, schema *spec.RecordType, f *spec.Field, i, offset int) string {
	// A literal default can name the first record, but cannot name the entire preview.
	if i == 0 && useDefault(f) {
		if value, err := defaultValue(f); err == nil {
			if text, ok := value.(string); ok && readableTitle(text) {
				return text
			}
		}
	}
	return kindOf(typ, schema).title(typ, schema, i, offset)
}

// rowTitle adds the person or company an item belongs to ("Spring deal —
// Harbor Group") when the record refers to one. Person and company records
// keep their own names.
func rowTitle(c cell) string {
	title := titleValue(c.typ, c.schema, c.f, c.i, typeOffset(c.app, c.typ))
	if c.app == nil || c.i == 0 && useDefault(c.f) || kindOf(c.typ, c.schema).name != kindItem {
		return title
	}
	if owner, _ := ownerTitle(c); owner != "" {
		return title + " — " + owner
	}
	return title
}

// ownerTitle is the title of the first person or company this row refers to,
// chosen exactly as referenceValue chooses the target, and the target count.
func ownerTitle(c cell) (string, int) {
	for _, name := range keys(c.schema.Fields) {
		f := c.schema.Fields[name]
		target := c.app.Records[f.To]
		if f.Type != spec.FieldRef || target == nil || f.To == c.typ || len(c.rows[f.To]) == 0 {
			continue
		}
		if kindOf(f.To, target).name == kindItem {
			continue
		}
		n := len(c.rows[f.To])
		j := c.i % n
		if title, ok := c.words.title(f.To, j); ok {
			return title, n
		}
		if tf := target.Fields[target.Title]; tf != nil && textual(tf.Type) {
			return titleValue(f.To, target, tf, j, 0), n
		}
	}
	return "", 0
}

// recordKind decides how a record type is named in previews.
type recordKind struct {
	name  string
	match func(typ string, schema *spec.RecordType) bool
	title func(typ string, schema *spec.RecordType, i, offset int) string
}

const (
	kindPerson = "person"
	kindOrg    = "organization"
	kindItem   = "item"
)

var recordKinds = []recordKind{
	{kindPerson, func(typ string, s *spec.RecordType) bool {
		return personTitle(typ) || personTitle(s.Label) || personField(s.Title, titleLabel(s))
	}, func(_ string, _ *spec.RecordType, i, _ int) string { return personName(i) }},
	{kindOrg, func(typ string, s *spec.RecordType) bool {
		return orgTitle(typ) || orgTitle(s.Label) || orgField(s.Title, titleLabel(s))
	},
		func(_ string, _ *spec.RecordType, i, _ int) string { return orgName(i) }},
	{kindItem, func(string, *spec.RecordType) bool { return true }, itemTitle},
}

func kindOf(typ string, schema *spec.RecordType) recordKind {
	for _, kind := range recordKinds {
		if kind.match(typ, schema) {
			return kind
		}
	}
	return recordKinds[len(recordKinds)-1]
}

func titleLabel(schema *spec.RecordType) string {
	if f := schema.Fields[schema.Title]; f != nil {
		return f.Label
	}
	return ""
}

func itemTitle(typ string, schema *spec.RecordType, i, offset int) string {
	head := strings.ToLower(label(schema.Label, typ))
	modifiers := schemaModifiers(schema)
	if len(modifiers) == 0 {
		modifiers = []string{""}
	}
	m := modifiers[i%len(modifiers)]
	i /= len(modifiers)
	// A descriptive value names a row on its own; a bare head needs a qualifier.
	qualifiers := titleQualifiers
	if m != "" {
		qualifiers = append([]string{""}, titleQualifiers...)
	}
	// Each type starts at its own qualifier so two lists never read alike.
	q := qualifiers[(i+offset)%len(qualifiers)]
	i /= len(qualifiers)
	s := titleSeries[i%len(titleSeries)]
	i /= len(titleSeries)
	words := []string{q, s, m, head}
	if i > 0 {
		words = append(words, letters(i-1))
	}
	return upperFirst(strings.Join(strings.Fields(strings.Join(words, " ")), " "))
}

// typeOffset spaces the record types of one app across the qualifier list;
// the stride is coprime with its length, so offsets stay distinct.
func typeOffset(app *spec.App, typ string) int {
	if app == nil {
		return 0
	}
	const stride = 5
	for n, name := range keys(app.Records) {
		if name == typ {
			return n * stride % len(titleQualifiers)
		}
	}
	return 0
}

// schemaModifiers are short descriptive enum values (a category, a material,
// a channel); classification values such as status or priority are left out.
func schemaModifiers(schema *spec.RecordType) []string {
	var result []string
	seen := map[string]bool{}
	for _, name := range keys(schema.Fields) {
		f := schema.Fields[name]
		if name == schema.Title || f.Type != spec.FieldEnum && f.Type != spec.FieldEnums || classificationField(name+" "+f.Label) {
			continue
		}
		for _, value := range f.Values {
			key := strings.ToLower(value)
			if readableSubject(key) && len(strings.Fields(value)) <= 2 && !seen[key] {
				result = append(result, value)
				seen[key] = true
			}
		}
	}
	return result
}

func classificationField(text string) bool {
	return hasWord(text, "status", "state", "stage", "phase", "severity", "priority", "size", "id", "identifier", "code", "serial", "sku", "created", "updated", "owner", "assignee", "type", "kind")
}

func readableSubject(text string) bool {
	if !readableTitle(text) || len(strings.Fields(text)) > 3 {
		return false
	}
	return !strings.ContainsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsSpace(r) && r != '-'
	})
}

func readableTitle(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || text == "<nil>" || strings.Contains(strings.ToLower(text), "sample_") {
		return false
	}
	// Stored identifiers are not names, even when they came from a default or computation.
	if strings.ContainsAny(text, "_/\\") || len([]rune(text)) <= 2 {
		return false
	}
	return len(strings.Fields(text)) > 1 || !strings.ContainsFunc(text, unicode.IsDigit)
}
