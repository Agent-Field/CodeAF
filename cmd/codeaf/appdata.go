package main

// ── codeaf app: THE PERSON'S DATA (fluid apps) ──────────────────────────────
//
// The data.* methods are this file's translation of the wire into
// internal/apps/data, which holds every rule: capabilities, validation,
// freeze, history, undo, live queries. Nothing here decides anything about a
// record; it opens the place, names the scope a call acts in, and turns the
// store's refusals into the wire's codes.
//
// THE STORE KNOWS AN APP ONLY ONCE IT IS REGISTERED. The app definitions live
// in <home>/apps/<id>/current/app.json (the app store keeps them); the first call
// that opens the data registers every one found there, so a restart, or an
// app kept while this process was not running, is never an app whose screens
// are all refused. The app store and the Builder register a version again the
// moment it goes live, through [appBackend.appData].

import (
	"path/filepath"
	"sync"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/appwire"
)

// appDataState is the backend's data place and the live queries this
// connection holds.
type appDataState struct {
	mu     sync.Mutex
	place  *data.Place
	closed bool
	// sync registers the kept apps once; a second first call waits for it,
	// so no call ever sees the store before its apps are registered.
	sync    sync.Once
	syncErr error
	// subs is each subscription's store, so it is stopped where it lives.
	subs map[string]*data.Store
	// home replaces the codeaf home in a test.
	home string
}

// appData is the person's data place, opened once.
func (b *appBackend) appData() (*data.Place, error) {
	d := &b.data
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, appwire.Errorf(appwire.CodeClosed, "codeaf is shutting down")
	}
	if d.place == nil {
		d.place = data.NewPlace(b.dataRoot(), data.Options{})
		if emit := b.emit; emit != nil {
			d.place.OnCommit(func(change data.Change) {
				for _, item := range change.Items {
					if item.Table == data.TableWorkItems {
						emit.Emit(appwire.EventWorkItemsChanged, appwire.WorkItemsChanged{ID: item.ID})
					}
				}
			})
		}
	}
	return d.place, nil
}

// dataStore is the store a call names, with every kept app registered on
// the person's own the first time it opens.
func (b *appBackend) dataStore(draft string) (*data.Store, error) {
	place, err := b.appData()
	if err != nil {
		return nil, err
	}
	s, err := place.Store(draft)
	if err != nil {
		return nil, appDataError(err)
	}
	if draft == "" {
		b.data.sync.Do(func() { b.data.syncErr = b.AppDefinitions().SyncData(s) })
		if b.data.syncErr != nil {
			return nil, appDataError(b.data.syncErr)
		}
	}
	return s, nil
}

// closeData stops this connection's live queries and closes the data files.
func (b *appBackend) closeData() {
	if place := b.data.takePlace(); place != nil {
		_ = place.Close()
	}
}

// takePlace marks the state closed and hands back the place to close, which
// is closed outside the lock: closing waits on hooks that may take it.
func (d *appDataState) takePlace() *data.Place {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	place := d.place
	d.place = nil
	d.subs = nil
	return place
}

// dataScope is who a wire call acts as: its app, or the person.
func dataScope(p appwire.DataScope) data.Scope { return data.Scope{App: p.App} }

// appDataError turns a store refusal into the wire's code, with the detail a
// client acts on: the capability a not_allowed would take, the field a
// refused value is about.
func appDataError(err error) error {
	if err == nil {
		return nil
	}
	e, ok := data.AsError(err)
	if !ok {
		return appEngineError(err)
	}
	out := &appwire.Error{Code: e.Code, Message: e.Message}
	switch {
	case e.Need != "":
		out.Detail = map[string]string{"need": e.Need}
	case e.Field != "":
		out.Detail = map[string]string{"field": e.Field}
	}
	return out
}

func wireRows(rows []data.Row) []appwire.DataRow {
	out := make([]appwire.DataRow, len(rows))
	for i, r := range rows {
		out[i] = appwire.DataRow(r)
	}
	return out
}

func wireWrite(res *data.WriteResult, err error) (*appwire.DataWriteResult, error) {
	if err != nil {
		return nil, appDataError(err)
	}
	out := &appwire.DataWriteResult{Batch: res.Batch, Rows: wireRows(res.Rows), Removes: res.Removes}
	for _, h := range res.Held {
		out.Held = append(out.Held, appwire.DataHeld{ID: h.ID, Field: h.Field})
	}
	return out, nil
}

func (b *appBackend) DataTypes(p appwire.DataScope) (*appwire.DataTypesResult, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	types, err := s.Types(dataScope(p))
	if err != nil {
		return nil, appDataError(err)
	}
	out := &appwire.DataTypesResult{Types: []appwire.DataType{}}
	for _, t := range types {
		wt := appwire.DataType{
			ID: t.ID, Name: t.Name, Title: t.Title, Label: t.Label, CreatedByApp: t.CreatedByApp, DefinedBy: t.DefinedBy,
			UsedBy: nonNilStrings(t.UsedBy), Count: t.Count, Hidden: t.Hidden, Ledger: t.Ledger, Fields: []appwire.DataField{},
		}
		if lc := t.Lifecycle; lc != nil {
			wl := &appwire.DataLifecycle{Field: lc.Field, Initial: lc.Initial, Transitions: []appwire.DataTransition{}}
			for _, f := range t.Fields {
				if f.Name == lc.Field && wl.Initial == "" && len(f.Values) > 0 {
					wl.Initial = f.Values[0]
				}
			}
			for _, tr := range lc.Transitions {
				by := tr.By
				if len(by) == 0 {
					by = []string{data.ByPerson, data.ByFlow}
				}
				wl.Transitions = append(wl.Transitions, appwire.DataTransition{
					ID: tr.ID, Label: tr.Label, From: tr.From, To: tr.To, Guard: tr.Guard, By: by, Effects: tr.Effects,
				})
			}
			wt.Lifecycle = wl
		}
		for _, f := range t.Fields {
			wf := appwire.DataField{
				ID: f.ID, Name: f.Name, Type: f.Type, App: f.App, Hidden: f.Hidden, Label: f.Label,
				Required: f.Required, Unique: f.Unique, Masked: f.Masked, Values: f.Values, To: f.To,
				Currency: f.Currency, Computed: f.Computed != "", AI: f.AI != "",
			}
			if sq := f.Sequence; sq != nil {
				wf.Sequence = &appwire.DataSequence{Prefix: sq.Prefix, Pad: sq.Pad}
			}
			wt.Fields = append(wt.Fields, wf)
		}
		out.Types = append(out.Types, wt)
	}
	return out, nil
}

func nonNilStrings(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

func (b *appBackend) DataQuery(p appwire.DataQueryParams) (*appwire.DataQueryResult, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	ir, err := queryIR(p.Type, p.Query, p.Where)
	if err != nil {
		return nil, err
	}
	if p.Sort != "" {
		ir.Sort = p.Sort
	}
	ir.Select = p.Select
	page, err := s.QueryRows(dataScope(p.DataScope), ir, p.Cursor, p.Limit)
	if err != nil {
		return nil, appDataError(err)
	}
	return &appwire.DataQueryResult{Rows: wireRows(page.Rows), Next: page.Next, Total: page.Total}, nil
}

// queryIR is a wire query in the store's one intermediate form: the typed
// tokens parsed, then the structured conditions added.
func queryIR(typ, query string, where []appwire.DataCond) (*data.QueryIR, error) {
	ir, err := data.ParseQuery(query)
	if err != nil {
		return nil, appDataError(err)
	}
	ir.Type = typ
	for _, c := range where {
		ir.Where = append(ir.Where, data.Cond{Field: c.Field, Op: c.Op, Value: c.Value, Neg: c.Neg})
	}
	return ir, nil
}

func (b *appBackend) DataAggregate(p appwire.DataAggregateParams) (*appwire.DataAggregateResult, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	ir, err := queryIR(p.Type, p.Query, p.Where)
	if err != nil {
		return nil, err
	}
	ir.Group = p.Group
	for _, a := range p.Aggregate {
		ir.Aggregate = append(ir.Aggregate, data.Agg{Fn: a.Fn, Field: a.Field, As: a.As})
	}
	res, err := s.Aggregate(dataScope(p.DataScope), ir)
	if err != nil {
		return nil, appDataError(err)
	}
	out := &appwire.DataAggregateResult{Group: nonNilStrings(res.Group), Groups: []appwire.DataAggregate{}, Total: res.Total}
	for _, g := range res.Groups {
		out.Groups = append(out.Groups, appwire.DataAggregate{Key: g.Key, Values: g.Values})
	}
	return out, nil
}

func (b *appBackend) DataCount(p appwire.DataCountParams) (*appwire.DataCountResult, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	groups, total, err := s.Count(dataScope(p.DataScope), p.Type, p.Query, p.Group)
	if err != nil {
		return nil, appDataError(err)
	}
	out := &appwire.DataCountResult{Groups: []appwire.DataGroup{}, Total: total}
	for _, g := range groups {
		out.Groups = append(out.Groups, appwire.DataGroup{Key: g.Key, Count: g.Count})
	}
	return out, nil
}

func (b *appBackend) DataGet(p appwire.DataRecordParams) (*appwire.DataRowResult, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	row, err := s.Get(dataScope(p.DataScope), p.Type, p.ID, p.Reveal)
	if err != nil {
		return nil, appDataError(err)
	}
	return &appwire.DataRowResult{Row: appwire.DataRow(row)}, nil
}

func (b *appBackend) DataHistory(p appwire.DataRecordParams) (*appwire.DataHistoryResult, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	entries, err := s.History(dataScope(p.DataScope), p.Type, p.ID)
	if err != nil {
		return nil, appDataError(err)
	}
	out := &appwire.DataHistoryResult{History: []appwire.DataHistoryEntry{}}
	for _, e := range entries {
		out.History = append(out.History, appwire.DataHistoryEntry{
			Seq: e.Seq, Batch: e.Batch, Op: e.Op, App: e.App, Actor: e.Actor, ByKind: e.ByKind, Cause: e.Cause, Note: e.Note, At: e.At,
			Undone: e.Undone, Before: e.Before, After: e.After, Changed: nonNilStrings(e.Changed),
		})
	}
	return out, nil
}

func (b *appBackend) DataCreate(p appwire.DataCreateParams) (*appwire.DataWriteResult, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	sets := p.Rows
	if p.Set != nil {
		sets = []map[string]any{p.Set}
	}
	return wireWrite(s.Create(dataScope(p.DataScope), p.Type, sets))
}

func (b *appBackend) DataUpdate(p appwire.DataUpdateParams) (*appwire.DataWriteResult, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	return wireWrite(s.Update(dataScope(p.DataScope), p.Type, p.IDs, p.Set, data.UpdateOptions{Unfreeze: p.Unfreeze, Rev: p.Rev, Reset: p.Reset}))
}

func (b *appBackend) DataDelete(p appwire.DataDeleteParams) (*appwire.DataWriteResult, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	return wireWrite(s.Delete(dataScope(p.DataScope), p.Type, p.IDs, data.UpdateOptions{Unfreeze: p.Unfreeze, Rev: p.Rev}))
}

func (b *appBackend) DataUndo(p appwire.DataUndoParams) (*appwire.DataWriteResult, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	return wireWrite(s.Undo(dataScope(p.DataScope), p.Batch))
}

func (b *appBackend) DataImportCSV(p appwire.DataImportParams) (*appwire.DataImportResult, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(p.Path) {
		return nil, appwire.Errorf(appwire.CodeBadParams, "path must be absolute")
	}
	res, err := s.ImportCSV(dataScope(p.DataScope), p.Type, data.ImportOptions{Path: p.Path, Mapping: p.Mapping, DryRun: p.DryRun})
	if err != nil {
		return nil, appDataError(err)
	}
	issues := make([]appwire.DataImportIssue, 0, len(res.Problems))
	for _, pr := range res.Problems {
		issues = append(issues, appwire.DataImportIssue{Line: pr.Line, Field: pr.Field, Message: pr.Message})
	}
	if p.DryRun {
		return &appwire.DataImportResult{Preview: &appwire.DataImportPreview{
			Columns: res.Columns, Mapping: res.Mapping, Rows: wireRows(res.Rows),
			Valid: res.Created, Failed: res.Failed, Problems: issues,
		}}, nil
	}
	created := res.Created
	return &appwire.DataImportResult{Created: &created, Batch: res.Batch, Failed: res.Failed, Problems: issues}, nil
}

func (b *appBackend) DataSubscribe(p appwire.DataSubscribeParams) (*appwire.DataSubscribeResult, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	// A sub id moving between stores (main to a draft) stops where it was.
	_ = b.DataUnsubscribe(p.Sub)
	spec := data.SubSpec{ID: p.Sub, Type: p.Type, Query: p.Query, Record: p.ID, Table: p.Table}
	if p.Cursor != nil {
		spec.Resume, spec.After = true, *p.Cursor
	}
	emit := b.emit
	// The live query is held before it can be told anything, so an early
	// diff is never sent for a sub this connection has already let go of.
	b.data.holdSub(p.Sub, s)
	cursor, err := s.Subscribe(dataScope(p.DataScope), spec, func(d data.Diff) {
		if d.Reset {
			emit.Emit(appwire.EventDataReset, appwire.DataReset{Sub: d.Sub, Cursor: d.Cursor})
			return
		}
		emit.Emit(appwire.EventDataChanged, appwire.DataChanged{
			Sub: d.Sub, Upserts: wireRows(d.Upserts), Removes: nonNilStrings(d.Removes), Total: d.Total, Cursor: d.Cursor,
		})
	})
	if err != nil {
		_ = b.data.takeSub(p.Sub)
		return nil, appDataError(err)
	}
	return &appwire.DataSubscribeResult{Cursor: cursor}, nil
}

// holdSub keeps the store a live query lives on, to stop it there.
func (d *appDataState) holdSub(sub string, s *data.Store) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.subs == nil {
		d.subs = map[string]*data.Store{}
	}
	d.subs[sub] = s
}

// takeSub forgets one live query and hands back the store it was held on.
func (d *appDataState) takeSub(sub string) *data.Store {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.subs[sub]
	delete(d.subs, sub)
	return s
}

func (b *appBackend) DataUnsubscribe(sub string) error {
	if s := b.data.takeSub(sub); s != nil {
		s.Unsubscribe(sub)
	}
	return nil
}
