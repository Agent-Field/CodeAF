package main

// ── codeaf app: THE PERSON'S DATA (fluid apps) ──────────────────────────────
//
// The data.* methods are this file's translation of the wire into
// internal/apps/data, which holds every rule: capabilities, validation,
// freeze, history, undo, live queries. Nothing here decides anything about a
// record; it opens the place, names the scope a call acts in, and turns the
// store's refusals into the wire's codes.
//
// The writer owns published app grants. Startup adopts only legacy app
// definitions that have no publication pointer in the data file.

import (
	"context"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/apps/connections"
	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/guard"
)

// appDataState is the backend's data place and the live queries this
// connection holds.
type appDataState struct {
	mu     sync.Mutex
	place  *data.Place
	closed bool
	// syncMu serializes adoption of legacy app definitions.
	syncMu sync.Mutex
	synced bool
	// The backend belongs to one authenticated wire connection.
	owner  data.SubscriptionOwner
	subsMu sync.Mutex
	subs   map[string]appDataSubscription
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
		d.place = data.NewPlace(b.dataRoot(), data.Options{RequireAgentPrincipal: true})
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

// dataStore opens the call's store and adopts legacy publications once.
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
		b.data.syncMu.Lock()
		defer b.data.syncMu.Unlock()
		if !b.data.synced {
			if err := b.AppDefinitions().SyncData(s); err != nil {
				return nil, appDataError(err)
			}
			b.data.synced = true
		}
	}
	return s, nil
}

// closeData stops this connection's live queries and closes the data files.
func (b *appBackend) closeData() {
	place := b.data.takePlace()
	b.stopDataSubscriptions()
	if place != nil {
		_ = place.Close()
	}
}

func (b *appBackend) stopDataSubscriptions() {
	b.data.subsMu.Lock()
	defer b.data.subsMu.Unlock()
	for id := range b.data.subs {
		b.stopDataSubscription(id)
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
			ID: t.ID, Name: t.Name, Common: string(t.Common), Title: t.Title, Label: t.Label, CreatedByApp: t.CreatedByApp, DefinedBy: t.DefinedBy,
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
	page, source, err := b.appQueryRows(s, p.DataScope, dataScope(p.DataScope), ir, p.Cursor, p.Limit)
	if err != nil {
		return nil, err
	}
	return &appwire.DataQueryResult{Rows: wireRows(page.Rows), Next: page.Next, Total: page.Total, Source: &source}, nil
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
	planned, source, _, err := b.planAppQuery(p.DataScope, ir)
	if err != nil {
		return nil, err
	}
	res, err := s.Aggregate(dataScope(p.DataScope), planned)
	if err != nil {
		return nil, appDataError(err)
	}
	out := &appwire.DataAggregateResult{Group: nonNilStrings(res.Group), Groups: []appwire.DataAggregate{}, Total: res.Total, Source: &source}
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
	ir, err := queryIR(p.Type, p.Query, nil)
	if err != nil {
		return nil, err
	}
	planned, source, _, err := b.planAppQuery(p.DataScope, ir)
	if err != nil {
		return nil, err
	}
	groups, total, err := s.CountRows(dataScope(p.DataScope), planned, p.Group)
	if err != nil {
		return nil, appDataError(err)
	}
	out := &appwire.DataCountResult{Groups: []appwire.DataGroup{}, Total: total, Source: &source}
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
	source := connections.NativeSource()
	var row data.Row
	if p.App != "" && p.Draft == "" {
		row, source, err = b.appConnections().GetRecord(context.Background(), p.App, p.Type, p.ID, p.Reveal)
	} else {
		row, err = s.Get(dataScope(p.DataScope), p.Type, p.ID, p.Reveal)
	}
	if err != nil {
		return nil, appConnectionError(err)
	}
	return &appwire.DataRowResult{Row: appwire.DataRow(row), Source: &source}, nil
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
	agentLabels := map[string]string{}
	for _, e := range entries {
		actor := e.Actor
		if e.ByKind == data.ByAI && e.App != "" && actor == data.AgentAssigneePrefix+e.App {
			label, loaded := agentLabels[e.App]
			if !loaded {
				label = actor
				if def, err := b.AppDefinitions().Get(e.App); err == nil {
					label = def.Meta.Name + " agent"
				}
				agentLabels[e.App] = label
			}
			actor = label
		}
		out.History = append(out.History, appwire.DataHistoryEntry{
			Seq: e.Seq, Batch: e.Batch, Op: e.Op, App: e.App, Actor: actor, ByKind: e.ByKind, Cause: e.Cause, Note: e.Note, At: e.At,
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

// appDataSubscription orders early events after the subscription reply and
// prevents a replaced subscription from emitting on its former connection.
type appDataSubscription struct {
	store *data.Store
	gate  *appDataDelivery
}

// appDataDelivery coalesces a slow connection's pending events without
// holding the store's policy lock while the wire waits for buffer space.
type appDataDelivery struct {
	ctx      context.Context
	stop     context.CancelFunc
	wake     chan struct{}
	mu       sync.Mutex
	deferred bool
	pending  *data.Diff
	sending  context.CancelFunc
}

func newAppDataDelivery(deferred bool) *appDataDelivery {
	ctx, cancel := context.WithCancel(context.Background())
	return &appDataDelivery{ctx: ctx, stop: cancel, wake: make(chan struct{}, 1), deferred: deferred}
}

func (d *appDataDelivery) queue(diff data.Diff) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.ctx.Err() != nil {
		return
	}
	// Pending deltas are only useful in order. An early or superseded
	// delta becomes a payload-free reset at the latest observed cursor.
	if d.deferred || d.pending != nil {
		diff = data.Diff{Sub: diff.Sub, Cursor: diff.Cursor, Reset: true}
	}
	if diff.Reset && d.sending != nil {
		d.sending()
	}
	d.pending = &diff
	if !d.deferred {
		d.signal()
	}
}

func (d *appDataDelivery) release() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deferred = false
	if d.pending != nil {
		d.signal()
	}
}

func (d *appDataDelivery) signal() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

func (d *appDataDelivery) run(emit appwire.Emitter) {
	for {
		select {
		case <-d.ctx.Done():
			return
		case <-d.wake:
			diff, ctx, cancel := d.take()
			if diff == nil {
				continue
			}

			event, body := appDataEvent(*diff)
			if sink, ok := emit.(interface {
				EmitContext(context.Context, string, any)
			}); ok {
				sink.EmitContext(ctx, event, body)
			} else if emit != nil && ctx.Err() == nil {
				emit.Emit(event, body)
			}
			cancel()
			d.finishSend()
		}
	}
}

func (d *appDataDelivery) take() (*data.Diff, context.Context, context.CancelFunc) {
	d.mu.Lock()
	defer d.mu.Unlock()
	diff := d.pending
	d.pending = nil
	if diff == nil || d.ctx.Err() != nil {
		return nil, nil, nil
	}
	ctx, cancel := context.WithCancel(d.ctx)
	d.sending = cancel
	return diff, ctx, cancel
}

func (d *appDataDelivery) finishSend() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sending = nil
}

func appDataEvent(d data.Diff) (string, any) {
	if d.Reset {
		return appwire.EventDataReset, appwire.DataReset{Sub: d.Sub, Cursor: d.Cursor}
	}
	return appwire.EventDataChanged, appwire.DataChanged{
		Sub: d.Sub, Upserts: wireRows(d.Upserts), Removes: nonNilStrings(d.Removes), Total: d.Total, Cursor: d.Cursor,
	}
}

func (b *appBackend) DataSubscribe(p appwire.DataSubscribeParams) (*appwire.DataSubscribeResult, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	p.Sub = strings.TrimSpace(p.Sub)
	spec := data.SubSpec{ID: p.Sub, Type: p.Type, Query: p.Query, Record: p.ID, Table: p.Table, InvalidateOnly: p.InvalidateOnly}
	if p.Draft == "" && p.App != "" && p.Type != "" && p.Table == "" {
		state, err := b.appConnections().Residency(p.App, p.Type)
		if err != nil {
			return nil, appConnectionError(err)
		}
		if state.Connection != "" {
			spec.InvalidateOnly = true
			spec.Bindings = []string{state.Binding}
		}
	}
	if p.Cursor != nil {
		spec.Resume, spec.After = true, *p.Cursor
	}
	emit := b.emit
	if tap, ok := emit.(*appInboxTap); ok {
		// Subscription events travel to the wire without Inbox processing.
		emit = tap.next
	}
	b.data.subsMu.Lock()
	defer b.data.subsMu.Unlock()
	b.stopDataSubscription(p.Sub)
	gate := newAppDataDelivery(p.DeferEvents)
	snapshot, err := s.Watch(&b.data.owner, dataScope(p.DataScope), spec, gate.queue)
	if err != nil {
		gate.stop()
		return nil, appDataError(err)
	}
	guard.Go("app/data-subscription", func() { gate.run(emit) })
	if b.data.subs == nil {
		b.data.subs = map[string]appDataSubscription{}
	}
	b.data.subs[p.Sub] = appDataSubscription{store: s, gate: gate}
	result := &appwire.DataSubscribeResult{Cursor: snapshot.Cursor}
	if snapshot.Rows != nil {
		source := connections.NativeSource()
		result.Snapshot = &appwire.DataQueryResult{Source: &source, Rows: wireRows(snapshot.Rows), Next: snapshot.Next, Total: snapshot.Total}
	}
	if p.DeferEvents {
		result.ReleaseEvents = gate.release
	}
	return result, nil
}

// stopDataSubscription must run under the connection's subscription lock.
func (b *appBackend) stopDataSubscription(id string) {
	if sub, ok := b.data.subs[id]; ok {
		sub.gate.stop()
		sub.store.Unwatch(&b.data.owner, id)
		delete(b.data.subs, id)
	}
}

func (b *appBackend) DataUnsubscribe(sub string) error {
	b.data.subsMu.Lock()
	defer b.data.subsMu.Unlock()
	b.stopDataSubscription(strings.TrimSpace(sub))
	return nil
}
