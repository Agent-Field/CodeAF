package sample

import "github.com/Agent-Field/codeaf/internal/apps/spec"

// rowCounts caps each type's row count at the number of distinct values its
// unique fields can hold: a unique enum has one row per value, a unique bool
// two, a unique ref one per target row. A valid app is never failed for a
// value space smaller than the preview.
func rowCounts(app *spec.App, count int) map[string]int {
	counts := map[string]int{}
	for typ := range app.Records {
		counts[typ] = count
	}
	// Ref caps follow their targets; one pass per type reaches a fixpoint.
	for range app.Records {
		for typ, schema := range app.Records {
			for _, f := range schema.Fields {
				if limit, ok := valueSpace(f, counts); ok && limit < counts[typ] {
					counts[typ] = max(limit, 1)
				}
			}
		}
	}
	return counts
}

// valueSpace is how many distinct values the generator can give a unique
// field, when that space is bounded.
func valueSpace(f *spec.Field, counts map[string]int) (int, bool) {
	if !f.Unique || f.Computed != "" || useDefault(f) {
		return 0, false
	}
	switch f.Type {
	case spec.FieldEnum, spec.FieldEnums:
		return len(f.Values), true
	case spec.FieldBool:
		return 2, true
	case spec.FieldRef, spec.FieldRefs:
		n, ok := counts[f.To]
		return n, ok
	default:
		return 0, false
	}
}

// distinctTitle reports whether the sample keeps a type's titles distinct:
// only generated text titles, which the generator can always vary. An enum,
// date, person or computed title may repeat, as it can in real records.
func distinctTitle(schema *spec.RecordType) bool {
	f := schema.Fields[schema.Title]
	return f != nil && f.Computed == "" && textual(f.Type)
}

// distinctText varies a repeated text value for a field that must stay
// distinct, without numbering it: "Toronto", "Toronto AA", "Toronto AB".
func distinctText(value string, taken map[string]bool) string {
	base := value
	for k := 0; taken[quoted(value)]; k++ {
		value = base + " " + letters(k)
	}
	return value
}
