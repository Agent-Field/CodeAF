package handoff

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
)

const (
	devA   = "dev_a"
	devB   = "dev_b"
	chatID = "01J0000000000000000000000A"
)

// world is the shared store and directory that every device of one identity sees.
type world struct {
	t     *testing.T
	clock *directorytest.FakeClock
	dir   *directory.Memory
	mem   *blobstore.Memory
	meta  string // a valid .cell/meta.json every sealed head carries
	bare  bool   // seal heads without it, so only a hook can make the folder openable
	trace *trace
	ids   int
}

// device is one machine: its own engine, its own disk, its own directory client.
type device struct {
	w      *world
	name   string
	eng    *cellsync.FakeEngine
	dir    *tracedDir
	store  *tracedStore
	local  *fakeLocal
	base   string
	hooks  []func(context.Context, cell.Cell) error
	onFetc func() // runs after each fetch, to move the world under the taker
}

func newWorld(t *testing.T) *world {
	t.Helper()
	clock := directorytest.NewFakeClock()
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(c.Root, cell.MetaPath))
	if err != nil {
		t.Fatal(err)
	}
	return &world{t: t, clock: clock, dir: directory.NewMemory(clock.Now), mem: blobstore.NewMemory(),
		meta: string(raw), trace: &trace{}}
}

func (w *world) device(name string) *device {
	d := &device{w: w, name: name, eng: cellsync.NewFakeEngine(w.t.TempDir()), base: w.t.TempDir()}
	d.dir = &tracedDir{Client: w.dir.For(name), trace: w.trace}
	d.store = &tracedStore{Store: w.mem}
	d.local = &fakeLocal{d: d}
	return d
}

func (d *device) root(id string) string { return filepath.Join(d.base, "roots", id) }

func (d *device) cell(id string) cell.Cell { return cell.Cell{ID: id, Root: d.root(id)} }

// withMeta adds the cell's meta.json to files, as every real head carries it.
func (w *world) withMeta(files map[string]string) map[string]string {
	out := map[string]string{}
	if !w.bare {
		out[cell.MetaPath] = w.meta
	}
	for k, v := range files {
		out[k] = v
	}
	return out
}

// taker is this device's Taker, wired as the real wiring would be.
func (d *device) taker() Taker {
	fetcher := &cellsync.Fetcher{Engine: d.eng, Store: d.store, Inbox: func(c cell.Cell) string {
		return filepath.Join(d.base, "inbox", c.ID)
	}}
	pub := &cellsync.Publisher{Engine: d.eng, Store: d.store, Dir: d.dir}
	brancher := cellsync.Brancher{Dir: d.dir, Publisher: pub, NewID: d.newID}
	return Taker{
		Dir:     d.dir,
		Fetch:   &tracedFetch{d: d, inner: fetcher},
		Local:   d.local,
		Branch:  d.branch(brancher),
		RootFor: d.root,
		After:   d.hooks,
	}
}

func (d *device) newID() (string, error) {
	d.w.ids++
	return fmt.Sprintf("branch-%s-%d", d.name, d.w.ids), nil
}

// branch notes the step and hands over to the Brancher, which uploads head
// itself: nothing in this wiring uploads for it.
func (d *device) branch(b cellsync.Brancher) func(context.Context, *cellsync.Driving, string, uint32) (string, error) {
	return func(ctx context.Context, from *cellsync.Driving, head string, turns uint32) (string, error) {
		d.w.trace.add("branch")
		return b.Branch(ctx, from, head, turns, cellsync.PublishInfo{Class: "chat"})
	}
}

// start makes this device create the chat with files and hold it.
func (d *device) start(files map[string]string) cellsync.Driving {
	d.w.t.Helper()
	c := d.cell(chatID)
	drv := cellsync.Driving{Cell: c}
	head := d.eng.Seal(c, d.w.withMeta(files))
	d.materialize(c, files)
	pub := &cellsync.Publisher{Engine: d.eng, Store: d.store, Dir: d.dir}
	if err := pub.Publish(context.Background(), &drv, head, cellsync.PublishInfo{Class: "chat"}); err != nil {
		d.w.t.Fatalf("%s start: %v", d.name, err)
	}
	return drv
}

// work is one turn of this device on the chat: take the lease, seal files,
// publish, and let the lease go.
func (d *device) work(files map[string]string) string {
	d.w.t.Helper()
	ctx := context.Background()
	view, err := d.dir.Client.Cell(ctx, chatID)
	if err != nil {
		d.w.t.Fatal(err)
	}
	got, err := d.dir.Client.Acquire(ctx, chatID)
	if err != nil {
		d.w.t.Fatalf("%s acquire: %v", d.name, err)
	}
	c := d.cell(chatID)
	head := d.eng.Seal(c, d.w.withMeta(files))
	drv := cellsync.Driving{Cell: c, Fence: got.Cell.Lease.Fence, Head: view.Cell.Head}
	pub := &cellsync.Publisher{Engine: d.eng, Store: d.store, Dir: d.dir.Client}
	if err := pub.Publish(ctx, &drv, head, cellsync.PublishInfo{Class: "chat"}); err != nil {
		d.w.t.Fatalf("%s publish: %v", d.name, err)
	}
	d.release(drv)
	return head
}

func (d *device) release(drv cellsync.Driving) {
	d.w.t.Helper()
	if err := d.dir.Client.Release(context.Background(), chatID, drv.Fence); err != nil {
		d.w.t.Fatalf("%s release: %v", d.name, err)
	}
}

// materialize writes files under the device's root for c, as a live session would.
func (d *device) materialize(c cell.Cell, files map[string]string) {
	d.w.t.Helper()
	for path, body := range d.w.withMeta(files) {
		target := filepath.Join(c.Root, path)
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			d.w.t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(body), 0o600); err != nil {
			d.w.t.Fatal(err)
		}
	}
}

func (d *device) lapse() { d.w.clock.Advance(directory.LeaseTTL + 1) }

// cellRec is the directory's record of id.
func (w *world) cellRec(id string) directory.Cell {
	w.t.Helper()
	v, err := w.dir.For("observer").Cell(context.Background(), id)
	if err != nil {
		w.t.Fatalf("cell %s: %v", id, err)
	}
	return v.Cell
}

// tree reads every file under root except the cell's own state as path -> body.
func tree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.WalkDir(root, func(p string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		raw, err := os.ReadFile(p)
		out[rel] = string(raw)
		return err
	})
	delete(out, cell.MetaPath)
	return out
}

// trace records the order of the steps a takeover makes.
type trace struct{ steps []string }

func (t *trace) add(s string) { t.steps = append(t.steps, s) }

func (t *trace) has(s string) bool {
	for _, x := range t.steps {
		if x == s {
			return true
		}
	}
	return false
}

// tracedDir notes acquires and can be made unreachable.
type tracedDir struct {
	directory.Client
	trace *trace
	down  error
}

func (d *tracedDir) Cell(ctx context.Context, id string) (directory.CellView, error) {
	if d.down != nil {
		return directory.CellView{}, d.down
	}
	return d.Client.Cell(ctx, id)
}

func (d *tracedDir) Acquire(ctx context.Context, id string) (directory.CellView, error) {
	d.trace.add("acquire")
	return d.Client.Acquire(ctx, id)
}

// tracedStore can lose an object or go away.
type tracedStore struct {
	blobstore.Store
	down    error
	hide    bool   // hide the next object asked for
	Missing string // the rid that was hidden
}

func (s *tracedStore) Get(ctx context.Context, rid string) ([]byte, error) {
	if s.down != nil {
		return nil, s.down
	}
	if s.hide {
		s.hide, s.Missing = false, rid
	}
	if rid == s.Missing && rid != "" {
		return nil, blobstore.ErrNotFound
	}
	return s.Store.Get(ctx, rid)
}

// tracedFetch notes each fetch and lets a test move the world after one.
type tracedFetch struct {
	d     *device
	inner Fetch
	heads []string
}

func (f *tracedFetch) Fetch(ctx context.Context, c cell.Cell, head string) error {
	f.d.w.trace.add("fetch")
	f.heads = append(f.heads, head)
	err := f.inner.Fetch(ctx, c, head)
	if err == nil && f.d.onFetc != nil {
		hook := f.d.onFetc
		f.d.onFetc = nil // once
		hook()
	}
	return err
}

// fakeLocal is a device's view of its own root: dirty when told, and sealed
// from whatever files the root holds.
type fakeLocal struct {
	d     *device
	dirty bool
}

func (l *fakeLocal) Dirty(context.Context, cell.Cell) (bool, error) { return l.dirty, nil }

func (l *fakeLocal) Seal(_ context.Context, c cell.Cell) (string, uint32, error) {
	l.d.w.trace.add("seal")
	files := map[string]string{}
	for p, body := range tree(l.d.w.t, c.Root) {
		files[p] = body
	}
	return l.d.eng.Seal(c, l.d.w.withMeta(files)), 1, nil
}
