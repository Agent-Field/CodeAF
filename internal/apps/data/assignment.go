package data

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

type assignmentRule struct {
	id           string
	def          routingDeclaration
	when         *program
	field, owner *Field
	linked       *Type
	candidates   []string
}

func (s *schema) compileAssignment(t *Type, id string, def routingDeclaration, whenMatch *program, now time.Time) (assignmentRule, error) {
	r := assignmentRule{id: id, def: def, when: whenMatch, field: ruleField(t, def.Field)}
	title := strings.TrimSpace(def.Work.Title)
	if title == "" || len(title) > titleMax || len(def.Work.Form) > 200 {
		return r, ruleError(id, "invalid work item title or form")
	}
	for _, date := range []string{def.Work.Due, def.Work.SLA} {
		if _, err := workDeadline(date, now); err != nil {
			return r, ruleError(id, err.Error())
		}
	}
	for _, candidate := range def.Candidates {
		value, err := s.personValue(candidate)
		if err != nil || value == "" {
			return r, ruleError(id, "each candidate must name a person or agent")
		}
		if !slices.Contains(r.candidates, value) {
			r.candidates = append(r.candidates, value)
		}
	}
	if len(def.Roles) != 0 && !slices.Contains(r.candidates, s.person) {
		r.candidates = append(r.candidates, s.person)
	}
	switch def.Strategy {
	case "round-robin", "load", "role":
		if len(r.candidates) == 0 {
			return r, ruleError(id, "provide candidates or roles to assign")
		}
	case "field":
		if r.field == nil {
			return r, ruleError(id, "field assignment needs a visible field")
		}
		if len(def.Matches) == 0 && r.field.Type != TypePerson {
			return r, ruleError(id, "field assignment needs a person field or a value-to-assignee map")
		}
		if len(def.Matches) != 0 {
			switch r.field.Type {
			case TypeText, TypeEnum, TypePerson:
			default:
				return r, ruleError(id, "field matching needs a text, enum or person field")
			}
			for _, key := range sortedKeys(def.Matches) {
				if value, err := s.personValue(def.Matches[key]); err != nil || value == "" {
					return r, ruleError(id, "each field match must name a person or agent")
				}
			}
		}
	case "linked-owner":
		if r.field == nil || r.field.Type != TypeRef {
			return r, ruleError(id, "linked owner needs a single record reference")
		}
		target, err := s.allow(Scope{App: t.DefinedBy}, r.field.To, false)
		if err != nil {
			return r, ruleError(id, "the defining app must be able to read the linked type")
		}
		r.linked, r.owner = target, ruleField(target, def.OwnerField)
		if r.owner == nil || r.owner.Type != TypePerson {
			return r, ruleError(id, "linked owner needs a visible person field on the linked type")
		}
	case "skill":
		return r, ruleError(id, "skill assignment requires a member directory")
	default:
		return r, ruleError(id, "unknown assignment strategy")
	}
	return r, nil
}

func workDeadline(value string, now time.Time) (string, error) {
	if span, ok := strings.CutPrefix(value, "PT"); ok {
		if d, err := time.ParseDuration(strings.ToLower(span)); err == nil && d >= 0 {
			return now.Add(d).UTC().Format(stamp), nil
		}
	} else if span, ok := strings.CutPrefix(value, "P"); ok {
		if d, err := parseSpan(strings.ToLower(span)); err == nil {
			return now.Add(d).UTC().Format(stamp), nil
		}
	}
	return when(value, now)
}

func (w *writeTx) assign(sc Scope, t *Type, r *record, transition string) error {
	for _, rule := range t.rules.assignments {
		def := rule.def
		if def.Transition != "" && def.Transition != transition {
			continue
		}
		if rule.when != nil && !rule.when.evalBool(t, r.values(), nil) {
			if err := w.ruleActivity(sc, t, r.ID, fmt.Sprintf("%s: no assignment because the condition did not match", rule.id)); err != nil {
				return err
			}
			continue
		}
		to, why, err := w.assignee(t, r, rule)
		if err != nil {
			return err
		}
		sourceType := ""
		if rule.linked != nil {
			sourceType = rule.linked.ID
		}
		if to != "" {
			due, err := workDeadline(def.Work.Due, w.now)
			if err != nil {
				return err
			}
			sla, err := workDeadline(def.Work.SLA, w.now)
			if err != nil {
				return err
			}
			owner := sc
			owner.Actor, owner.App = w.schema.actor(sc), t.DefinedBy
			_, err = w.createWorkItem(owner, t, WorkItem{Title: strings.TrimSpace(def.Work.Title), Record: r.ID, Assignee: to, Form: def.Work.Form, Due: due, SLAAt: sla, sourceType: sourceType})
			if err != nil {
				return err
			}
			why = fmt.Sprintf("assigned %s to %s by %s", def.Work.Title, to, why)
		}
		if err := w.scopedRuleActivity(sc, t, r.ID, rule.id+": "+why, sourceType); err != nil {
			return err
		}
	}
	return nil
}

func (w *writeTx) assignee(t *Type, r *record, rule assignmentRule) (string, string, error) {
	switch rule.def.Strategy {
	case "field":
		value, _ := r.Data[rule.field.Name].(string)
		if len(rule.def.Matches) != 0 {
			value = rule.def.Matches[value]
		}
		if value == "" {
			return "", "no assignment because the field has no assignee match", nil
		}
		to, err := w.schema.personValue(value)
		return to, "field match", err
	case "linked-owner":
		id, _ := r.Data[rule.field.Name].(string)
		linked, err := loadRecord(w.tx, w.schema, id)
		if err != nil {
			return "", "", err
		}
		if linked != nil && linked.DeletedAt == "" && linked.Type == rule.linked.Name {
			if owner, _ := linked.Data[rule.owner.Name].(string); owner != "" {
				to, err := w.schema.personValue(owner)
				return to, "linked record owner", err
			}
		}
		return "", "no assignment because the linked record has no owner", nil
	case "round-robin":
		var next int
		err := w.tx.QueryRow(`SELECT next FROM rule_rotations WHERE type_id = ? AND rule = ?`, t.ID, rule.id).Scan(&next)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", "", err
		}
		next %= len(rule.candidates)
		_, err = w.tx.Exec(`INSERT INTO rule_rotations(type_id,rule,next) VALUES(?,?,?)
			ON CONFLICT(type_id,rule) DO UPDATE SET next = excluded.next`, t.ID, rule.id, (next+1)%len(rule.candidates))
		return rule.candidates[next], "round-robin", err
	case "load":
		pick, least := "", 0
		for _, candidate := range rule.candidates {
			var count int
			if err := w.tx.QueryRow(`SELECT count(*) FROM work_items WHERE assignee = ? AND status = ?`, candidate, WorkOpen).Scan(&count); err != nil {
				return "", "", err
			}
			if pick == "" || count < least {
				pick, least = candidate, count
			}
		}
		return pick, "fewest open work items", nil
	case "role":
		return rule.candidates[0], "role", nil
	default:
		return "", "", ruleError(rule.id, "unknown assignment strategy")
	}
}
