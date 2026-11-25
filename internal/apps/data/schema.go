package data

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	appschema "github.com/Agent-Field/codeaf/internal/apps/schema"
)

// Me is how a person value, a default or an expression names the person;
// it is stored as the person's id ([Store.Person]).
const Me = "me"

// The kinds of writer every batch and history row names. Person wins by
// provenance: a value a person set (or cleared) holds against a later AI
// write until the person resets it, and a flow that forwards a model's output
// writes as AI.
const (
	ByPerson = "person"
	ByAI     = "ai"
	ByFlow   = "flow"
	ByImport = "import"
	BySync   = "sync"
)

// Field is one field of a merged type: its id, the declaration that wins
// (the defining app's, else the first app's to declare it), every app that
// declares it, and whether it is hidden (no app declares it any more; its
// values are kept).
type Field struct {
	ID   string
	Name string
	FieldDef
	App    string
	Apps   []string
	Hidden bool

	computed *program
}

// Type is one record type as every registered app's declarations merge into
// it. A type belongs to the person's space, not to an app: CreatedByApp is
// only where it came from, and DefinedBy is the app whose declaration (title,
// rules, lifecycle) it has now. Hidden is a type no app declares any more;
// its records are kept and the Data place still shows them.
type Type struct {
	rules        writeRules
	ID           string
	Name         string
	CreatedByApp string
	DefinedBy    string
	Title        string
	Label        string
	CreatedAt    string
	Hidden       bool
	UsedBy       []string
	Fields       map[string]*Field
	// Order is every field, visible and hidden, in the order the apps
	// declared them: the defining app's first.
	Order     []string
	Constants map[string]any
	Freeze    []*freezeRule
	// Lifecycle is the type's states and transitions, when it has them.
	Lifecycle  *lifecycle
	checklists []checklistDef
	// Ledger says records of the type are never changed or deleted, only
	// reversed by a new entry; LedgerReversal is the ref field a reversing
	// entry fills with the entry it reverses, when the type names one.
	Ledger         bool
	LedgerReversal string

	// declaredBy is every app that declares the type, using it or not.
	declaredBy []string
	// byID is the fields by id; aliases is every name a field was once known
	// by, to the name it has now.
	byID    map[string]*Field
	aliases map[string]string

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

// schema is the merged types, every app's record capabilities and the
// person, read from the database. It is immutable once built: a registration
// builds a new one and the writer swaps it in after the commit, so a reader
// always sees one whole schema.
type schema struct {
	bindings map[string]map[string]TypeBinding
	types    map[string]*Type
	byID     map[string]*Type
	// typeAliases is every name a type was once known by, to its name now.
	typeAliases map[string]string
	// caps is app → type → "r" or "rw", for every registered app.
	caps   map[string]map[string]string
	person string
}

// field answers a visible field of t by its name or a name it once had, or
// nil.
func (t *Type) field(name string) *Field {
	f := t.Fields[name]
	if f == nil {
		if now, ok := t.aliases[name]; ok {
			f = t.Fields[now]
		}
	}
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

// typeNamed answers a type by its name or a name it once had.
func (s *schema) typeNamed(name string) *Type {
	if t := s.byID[name]; t != nil {
		return t
	}
	if t := s.types[name]; t != nil {
		return t
	}
	if now, ok := s.typeAliases[name]; ok {
		return s.types[now]
	}
	return nil
}

// Scope is who is acting. App is the app a call is made for; empty is the
// person in the Data place, who may do everything. Agent says the app's own
// agent is acting: it never sees a masked value, cannot unfreeze or sign,
// and writes as AI. AI says the write is a model's output (an ai field
// filled, a flow forwarding what a model said); Flow says a run or rule is
// writing on its own grant. Cause names what led to the write (a run step,
// a transition, a timer), kept on every history row.
type Scope struct {
	App        string
	Actor      string
	Agent      bool
	AI         bool
	Flow       bool
	Cause      string
	CauseChain []string

	sync        bool
	sourceEvent string
}

// kind is the writer kind a write of op by sc is recorded as.
func (sc Scope) kind(op string) string {
	switch {
	case sc.sync:
		return BySync
	case sc.AI || sc.Agent:
		return ByAI
	case sc.Flow:
		return ByFlow
	case op == OpImport:
		return ByImport
	}
	return ByPerson
}

// below says sc's writes sit below a person's: a value a person set is
// left as it is.
func (sc Scope) below() bool { return sc.AI || sc.Agent }

// actor is who a history row names: the caller's own name for itself, an
// app's agent, or the person's id.
func (s *schema) actor(sc Scope) string {
	if sc.Actor != "" {
		return sc.Actor
	}
	if sc.Agent && sc.App != "" {
		return sc.App + " agent"
	}
	return s.person
}

// allow answers the type a scope may read (or, with write, change), or the
// refusal. An app's capability is checked before the type's existence, so an
// app learns nothing about types it was not given.
func (s *schema) allow(sc Scope, typ string, write bool) (*Type, error) {
	t := s.appType(sc.App, typ)
	if sc.App != "" {
		access := ""
		if t != nil {
			access = s.caps[sc.App][t.Name]
		}
		if access == "" || write && access != "rw" {
			return nil, notAllowed(sc.App, typ, write)
		}
	}
	if t == nil {
		return nil, errorf(CodeNotFound, "there is no record type named %q", typ)
	}
	return t, nil
}

// appType resolves the app alias only within the catalog that owns the grant.
func (s *schema) appType(app, alias string) *Type {
	if binding, ok := s.bindings[app][alias]; ok {
		return s.byID[binding.TypeID]
	}
	return s.typeNamed(alias)
}

// loadSchema reads the whole schema from q and compiles it. A field whose
// expression no longer compiles (which registration prevents) is kept with no
// program, so a broken expression empties one field instead of the store.
func loadSchema(q querier, now func() time.Time) (*schema, map[string][]error, error) {
	s := &schema{types: map[string]*Type{}, byID: map[string]*Type{}, typeAliases: map[string]string{}, caps: map[string]map[string]string{}}
	if err := q.QueryRow(`SELECT value FROM space WHERE key = 'person'`).Scan(&s.person); err != nil {
		return nil, nil, fmt.Errorf("the data file has no person: %w", err)
	}
	declared := map[string][]string{}
	uses := map[string][]string{}
	err := eachRow(q, `SELECT app, type_id, uses FROM app_types ORDER BY app`, nil, func(scan func(...any) error) error {
		var app, typ string
		var used bool
		if err := scan(&app, &typ, &used); err != nil {
			return err
		}
		declared[typ] = append(declared[typ], app)
		if used {
			uses[typ] = append(uses[typ], app)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	err = eachRow(q, `SELECT id, name, created_by_app, def_app, def_json, created_at FROM types`, nil, func(scan func(...any) error) error {
		var id, name, created, defApp, raw, at string
		if err := scan(&id, &name, &created, &defApp, &raw, &at); err != nil {
			return err
		}
		var def TypeDef
		_ = json.Unmarshal([]byte(raw), &def)
		t := &Type{
			ID: id, Name: name, CreatedByApp: created, DefinedBy: defApp, Title: def.Title, Label: def.Label, CreatedAt: at,
			Hidden: len(declared[id]) == 0, UsedBy: uses[id], declaredBy: declared[id], Fields: map[string]*Field{},
			rules: writeRules{declarations: def.Rules}, Constants: def.Constants, checklists: def.Checklists, Ledger: def.Ledger.On, LedgerReversal: def.Ledger.Reversal, byID: map[string]*Field{}, aliases: map[string]string{},
		}
		for _, rule := range def.Freeze {
			t.Freeze = append(t.Freeze, &freezeRule{FreezeRule: rule})
		}
		if def.Lifecycle != nil {
			t.Lifecycle = &lifecycle{LifecycleDef: *def.Lifecycle}
		}
		s.types[name] = t
		s.byID[id] = t
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	err = eachRow(q, `SELECT a.alias, t.name FROM type_aliases a JOIN types t ON t.id = a.type_id`, nil, func(scan func(...any) error) error {
		var alias, name string
		if err := scan(&alias, &name); err != nil {
			return err
		}
		if s.types[alias] == nil {
			s.typeAliases[alias] = name
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	s.bindings = map[string]map[string]TypeBinding{}
	err = eachRow(q, `SELECT b.app, b.alias, b.type_id, COALESCE(c.key, b.common_key)
		FROM app_type_bindings b LEFT JOIN common_types c ON c.type_id = b.type_id`, nil, func(scan func(...any) error) error {
		var app string
		var b TypeBinding
		if err := scan(&app, &b.Alias, &b.TypeID, &b.Common); err != nil {
			return err
		}
		if s.bindings[app] == nil {
			s.bindings[app] = map[string]TypeBinding{}
		}
		s.bindings[app][b.Alias] = b
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	err = eachRow(q, `SELECT app, def_json FROM apps`, nil, func(scan func(...any) error) error {
		var app, raw string
		if err := scan(&app, &raw); err != nil {
			return err
		}
		var declaredCaps map[string]string
		_ = json.Unmarshal([]byte(raw), &declaredCaps)
		// An app's capabilities name types as its file does; a type renamed
		// since is found by the name the app knew.
		caps := map[string]string{}
		for typ, access := range declaredCaps {
			if t := s.appType(app, typ); t != nil {
				typ = t.Name
			}
			if caps[typ] != "rw" {
				caps[typ] = access
			}
		}
		s.caps[app] = caps
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	// Fields come ordered: the defining app's declarations first, each app's
	// in its own order, so the winning declaration of a field is the first
	// row.
	err = eachRow(q, `
		SELECT f.id, f.type_id, f.name, d.app, d.def_json, d.hidden
		FROM fields f JOIN type_fields d ON d.field_id = f.id JOIN types t ON t.id = f.type_id
		ORDER BY f.type_id, (d.app != t.def_app), d.app, d.pos`, nil, func(scan func(...any) error) error {
		var id, typeID, name, app, raw string
		var hidden bool
		if err := scan(&id, &typeID, &name, &app, &raw, &hidden); err != nil {
			return err
		}
		t := s.byID[typeID]
		if t == nil {
			return nil
		}
		f := t.Fields[name]
		if f == nil {
			var def FieldDef
			_ = json.Unmarshal([]byte(raw), &def)
			f = &Field{ID: id, Name: name, FieldDef: def, App: app, Hidden: true}
			t.Fields[name] = f
			t.byID[id] = f
			t.Order = append(t.Order, name)
		}
		f.Apps = append(f.Apps, app)
		// A field is hidden only when every app that declared it has
		// dropped it.
		if !hidden {
			f.Hidden = false
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	err = eachRow(q, `SELECT a.type_id, a.alias, f.name FROM field_aliases a JOIN fields f ON f.id = a.field_id`, nil, func(scan func(...any) error) error {
		var typeID, alias, name string
		if err := scan(&typeID, &alias, &name); err != nil {
			return err
		}
		if t := s.byID[typeID]; t != nil && t.Fields[alias] == nil {
			t.aliases[alias] = name
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	problems := map[string][]error{}
	for _, name := range sortedKeys(s.types) {
		p := s.types[name].compile(now, s.person)
		p = append(p, s.compileRules(s.types[name], now)...)
		if len(p) > 0 {
			problems[name] = p
		}
	}
	return s, problems, nil
}

// eachRow runs a query and hands each row's scan to fn.
func eachRow(q querier, stmt string, args []any, fn func(scan func(...any) error) error) error {
	rows, err := q.Query(stmt, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := fn(rows.Scan); err != nil {
			return err
		}
	}
	return rows.Err()
}

// compile builds t's expression environment and compiles its computed fields
// and freeze rules, answering what did not compile.
func (t *Type) compile(now func() time.Time, person string) []error {
	var problems []error
	env, err := exprEnv(t, now, person)
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
	if t.Lifecycle != nil {
		problems = append(problems, t.Lifecycle.compile(t, env)...)
	}
	return problems
}

// registerApp writes one app's declarations into the tables, inside the
// writer's transaction, and answers the schema they make. Nothing is ever
// deleted: a field the app stopped declaring is hidden, a type it stopped
// declaring loses it as a declarer, and every value stays where it was. A
// type or field met for the first time is given its id here, once.
func registerApp(w *writeTx, def *AppDef, bindings map[string]CommonTypeKey) (*schema, error) {
	tx, current := w.tx, w.schema
	at := w.now.UTC().Format(stamp)
	app := def.ID
	resolved, err := w.resolveBindings(def, bindings)
	if err != nil {
		return nil, err
	}
	typeIDs := map[string]string{}
	for _, name := range def.typeOrder {
		t := def.Records[name]
		existing := current.byID[resolved[name].TypeID]
		if t.Uses {
			if existing == nil {
				return nil, errorf(CodeInvalid, "%s uses %s records, and there is no %s type yet", app, name, name)
			}
		} else if existing != nil {
			// A type is the space's; another app may add fields to it with
			// "uses", but only one app at a time declares its rules.
			var other string
			err := tx.QueryRow(`SELECT app FROM app_types WHERE type_id = ? AND uses = 0 AND app != ? LIMIT 1`, existing.ID, app).Scan(&other)
			if err == nil {
				return nil, errorf(CodeInvalid, "%s already defines %s records; %s can use them with \"uses\": true", other, name, app)
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
		}
		if existing != nil {
			typeIDs[name] = existing.ID
			for _, fname := range t.order {
				f := t.Fields[fname]
				if have := existing.field(fname); have != nil && have.Type != f.Type && !(len(have.Apps) == 1 && have.Apps[0] == app) {
					return nil, errorf(CodeInvalid, "%s.%s is already a %s field (%s); %s declares it as %s", name, fname, have.Type, have.App, app, f.Type)
				}
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
		id := typeIDs[name]
		if id == "" {
			id = w.s.ids.next(w.now)
			if _, err := tx.Exec(`INSERT INTO types(id, name, created_by_app, def_app, def_json, created_at) VALUES(?, ?, ?, ?, '{}', ?)`,
				id, name, app, app, at); err != nil {
				return nil, err
			}
		}
		binding := resolved[name]
		binding.TypeID = id
		resolved[name] = binding
		if _, err := tx.Exec(`INSERT INTO app_types(app, type_id, uses) VALUES(?, ?, ?)`, app, id, t.Uses); err != nil {
			return nil, err
		}
		if !t.Uses {
			stored := t
			stored.Fields = nil
			typeDef := canonicalJSON(stored)
			if _, err := tx.Exec(`UPDATE types SET def_app = ?, def_json = ? WHERE id = ?`, app, typeDef, id); err != nil {
				return nil, err
			}
		}
		var existing *Type
		if typeIDs[name] != "" {
			existing = current.byID[id]
		}
		for pos, fname := range t.order {
			fieldID := ""
			if existing != nil {
				if have := existing.Fields[fname]; have != nil {
					fieldID = have.ID
				} else if now, ok := existing.aliases[fname]; ok {
					fieldID = existing.Fields[now].ID
				}
			}
			if fieldID == "" {
				fieldID = w.s.ids.next(w.now)
				if _, err := tx.Exec(`INSERT INTO fields(id, type_id, name, created_at) VALUES(?, ?, ?, ?)`, fieldID, id, fname, at); err != nil {
					return nil, err
				}
			}
			if _, err := tx.Exec(`
				INSERT INTO type_fields(field_id, app, def_json, pos, hidden) VALUES(?, ?, ?, ?, 0)
				ON CONFLICT(field_id, app) DO UPDATE SET def_json = excluded.def_json, pos = excluded.pos, hidden = 0`,
				fieldID, app, canonicalJSON(boundField(t.Fields[fname], resolved, current)), pos); err != nil {
				return nil, err
			}
		}
	}
	if _, err := tx.Exec(`
		INSERT INTO apps(app, def_json, registered_at) VALUES(?, ?, ?)
		ON CONFLICT(app) DO UPDATE SET def_json = excluded.def_json, registered_at = excluded.registered_at`,
		app, canonicalJSON(def.Capabilities.Records), at); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(`DELETE FROM app_type_bindings WHERE app = ?`, app); err != nil {
		return nil, err
	}
	for _, alias := range sortedKeys(resolved) {
		b := resolved[alias]
		if _, err := tx.Exec(`INSERT INTO app_type_bindings VALUES(?, ?, ?, ?)`, app, alias, b.TypeID, b.Common); err != nil {
			return nil, err
		}
	}

	next, problems, err := loadSchema(tx, w.s.now)
	if err != nil {
		return nil, err
	}
	// Only the types this app declares can refuse it: a problem another
	// app's fields have (a copy of a field since masked) is that app's to
	// fix, and must not block every other app's registration.
	for _, name := range def.typeOrder {
		t := next.appType(app, name)
		if t == nil {
			continue
		}
		if p := problems[t.Name]; len(p) > 0 {
			if e, ok := AsError(p[0]); ok {
				return nil, e
			}
			return nil, errorf(CodeInvalid, "%v", p[0])
		}
	}
	if err := checkRegistered(tx, next, current, def); err != nil {
		return nil, err
	}
	return next, nil
}

// checkRegistered checks what can only be checked once every app's
// declarations have merged: refs point at types that exist, defaults are
// values of their fields, capabilities name types that exist, a field newly
// made unique holds no value twice, and the indexes the fields ask for exist.
func checkRegistered(tx *sql.Tx, s, before *schema, def *AppDef) error {
	for _, name := range def.typeOrder {
		t := s.appType(def.ID, name)
		for _, fname := range def.Records[name].order {
			f := t.field(fname)
			if f == nil {
				continue
			}
			if (f.Type == TypeRef || f.Type == TypeRefs) && s.typeNamed(f.To) == nil {
				return errorf(CodeInvalid, "%s.%s points to %s records, and there is no %s type", name, fname, f.To, f.To)
			}
			if f.Default != nil {
				if _, err := coerceDefault(f, s.person); err != nil {
					return errorf(CodeInvalid, "%s.%s: the default %v is not a value of the field: %v", name, fname, f.Default, err)
				}
			}
			if f.Sequence != nil && f.Type != TypeText {
				return errorf(CodeInvalid, "%s.%s numbers records, so it is a text field", name, fname)
			}
			// A FIELD MADE UNIQUE OVER DATA THAT REPEATS IS REFUSED, naming how
			// many values repeat, rather than declared unique and false.
			wasUnique := false
			if bt := before.byID[t.ID]; bt != nil {
				if bf := bt.field(fname); bf != nil {
					wasUnique = bf.unique()
				}
			}
			if f.unique() && !wasUnique {
				col := jsonPath("data_json", f.ID)
				var repeats int
				if err := tx.QueryRow(`SELECT count(*) FROM (SELECT `+col+` v FROM records WHERE type_id = ? AND deleted_at IS NULL AND `+col+` IS NOT NULL GROUP BY v HAVING count(*) > 1)`, t.ID).Scan(&repeats); err != nil {
					return err
				}
				if repeats > 0 {
					return &Error{Code: CodeDuplicate, Field: fname, Message: fmt.Sprintf("%s.%s cannot be unique: %d of its values are held by more than one record", name, fname, repeats)}
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
		if s.appType(def.ID, typ) == nil {
			return errorf(CodeInvalid, "%s's capabilities name %s records, and there is no %s type", def.ID, typ, typ)
		}
	}
	if err := ensureIndexes(tx, s); err != nil {
		return err
	}
	for _, name := range def.typeOrder {
		if err := checkNewBookings(tx, s, before, s.appType(def.ID, name)); err != nil {
			return err
		}
	}
	return nil
}

// declarers is the apps that declare t without "uses": its definer, while
// it still declares it.
func (t *Type) declarers() []string {
	var out []string
	for _, app := range t.declaredBy {
		if !slices.Contains(t.UsedBy, app) {
			out = append(out, app)
		}
	}
	return out
}

// unique says the field holds no value twice: declared unique, or a
// sequence's numbers.
func (f *Field) unique() bool { return f.Unique || f.Sequence != nil }

// ensureIndexes makes the partial expression index each indexed field needs.
//
// AN INDEX IS AN EXPRESSION INDEX OVER THE JSON COLUMN, PARTIAL TO ITS TYPE,
// rather than a generated column: every type shares one records table, and a
// generated column per type and field would widen every row of every type.
// The query compiler spells the same expression and the same literal type
// test, which is what lets SQLite's planner use the index. Both are spelled
// with ids, so a rename never remakes an index.
func ensureIndexes(tx *sql.Tx, s *schema) error {
	for _, tname := range sortedKeys(s.types) {
		t := s.types[tname]
		for _, fname := range t.Order {
			f := t.Fields[fname]
			if f.Hidden || !(f.Index || f.unique() || f.Type == TypeEnum || f.Type == TypeRef || f.Type == TypePerson) {
				continue
			}
			stmt := fmt.Sprintf(`CREATE INDEX IF NOT EXISTS "rx_%s__%s" ON records(%s) WHERE type_id = '%s' AND deleted_at IS NULL`,
				mustID(t.ID), mustID(f.ID), jsonPath("data_json", f.ID), mustID(t.ID))
			if _, err := tx.Exec(stmt); err != nil {
				return err
			}
		}
	}
	return nil
}

// forgetApp removes an app's capabilities and declarations. Its types and
// fields stay, hidden when no other app declares them, and so do their
// values: deleting an app never touches its types or their records.
func forgetApp(tx *sql.Tx, app string, now func() time.Time) (*schema, error) {
	for _, stmt := range []string{
		`DELETE FROM app_types WHERE app = ?`,
		`DELETE FROM app_type_bindings WHERE app = ?`,
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

// RenameType gives a type a new name. Its records, history and indexes are
// by id and do not move; the old name stays an alias, so an app file, a
// query or a capability that still says it keeps working.
func (s *Store) RenameType(typ, name string) error {
	if !identShape.MatchString(name) {
		return errorf(CodeInvalid, "%q is not a record type name: use lower-case letters, digits and _", name)
	}
	return s.write(func(w *writeTx) error {
		t := w.schema.typeNamed(typ)
		if t == nil {
			return errorf(CodeNotFound, "there is no record type named %q", typ)
		}
		if t.Name == name {
			return nil
		}
		if other := w.schema.typeNamed(name); other != nil {
			return errorf(CodeInvalid, "there is already a type named %s", name)
		}
		for _, stmt := range []struct {
			q    string
			args []any
		}{
			{`DELETE FROM type_aliases WHERE alias = ?`, []any{name}},
			{`INSERT OR REPLACE INTO type_aliases(alias, type_id) VALUES(?, ?)`, []any{t.Name, t.ID}},
			{`UPDATE types SET name = ? WHERE id = ?`, []any{name, t.ID}},
		} {
			if _, err := w.tx.Exec(stmt.q, stmt.args...); err != nil {
				return err
			}
		}
		next, _, err := loadSchema(w.tx, w.s.now)
		if err != nil {
			return err
		}
		w.next = next
		return w.invalidate(EventMappingChanged, t.ID, "")
	})
}

// RenameField gives a field a new name, on the same terms as [Store.RenameType].
// An expression that names the field by its old name keeps compiling, since
// the old name stays the field's alias.
func (s *Store) RenameField(typ, field, name string) error {
	if !identShape.MatchString(name) || reserved[name] {
		return errorf(CodeInvalid, "%q is not a field name: use lower-case letters, digits and _", name)
	}
	return s.write(func(w *writeTx) error {
		t := w.schema.typeNamed(typ)
		if t == nil {
			return errorf(CodeNotFound, "there is no record type named %q", typ)
		}
		f := t.Fields[field]
		if f == nil {
			if now, ok := t.aliases[field]; ok {
				f = t.Fields[now]
			}
		}
		if f == nil {
			return fieldErr(field, "%s records have no field %q", t.Name, field)
		}
		if f.Name == name {
			return nil
		}
		if t.Fields[name] != nil {
			return fieldErr(name, "%s records already have a field %s", t.Name, name)
		}
		for _, stmt := range []struct {
			q    string
			args []any
		}{
			{`DELETE FROM field_aliases WHERE type_id = ? AND alias = ?`, []any{t.ID, name}},
			{`INSERT OR REPLACE INTO field_aliases(type_id, alias, field_id) VALUES(?, ?, ?)`, []any{t.ID, f.Name, f.ID}},
			{`UPDATE fields SET name = ? WHERE id = ?`, []any{name, f.ID}},
		} {
			if _, err := w.tx.Exec(stmt.q, stmt.args...); err != nil {
				return err
			}
		}
		next, _, err := loadSchema(w.tx, w.s.now)
		if err != nil {
			return err
		}
		w.next = next
		return w.invalidate(EventMappingChanged, t.ID, "")
	})
}

// jsonPath is the SQL expression that reads one field of a row's JSON by
// the field's id. The id is a ULID the store made; this spelling is the one
// the indexes are made with, so it must not change without remaking them.
func jsonPath(column, fieldID string) string {
	return fmt.Sprintf(`json_extract(%s, '$."%s"')`, column, mustID(fieldID))
}

// mustID answers id when it is a ULID and panics otherwise: an id reaching
// SQL as text is spliced, and only the store's own ids may be.
func mustID(id string) string {
	if !validID(id) {
		panic(fmt.Sprintf("data: %q reached SQL as an id", id))
	}
	return id
}

// TypeInfo is one type as the Data place lists it.
type TypeInfo struct {
	ID           string
	Name         string
	Title        string
	Label        string
	CreatedByApp string
	DefinedBy    string
	Lifecycle    *LifecycleDef
	Ledger       bool
	UsedBy       []string
	Hidden       bool
	Count        int
	Fields       []FieldInfo
}

// FieldInfo is one field as the Data place lists it.
type FieldInfo struct {
	ID   string
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
	return s.TypeSetFor("")
}

// TypeSetFor omits fields this app already contributes so a replacement
// definition can declare them again.
func (s *Store) TypeSetFor(app string) appschema.TypeSet {
	out := appschema.TypeSet{}
	for name, t := range s.schema.Load().types {
		if t.Hidden {
			continue
		}
		fields := map[string]appschema.Field{}
		for fname, f := range t.Fields {
			if !f.Hidden && !slices.Contains(f.Apps, app) {
				fields[fname] = f.FieldDef.Field
			}
		}
		// The validator refuses an app that defines a type another app
		// defines; a type whose defining app is gone may be defined again.
		owner := t.DefinedBy
		if !slices.Contains(t.declarers(), owner) {
			owner = ""
		}
		out[name] = appschema.ExistingType{Owner: owner, Fields: fields}
	}
	return out
}
