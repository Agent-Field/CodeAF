package main

// ── codeaf app: THE PROCESS METHODS (fluid apps) ────────────────────────────
//
// Transitions, ledger reversals, work items, timers, sign-offs, comments and
// a record's activity, translated from the wire into internal/apps/data,
// which holds every rule. As in appdata.go, nothing here decides anything:
// it names the scope a call acts in and turns refusals into the wire's codes.

import (
	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/appwire"
)

func (b *appBackend) DataTransition(p appwire.DataTransitionParams) (*appwire.DataWriteResult, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	out, err := wireWrite(s.Transition(dataScope(p.DataScope), p.Type, p.ID, p.Transition,
		data.TransitionOptions{Rev: p.Rev, Reason: p.Reason, Unfreeze: p.Unfreeze}))
	if err == nil {
		out.Row = out.Rows[0]
	}
	return out, err
}

func (b *appBackend) DataTransitions(p appwire.DataRecordParams) (*appwire.DataTransitionsResult, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	list, err := s.Transitions(dataScope(p.DataScope), p.Type, p.ID)
	if err != nil {
		return nil, appDataError(err)
	}
	out := &appwire.DataTransitionsResult{Transitions: []appwire.DataTransitionState{}}
	for _, tr := range list {
		out.Transitions = append(out.Transitions, appwire.DataTransitionState{ID: tr.ID, Label: tr.Label, To: tr.To, Ready: tr.Ready, Allowed: tr.Allowed})
	}
	return out, nil
}

func (b *appBackend) DataReverse(p appwire.DataReverseParams) (*appwire.DataWriteResult, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	return wireWrite(s.Reverse(dataScope(p.DataScope), p.Type, p.ID, p.Reason))
}

// item is one table row as the wire carries it.
func item[T interface{ Row() map[string]any }](v T, err error) (*appwire.DataItemResult, error) {
	if err != nil {
		return nil, appDataError(err)
	}
	return &appwire.DataItemResult{Item: appwire.DataRow(v.Row())}, nil
}

// items is a list of table rows as the wire carries them.
func items[T any](list []T, err error, row func(*T) map[string]any) (*appwire.DataItemsResult, error) {
	if err != nil {
		return nil, appDataError(err)
	}
	out := &appwire.DataItemsResult{Items: make([]appwire.DataRow, 0, len(list))}
	for i := range list {
		out.Items = append(out.Items, appwire.DataRow(row(&list[i])))
	}
	return out, nil
}

func (b *appBackend) DataWorkCreate(p appwire.DataWorkCreateParams) (*appwire.DataItemResult, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	return item(s.CreateWorkItem(dataScope(p.DataScope), data.WorkItem{
		Title: p.Title, Type: p.Type, Record: p.Record, Assignee: p.Assignee, Due: p.Due, SLAAt: p.SLAAt, Form: p.Form,
	}))
}

func (b *appBackend) DataWorkUpdate(p appwire.DataWorkUpdateParams) (*appwire.DataItemResult, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	return item(s.UpdateWorkItem(dataScope(p.DataScope), p.ID, data.WorkPatch{
		Title: p.Title, Assignee: p.Assignee, Due: p.Due, SLAAt: p.SLAAt, Form: p.Form, Status: p.Status, Outcome: p.Outcome,
	}))
}

func (b *appBackend) DataWorkList(p appwire.DataWorkListParams) (*appwire.DataItemsResult, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	list, err := s.WorkItems(dataScope(p.DataScope), data.WorkFilter{Assignee: p.Assignee, Status: p.Status, Type: p.Type, Record: p.Record})
	return items(list, err, (*data.WorkItem).Row)
}

func (b *appBackend) DataTimerCreate(p appwire.DataTimerCreateParams) (*appwire.DataItemResult, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	return item(s.CreateTimer(dataScope(p.DataScope), data.Timer{
		Type: p.Type, Record: p.Record, FireAt: p.FireAt, Kind: p.Kind, Payload: p.Payload, Every: p.Every,
	}))
}

func (b *appBackend) DataTimerCancel(p appwire.DataIDParams) (*appwire.Empty, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	if err := s.CancelTimer(dataScope(p.DataScope), p.ID); err != nil {
		return nil, appDataError(err)
	}
	return &appwire.Empty{}, nil
}

func (b *appBackend) DataTimerList(p appwire.DataTimerListParams) (*appwire.DataItemsResult, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	list, err := s.Timers(dataScope(p.DataScope), p.Record, p.Pending)
	return items(list, err, (*data.Timer).Row)
}

func (b *appBackend) DataSign(p appwire.DataSignParams) (*appwire.DataItemResult, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	return item(s.Sign(dataScope(p.DataScope), p.Type, p.ID, p.Statement, p.Rev))
}

func (b *appBackend) DataSignoffs(p appwire.DataRecordParams) (*appwire.DataItemsResult, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	list, err := s.Signoffs(dataScope(p.DataScope), p.Type, p.ID)
	return items(list, err, (*data.Signoff).Row)
}

func (b *appBackend) DataCommentAdd(p appwire.DataCommentParams) (*appwire.DataItemResult, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	return item(s.AddComment(dataScope(p.DataScope), data.Comment{
		Type: p.Type, Record: p.Record, Node: p.Node, Body: p.Body, Mentions: p.Mentions, App: p.App,
	}))
}

func (b *appBackend) DataCommentList(p appwire.DataCommentListParams) (*appwire.DataItemsResult, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	list, err := s.Comments(dataScope(p.DataScope), p.Type, p.Record, p.App, p.Node)
	return items(list, err, (*data.Comment).Row)
}

func (b *appBackend) DataActivity(p appwire.DataRecordParams) (*appwire.DataItemsResult, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	list, err := s.RecordActivity(dataScope(p.DataScope), p.Type, p.ID)
	return items(list, err, (*data.Activity).Row)
}
