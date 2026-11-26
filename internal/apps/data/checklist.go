package data

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
)

type checklistDef struct {
	Signoff *checkSignoff  `json:"signoff,omitempty"`
	ID      string         `json:"id"`
	Steps   []checkStepDef `json:"steps"`
}

type checkSignoff struct {
	MakerCannotSign bool `json:"makerCannotSign,omitempty"`
	Roles           []struct {
		ID     string `json:"id"`
		Person string `json:"person"`
	} `json:"roles"`
}

type checkStepDef struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Evidence string `json:"evidence,omitempty"`
	Role     string `json:"role,omitempty"`
}

type checkReceipt struct {
	Rule     checkStepDef       `json:"rule"`
	Who      string             `json:"who"`
	At       string             `json:"at"`
	Evidence *ChecklistEvidence `json:"evidence,omitempty"`
}

// ChecklistEvidence is the evidence supplied when completing a declared step.
type ChecklistEvidence struct {
	Kind   string     `json:"kind"`
	Text   string     `json:"text,omitempty"`
	Record *RecordRef `json:"record,omitempty"`
}

// RecordRef names a record independently of the app using it.
type RecordRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

func (d *AppDef) readChecklists(raw []byte) error {
	var shape struct {
		Policies []struct {
			ID     string          `json:"id"`
			Kind   string          `json:"kind"`
			Config json.RawMessage `json:"config"`
		} `json:"policies"`
	}
	if err := json.Unmarshal(raw, &shape); err != nil {
		return errorf(CodeInvalid, "app policies could not be read: %v", err)
	}
	signoffs := map[string]json.RawMessage{}
	for _, policy := range shape.Policies {
		if policy.Kind == "signoff" {
			signoffs[policy.ID] = policy.Config
		}
	}
	for _, policy := range shape.Policies {
		if policy.Kind != "checklist" {
			continue
		}
		var config struct {
			Record  string         `json:"record"`
			Signoff string         `json:"signoff"`
			Steps   []checkStepDef `json:"steps"`
		}
		if err := json.Unmarshal(policy.Config, &config); err != nil {
			return errorf(CodeInvalid, "checklist %s could not be read: %v", policy.ID, err)
		}
		t, ok := d.Records[config.Record]
		if !ok || t.Uses {
			return errorf(CodeInvalid, "checklist %s must name a type this app declares", policy.ID)
		}
		if policy.ID == "" || len(config.Steps) == 0 || slices.ContainsFunc(t.Checklists, func(c checklistDef) bool { return c.ID == policy.ID }) {
			return errorf(CodeInvalid, "a checklist needs a unique id and at least one step")
		}
		seen := map[string]bool{}
		for i, step := range config.Steps {
			if step.ID == "" || seen[step.ID] {
				return errorf(CodeInvalid, "checklist %s needs unique step ids", policy.ID)
			}
			seen[step.ID] = true
			switch step.Evidence {
			case "":
				config.Steps[i].Evidence = "none"
			case "none", "note", "record", "file":
			default:
				return errorf(CodeInvalid, "checklist %s step %s has an unknown evidence kind", policy.ID, step.ID)
			}
		}
		c := checklistDef{ID: policy.ID, Steps: config.Steps}
		if config.Signoff != "" {
			c.Signoff = &checkSignoff{}
			if err := json.Unmarshal(signoffs[config.Signoff], c.Signoff); err != nil {
				return errorf(CodeInvalid, "checklist %s names an unreadable sign-off %s", policy.ID, config.Signoff)
			}
		}
		t.Checklists = append(t.Checklists, c)
		d.Records[config.Record] = t
	}
	return nil
}

func completedStep(r *record, checklist string, step checkStepDef) (checkReceipt, bool) {
	receipt, ok := r.Meta.Checklists[checklist][step.ID]
	// Label edits leave completed work intact. A changed requirement needs
	// a new completion under that requirement.
	return receipt, ok && receipt.Rule.Evidence == step.Evidence && receipt.Rule.Role == step.Role
}

func completedChecklists(t *Type, r *record) []string {
	ids := []string{}
	for _, checklist := range t.checklists {
		if !slices.ContainsFunc(checklist.Steps, func(step checkStepDef) bool {
			_, done := completedStep(r, checklist.ID, step)
			return !done
		}) {
			ids = append(ids, checklist.ID)
		}
	}
	if len(ids) != 0 && len(ids) == len(t.checklists) {
		ids = append(ids, "")
	}
	return ids
}

// ChecklistSet changes one declared step and returns the process as committed.
func (s *Store) ChecklistSet(sc Scope, typ, id, checklist, step string, done bool, rev int64, evidence *ChecklistEvidence) (*ProcessSnapshot, error) {
	return s.processWrite(sc, typ, id, true, func(w *writeTx, t *Type, before *record) (*record, error) {
		if rev <= 0 {
			return nil, errorf(CodeInvalid, "expected_rev is required")
		}
		if before.Rev != rev {
			return nil, staleTransition(t, rev, before.Rev)
		}
		for _, c := range t.checklists {
			if c.ID != checklist {
				continue
			}
			for _, def := range c.Steps {
				if def.ID == step {
					return w.setChecklistStep(sc, t, before, c, def, done, evidence)
				}
			}
		}
		return nil, errorf(CodeNotFound, "there is no checklist step %s/%s on %s", checklist, step, t.Name)
	})
}

func (w *writeTx) setChecklistStep(sc Scope, t *Type, before *record, checklist checklistDef, step checkStepDef, done bool, evidence *ChecklistEvidence) (*record, error) {
	if t.Ledger {
		return nil, errorf(CodeLedger, "%s entries are never changed; reverse the entry instead", t.Name)
	}
	if err := w.s.checkFrozen(t, before, []string{"checklist"}, sc, ""); err != nil {
		return nil, err
	}
	if step.Role != "" && !w.canCheck(sc, t, before, checklist.Signoff, step.Role) {
		return nil, errorf(CodeNotAllowed, "%s must be checked by the person assigned to %s", step.Label, step.Role)
	}
	if done {
		if err := w.checkEvidence(sc, step, evidence); err != nil {
			return nil, err
		}
	} else {
		evidence = nil
	}
	old, alreadyDone := completedStep(before, checklist.ID, step)
	if !done && !alreadyDone || done && alreadyDone && canonicalJSON(old.Evidence) == canonicalJSON(evidence) {
		return before, nil
	}
	next := before.clone()
	next.Meta.Checklists = maps.Clone(before.Meta.Checklists)
	if next.Meta.Checklists == nil {
		next.Meta.Checklists = map[string]map[string]checkReceipt{}
	}
	steps := maps.Clone(next.Meta.Checklists[checklist.ID])
	if steps == nil {
		steps = map[string]checkReceipt{}
	}
	if done {
		steps[step.ID] = checkReceipt{Rule: step, Who: w.schema.actor(sc), At: w.now.UTC().Format(stamp), Evidence: evidence}
	} else {
		delete(steps, step.ID)
	}
	next.Meta.Checklists[checklist.ID] = steps
	next.Rev++
	next.UpdatedAt = w.now.UTC().Format(stamp)
	if _, err := w.newBatch(sc, OpChecklist, t); err != nil {
		return nil, err
	}
	if err := w.save(t, next, false, sc); err != nil {
		return nil, err
	}
	return next, w.history(t, sc, OpChecklist, checklist.ID+"/"+step.ID, before, next)
}

// A named sign-off resolves its roles against the live record and the actor.
func (w *writeTx) canCheck(sc Scope, t *Type, r *record, signoff *checkSignoff, role string) bool {
	if sc.kind(OpChecklist) != ByPerson {
		return false
	}
	if signoff == nil {
		return true
	}
	resolve := func(selector string) string {
		if name, ok := strings.CutPrefix(selector, "$record."); ok {
			if field := t.field(name); field != nil && field.Type == TypePerson {
				selector, _ = r.Data[field.Name].(string)
			} else {
				return ""
			}
		}
		person, _ := w.schema.personValue(selector)
		return person
	}
	actor := w.schema.actor(sc)
	for i, assigned := range signoff.Roles {
		if assigned.ID == role && actor == resolve(assigned.Person) {
			return !signoff.MakerCannotSign || i == 0 || actor != resolve(signoff.Roles[0].Person)
		}
	}
	return false
}

func (w *writeTx) checkEvidence(sc Scope, step checkStepDef, e *ChecklistEvidence) error {
	if step.Evidence == "file" {
		return errorf(CodeInvalid, "file evidence for checklists is unavailable in this build")
	}
	if step.Evidence == "none" && e == nil {
		return nil
	}
	if e != nil && e.Kind == step.Evidence {
		switch e.Kind {
		case "note":
			if strings.TrimSpace(e.Text) != "" && len(e.Text) <= bodyMax {
				return nil
			}
		case "record":
			if e.Record != nil && e.Record.Type != "" && e.Record.ID != "" {
				_, _, err := w.subject(sc, e.Record.Type, e.Record.ID, false)
				return err
			}

		}
	}
	return errorf(CodeInvalid, "%s needs %s evidence", step.Label, step.Evidence)
}
