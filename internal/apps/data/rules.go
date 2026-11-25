package data

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/google/cel-go/cel"

	appschema "github.com/Agent-Field/codeaf/internal/apps/schema"
)

const writeRulesMigration = `
	ALTER TABLE work_items ADD COLUMN source_type_id TEXT NOT NULL DEFAULT '';
	CREATE TABLE rule_activity(
		id TEXT PRIMARY KEY,
		type_id TEXT NOT NULL,
		source_type_id TEXT NOT NULL,
		record TEXT NOT NULL,
		actor TEXT NOT NULL,
		by_kind TEXT NOT NULL,
		reason TEXT NOT NULL,
		at TEXT NOT NULL
	);
	CREATE INDEX rule_activity_record ON rule_activity(type_id, record, id);
	CREATE TABLE rule_rotations(
		type_id TEXT NOT NULL,
		rule TEXT NOT NULL,
		next INTEGER NOT NULL,
		PRIMARY KEY(type_id, rule)
	);
	CREATE TABLE approval_receipts(
		record TEXT NOT NULL,
		revision INTEGER NOT NULL,
		route TEXT NOT NULL,
		step INTEGER NOT NULL,
		signoff TEXT NOT NULL UNIQUE,
		PRIMARY KEY(record, revision, route, step)
	);
`

// Rule declarations travel with the type that owns the write, so every
// caller gets the same rules when writing the shared type.
type ruleDeclarations struct {
	Routing  map[string]routingDeclaration `json:"routing,omitempty"`
	Bookings map[string]appschema.Resource `json:"bookings,omitempty"`
}

type routingDeclaration struct {
	appschema.RoutingRule
	Work appschema.WorkItem `json:"work,omitzero"`
}

type writeRules struct {
	declarations ruleDeclarations
	bookings     []bookingRule
	assignments  []assignmentRule
	approvals    []approvalRule
	invalid      *Error
}

func (d *AppDef) readRules(raw []byte) error {
	var declared struct {
		Rules     map[string]appschema.RoutingRule
		Resources map[string]appschema.Resource
		Process   appschema.Process
		Roles     map[string]appschema.Role
	}
	if err := json.Unmarshal(raw, &declared); err != nil {
		return errorf(CodeInvalid, "app rules could not be read: %v", err)
	}
	for _, id := range sortedKeys(declared.Rules) {
		r := declared.Rules[id]
		t, ok := d.Records[r.Record]
		if !ok || t.Uses {
			return errorf(CodeInvalid, "rule %q must name a record type this app defines", id)
		}
		for _, role := range r.Roles {
			if _, ok := declared.Roles[role]; !ok {
				return errorf(CodeInvalid, "rule %q names unknown role %q", id, role)
			}
		}
		work := declared.Process.WorkItems[r.WorkItem]
		if r.Kind == "assignment" && (work.Title == "" || work.Subject.Type != r.Record) {
			return errorf(CodeInvalid, "rule %q needs a work item for %s", id, r.Record)
		}
		if t.Rules.Routing == nil {
			t.Rules.Routing = map[string]routingDeclaration{}
		}
		t.Rules.Routing[id] = routingDeclaration{RoutingRule: r, Work: work}
		d.Records[r.Record] = t
	}
	for _, id := range sortedKeys(declared.Resources) {
		r := declared.Resources[id]
		t, ok := d.Records[r.Booking.Type]
		if !ok || t.Uses {
			return errorf(CodeInvalid, "resource %q must name a booking type this app defines", id)
		}
		if t.Rules.Bookings == nil {
			t.Rules.Bookings = map[string]appschema.Resource{}
		}
		t.Rules.Bookings[id] = r
		d.Records[r.Booking.Type] = t
	}
	return nil
}

func (s *schema) compileRules(t *Type, now func() time.Time) []error {
	var problems []error
	for _, id := range sortedKeys(t.rules.declarations.Bookings) {
		rule, err := s.compileBooking(t, id, t.rules.declarations.Bookings[id])
		if err != nil {
			problems = append(problems, err)
			continue
		}
		t.rules.bookings = append(t.rules.bookings, rule)
	}
	for _, id := range sortedKeys(t.rules.declarations.Routing) {
		def := t.rules.declarations.Routing[id]
		when, err := compileRuleCondition(t, def.RoutingRule, now, s.person)
		if err != nil {
			problems = append(problems, ruleError(id, err.Error()))
			continue
		}
		switch def.Kind {
		case "assignment":
			rule, err := s.compileAssignment(t, id, def, when, now())
			if err != nil {
				problems = append(problems, err)
			} else {
				t.rules.assignments = append(t.rules.assignments, rule)
			}
		case "approval-routing":
			rule, err := compileApproval(id, def, when)
			if err != nil {
				problems = append(problems, err)
			} else {
				t.rules.approvals = append(t.rules.approvals, rule)
			}
		default:
			problems = append(problems, ruleError(id, "unknown rule kind"))
		}
	}
	slices.SortStableFunc(t.rules.approvals, func(a, b approvalRule) int {
		return cmp.Compare(b.def.Priority, a.def.Priority)
	})
	if len(problems) != 0 {
		t.rules.invalid = errorf(CodeInvalid, "%v", problems[0])
	}
	return problems
}

func compileRuleCondition(t *Type, rule appschema.RoutingRule, now func() time.Time, person string) (*program, error) {
	if rule.Transition != "" && (t.Lifecycle == nil || t.Lifecycle.byID[rule.Transition] == nil) {
		return nil, fmt.Errorf("there is no transition named %q", rule.Transition)
	}
	if rule.When == "" {
		return nil, nil
	}
	for name := range identifiers(rule.When) {
		switch name {
		case "_signed", "_open_work", "_checklists", "_ch", "_changed", "now", "days_since", "updated_at":
			return nil, fmt.Errorf("routing conditions must read stable record values")
		}
		if f := t.field(name); f != nil && (f.Masked || f.Hidden) {
			return nil, fmt.Errorf("routing conditions cannot read hidden field %q", name)
		}
	}
	env, err := exprEnv(t, now, person)
	if err != nil {
		return nil, err
	}
	ast, issues := env.Compile(rewrite(rule.When))
	if issues != nil && issues.Err() != nil {
		return nil, issues.Err()
	}
	if ast.OutputType() != cel.BoolType && ast.OutputType() != cel.DynType {
		return nil, fmt.Errorf("routing conditions must return true or false")
	}
	return compile(env, rule.When)
}

func (w *writeTx) ruleActivity(sc Scope, t *Type, record, reason string) error {
	return w.scopedRuleActivity(sc, t, record, reason, "")
}

// A linked owner's identity is derived from a second type. Only readers of
// both types may see the assignment reason or work item.
func (w *writeTx) scopedRuleActivity(sc Scope, t *Type, record, reason, sourceType string) error {
	kind := sc.kind("")
	if w.change != nil {
		kind = w.change.ByKind
	}
	_, err := w.tx.Exec(`INSERT INTO rule_activity(id, type_id, source_type_id, record, actor, by_kind, reason, at) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		w.s.ids.next(w.now), t.ID, sourceType, record, w.schema.actor(sc), kind, reason, w.now.UTC().Format(stamp))
	return err
}

func (s *schema) canReadSource(sc Scope, sourceType string) bool {
	if sc.App == "" || sourceType == "" {
		return true
	}
	t := s.byID[sourceType]
	return t != nil && s.caps[sc.App][t.Name] != ""
}

func ruleField(t *Type, name string) *Field {
	if f := t.field(name); f != nil && !f.Masked && !f.Hidden {
		return f
	}
	return nil
}

func ruleError(id, message string) error {
	return fmt.Errorf("rule %q: %s", id, message)
}
