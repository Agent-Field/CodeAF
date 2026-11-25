package data

import (
	"bytes"

	"encoding/json"
	"fmt"
	appschema "github.com/Agent-Field/codeaf/internal/apps/schema"
	"regexp"
	"sort"
	"strings"
)

// THIS FILE READS ONLY WHAT THE DATA STORE NEEDS FROM app.json: the record
// types, their fields and rules, and the app's record capabilities. The whole
// file is validated against its JSON Schema elsewhere (internal/apps/schema);
// keys this file does not know are ignored rather than refused, so a newer
// app.json still registers its records on this store.

// The field types of v0 (SPEC §5).
const (
	TypeText     = "text"
	TypeLongtext = "longtext"
	TypeNumber   = "number"
	TypeMoney    = "money"
	TypeDate     = "date"
	TypeDatetime = "datetime"
	TypeEnum     = "enum"
	TypeEnums    = "enums"
	TypeBool     = "bool"
	TypePerson   = "person"
	TypeRef      = "ref"
	TypeRefs     = "refs"
	TypeEmail    = "email"
	TypePhone    = "phone"
	TypeURL      = "url"
)

var fieldTypes = map[string]bool{
	TypeText: true, TypeLongtext: true, TypeNumber: true, TypeMoney: true,
	TypeDate: true, TypeDatetime: true, TypeEnum: true, TypeEnums: true,
	TypeBool: true, TypePerson: true, TypeRef: true, TypeRefs: true,
	TypeEmail: true, TypePhone: true, TypeURL: true,
}

// identShape is every type and field name. NAMES ARE SPLICED INTO SQL AS
// JSON PATHS AND INDEX NAMES, so nothing else may ever be a name: lower-case
// letters, digits and underscores, starting with a letter.
var identShape = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

// reserved are the names a row carries for itself, which no field may take.
var reserved = map[string]bool{
	"id": true, "type": true, "created_at": true, "updated_at": true, "deleted_at": true,
}

// celReserved are CEL's reserved words; a field named one is stored and
// queried like any other but cannot be named in an expression.
var celReserved = map[string]bool{
	"as": true, "break": true, "const": true, "continue": true, "else": true,
	"false": true, "for": true, "function": true, "if": true, "import": true,
	"in": true, "let": true, "loop": true, "package": true, "namespace": true,
	"null": true, "return": true, "true": true, "var": true, "void": true, "while": true,
	// CEL's own type names and conversions are already declared.
	"type": true, "int": true, "uint": true, "double": true, "bool": true, "string": true,
	"bytes": true, "list": true, "map": true, "null_type": true, "dyn": true,
	"duration": true, "timestamp": true,
}

// FieldDef is one field as an app declares it: internal/appschema's definition type, so
// the validator and the store read one shape.
type FieldDef = appschema.Field

// FreezeRule makes matching records read-only: by a condition (When, with
// Except naming fields that stay writable), or by a period (the record locks
// After a duration once the Period holding its Field's date has ended).
type FreezeRule = appschema.Freeze

// TypeDef is one record type as an app declares it. Uses says the app uses a
// type another app owns; its Fields are then the fields it adds.
type TypeDef struct {
	Uses      bool                `json:"uses,omitempty"`
	Title     string              `json:"title,omitempty"`
	Label     string              `json:"label,omitempty"`
	Fields    map[string]FieldDef `json:"fields,omitempty"`
	Constants map[string]any      `json:"constants,omitempty"`
	Freeze    []FreezeRule        `json:"freeze,omitempty"`

	// order is the fields in the order the file declares them.
	order []string
}

// AppDef is what the data store reads from one app.json.
type AppDef struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	Records      map[string]TypeDef `json:"records"`
	Capabilities struct {
		Records map[string]string `json:"records"`
	} `json:"capabilities"`

	// typeOrder is the types in the order the file declares them.
	typeOrder []string
}

// ParseApp reads the parts of an app.json the data store needs and checks
// them: names, field types, enum values, refs, defaults and capabilities.
// Expressions are compiled when the app is registered, against the merged
// types.
func ParseApp(raw []byte) (*AppDef, error) {
	var def AppDef
	if err := json.Unmarshal(raw, &def); err != nil {
		return nil, errorf(CodeInvalid, "app.json could not be read: %v", err)
	}
	def.ID = strings.TrimSpace(def.ID)
	if def.ID == "" {
		return nil, errorf(CodeInvalid, "app.json has no id")
	}
	// The order of types and fields is read separately: a Go map forgets it,
	// and the Data place shows columns in the order the app declared them.
	var shape struct {
		Records json.RawMessage `json:"records"`
	}
	_ = json.Unmarshal(raw, &shape)
	def.typeOrder = objectKeys(shape.Records)
	var records map[string]json.RawMessage
	_ = json.Unmarshal(shape.Records, &records)
	for name, t := range def.Records {
		var fields struct {
			Fields json.RawMessage `json:"fields"`
		}
		_ = json.Unmarshal(records[name], &fields)
		t.order = objectKeys(fields.Fields)
		def.Records[name] = t
	}
	if err := def.check(); err != nil {
		return nil, err
	}
	return &def, nil
}

// check refuses what the store could not keep or would keep wrongly.
func (d *AppDef) check() error {
	for _, name := range d.typeOrder {
		t := d.Records[name]
		if !identShape.MatchString(name) {
			return errorf(CodeInvalid, "%q is not a record type name: use lower-case letters, digits and _", name)
		}
		if t.Title != "" {
			if f, ok := t.Fields[t.Title]; !t.Uses && (!ok || f.Type == "") {
				return errorf(CodeInvalid, "%s's title %q is not one of its fields", name, t.Title)
			}
		}
		for _, fname := range t.order {
			if err := checkField(name, fname, t.Fields[fname]); err != nil {
				return err
			}
		}
		for i, rule := range t.Freeze {
			if rule.When == "" && rule.Period == "" {
				return errorf(CodeInvalid, "%s freeze rule %d has neither when nor period", name, i+1)
			}
			if rule.Period != "" {
				if !periods[rule.Period] {
					return errorf(CodeInvalid, "%s freeze rule %d: period is week, month, quarter or year", name, i+1)
				}
				if _, err := parseSpan(rule.After); rule.After != "" && err != nil {
					return errorf(CodeInvalid, "%s freeze rule %d: after %q is not a span like 5d", name, i+1, rule.After)
				}
			}
		}
	}
	for typ, access := range d.Capabilities.Records {
		if access != "r" && access != "rw" {
			return errorf(CodeInvalid, "capability records.%s is %q; it is r or rw", typ, access)
		}
	}
	return nil
}

// checkField checks what one field declaration can be checked for on its own.
func checkField(typ, name string, f FieldDef) error {
	if !identShape.MatchString(name) {
		return errorf(CodeInvalid, "%s.%s is not a field name: use lower-case letters, digits and _", typ, name)
	}
	if reserved[name] {
		return errorf(CodeInvalid, "%s.%s: %s is a name every record already has", typ, name, name)
	}
	if !fieldTypes[f.Type] {
		return errorf(CodeInvalid, "%s.%s has type %q, which is not a field type", typ, name, f.Type)
	}
	if (f.Type == TypeEnum || f.Type == TypeEnums) && len(f.Values) == 0 {
		return errorf(CodeInvalid, "%s.%s is an %s with no values", typ, name, f.Type)
	}
	seen := map[string]bool{}
	for _, v := range f.Values {
		if strings.TrimSpace(v) == "" || seen[strings.ToLower(v)] {
			return errorf(CodeInvalid, "%s.%s has an empty or repeated value", typ, name)
		}
		seen[strings.ToLower(v)] = true
	}
	if (f.Type == TypeRef || f.Type == TypeRefs) && !identShape.MatchString(f.To) {
		return errorf(CodeInvalid, "%s.%s is a %s with no type to point to", typ, name, f.Type)
	}
	if f.Computed != "" && f.AI != "" {
		return errorf(CodeInvalid, "%s.%s cannot be both computed and filled by AI", typ, name)
	}
	if f.Computed != "" && (f.Required || f.Unique || f.Default != nil) {
		return errorf(CodeInvalid, "%s.%s is computed; it cannot also be required, unique or have a default", typ, name)
	}
	return nil
}

// objectKeys answers a JSON object's keys in the order they are written, or
// nil when raw is not an object.
func objectKeys(raw json.RawMessage) []string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil
	}
	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return keys
		}
		key, _ := tok.(string)
		keys = append(keys, key)
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return keys
		}
	}
	return keys
}

// sortedKeys is a map's keys in byte order, for a deterministic walk.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// canonicalJSON is v encoded with map keys sorted, which encoding/json
// already does; it names the intent at the call sites that compare encodings.
func canonicalJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("!%v", err)
	}
	return string(raw)
}
