package data

import (
	"database/sql"

	"encoding/json"
	"fmt"
	appschema "github.com/Agent-Field/codeaf/internal/apps/schema"
	"sort"
	"strings"
	"time"
)

// Me is the person in v0: there is one, and a person field's "$me" is them.
const Me = "me"

// Field is one field of a merged type: the declaration that wins (the owner
// app's, else the first app's to declare it), every app that declares it, and
// whether it is hidden (no app declares it any more; its values are kept).
type Field struct {
	Name string
	FieldDef
	App    string
	Apps   []string
	Hidden bool

	computed *program
}

// Type is one record type as every registered app's declarations merge into
// it. Hidden is a type no app declares any more; its records are kept and
// the Data place still shows them.
type Type struct {
	Name      string
	Owner     string
	Title     string
	Label     string
	CreatedAt string
	Hidden    bool
	UsedBy    []string
	Fields    map[string]*Field
	// Order is every field, visible and hidden, in the order the apps
	// declared them: the owner's first.
	Order     []string
	Constants map[string]any
	Freeze    []*freezeRule

	// computedOrder is the computed fields in declaration order; clocked
	// says one of them reads the clock and goes stale with time alone.
	computedOrder []string
	clocked       bool
}

// freezeRule is a compiled [FreezeRule].
type freezeRule struct {
	FreezeRule
	when   *program
	except map[string]bool
	after  time.Duration
}

// schema is the merged types and every app's record capabilities, read from
// the database. It is immutable once built: a registration builds a new one
// and the writer swaps it in after the commit, so a reader always sees one
// whole schema.
type schema struct {
	types map[string]*Type
	// caps is app → type → "r" or "rw", for every registered app.
	caps map[string]map[string]string
}

// field answers a visible field of t, or nil.
func (t *Type) field(name string) *Field {
	f := t.Fields[name]
	if f == nil || f.Hidden {
		return nil
	}
	return f
}

// titleField is the field that names a record of t: the declared title, else
// the first visible text field, else none.
func (t *Type) titleField() string {
	if f := t.field(t.Title); f != nil {
		return f.Name
	}
	for _, name := range t.Order {
		if f := t.field(name); f != nil && f.Type == TypeText {
			return name
		}
	}
	return ""
}

// Scope is who is acting. App is the app a call is made for; empty is the
// person in the Data place, who may do everything. Agent says the app's own
// agent is acting: it never sees a masked value and cannot unfreeze. AI says
// the write fills ai fields and must leave a person's pinned value alone.
type Scope struct {
	App   string
	Actor string
	Agent bool
	AI    bool
}

func (sc Scope) actor() string {
	if sc.Actor != "" {
		return sc.Actor
	}
	if sc.Agent && sc.App != "" {
		return sc.App + " agent"
	}
	return Me
}

// allow answers the type a scope may read (or, with write, change), or the
// refusal. An app's capability is checked before the type's existence, so an
// app learns nothing about types it was not given.
func (s *schema) allow(sc Scope, typ string, write bool) (*Type, error) {
	if sc.App != "" {
		caps, ok := s.caps[sc.App]
		access := caps[typ]
		if !ok || access == "" || write && access != "rw" {
			return nil, notAllowed(sc.App, typ, write)
		}
	}
	t := s.types[typ]
	if t == nil {
		return nil, errorf(CodeNotFound, "there is no record type named %q", typ)
	}
	return t, nil
}

// loadSchema reads the whole schema from q and compiles it. A field whose
// expression no longer compiles (which registration prevents) is kept with no
// program, so a broken expression empties one field instead of the store.
func loadSchema(q querier, now func() time.Time) (*schema, map[string][]error, error) {
	s := &schema{types: map[string]*Type{}, caps: map[string]map[string]string{}}
	rows, err := q.Query(`SELECT app, def_json FROM apps`)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var app, raw string
		if err := rows.Scan(&app, &raw); err != nil {
			rows.Close()
			return nil, nil, err
		}
		var caps map[string]string
		_ = json.Unmarshal([]byte(raw), &caps)
		if caps == nil {
			caps = map[string]string{}
		}
		s.caps[app] = caps
	}
	rows.Close()

	declared := map[string][]string{}
	uses := map[string][]string{}
	rows, err = q.Query(`SELECT app, type, uses FROM app_types ORDER BY app`)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var app, typ string
		var used bool
		if err := rows.Scan(&app, &typ, &used); err != nil {
			rows.Close()
			return nil, nil, err
		}
		declared[typ] = append(declared[typ], app)
		if used {
			uses[typ] = append(uses[typ], app)
		}
	}
	rows.Close()

	rows, err = q.Query(`SELECT name, owner_app, def_json, created_at FROM types`)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var name, owner, raw, created string
		if err := rows.Scan(&name, &owner, &raw, &created); err != nil {
			rows.Close()
			return nil, nil, err
		}
		var def TypeDef
		_ = json.Unmarshal([]byte(raw), &def)
		t := &Type{
			Name: name, Owner: owner, Title: def.Title, Label: def.Label, CreatedAt: created,
			Hidden: len(declared[name]) == 0, UsedBy: uses[name], Fields: map[string]*Field{},
			Constants: def.Constants,
		}
		for _, rule := range def.Freeze {
			t.Freeze = append(t.Freeze, &freezeRule{FreezeRule: rule})
		}
		s.types[name] = t
	}
	rows.Close()

	// Fields come ordered: the owner's declarations first, each app's in its
	// own order, so the winning declaration of a field is the first row.
	rows, err = q.Query(`
		SELECT f.type, f.field, f.app, f.def_json, f.hidden
		FROM type_fields f JOIN types t ON t.name = f.type
		ORDER BY f.type, (f.app != t.owner_app), f.app, f.pos`)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var typ, name, app, raw string
		var hidden bool
		if err := rows.Scan(&typ, &name, &app, &raw, &hidden); err != nil {
			rows.Close()
			return nil, nil, err
		}
		t := s.types[typ]
		if t == nil {
			continue
		}
		f := t.Fields[name]
		if f == nil {
			var def FieldDef
			_ = json.Unmarshal([]byte(raw), &def)
			f = &Field{Name: name, FieldDef: def, App: app, Hidden: true}
			t.Fields[name] = f
			t.Order = append(t.Order, name)
		}
		f.Apps = append(f.Apps, app)
		// A field is hidden only when every app that declared it has
		// dropped it.
		if !hidden {
			f.Hidden = false
		}
	}
	rows.Close()

	problems := map[string][]error{}
	for _, name := range sortedKeys(s.types) {
		if p := s.types[name].compile(now); len(p) > 0 {
			problems[name] = p
		}
	}
	return s, problems, nil
}

// compile builds t's expression environment and compiles its computed fields
// and freeze rules, answering what did not compile.
func (t *Type) compile(now func() time.Time) []error {
	var problems []error
	env, err := exprEnv(t, now)
	if err != nil {
		return []error{fmt.Errorf("%s: %v", t.Name, err)}
	}
	t.computedOrder, t.clocked = nil, false
	for _, name := range t.Order {
		f := t.Fields[name]
		if f.Computed == "" || f.Hidden {
			continue
		}
		p, err := compile(env, f.Computed)
		if err != nil {
			problems = append(problems, fieldErr(name, "%s.%s: %v", t.Name, name, err))
			continue
		}
		// A COMPUTED FIELD THAT READS A MASKED ONE IS MASKED TOO, or it is
		// the masked value with the mask taken off: "computed": "ssn" would
		// show, sort and search what the mask hides.
		leaks := false
		if !f.Masked {
			for ref := range identifiers(f.Computed) {
				if src := t.Fields[ref]; src != nil && src.Masked {
					problems = append(problems, fieldErr(name, "%s.%s reads %s, which is masked; mark %s masked too", t.Name, name, ref, name))
					leaks = true
				}
			}
		}
		if leaks {
			// Left empty rather than computed: a stored definition that
			// leaks (a field masked after the copy was declared) never runs.
			continue
		}
		f.computed = p
		t.computedOrder = append(t.computedOrder, name)
		t.clocked = t.clocked || p.readsAt
	}
	for i, rule := range t.Freeze {
		rule.except = map[string]bool{}
		for _, name := range rule.Except {
			rule.except[name] = true
		}
		if rule.When != "" {
			p, err := compile(env, rule.When)
			if err != nil {
				problems = append(problems, fmt.Errorf("%s freeze rule %d: %v", t.Name, i+1, err))
				continue
			}
			rule.when = p
		}
		if rule.Period != "" {
			if f := t.field(rule.Field); f == nil || f.Type != TypeDate && f.Type != TypeDatetime {
				problems = append(problems, fmt.Errorf("%s freeze rule %d: %q is not a date field", t.Name, i+1, rule.Field))
			}
			rule.after, _ = parseSpan(rule.After)
		}
	}
	return problems
}

// registerApp writes one app's declarations into the tables, inside the
// writer's transaction, and answers the schema they make. Nothing is ever
// deleted: a field the app stopped declaring is hidden, a type it stopped
// declaring loses it as a declarer, and every value stays where it was.
func registerApp(tx *sql.Tx, current *schema, def *AppDef, capsJSON string, now func() time.Time) (*schema, error) {
	at := now().UTC().Format(stamp)
	app := def.ID
	for _, name := range def.typeOrder {
		t := def.Records[name]
		existing := current.types[name]
		if t.Uses {
			if existing == nil {
				return nil, errorf(CodeInvalid, "%s uses %s records, and there is no %s type yet", app, name, name)
			}
		} else if existing != nil && existing.Owner != app {
			var owners int
			if err := tx.QueryRow(`SELECT count(*) FROM app_types WHERE type = ? AND uses = 0 AND app != ?`, name, app).Scan(&owners); err != nil {
				return nil, err
			}
			if owners > 0 {
				return nil, errorf(CodeInvalid, "%s already owns %s records; %s can use them with \"uses\": true", existing.Owner, name, app)
			}
		}
		for _, fname := range t.order {
			f := t.Fields[fname]
			if existing == nil {
				continue
			}
			if have := existing.Fields[fname]; have != nil && have.Type != f.Type && !(len(have.Apps) == 1 && have.Apps[0] == app) {
				return nil, errorf(CodeInvalid, "%s.%s is already a %s field (%s); %s declares it as %s", name, fname, have.Type, have.App, app, f.Type)
			}
		}
	}

	if _, err := tx.Exec(`DELETE FROM app_types WHERE app = ?`, app); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE type_fields SET hidden = 1 WHERE app = ?`, app); err != nil {
		return nil, err
	}
	for _, name := range def.typeOrder {
		t := def.Records[name]
		if _, err := tx.Exec(`INSERT INTO app_types(app, type, uses) VALUES(?, ?, ?)`, app, name, t.Uses); err != nil {
			return nil, err
		}
		if !t.Uses {
			typeDef := canonicalJSON(TypeDef{Title: t.Title, Label: t.Label, Constants: t.Constants, Freeze: t.Freeze})
			if _, err := tx.Exec(`
				INSERT INTO types(name, owner_app, def_json, created_at) VALUES(?, ?, ?, ?)
				ON CONFLICT(name) DO UPDATE SET owner_app = excluded.owner_app, def_json = excluded.def_json`,
				name, app, typeDef, at); err != nil {
				return nil, err
			}
		}
		for pos, fname := range t.order {
			if _, err := tx.Exec(`
				INSERT INTO type_fields(type, field, app, def_json, pos, hidden) VALUES(?, ?, ?, ?, ?, 0)
				ON CONFLICT(type, field, app) DO UPDATE SET def_json = excluded.def_json, pos = excluded.pos, hidden = 0`,
				name, fname, app, canonicalJSON(t.Fields[fname]), pos); err != nil {
				return nil, err
			}
		}
	}
	if _, err := tx.Exec(`
		INSERT INTO apps(app, def_json, registered_at) VALUES(?, ?, ?)
		ON CONFLICT(app) DO UPDATE SET def_json = excluded.def_json, registered_at = excluded.registered_at`,
		app, capsJSON, at); err != nil {
		return nil, err
	}

	next, problems, err := loadSchema(tx, now)
	if err != nil {
		return nil, err
	}
	// Only the types this app declares can refuse it: a problem another
	// app's fields have (a copy of a field since masked) is that app's to
	// fix, and must not block every other app's registration.
	for _, name := range def.typeOrder {
		if p := problems[name]; len(p) > 0 {
			if e, ok := AsError(p[0]); ok {
				return nil, e
			}
			return nil, errorf(CodeInvalid, "%v", p[0])
		}
	}
	return next, checkRegistered(tx, next, def)
}

// checkRegistered checks what can only be checked once every app's
// declarations have merged: refs point at types that exist, defaults are
// values of their fields, capabilities name types that exist, and the indexes
// the fields ask for exist.
func checkRegistered(tx *sql.Tx, s *schema, def *AppDef) error {
	for _, name := range def.typeOrder {
		t := s.types[name]
		for _, fname := range def.Records[name].order {
			f := t.Fields[fname]
			if f == nil || f.Hidden {
				continue
			}
			if (f.Type == TypeRef || f.Type == TypeRefs) && s.types[f.To] == nil {
				return errorf(CodeInvalid, "%s.%s points to %s records, and there is no %s type", name, fname, f.To, f.To)
			}
			if f.Default != nil {
				if _, err := coerceDefault(f); err != nil {
					return errorf(CodeInvalid, "%s.%s: the default %v is not a value of the field: %v", name, fname, f.Default, err)
				}
			}
		}
		for _, rule := range t.Freeze {
			for _, ex := range rule.Except {
				if t.field(ex) == nil {
					return errorf(CodeInvalid, "%s freeze rule names %q in except, which is not a field", name, ex)
				}
			}
		}
	}
	for typ := range def.Capabilities.Records {
		if s.types[typ] == nil {
			return errorf(CodeInvalid, "%s's capabilities name %s records, and there is no %s type", def.ID, typ, typ)
		}
	}
	return ensureIndexes(tx, s)
}

// ensureIndexes makes the partial expression index each indexed field needs.
//
// AN INDEX IS AN EXPRESSION INDEX OVER THE JSON COLUMN, PARTIAL TO ITS TYPE,
// rather than a generated column: every type shares one records table, and a
// generated column per type and field would widen every row of every type.
// The query compiler spells the same expression and the same literal type
// test, which is what lets SQLite's planner use the index.
func ensureIndexes(tx *sql.Tx, s *schema) error {
	for _, tname := range sortedKeys(s.types) {
		t := s.types[tname]
		for _, fname := range t.Order {
			f := t.Fields[fname]
			if f.Hidden || !(f.Index || f.Unique || f.Type == TypeEnum || f.Type == TypeRef || f.Type == TypePerson) {
				continue
			}
			if !identShape.MatchString(tname) || !identShape.MatchString(fname) {
				return fmt.Errorf("refusing to index %q.%q", tname, fname)
			}
			stmt := fmt.Sprintf(`CREATE INDEX IF NOT EXISTS "rx_%s__%s" ON records(%s) WHERE type = '%s' AND deleted_at IS NULL`,
				tname, fname, jsonPath("data_json", fname), tname)
			if _, err := tx.Exec(stmt); err != nil {
				return err
			}
		}
	}
	return nil
}

// forgetApp removes an app's capabilities and declarations. Its types and
// fields stay, hidden when no other app declares them, and so do their
// values: deleting an app keeps its data.
func forgetApp(tx *sql.Tx, app string, now func() time.Time) (*schema, error) {
	for _, stmt := range []string{
		`DELETE FROM app_types WHERE app = ?`,
		`UPDATE type_fields SET hidden = 1 WHERE app = ?`,
		`DELETE FROM apps WHERE app = ?`,
	} {
		if _, err := tx.Exec(stmt, app); err != nil {
			return nil, err
		}
	}
	next, _, err := loadSchema(tx, now)
	return next, err
}

// jsonPath is the SQL expression that reads one field of a row's JSON. The
// field is an identifier the schema checked; this spelling is the one the
// indexes are made with, so it must not change without remaking them.
func jsonPath(column, field string) string {
	return fmt.Sprintf("json_extract(%s, '$.%s')", column, field)
}

// TypeInfo is one type as the Data place lists it.
type TypeInfo struct {
	Name     string
	Title    string
	Label    string
	OwnerApp string
	UsedBy   []string
	Hidden   bool
	Count    int
	Fields   []FieldInfo
}

// FieldInfo is one field as the Data place lists it.
type FieldInfo struct {
	Name string
	FieldDef
	App    string
	Hidden bool
}

// sortedTypes is s's types, by name.
func (s *schema) sortedTypes() []*Type {
	out := make([]*Type, 0, len(s.types))
	for _, t := range s.types {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// periods are the periods a freeze rule may lock by.
var periods = map[string]bool{"week": true, "month": true, "quarter": true, "year": true}

// parseSpan reads a span like 5d, 2w, 12h, 30m (minutes) or 1y.
func parseSpan(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return 0, fmt.Errorf("%q is not a span", s)
	}
	var n int
	if _, err := fmt.Sscanf(s[:len(s)-1], "%d", &n); err != nil || n < 0 || fmt.Sprint(n) != s[:len(s)-1] {
		return 0, fmt.Errorf("%q is not a span", s)
	}
	unit := map[byte]time.Duration{'m': time.Minute, 'h': time.Hour, 'd': 24 * time.Hour, 'w': 7 * 24 * time.Hour, 'y': 365 * 24 * time.Hour}[s[len(s)-1]]
	if unit == 0 {
		return 0, fmt.Errorf("%q is not a span", s)
	}
	return time.Duration(n) * unit, nil
}

// TypeSet is every visible type the store holds, with its owner and its
// visible fields: what internal/appschema's validator checks a new or changed app
// against (its `uses` types, its refs into the person's data).
func (s *Store) TypeSet() appschema.TypeSet {
	out := appschema.TypeSet{}
	for name, t := range s.schema.Load().types {
		if t.Hidden {
			continue
		}
		fields := map[string]appschema.Field{}
		for fname, f := range t.Fields {
			if !f.Hidden {
				fields[fname] = f.FieldDef
			}
		}
		out[name] = appschema.ExistingType{Owner: t.Owner, Fields: fields}
	}
	return out
}
