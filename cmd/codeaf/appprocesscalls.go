package main

import (
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/appwire"
)

func (b *appBackend) DataProcessSnapshot(p appwire.ProcessParams) (*appwire.DataProcessResult, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	view, err := s.Process(dataScope(p.DataScope), p.Record.Type, p.Record.ID)
	return b.wireProcess(s, p.DataScope, view, err)
}

func (b *appBackend) DataTransitionBatch(p appwire.TransitionBatchParams) (*appwire.DataWriteResult, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	refs := make([]data.TransitionRecord, 0, len(p.Records))
	for _, row := range p.Records {
		refs = append(refs, data.TransitionRecord{RecordRef: data.RecordRef(row.Record), Rev: int64(row.ExpectedRev)})
	}
	return wireWrite(s.TransitionBatch(dataScope(p.DataScope), p.Transition, refs,
		data.TransitionOptions{Reason: p.Reason, Unfreeze: p.Unfreeze}))
}

func (b *appBackend) DataChecklistSet(p appwire.ChecklistSetParams) (*appwire.DataProcessResult, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	var provided *data.ChecklistEvidence
	if e := p.Evidence; e != nil {
		provided = &data.ChecklistEvidence{Kind: e.Kind, Text: e.Text,
			Record: (*data.RecordRef)(e.Record)}
	}
	view, err := s.ChecklistSet(dataScope(p.DataScope), p.Record.Type, p.Record.ID,
		p.Checklist, p.Step, *p.Done, int64(p.ExpectedRev), provided)
	return b.wireProcess(s, p.DataScope, view, err)
}

func (b *appBackend) wireProcess(s *data.Store, scope appwire.DataScope, v *data.ProcessSnapshot, err error) (*appwire.DataProcessResult, error) {
	if err != nil {
		return nil, appDataError(err)
	}
	out := &appwire.DataProcessResult{
		State: v.State, Rev: v.Rev, Transitions: []appwire.ProcessTransition{}, Checklists: []appwire.ProcessChecklist{},
		WorkItems: b.workRows(s, scope, v.WorkItems), Timers: []appwire.DataRow{}, Signoffs: []appwire.DataRow{},
		Timeline: []appwire.ProcessTimelineEntry{},
	}
	for _, tr := range v.Transitions {
		out.Transitions = append(out.Transitions, appwire.ProcessTransition{
			DataTransitionState: appwire.DataTransitionState{ID: tr.ID, Label: tr.Label, To: tr.To, Ready: tr.Ready, Allowed: tr.Allowed},
			Blocked:             tr.Blocked,
		})
	}
	for _, c := range v.Checklists {
		check := appwire.ProcessChecklist{ID: c.ID}
		for _, step := range c.Steps {
			entry := appwire.ProcessChecklistStep{ID: step.ID, Label: step.Label, Done: step.Done, Who: step.Who, At: step.At, Evidence: step.Evidence}
			if e := step.Attached; e != nil {
				entry.Attached = &appwire.ChecklistEvidence{Kind: e.Kind, Text: e.Text,
					Record: (*appwire.DataRecordRef)(e.Record)}
			}
			check.Steps = append(check.Steps, entry)
		}
		out.Checklists = append(out.Checklists, check)
	}
	if len(out.Checklists) != 0 {
		out.Checklist = &out.Checklists[0]
	}
	for _, timer := range v.Timers {
		out.Timers = append(out.Timers, timer.Row())
	}
	for _, signoff := range v.Signoffs {
		out.Signoffs = append(out.Signoffs, signoff.Row())
	}
	if route := v.Approval; route != nil {
		out.Approval = &appwire.ProcessApproval{
			Rule: route.Rule, Order: route.Order, Statement: route.Statement,
			Steps: make([]appwire.ProcessApprovalStep, 0, len(route.Steps)),
		}
		for _, step := range route.Steps {
			out.Approval.Steps = append(out.Approval.Steps, appwire.ProcessApprovalStep(step))
		}
	}
	for _, line := range v.Timeline {
		why := line.Reason
		if why == "" {
			why = line.Body
		}
		if why == "" && line.Transition != "" {
			why = line.From + " → " + line.To
		}
		out.Timeline = append(out.Timeline, appwire.ProcessTimelineEntry{
			ID: line.ID, Kind: line.Kind, Who: line.Actor, Why: why, At: line.At,
		})
	}
	return out, nil
}

func (b *appBackend) WorkItemsList(p appwire.DataScope) (*appwire.DataItemsResult, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	list, err := s.MyWork(dataScope(p))
	if err != nil {
		return nil, appDataError(err)
	}
	return &appwire.DataItemsResult{Items: b.workRows(s, p, list)}, nil
}

func (b *appBackend) WorkItemsClaim(p appwire.DataIDParams) (*appwire.DataItemResult, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	wi, err := s.ClaimWorkItem(dataScope(p.DataScope), p.ID)
	return b.workItem(s, p.DataScope, wi, err)
}

func (b *appBackend) WorkItemsComplete(p appwire.WorkCompleteParams) (*appwire.DataItemResult, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	wi, err := s.CompleteWorkItem(dataScope(p.DataScope), p.ID, p.Values)
	return b.workItem(s, p.DataScope, wi, err)
}

func (b *appBackend) WorkItemsReassign(p appwire.WorkReassignParams) (*appwire.DataItemResult, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	wi, err := s.ReassignWorkItem(dataScope(p.DataScope), p.ID, *p.Assignee)
	return b.workItem(s, p.DataScope, wi, err)
}

func (b *appBackend) workItem(s *data.Store, scope appwire.DataScope, wi *data.WorkItem, err error) (*appwire.DataItemResult, error) {
	if err != nil {
		return nil, appDataError(err)
	}
	return &appwire.DataItemResult{Item: b.workRows(s, scope, []data.WorkItem{*wi})[0]}, nil
}

type workApp struct {
	Name    string `json:"name"`
	Process struct {
		Forms map[string]workForm `json:"forms"`
	} `json:"process"`
}

type workForm struct {
	Record string `json:"record"`
	Steps  []struct {
		ID     string          `json:"id"`
		Label  string          `json:"label"`
		When   json.RawMessage `json:"when"`
		Fields []struct {
			Field    string          `json:"field"`
			When     json.RawMessage `json:"when"`
			Required *bool           `json:"required"`
		} `json:"fields"`
	} `json:"steps"`
}

func (b *appBackend) workRows(s *data.Store, scope appwire.DataScope, list []data.WorkItem) []appwire.DataRow {
	out := make([]appwire.DataRow, 0, len(list))
	apps := map[string]workApp{}
	defs := b.AppDefinitions()
	for _, wi := range list {
		app, ok := apps[wi.App]
		if !ok && wi.App != "" {
			if def, err := defs.Get(wi.App); err == nil {
				if err := json.Unmarshal(def.App, &app); err != nil {
					app = workApp{Name: def.Meta.Name}
				}
			}
			apps[wi.App] = app
		}
		r := wi.Row()
		name := wi.Assignee
		kind := "person"
		if agent, ok := strings.CutPrefix(name, data.AgentAssigneePrefix); ok {
			kind, name = "agent", agent
		} else if name == s.Person() {
			name = "me"
		}
		appName := app.Name
		if appName == "" {
			appName = wi.App
		}
		r["app_name"], r["subject"] = appName, appwire.DataRecordRef{Type: wi.Type, ID: wi.Record}
		r["assignee"] = map[string]string{"kind": kind, "name": name}
		if wi.Due != "" {
			r["due_at"] = wi.Due
		}
		r["form_ref"] = wi.Form
		r["form"] = workFormRow(s, dataScope(scope), app.Process.Forms[wi.Form])
		out = append(out, r)
	}
	return out
}

func workFormRow(s *data.Store, sc data.Scope, form workForm) appwire.DataRow {
	steps := []appwire.DataRow{}
	if typ, err := s.Type(sc, form.Record); err == nil {
		for _, step := range form.Steps {
			fields := []appwire.DataRow{}
			for _, use := range step.Fields {
				def := typ.Fields[use.Field]
				if def == nil || def.Hidden || def.Masked {
					continue
				}
				var field appwire.DataRow
				raw, _ := json.Marshal(def.FieldDef)
				if json.Unmarshal(raw, &field) != nil || field == nil {
					continue
				}
				field["id"] = use.Field
				if len(use.When) > 0 {
					field["when"] = use.When
				}
				if use.Required != nil {
					field["required"] = *use.Required
				}
				fields = append(fields, field)
			}
			entry := appwire.DataRow{"id": step.ID, "label": step.Label, "fields": fields}
			if len(step.When) > 0 {
				entry["when"] = step.When
			}
			steps = append(steps, entry)
		}
	}
	return appwire.DataRow{"steps": steps}
}
