package data

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"

	appschema "github.com/Agent-Field/codeaf/internal/apps/schema"
)

// PersonPrincipal is supplied by the authenticated engine door. The local
// profile defaults to the data store's person. An agent acts for this person.
type PersonPrincipal struct {
	ID         string
	Attributes map[string]string
}

func (sc Scope) attribute(person, name string) string {
	if name == "id" {
		if sc.Person.ID != "" {
			return sc.Person.ID
		}
		return person
	}
	return sc.Person.Attributes[name]
}

// Record checks compile against stable field IDs and deny visibility when the
// declared attribute or the field is missing.
type rowPolicy struct {
	field     *Field
	attribute string
}

func (t *Type) rowAllowed(sc Scope, person string, values map[string]any) bool {
	if t.rowRule == nil {
		return true
	}
	if t.rowRule.field == nil {
		return false
	}
	subject := sc.attribute(person, t.rowRule.attribute)
	value, ok := values[t.rowRule.field.Name].(string)
	return subject != "" && ok && value == subject
}

func (c *compiled) applyPolicy(sc Scope) {
	rule := c.t.rowRule
	if rule == nil {
		return
	}
	c.sch.notePolicy(sc, c.t, "rows_filtered")
	value := sc.attribute(c.sch.person, rule.attribute)
	if value == "" || rule.field == nil {
		c.add("0", false)
		return
	}
	c.add(c.col(rule.field)+" COLLATE BINARY = ?", false, value)
}

// FieldPolicy is also used by live mappings so they cannot expose a value
// the native projection would hide.
func (t *Type) FieldPolicy(f *Field) string {
	if t.fieldRules[f.Name] == "hide" {
		return "hide"
	}
	if f.Class.Rank() >= appschema.ClassConfidential.Rank() || t.fieldRules[f.Name] == "mask" {
		return "mask"
	}
	return ""
}

// A ref to a row-protected type is withheld until the reader enters that
// type through its own authorized view. Raw IDs would disclose membership.
func (sch *schema) withheldRef(f *Field) bool {
	if f.Type != TypeRef && f.Type != TypeRefs {
		return false
	}
	t := sch.typeNamed(f.To)
	return t != nil && t.rowRule != nil
}

func (t *Type) declarePolicy(p *appschema.RecordPolicy) []error {
	if p == nil {
		return nil
	}
	var problems []error
	t.fieldRules = map[string]string{}
	for name, mode := range p.Fields {
		f := t.field(name)
		if f == nil || mode != "hide" && mode != "mask" {
			problems = append(problems, fieldErr(name, "a field policy needs a declared field and hide or mask"))
			continue
		}
		t.fieldRules[f.Name] = mode
	}
	if p.Row != nil {
		f := t.field(p.Row.Field)
		t.rowRule = &rowPolicy{attribute: p.Row.Attribute}
		if f == nil || f.Type != TypeText && f.Type != TypePerson && f.Type != TypeEmail && f.Type != TypeEnum ||
			f.Computed != "" || !identShape.MatchString(p.Row.Attribute) {
			problems = append(problems, fieldErr(p.Row.Field, "a row policy needs a stored text, person, email or enum field and a principal attribute"))
		} else {
			t.rowRule.field = f
		}
	}
	return problems
}

// AuditPolicy appends only identifiers and the decision, never row values,
// principal attributes, model prompts or source messages.
func (s *Store) AuditPolicy(app, typeID, decision string) (err error) {
	defer func() {
		if err != nil {
			log.Printf("app policy app=%q type=%q decision=%q", app, typeID, decision)
		}
	}()
	out := struct {
		At       string `json:"at"`
		App      string `json:"app,omitempty"`
		Type     string `json:"type_id,omitempty"`
		Decision string `json:"decision"`
	}{s.now().UTC().Format(stamp), app, typeID, decision}
	f, err := os.OpenFile(filepath.Join(filepath.Dir(s.path), "policy.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(out)
}

func (sch *schema) notePolicy(sc Scope, t *Type, decision string) {
	if sch.policyAudit != nil {
		sch.policyAudit(sc, t, decision)
	}
}

func (c *compiled) protectedField(sc Scope, f *Field) bool {
	if c.t.FieldPolicy(f) == "" && !c.sch.withheldRef(f) {
		return false
	}
	c.sch.notePolicy(sc, c.t, "query_field_refused")
	return true
}

func (s *Store) auditPolicy(sc Scope, t *Type, decision string) {
	_ = s.AuditPolicy(sc.App, t.ID, decision)
}

// LiveRowAllowed applies the type's inherited row rule to mapped source data
// before a connector reserves an identity or returns a row.
func (s *Store) LiveRowAllowed(sc Scope, t *Type, values map[string]any) bool {
	if t.rowAllowed(sc, s.Person(), values) {
		return true
	}
	s.auditPolicy(sc, t, "row_refused")
	return false
}

// HasRowPolicy allows live sources to budget before they fetch their rows.
func (t *Type) HasRowPolicy() bool { return t.rowRule != nil }

// MaskValue is the same opaque placeholder in native and connected reads.
const MaskValue = masked

// LiveRowPredicate supplies the local policy field and equality value for SQL
// pushdown. The engine also checks the mapped result before returning it.
func (t *Type) LiveRowPredicate(sc Scope, person string) (field, value string, required bool) {
	if t.rowRule == nil {
		return "", "", false
	}
	if t.rowRule.field == nil {
		return "", "", true
	}
	return t.rowRule.field.Name, sc.attribute(person, t.rowRule.attribute), true
}

// ModelDataClasses includes the inherited declarations and all types the app
// may read. A run can carry derived values from any of these types.
func (s *Store) ModelDataClasses(app string) []appschema.DataClass {
	sch := s.schema.Load()
	var classes []appschema.DataClass
	for _, t := range sch.types {
		if !slices.Contains(t.declaredBy, app) && sch.caps[app][t.Name] == "" {
			continue
		}
		for _, field := range t.Fields {
			if field.Class.Restricted() && !slices.Contains(classes, field.Class) {
				classes = append(classes, field.Class)
			}
		}
	}
	return classes
}

func (w *writeTx) liveFor(sc Scope, t *Type, id string) (*record, error) {
	r, err := w.live(t, id)
	if err == nil && !w.s.LiveRowAllowed(sc, t, r.Data) {
		return nil, errorf(CodeNotFound, "there is no %s record %s", t.Name, id)
	}
	return r, err
}

// tablePolicy filters work table rows in SQLite before the visible order or
// limit. The outer tables all carry a type_id and a record column.
func (sch *schema) tablePolicy(sc Scope, table string) (string, []any) {
	if !sch.hasRowPolicies {
		return "1", nil
	}
	var clauses []string
	var args []any
	for _, t := range sch.byID {
		if !t.HasRowPolicy() {
			continue
		}
		policy := compiled{t: t, sch: sch, alias: "r"}
		policy.applyPolicy(sc)
		where, parameters := policy.filter()
		clauses = append(clauses, "("+table+".type_id != ? OR EXISTS (SELECT 1 FROM records r WHERE r.id = "+table+".record AND "+where+"))")
		args = append(args, t.ID)
		args = append(args, parameters...)
	}
	return strings.Join(clauses, " AND "), args
}
