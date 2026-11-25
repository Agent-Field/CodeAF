package main

// ── codeaf app: THE PERSON'S DATA (fluid apps, SPEC §6, lane B) ─────────────
//
// The data.* methods are this file's translation of the wire into
// internal/apps/data, which holds every rule: capabilities, validation,
// freeze, history, undo, live queries. Nothing here decides anything about a
// record; it opens the place, names the scope a call acts in, and turns the
// store's refusals into the wire's codes.
//
// THE STORE KNOWS AN APP ONLY ONCE IT IS REGISTERED. The app definitions live
// in <home>/apps/<id>/current/app.json (lane C keeps them); the first call
// that opens the data registers every one found there, so a restart, or an
// app kept while this process was not running, is never an app whose screens
// are all refused. Lane C and the Builder register a version again the
// moment it goes live, through [appBackend.appData].

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/home"
)

// appDataState is the backend's data place and the live queries this
// connection holds.
type appDataState struct {
	mu     sync.Mutex
	place  *data.Place
	closed bool
	// sync registers the kept apps once; a second first call waits for it,
	// so no call ever sees the store before its apps are registered.
	sync sync.Once
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
		root := d.home
		if root == "" {
			root = home.Dir()
		}
		d.place = data.NewPlace(root, data.Options{})
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
		b.data.sync.Do(func() { b.syncAppDefinitions(s) })
	}
	return s, nil
}

// syncAppDefinitions registers every kept app's definition once per process.
// An app that uses another's type can only register after it, and the
// folders do not say which comes first, so the apps are tried in passes until
// a pass registers none. One app whose file never registers (a hand edit, a
// newer format) is left out and the rest go on: its screens are refused,
// nobody else's are.
func (b *appBackend) syncAppDefinitions(s *data.Store) {
	root := b.data.home
	if root == "" {
		root = home.Dir()
	}
	matches, _ := filepath.Glob(filepath.Join(root, "apps", "*", "current", "app.json"))
	var pending [][]byte
	for _, path := range matches {
		if filepath.Base(filepath.Dir(filepath.Dir(path)))[0] == '_' {
			continue
		}
		if raw, err := os.ReadFile(path); err == nil {
			pending = append(pending, raw)
		}
	}
	for len(pending) > 0 {
		var left [][]byte
		for _, raw := range pending {
			if err := s.RegisterApp(raw); err != nil {
				left = append(left, raw)
			}
		}
		if len(left) == len(pending) {
			return
		}
		pending = left
	}
}

// closeData stops this connection's live queries and closes the data files.
func (b *appBackend) closeData() {
	d := &b.data
	d.mu.Lock()
	d.closed = true
	place := d.place
	d.place = nil
	d.subs = nil
	d.mu.Unlock()
	if place != nil {
		_ = place.Close()
	}
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
	return &appwire.DataWriteResult{Batch: res.Batch, Rows: wireRows(res.Rows), Removes: res.Removes}, nil
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
		wt := appwire.DataType{Name: t.Name, Title: t.Title, Label: t.Label, OwnerApp: t.OwnerApp, UsedBy: nonNilStrings(t.UsedBy), Count: t.Count, Hidden: t.Hidden, Fields: []appwire.DataField{}}
		for _, f := range t.Fields {
			wt.Fields = append(wt.Fields, appwire.DataField{
				Name: f.Name, Type: f.Type, App: f.App, Hidden: f.Hidden, Label: f.Label,
				Required: f.Required, Unique: f.Unique, Masked: f.Masked, Values: f.Values, To: f.To,
				Currency: f.Currency, Computed: f.Computed != "", AI: f.AI != "",
			})
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
	page, err := s.Query(dataScope(p.DataScope), p.Type, p.Query, p.Cursor, p.Limit)
	if err != nil {
		return nil, appDataError(err)
	}
	return &appwire.DataQueryResult{Rows: wireRows(page.Rows), Next: page.Next, Total: page.Total}, nil
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
			Seq: e.Seq, Batch: e.Batch, Op: e.Op, App: e.App, Actor: e.Actor, Note: e.Note, At: e.At,
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
	return wireWrite(s.Update(dataScope(p.DataScope), p.Type, p.IDs, p.Set, data.UpdateOptions{Unfreeze: p.Unfreeze}))
}

func (b *appBackend) DataDelete(p appwire.DataDeleteParams) (*appwire.DataWriteResult, error) {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	return wireWrite(s.Delete(dataScope(p.DataScope), p.Type, p.IDs, data.UpdateOptions{Unfreeze: p.Unfreeze}))
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

func (b *appBackend) DataSubscribe(p appwire.DataSubscribeParams) error {
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return err
	}
	// A sub id moving between stores (main to a draft) stops where it was.
	_ = b.DataUnsubscribe(p.Sub)
	spec := data.SubSpec{ID: p.Sub, Type: p.Type, Query: p.Query, Record: p.ID}
	emit := b.emit
	err = s.Subscribe(dataScope(p.DataScope), spec, func(d data.Diff) {
		emit.Emit(appwire.EventDataChanged, appwire.DataChanged{
			Sub: d.Sub, Upserts: wireRows(d.Upserts), Removes: nonNilStrings(d.Removes), Total: d.Total,
		})
	})
	if err != nil {
		return appDataError(err)
	}
	b.data.mu.Lock()
	defer b.data.mu.Unlock()
	if b.data.subs == nil {
		b.data.subs = map[string]*data.Store{}
	}
	b.data.subs[p.Sub] = s
	return nil
}

func (b *appBackend) DataUnsubscribe(sub string) error {
	b.data.mu.Lock()
	s := b.data.subs[sub]
	delete(b.data.subs, sub)
	b.data.mu.Unlock()
	if s != nil {
		s.Unsubscribe(sub)
	}
	return nil
}
