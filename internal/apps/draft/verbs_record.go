package draft

import (
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

func init() {
	register("record", recordDef())
	register("field", fieldDef())
	register("constant", constantDef())
}

type recordArgs struct {
	id, label, plural, title string
	isNew                    bool
}

func recordDef() def[recordArgs] {
	return def[recordArgs]{
		Usage:  "codeaf app record <id> [--label L] [--plural P] [--title <field>] [--new]",
		Values: []string{"label", "plural", "title"},
		Bools:  []string{"new"},
		Decode: func(a argv, _ string) (recordArgs, error) {
			if err := a.exactly(1); err != nil {
				return recordArgs{}, err
			}
			return recordArgs{a.pos[0], a.str("label"), a.str("plural"), a.str("title"), a.bools["new"]}, nil
		},
		Piece: func(r recordArgs) Piece { return Piece{Kind: "record", ID: r.id} },
		Check: func(st State, _ Entry, r recordArgs) error {
			if err := checkSlug("record", r.id); err != nil {
				return err
			}
			if st.Records[r.id] != nil || r.isNew || planHas(st.Plan.Data, r.id) {
				return nil
			}
			return refuse("record %q is not in the plan (data lines: %s) — plan it first with: codeaf app plan data %s \"<line>\", or add --new", r.id, ids(planIDs(st.Plan.Data)), r.id)
		},
		Apply: func(st *State, r recordArgs) {
			rec := ensureRecord(st, r.id)
			rec.Label = orElse(r.label, rec.Label)
			rec.Plural = orElse(r.plural, rec.Plural)
			rec.Title = orElse(r.title, rec.Title)
		},
		Line: func(st State, r recordArgs) string { return recordLine(st, r.id) },
	}
}

func orElse(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}

func ensureRecord(st *State, id string) *spec.RecordType {
	if st.Records[id] == nil {
		st.Records[id] = &spec.RecordType{Fields: map[string]*spec.Field{}}
	}
	return st.Records[id]
}

// recordLine is the owner-readable sentence for a record: its name and the
// fields it has so far, in the order they were added.
func recordLine(st State, id string) string {
	rec := st.Records[id]
	if rec == nil {
		return titleOf(id)
	}
	name := orElse(rec.Label, titleOf(id))
	names := fieldOrder(st, id)
	if len(names) == 0 {
		return name + " · record added"
	}
	return name + " · " + strings.Join(names, ", ")
}

// fieldOrder lists a record's fields in the order they were added.
func fieldOrder(st State, id string) []string { return st.FieldOrder[id] }

type fieldArgs struct {
	record, name string
	field        spec.Field
}

// stringTyped field types keep a --default as text; the rest read it as JSON.
var stringTyped = map[spec.FieldType]bool{
	spec.FieldText: true, spec.FieldLongtext: true, spec.FieldEnum: true, spec.FieldDate: true,
	spec.FieldDatetime: true, spec.FieldDuration: true, spec.FieldPerson: true, spec.FieldRef: true,
	spec.FieldEmail: true, spec.FieldPhone: true, spec.FieldURL: true, spec.FieldPlace: true,
}

func fieldDef() def[fieldArgs] {
	return def[fieldArgs]{
		Usage:  "codeaf app field <record> <name> <type> [--required] [--unique] [--index] [--masked] [--label L] [--values a,b] [--to <record>] [--default v] [--currency USD] [--computed expr] [--ai prompt]",
		Values: []string{"label", "values", "to", "default", "currency", "computed", "ai"},
		Bools:  []string{"required", "unique", "index", "masked"},
		Decode: func(a argv, _ string) (fieldArgs, error) {
			if err := a.exactly(3); err != nil {
				return fieldArgs{}, err
			}
			f := spec.Field{
				Type: spec.FieldType(a.pos[2]), Label: a.str("label"), Required: a.bools["required"],
				Unique: a.bools["unique"], Index: a.bools["index"], Masked: a.bools["masked"],
				Values: a.list("values"), To: a.str("to"), Currency: a.str("currency"),
				Computed: a.str("computed"), AI: a.str("ai"),
			}
			if d := a.str("default"); d != "" {
				f.Default = fieldDefault(f.Type, d)
			}
			return fieldArgs{a.pos[0], a.pos[1], f}, nil
		},
		Piece: func(f fieldArgs) Piece { return Piece{Kind: "field", ID: f.record + "." + f.name} },
		Check: checkField,
		Apply: func(st *State, f fieldArgs) {
			rec := ensureRecord(st, f.record)
			if rec.Fields[f.name] == nil {
				st.FieldOrder[f.record] = append(st.FieldOrder[f.record], f.name)
			}
			field := f.field
			rec.Fields[f.name] = &field
		},
		Line: func(st State, f fieldArgs) string { return recordLine(st, f.record) },
	}
}

func fieldDefault(t spec.FieldType, text string) json.RawMessage {
	if !stringTyped[t] && json.Valid([]byte(text)) {
		return json.RawMessage(text)
	}
	raw, _ := json.Marshal(text)
	return raw
}

func checkField(st State, _ Entry, f fieldArgs) error {
	if st.Records[f.record] == nil {
		return refuse("record %q does not exist (records: %s) — make it first with: codeaf app record %s", f.record, ids(sortedKeys(st.Records)), f.record)
	}
	if err := checkSlug("field", f.name); err != nil {
		return err
	}
	label := f.record + "." + f.name
	if issues := spec.ValidateDef(label, spec.AppSchemaURL, "field", asAny(f.field)); len(issues) > 0 {
		return refuseAll(issueLines(label, issues, "codeaf app schema field"))
	}
	return checkFieldRefs(st, label, f.field)
}

// checkFieldRefs holds the rules the schema cannot see because they need the
// rest of the draft: the target record, the enum default, the currency type.
func checkFieldRefs(st State, label string, f spec.Field) error {
	if f.To != "" && st.Records[f.To] == nil {
		return refuse("%s: /to %q is not a record yet (records: %s) — define record %s first", label, f.To, ids(sortedKeys(st.Records)), f.To)
	}
	if f.Currency != "" && f.Type != spec.FieldMoney {
		return refuse("%s: /currency only applies to a money field, this one is %s", label, f.Type)
	}
	if f.Type == spec.FieldEnum && len(f.Default) > 0 {
		var def string
		if json.Unmarshal(f.Default, &def) == nil && !contains(f.Values, def) {
			return refuse("%s: /default %q is not one of its values (values: %s)", label, def, strings.Join(f.Values, ", "))
		}
	}
	return nil
}

type constantArgs struct {
	record, name string
	value        any
}

func constantDef() def[constantArgs] {
	return def[constantArgs]{
		Usage: "codeaf app constant <record> <name> < value.json",
		Body:  true,
		Decode: func(a argv, body string) (constantArgs, error) {
			if err := a.exactly(2); err != nil {
				return constantArgs{}, err
			}
			var v any
			if err := json.Unmarshal([]byte(body), &v); err != nil {
				return constantArgs{}, refuse("constant %s.%s: the body on stdin is not JSON: %v", a.pos[0], a.pos[1], err)
			}
			return constantArgs{a.pos[0], a.pos[1], v}, nil
		},
		Piece: func(c constantArgs) Piece { return Piece{Kind: "constant", ID: c.record + "." + c.name} },
		Check: func(st State, _ Entry, c constantArgs) error {
			if st.Records[c.record] == nil {
				return refuse("record %q does not exist (records: %s)", c.record, ids(sortedKeys(st.Records)))
			}
			return checkSlug("constant", c.name)
		},
		Apply: func(st *State, c constantArgs) {
			rec := ensureRecord(st, c.record)
			if rec.Constants == nil {
				rec.Constants = map[string]any{}
			}
			rec.Constants[c.name] = c.value
		},
		Line: func(st State, c constantArgs) string {
			return orElse(st.Records[c.record].Label, titleOf(c.record)) + " · constant " + c.name
		},
	}
}
