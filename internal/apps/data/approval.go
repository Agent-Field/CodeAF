package data

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
)

type approvalRule struct {
	id, key string
	def     routingDeclaration
	when    *program
}

func compileApproval(id string, def routingDeclaration, match *program) (approvalRule, error) {
	rule := approvalRule{id: id, def: def, when: match}
	rule.def.Statement = strings.TrimSpace(rule.def.Statement)
	if def.Order != "serial" && def.Order != "parallel" {
		return rule, ruleError(id, "approvals need serial or parallel order")
	}
	if len(def.Roles) == 0 || len(rule.def.Statement) > statementMax {
		return rule, ruleError(id, "approvals need at least one role and a valid statement")
	}
	for i, role := range def.Roles {
		if slices.Contains(def.Roles[:i], role) {
			return rule, ruleError(id, "each approval role appears only once")
		}
	}
	rule.key = fmt.Sprintf("%s:%x", id, sha256.Sum256([]byte(canonicalJSON(rule.def.RoutingRule))))
	return rule, nil
}

// ApprovalRoute is the sign-off chain for this revision. Each step has its
// own immutable receipt, even when Personal binds several roles to one person.
type ApprovalRoute struct {
	Rule      string
	Order     string
	Statement string
	Steps     []ApprovalStep
}

type ApprovalStep struct {
	Role     string
	Approver string
	Signoff  string
	Ready    bool
	Blocked  string
}

func (t *Type) approvalFor(r *record) *approvalRule {
	if r == nil || r.DeletedAt != "" {
		return nil
	}
	for i := range t.rules.approvals {
		rule := &t.rules.approvals[i]
		if rule.def.Transition != "" && rule.def.Transition != r.Meta.LastTransition {
			continue
		}
		if rule.when == nil || rule.when.evalBool(t, r.values(), nil) {
			return rule
		}
	}
	return nil
}

const needsChecker = "The creator cannot approve their own record. This approval requires a second person."

func (r *record) madeBy(person string) bool {
	// Records made before routing used the Personal space's sole human too.
	return r.Meta.Maker == "" || r.Meta.Maker == person
}

func (rule *approvalRule) steps(q querier, r *record, person string) (*ApprovalRoute, error) {
	route := &ApprovalRoute{
		Rule: rule.id, Order: rule.def.Order, Statement: rule.def.Statement,
		Steps: make([]ApprovalStep, len(rule.def.Roles)),
	}
	for i, role := range rule.def.Roles {
		route.Steps[i] = ApprovalStep{Role: role, Approver: person}
	}
	err := eachRow(q, `SELECT step, signoff FROM approval_receipts WHERE record = ? AND revision = ? AND route = ?`,
		[]any{r.ID, r.Rev, rule.key}, func(scan func(...any) error) error {
			var step int
			var signoff string
			if err := scan(&step, &signoff); err != nil {
				return err
			}
			if step >= 0 && step < len(route.Steps) {
				route.Steps[step].Signoff = signoff
			}
			return nil
		})
	if err != nil {
		return nil, err
	}
	waiting := false
	for i := range route.Steps {
		step := &route.Steps[i]
		if step.Signoff != "" {
			continue
		}
		switch {
		case rule.def.MakerCannotSign && r.madeBy(person):
			step.Blocked = needsChecker
		case route.Order == "serial" && waiting:
			step.Blocked = "An earlier approval step is still open."
		default:
			step.Ready = true
		}
		waiting = true
	}
	return route, nil
}

func (w *writeTx) recordApprovalRoute(sc Scope, t *Type, r *record) error {
	if len(t.rules.approvals) == 0 {
		return nil
	}
	rule := t.approvalFor(r)
	if rule == nil {
		return w.ruleActivity(sc, t, r.ID, "No approval route matched this record.")
	}
	reason := fmt.Sprintf("%s: selected %s approval by %s", rule.id, rule.def.Order, strings.Join(rule.def.Roles, ", "))
	if rule.def.When != "" {
		reason += " because " + rule.def.When + " matched"
	}
	if rule.def.MakerCannotSign && r.madeBy(w.schema.person) {
		reason += ". " + needsChecker
	}
	return w.ruleActivity(sc, t, r.ID, reason)
}

// nextApproval checks the same chain the process snapshot shows. A generic
// sign-off cannot count for an active route or skip a pending step.
func (w *writeTx) nextApproval(r *record, rule *approvalRule, statement string) (int, error) {
	if rule.def.MakerCannotSign && r.madeBy(w.schema.person) {
		return 0, &Error{Code: CodeNotAllowed, Need: "approval.checker", Message: needsChecker}
	}
	route, err := rule.steps(w.tx, r, w.schema.person)
	if err != nil {
		return 0, err
	}
	for i, step := range route.Steps {
		if step.Ready {
			if route.Statement != "" && route.Statement != statement {
				return 0, fieldErr("statement", "sign the statement declared by approval rule %q", rule.id)
			}
			return i, nil
		}
	}
	return 0, errorf(CodeStale, "all steps for approval rule %q are already signed", rule.id)
}

func currentApproval(q querier, t *Type, r *record) (bool, error) {
	if t.rules.invalid != nil {
		return false, t.rules.invalid
	}
	if rule := t.approvalFor(r); rule != nil {
		var signed int
		err := q.QueryRow(`SELECT count(*) FROM approval_receipts WHERE record = ? AND revision = ? AND route = ?`,
			r.ID, r.Rev, rule.key).Scan(&signed)
		return signed == len(rule.def.Roles), err
	}
	var signed int
	err := q.QueryRow(`SELECT 1 FROM signoffs s WHERE record = ? AND version_hash = ?
		AND NOT EXISTS (SELECT 1 FROM approval_receipts a WHERE a.signoff = s.id) LIMIT 1`, r.ID, versionHash(t, r)).Scan(&signed)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return signed == 1, err
}
