package data

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	appschema "github.com/Agent-Field/codeaf/internal/apps/schema"
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

// FieldDef is one field as an app declares it: internal/appschema's
// definition type, so the validator and the store read one shape, and the
// declarations the store enforces that the schema has not yet named.
type FieldDef struct {
	appschema.Field
	// Sequence numbers each new record: INV-0042.
	Sequence *SequenceDef `json:"sequence,omitempty"`
}

// SequenceDef is a numbering: the text before the number and how many
// digits it is padded to.
type SequenceDef struct {
	Prefix string `json:"prefix,omitempty"`
	Pad    int    `json:"pad,omitempty"`
	// Padding is the schema's spelling of Pad; either is read.
	Padding int `json:"padding,omitempty"`
}

// digits is how many digits the number is padded to, by either spelling.
func (sq *SequenceDef) digits() int {
	return max(sq.Pad, sq.Padding)
}

// LedgerOption makes a type append-only. An app writes it as true, or as
// {"reversal": "<field>"} to name a reference field that a reversing entry
// fills with the entry it reverses.
type LedgerOption struct {
	On       bool
	Reversal string
}

func (l *LedgerOption) UnmarshalJSON(raw []byte) error {
	var on bool
	if err := json.Unmarshal(raw, &on); err == nil {
		*l = LedgerOption{On: on}
		return nil
	}
	var obj struct {
		Reversal string `json:"reversal"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return fmt.Errorf("ledger is true or {\"reversal\": field}")
	}
	*l = LedgerOption{On: true, Reversal: obj.Reversal}
	return nil
}

func (l LedgerOption) MarshalJSON() ([]byte, error) {
	if !l.On || l.Reversal == "" {
		return json.Marshal(l.On)
	}
	return json.Marshal(map[string]string{"reversal": l.Reversal})
}

// LifecycleDef is a type's process: the enum field that holds its state,
// the state a new record starts in (the field's first value when empty),
// and the named transitions that are the only way the state moves.
type LifecycleDef struct {
	Field       string          `json:"field"`
	Initial     string          `json:"initial,omitempty"`
	Transitions []TransitionDef `json:"transitions"`
}

// TransitionDef is one named move between states: from any of From ("*" is
// any state) to To, allowed when Guard holds, fired by the writer kinds in By
// (person and flow when empty), with Effects the engine carries out after the
// commit (a work item, a timer, a run), handed over through the outbox.
type TransitionDef struct {
	ID      string           `json:"id"`
	Label   string           `json:"label,omitempty"`
	From    []string         `json:"from,omitempty"`
	To      string           `json:"to"`
	Guard   string           `json:"guard,omitempty"`
	By      []string         `json:"by,omitempty"`
	Effects []map[string]any `json:"effects,omitempty"`
}

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
	Lifecycle *LifecycleDef       `json:"lifecycle,omitempty"`
	// Ledger makes records append-only: never changed or deleted, only
	// reversed by a new entry.
	Ledger LedgerOption `json:"ledger"`

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
	if err := def.readProcess(raw); err != nil {
		return nil, err
	}
	if err := def.check(); err != nil {
		return nil, err
	}
	return &def, nil
}

// readProcess reads the lifecycles an app declares under process, the
// schema's place for them, keyed by record type with transitions keyed by
// name, into the type's own lifecycle. A transition's actors are person,
// agent or flow; an agent writes as ai here. A type declares its lifecycle in
// one place or the other, not both.
func (d *AppDef) readProcess(raw []byte) error {
	var shape struct {
		Process struct {
			Lifecycles map[string]json.RawMessage `json:"lifecycles"`
		} `json:"process"`
	}
	if err := json.Unmarshal(raw, &shape); err != nil {
		return nil
	}
	for _, typ := range sortedKeys(shape.Process.Lifecycles) {
		var lc struct {
			Field       string          `json:"field"`
			Initial     string          `json:"initial"`
			States      []string        `json:"states"`
			Transitions json.RawMessage `json:"transitions"`
		}
		if err := json.Unmarshal(shape.Process.Lifecycles[typ], &lc); err != nil {
			return errorf(CodeInvalid, "process.lifecycles.%s could not be read: %v", typ, err)
		}
		t, ok := d.Records[typ]
		if !ok || t.Uses {
			return errorf(CodeInvalid, "process.lifecycles.%s names a record type this app does not declare", typ)
		}
		if t.Lifecycle != nil {
			return errorf(CodeInvalid, "%s declares its lifecycle twice, in records and in process", typ)
		}
		if f, ok := t.Fields[lc.Field]; ok {
			values := map[string]bool{}
			for _, v := range f.Values {
				values[v] = true
			}
			for _, st := range lc.States {
				if !values[st] {
					return errorf(CodeInvalid, "%s's lifecycle state %q is not a value of %s", typ, st, lc.Field)
				}
			}
		}
		var trs map[string]struct {
			Label   string           `json:"label"`
			From    []string         `json:"from"`
			To      string           `json:"to"`
			Guard   string           `json:"guard"`
			Actors  []string         `json:"actors"`
			Effects []map[string]any `json:"effects"`
		}
		if len(lc.Transitions) > 0 {
			if err := json.Unmarshal(lc.Transitions, &trs); err != nil {
				return errorf(CodeInvalid, "process.lifecycles.%s.transitions could not be read: %v", typ, err)
			}
		}
		out := &LifecycleDef{Field: lc.Field, Initial: lc.Initial}
		for _, id := range objectKeys(lc.Transitions) {
			tr := trs[id]
			def := TransitionDef{ID: id, Label: tr.Label, From: tr.From, To: tr.To, Guard: tr.Guard, Effects: tr.Effects}
			for _, actor := range tr.Actors {
				if actor == "agent" {
					actor = ByAI
				}
				if !slices.Contains(def.By, actor) {
					def.By = append(def.By, actor)
				}
			}
			out.Transitions = append(out.Transitions, def)
		}
		t.Lifecycle = out
		d.Records[typ] = t
	}
	return nil
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
		if lc := t.Lifecycle; lc != nil {
			if err := checkLifecycle(name, t, lc); err != nil {
				return err
			}
		}
		if field := t.Ledger.Reversal; field != "" {
			if f, ok := t.Fields[field]; !ok || f.Type != TypeRef || f.To != name {
				return errorf(CodeInvalid, "%s's ledger reversal %q needs to be a ref to %s that the type declares", name, field, name)
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
	if sq := f.Sequence; sq != nil {
		if f.Type != TypeText || f.Computed != "" || f.AI != "" || f.Default != nil {
			return errorf(CodeInvalid, "%s.%s numbers records: it is a plain text field with no default", typ, name)
		}
		if sq.Pad < 0 || sq.Padding < 0 || sq.digits() > 12 || len(sq.Prefix) > 20 {
			return errorf(CodeInvalid, "%s.%s: a sequence pads to at most 12 digits after a prefix of at most 20 characters", typ, name)
		}
	}
	return nil
}

// checkLifecycle checks a lifecycle against its type's own declaration: the
// state field is an enum the type declares, every state named is one of its
// values, and transition ids are names, each once.
func checkLifecycle(typ string, t TypeDef, lc *LifecycleDef) error {
	f, ok := t.Fields[lc.Field]
	if !ok || f.Type != TypeEnum {
		return errorf(CodeInvalid, "%s's lifecycle needs field %q to be an enum the type declares", typ, lc.Field)
	}
	if f.Computed != "" || f.AI != "" {
		return errorf(CodeInvalid, "%s's lifecycle field %s cannot be computed or filled by AI", typ, lc.Field)
	}
	states := map[string]bool{}
	for _, v := range f.Values {
		states[v] = true
	}
	if lc.Initial != "" && !states[lc.Initial] {
		return errorf(CodeInvalid, "%s's lifecycle starts in %q, which is not a value of %s", typ, lc.Initial, lc.Field)
	}
	seen := map[string]bool{}
	for _, tr := range lc.Transitions {
		if !identShape.MatchString(tr.ID) || seen[tr.ID] {
			return errorf(CodeInvalid, "%s transition %q needs a name of its own (lower-case letters, digits, _)", typ, tr.ID)
		}
		seen[tr.ID] = true
		if !states[tr.To] {
			return errorf(CodeInvalid, "%s transition %s goes to %q, which is not a state", typ, tr.ID, tr.To)
		}
		for _, from := range tr.From {
			if from != "*" && !states[from] {
				return errorf(CodeInvalid, "%s transition %s starts from %q, which is not a state", typ, tr.ID, from)
			}
		}
		for _, by := range tr.By {
			if by != ByPerson && by != ByAI && by != ByFlow {
				return errorf(CodeInvalid, "%s transition %s: by is person, ai or flow, not %q", typ, tr.ID, by)
			}
		}
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
