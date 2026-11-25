package data

import "slices"

// ChecklistState is the current progress through one declared checklist.
type ChecklistState struct {
	ID    string
	Steps []ChecklistStep
}

// ChecklistStep is one declared step and the evidence attached to it.
type ChecklistStep struct {
	ID       string
	Label    string
	Evidence string
	Done     bool
	Attached *ChecklistEvidence
}

// ProcessSnapshot is the process of one record, read at one point in the
// writer's order. The revision is the one the guards and checklist read.
type ProcessSnapshot struct {
	State       string
	Rev         int64
	Transitions []TransitionState
	Checklists  []ChecklistState
	WorkItems   []WorkItem
	Timers      []Timer
	Signoffs    []Signoff
	Approval    *ApprovalRoute
	Timeline    []Activity
}

// Process returns one consistent view, including the guard results that
// govern an attempted transition.
func (s *Store) Process(sc Scope, typ, id string) (*ProcessSnapshot, error) {
	return s.processWrite(sc, typ, id, false, nil)
}

func (s *Store) processWrite(sc Scope, typ, id string, write bool, change func(*writeTx, *Type, *record) (*record, error)) (*ProcessSnapshot, error) {
	if typ == "" || id == "" {
		return nil, errorf(CodeInvalid, "a process names a record type and id")
	}
	var out *ProcessSnapshot
	err := s.write(func(w *writeTx) error {
		t, r, err := w.subject(sc, typ, id, write)
		if err != nil {
			return err
		}
		if change != nil {
			r, err = change(w, t, r)
			if err != nil {
				return err
			}
		}
		out, err = w.process(sc, t, r)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (w *writeTx) process(sc Scope, t *Type, r *record) (*ProcessSnapshot, error) {
	out := &ProcessSnapshot{Rev: r.Rev, Checklists: []ChecklistState{}}
	if field := t.processField(); field != nil {
		out.State, _ = r.Data[field.Name].(string)
	}
	var err error
	if out.Transitions, err = w.transitionsOn(w.tx, sc, t, r); err != nil {
		return nil, err
	}
	if out.WorkItems, err = workItemsOn(w.tx, w.schema, sc, WorkFilter{Record: r.ID, Status: WorkOpen}, false); err != nil {
		return nil, err
	}
	if out.Timers, err = timersOn(w.tx, w.schema, sc, r.ID, false); err != nil {
		return nil, err
	}
	if out.Signoffs, err = signoffsOn(w.tx, t, r.ID, r); err != nil {
		return nil, err
	}
	if rule := t.approvalFor(r); rule != nil {
		if out.Approval, err = rule.steps(w.tx, r, w.schema.person); err != nil {
			return nil, err
		}
	}
	if out.Timeline, err = activityOn(w.tx, w.schema, sc, t, r.ID, out.Signoffs); err != nil {
		return nil, err
	}
	slices.Reverse(out.Timeline)
	for _, c := range t.checklists {
		checklist := ChecklistState{ID: c.ID}
		for _, step := range c.Steps {
			receipt, done := completedStep(r, c.ID, step)
			state := ChecklistStep{ID: step.ID, Label: step.Label, Evidence: step.Evidence, Done: done}
			if e := receipt.Evidence; done && e != nil {
				copy := *e
				state.Attached = &copy
				if e.Record != nil {
					if _, err := w.schema.allow(sc, e.Record.Type, false); err != nil {
						state.Attached = nil
					}
				}
			}
			checklist.Steps = append(checklist.Steps, state)
		}
		out.Checklists = append(out.Checklists, checklist)
	}
	return out, nil
}
