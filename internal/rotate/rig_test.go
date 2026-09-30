package rotate

import (
	"context"
	"encoding/hex"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/keys"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

// namespace is one identity's side of a fake relay. Its state is the relay's
// rotation state machine in the smallest form: live, frozen, retired.
type namespace struct {
	dir     *directory.Memory
	store   *blobstore.Memory
	mu      sync.Mutex
	state   string
	grace   time.Duration
	getFail error // what a read of the store answers, to cut a fetch short
}

func (n *namespace) writable() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.state == "live"
}

// fakeRelay is every identity's namespace on one relay, found by signer.
type fakeRelay struct {
	clock *directorytest.FakeClock
	mu    sync.Mutex
	ns    map[string]*namespace
}

func (f *fakeRelay) of(pub []byte) *namespace {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := hex.EncodeToString(pub)
	if f.ns[id] == nil {
		f.ns[id] = &namespace{dir: directory.NewMemory(f.clock.Now), store: blobstore.NewMemory(), state: "live"}
	}
	return f.ns[id]
}

// connect is Env.Connect: the namespace of the signer's identity, as its device.
func (f *fakeRelay) connect(s reqsign.Signer) Relay {
	n := f.of(s.IdentityKey())
	return Relay{
		Dir:   guardedDir{n.dir.For(s.Cert().DeviceID()), n},
		Store: guardedStore{n.store, n},
		Gate:  gate{n},
	}
}

type guardedDir struct {
	directory.Client
	n *namespace
}

func (g guardedDir) refuse() error {
	if !g.n.writable() {
		return ErrRotated
	}
	return nil
}

func (g guardedDir) PutDevice(ctx context.Context, id string, d directory.Device) error {
	if err := g.refuse(); err != nil {
		return err
	}
	return g.Client.PutDevice(ctx, id, d)
}

func (g guardedDir) SetVault(ctx context.Context, old, next string) error {
	if err := g.refuse(); err != nil {
		return err
	}
	return g.Client.SetVault(ctx, old, next)
}

func (g guardedDir) Create(ctx context.Context, id string, in directory.CellInit) (directory.CellView, error) {
	if err := g.refuse(); err != nil {
		return directory.CellView{}, err
	}
	return g.Client.Create(ctx, id, in)
}

func (g guardedDir) Acquire(ctx context.Context, id string, o directory.AcquireOpts) (directory.CellView, error) {
	if err := g.refuse(); err != nil {
		return directory.CellView{}, err
	}
	return g.Client.Acquire(ctx, id, o)
}

func (g guardedDir) Publish(ctx context.Context, id string, p directory.Publish) (directory.CellView, error) {
	if err := g.refuse(); err != nil {
		return directory.CellView{}, err
	}
	return g.Client.Publish(ctx, id, p)
}

func (g guardedDir) Release(ctx context.Context, id string, fence uint64) error {
	if err := g.refuse(); err != nil {
		return err
	}
	return g.Client.Release(ctx, id, fence)
}

func (g guardedDir) Archive(ctx context.Context, id string) error {
	if err := g.refuse(); err != nil {
		return err
	}
	return g.Client.Archive(ctx, id)
}

type guardedStore struct {
	*blobstore.Memory
	n *namespace
}

func (g guardedStore) PutFrame(ctx context.Context, frame []byte) (blobstore.FrameID, error) {
	if !g.n.writable() {
		return "", ErrRotated
	}
	return g.Memory.PutFrame(ctx, frame)
}

func (g guardedStore) GetMany(ctx context.Context, rids []string) ([]blobstore.Object, error) {
	if g.n.getFail != nil {
		return nil, g.n.getFail
	}
	return g.Memory.GetMany(ctx, rids)
}

// gate is the fake relay's rotation verb.
type gate struct{ n *namespace }

func (g gate) Freeze(context.Context) error {
	g.n.mu.Lock()
	defer g.n.mu.Unlock()
	if g.n.state == "retired" {
		return ErrRotated
	}
	g.n.state = "frozen"
	return nil
}

func (g gate) Thaw(context.Context) error {
	g.n.mu.Lock()
	defer g.n.mu.Unlock()
	if g.n.state == "retired" {
		return ErrRotated
	}
	g.n.state = "live"
	return nil
}

func (g gate) Retire(_ context.Context, grace time.Duration) error {
	g.n.mu.Lock()
	defer g.n.mu.Unlock()
	g.n.state, g.n.grace = "retired", grace
	return nil
}

// rig is one person with one keeper machine, a fake relay and two chats.
type rig struct {
	t      *testing.T
	home   string
	relay  *fakeRelay
	old    identity.Identity
	dev    identity.Dev
	base   *cellsync.FakeEngine // the keeper's own store, under the old identity
	engine map[string]*cellsync.FakeEngine
	cells  map[string]cell.Cell
	env    Env
}

const (
	chatA = "01J0000000000000000000000A"
	chatB = "01J0000000000000000000000B"
)

func newRig(t *testing.T) *rig {
	t.Helper()
	old, err := identity.Mint()
	if err != nil {
		t.Fatal(err)
	}
	dev, err := identity.NewDevice(old)
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{
		t: t, home: t.TempDir(), old: old, dev: dev,
		relay: &fakeRelay{clock: directorytest.NewFakeClock(), ns: map[string]*namespace{}},
		base:  cellsync.NewFakeEngine(t.TempDir()), engine: map[string]*cellsync.FakeEngine{}, cells: map[string]cell.Cell{},
	}
	if err := identity.Install(r.home, old, dev); err != nil {
		t.Fatal(err)
	}
	r.engine[old.ID()] = r.base
	r.publishChat(chatA, "hello", "note a")
	r.publishChat(chatB, "", "note b")
	r.env = r.newEnv()
	return r
}

func (r *rig) cell(id string) cell.Cell {
	if c, ok := r.cells[id]; ok {
		return c
	}
	c := cell.Cell{ID: id, Root: r.t.TempDir()}
	r.cells[id] = c
	return c
}

// publishChat seals one turn in the keeper's store and publishes it under the
// old identity, as a device would.
func (r *rig) publishChat(id, title, text string) {
	r.t.Helper()
	c := r.cell(id)
	head := r.base.Seal(c, map[string]string{"a.txt": text})
	ns := r.relay.of(r.old.PublicKey())
	pub := cellsync.Publisher{Engine: r.base, Store: ns.store}
	if _, err := pub.Upload(context.Background(), c, head); err != nil {
		r.t.Fatal(err)
	}
	sealed, err := directory.SealName(directory.MetadataKey(r.old.CellKey()), title)
	if title == "" {
		sealed = ""
	}
	if err != nil {
		r.t.Fatal(err)
	}
	if _, err := ns.dir.For(r.dev.ID()).Create(context.Background(), id, directory.CellInit{Head: head, Class: "host-bound", Title: sealed, Size: 1 << 20}); err != nil {
		r.t.Fatal(err)
	}
	if err := ns.dir.For(r.dev.ID()).Release(context.Background(), id, 1); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) newEnv() Env {
	return Env{
		Home: r.home, Identity: r.old, Device: r.dev,
		Connect: r.relay.connect,
		Engine:  r.engineFor,
		Cell:    r.cell,
		Record: func(id identity.Identity) (directory.Device, error) {
			name, err := directory.SealName(directory.MetadataKey(id.CellKey()), "keeper")
			return directory.Device{V: 1, Name: name, AddedBy: id.ID()}, err
		},
	}
}

// engineFor is one engine per identity: the keeper's own store reads the same
// objects under every identity, and each identity's ledger starts empty.
func (r *rig) engineFor(id identity.Identity) cellsync.Engine {
	if e, ok := r.engine[id.ID()]; ok {
		return e
	}
	e := r.base.Ledger(r.t.TempDir())
	r.engine[id.ID()] = e
	return e
}

// reload is the keeper after a restart: its environment built from what is on disk.
func (r *rig) reload() Env {
	id, err := identity.Load(r.home)
	if err != nil {
		r.t.Fatal(err)
	}
	dev, err := identity.Device(r.home)
	if err != nil {
		r.t.Fatal(err)
	}
	e := r.newEnv()
	e.Identity, e.Device = id, dev
	return e
}

var errCrash = errors.New("crash")

// crashAt is an After hook that fails at the named step.
func crashAt(step string) func(string) error {
	return func(got string) error {
		if got == step {
			return errCrash
		}
		return nil
	}
}

func (r *rig) putVault() {
	r.t.Helper()
	v, err := keys.Open(r.home)
	if err != nil {
		r.t.Fatal(err)
	}
	if err := v.Put("p/KEY", keys.Entry{Name: "KEY", Value: "secret", Scope: "p"}); err != nil {
		r.t.Fatal(err)
	}
}

func (n *namespace) gate() Gate { return gate{n} }

func cellsyncEngine(t *testing.T) *cellsync.FakeEngine { return cellsync.NewFakeEngine(t.TempDir()) }

// publishFrom uploads what eng holds for head into a namespace's store.
func publishFrom(t *testing.T, eng *cellsync.FakeEngine, n *namespace, c cell.Cell, head string) {
	t.Helper()
	if _, err := (&cellsync.Publisher{Engine: eng, Store: n.store}).Upload(context.Background(), c, head); err != nil {
		t.Fatal(err)
	}
}
