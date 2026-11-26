package data

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"

	appschema "github.com/Agent-Field/codeaf/internal/apps/schema"
)

// Place is every data file one codeaf home holds: the person's own
// (<home>/data/data.db) and each draft's sample (<home>/apps/_drafts/<draft>/
// sample.db). Each is opened by the first call that needs it and stays open
// until Close; a draft's is closed by [Place.CloseDraft] before its folder is
// removed.
type Place struct {
	home string
	opts Options

	// seedMu makes one SeedSample at a time, so two never interleave closing,
	// removing and reopening one draft's file.
	seedMu sync.Mutex

	mu sync.Mutex
	// seeding is the drafts a SeedSample is remaking; Store refuses them
	// until it is done, so nothing opens a file about to be removed.
	seeding map[string]bool
	main    *Store
	drafts  map[string]*Store
	closed  bool

	hooksMu  sync.RWMutex
	hooks    map[int]func(Change)
	nextHook int
}

// draftShape is a draft id: it names a folder, so nothing that could leave
// the drafts folder is one.
var draftShape = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// NewPlace is the data of the codeaf home at home (the engine's own resolved
// home, never one computed here). Nothing is opened until it is asked for.
func NewPlace(home string, opts Options) *Place {
	return &Place{home: home, opts: opts, drafts: map[string]*Store{}, hooks: map[int]func(Change){}}
}

// MainPath is where the person's data file is.
func (p *Place) MainPath() string { return filepath.Join(p.home, "data", "data.db") }

// DraftDir is a draft's working folder.
func (p *Place) DraftDir(draft string) string { return filepath.Join(p.home, "apps", "_drafts", draft) }

// Main is the person's own store.
func (p *Place) Main() (*Store, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, errorf(CodeClosed, "the data store is closed")
	}
	return p.mainLocked()
}

// mainLocked opens the person's own store if it is not open; p.mu is held.
func (p *Place) mainLocked() (*Store, error) {
	if p.main == nil {
		s, err := Open(p.MainPath(), p.opts)
		if err != nil {
			return nil, err
		}
		p.attach(s)
		p.main = s
	}
	return p.main, nil
}

// Store is the store a call names: the person's own for an empty draft, else
// that draft's sample, which must already have been seeded.
func (p *Place) Store(draft string) (*Store, error) {
	if draft == "" {
		return p.Main()
	}
	if !draftShape.MatchString(draft) {
		return nil, errorf(CodeInvalid, "%q is not a draft", draft)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, errorf(CodeClosed, "the data store is closed")
	}
	if s := p.drafts[draft]; s != nil {
		return s, nil
	}
	if p.seeding[draft] {
		return nil, errorf(CodeNotFound, "draft %s's sample data is being made", draft)
	}
	path := filepath.Join(p.DraftDir(draft), "sample.db")
	if _, err := os.Stat(path); err != nil {
		return nil, errorf(CodeNotFound, "draft %s has no sample data", draft)
	}
	return p.openDraft(draft, path)
}

// openDraft opens a draft's store; p.mu is held.
func (p *Place) openDraft(draft, path string) (*Store, error) {
	opts := p.opts
	opts.Draft = draft
	// The sample's "me" is the person: one id in both files, so a draft's
	// person field means the same person once the app is kept.
	main, err := p.mainLocked()
	if err != nil {
		return nil, err
	}
	opts.Person = main.Person()
	s, err := Open(path, opts)
	if err != nil {
		return nil, err
	}
	p.attach(s)
	p.drafts[draft] = s
	return s, nil
}

// SeedSample makes a draft's sample data afresh: the draft's app registered
// on an empty sample file, and rows (type → records) created in it. A type
// the draft uses from the person's own data has its definition (its shape,
// never its records) copied first. A row may carry an "id" (a ULID) so other
// rows can point to it; a ref may also name its record by title.
func (p *Place) SeedSample(draft string, appJSON []byte, rows map[string][]map[string]any) error {
	if !draftShape.MatchString(draft) {
		return errorf(CodeInvalid, "%q is not a draft", draft)
	}
	def, err := ParseApp(appJSON)
	if err != nil {
		return err
	}
	p.seedMu.Lock()
	defer p.seedMu.Unlock()
	// The draft is marked as being made, and its store taken out of the map,
	// under p.mu; the old store is closed outside it (its dispatcher may be
	// running a hook that asks this place for a store), and Store refuses the
	// draft until the new file is open.
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return errorf(CodeClosed, "the data store is closed")
	}
	if p.seeding == nil {
		p.seeding = map[string]bool{}
	}
	p.seeding[draft] = true
	old := p.drafts[draft]
	delete(p.drafts, draft)
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.seeding, draft)
		p.mu.Unlock()
	}()
	if old != nil {
		if err := old.Close(); err != nil {
			return err
		}
	}
	dir := p.DraftDir(draft)
	path := filepath.Join(dir, "sample.db")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(path + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return errorf(CodeClosed, "the data store is closed")
	}
	s, err := p.openDraft(draft, path)
	p.mu.Unlock()
	if err != nil {
		return err
	}

	// The types the draft uses come from the person's own store, shape only.
	if used := usedTypes(def); len(used) > 0 {
		main, err := p.Main()
		if err != nil {
			return err
		}
		sch := main.schema.Load()
		names := make([]string, 0, len(used))
		for _, name := range used {
			if t := sch.common[def.Records[name].Common]; t != nil {
				name = t.Name
			}
			names = append(names, name)
		}
		raw, _ := json.Marshal(shapesOf(sch, names))
		if err := s.RegisterApp(raw); err != nil {
			return err
		}
	}
	if err := s.RegisterApp(appJSON); err != nil {
		return err
	}
	// Sample rows are keyed by the draft's app aliases. The seed writer
	// uses the underlying draft types so cross-type references keep working.
	resolved := map[string][]map[string]any{}
	sch := s.schema.Load()
	for _, name := range sortedKeys(rows) {
		typ := name
		if t := sch.appType(def.ID, name); t != nil {
			typ = t.Name
		}
		resolved[typ] = append(resolved[typ], rows[name]...)
	}
	return s.seed(resolved)
}

// usedTypes is the types an app uses rather than owns.
func usedTypes(def *AppDef) []string {
	var out []string
	for _, name := range def.typeOrder {
		if def.Records[name].Uses {
			out = append(out, name)
		}
	}
	return out
}

// shapeApp is an owner app's declaration of the types a draft uses, as an
// app.json the sample store registers.
type shapeApp struct {
	ID      string             `json:"id"`
	Records map[string]TypeDef `json:"records"`
}

// sharedShapes is the owner a sample file registers the used types under.
// The sample only needs their shape; one app holding them all registers in
// one step whatever the refs between them.
const sharedShapes = "shared"

// shapesOf rebuilds, from a merged schema, one app.json holding the used
// types (and every type those point to) with their visible fields.
func shapesOf(sch *schema, used []string) shapeApp {
	want := map[string]bool{}
	var walk func(string)
	walk = func(name string) {
		t := sch.typeNamed(name)
		if t == nil || want[t.Name] {
			return
		}
		name = t.Name
		want[name] = true
		for _, fname := range t.Order {
			if f := t.field(fname); f != nil && (f.Type == TypeRef || f.Type == TypeRefs) {
				walk(f.To)
			}
		}
	}
	for _, name := range used {
		walk(name)
	}
	app := shapeApp{ID: sharedShapes, Records: map[string]TypeDef{}}
	for _, name := range sortedKeys(want) {
		t := sch.types[name]
		def := TypeDef{Title: t.Title, Label: t.Label, Constants: t.Constants, Fields: map[string]FieldDef{}, Ledger: LedgerOption{On: t.Ledger, Reversal: t.LedgerReversal}}
		if t.Lifecycle != nil {
			lc := t.Lifecycle.LifecycleDef
			def.Lifecycle = &lc
		}
		for _, rule := range t.Freeze {
			def.Freeze = append(def.Freeze, rule.FreezeRule)
		}
		for _, fname := range t.Order {
			if f := t.field(fname); f != nil {
				def.Fields[fname] = f.FieldDef
			}
		}
		if appschema.HasCommonType(string(t.Common)) {
			def.Common = t.Common
		}
		app.Records[name] = def
	}
	return app
}

// seed creates sample rows as the person, the types a row points to first.
func (s *Store) seed(rows map[string][]map[string]any) error {
	sch := s.schema.Load()
	for _, typ := range seedOrder(sch, rows) {
		list := rows[typ]
		if len(list) == 0 {
			continue
		}
		err := s.write(func(w *writeTx) error {
			t, err := w.schema.allow(Scope{}, typ, true)
			if err != nil {
				return err
			}
			if _, err := w.newBatch(Scope{Actor: "sample"}, OpSeed, t); err != nil {
				return err
			}
			for _, set := range list {
				set = copyMap(set)
				id, _ := set["id"].(string)
				delete(set, "id")
				if err := w.resolveTitles(t, set); err != nil {
					return err
				}
				if id != "" && !validID(id) {
					return errorf(CodeInvalid, "sample %s id %q is not a ULID", typ, id)
				}
				if _, err := w.createOneAs(t, Scope{Actor: "sample"}, set, OpSeed, id); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// seedOrder is the sample's types, each after the types it points to.
func seedOrder(sch *schema, rows map[string][]map[string]any) []string {
	var out []string
	done := map[string]bool{}
	var visit func(string, int)
	visit = func(name string, depth int) {
		if done[name] || depth > 32 {
			return
		}
		if t := sch.types[name]; t != nil {
			for _, fname := range t.Order {
				if f := t.field(fname); f != nil && (f.Type == TypeRef || f.Type == TypeRefs) && f.To != name {
					if _, ok := rows[f.To]; ok {
						visit(f.To, depth+1)
					}
				}
			}
		}
		if !done[name] {
			done[name] = true
			out = append(out, name)
		}
	}
	for _, name := range sortedKeys(rows) {
		visit(name, 0)
	}
	return out
}

func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// CloseDraft closes a draft's store, which must happen before its folder is
// removed (Keep, Discard). A draft never opened is not an error.
func (p *Place) CloseDraft(draft string) error {
	p.mu.Lock()
	s := p.drafts[draft]
	delete(p.drafts, draft)
	p.mu.Unlock()
	if s == nil {
		return nil
	}
	return s.Close()
}

// OnCommit tells fn of every change committed to any of the place's stores,
// those open now and those opened later ([Change.Draft] says which). The
// same rules as [Store.OnCommit] hold: fn must return quickly.
func (p *Place) OnCommit(fn func(Change)) (stop func()) {
	p.hooksMu.Lock()
	defer p.hooksMu.Unlock()
	id := p.nextHook
	p.nextHook++
	p.hooks[id] = fn
	return func() {
		p.hooksMu.Lock()
		defer p.hooksMu.Unlock()
		delete(p.hooks, id)
	}
}

// attach forwards a store's changes to the place's hooks.
func (p *Place) attach(s *Store) {
	s.OnCommit(func(c Change) {
		p.hooksMu.RLock()
		ids := make([]int, 0, len(p.hooks))
		for id := range p.hooks {
			ids = append(ids, id)
		}
		sort.Ints(ids)
		fns := make([]func(Change), 0, len(ids))
		for _, id := range ids {
			fns = append(fns, p.hooks[id])
		}
		p.hooksMu.RUnlock()
		for _, fn := range fns {
			func() {
				defer func() { _ = recover() }()
				fn(c)
			}()
		}
	})
}

// Close closes every store the place opened.
func (p *Place) Close() error {
	p.mu.Lock()
	p.closed = true
	stores := []*Store{}
	if p.main != nil {
		stores = append(stores, p.main)
	}
	for _, s := range p.drafts {
		stores = append(stores, s)
	}
	p.main, p.drafts = nil, map[string]*Store{}
	p.mu.Unlock()
	var errs []error
	for _, s := range stores {
		errs = append(errs, s.Close())
	}
	return errors.Join(errs...)
}
