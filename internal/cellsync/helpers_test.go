package cellsync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
)

const (
	devA   = "dev_a"
	devB   = "dev_b"
	cellID = "01J0000000000000000000000A"
	// otherHead is a head device B writes directly, standing in for its work.
	otherHead = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// rig is device A of one identity plus the shared store and directory.
type rig struct {
	t      *testing.T
	clock  *directorytest.FakeClock
	dir    *directory.Memory
	mem    *blobstore.Memory
	store  *flakyStore
	dirA   *faultDir
	engA   *FakeEngine
	cellA  cell.Cell
	drv    *Driving
	pub    *Publisher
	branch *BranchMap
	ids    int
}

func newRig(t *testing.T) *rig {
	t.Helper()
	clock := directorytest.NewFakeClock()
	dir := directory.NewMemory(clock.Now)
	mem := blobstore.NewMemory()
	r := &rig{
		t: t, clock: clock, dir: dir, mem: mem,
		store: &flakyStore{Store: mem},
		dirA:  &faultDir{Client: dir.For(devA)},
		engA:  NewFakeEngine(t.TempDir()),
		cellA: cell.Cell{ID: cellID, Root: t.TempDir()},
	}
	r.drv = &Driving{Cell: r.cellA}
	r.pub = &Publisher{Engine: r.engA, Store: r.store, Dir: r.dirA}
	r.branch = &BranchMap{path: filepath.Join(t.TempDir(), "branches.json"), m: map[string]string{}}
	return r
}

func (r *rig) seal(files map[string]string) string { return r.engA.Seal(r.cellA, files) }

func (r *rig) newID() (string, error) {
	r.ids++
	return fmt.Sprintf("branch-%d", r.ids), nil
}

func (r *rig) info() PublishInfo { return PublishInfo{Class: "chat", Size: 7} }

// batcher is device A's batcher over the rig, with a short interval.
func (r *rig) batcher() *Batcher {
	return &Batcher{
		Publisher: r.pub,
		Brancher:  Brancher{Dir: r.dirA, Publisher: r.pub, NewID: r.newID, Map: r.branch},
		Driving:   r.drv,
		Interval:  5 * time.Second,
		Info:      r.info,
	}
}

// head reads the directory's view of a cell as device B would.
func (r *rig) head(id string) directory.Cell {
	r.t.Helper()
	v, err := r.dir.For(devB).Cell(context.Background(), id)
	if err != nil {
		r.t.Fatalf("cell %s: %v", id, err)
	}
	return v.Cell
}

// publishFirst makes h the durable head of cellID.
func (r *rig) publishFirst(files map[string]string) string {
	r.t.Helper()
	h := r.seal(files)
	if err := r.pub.Publish(context.Background(), r.drv, h, r.info()); err != nil {
		r.t.Fatalf("first publish: %v", err)
	}
	return h
}

// takeOver lets the lease lapse and makes device B hold it, as after a lid-close.
func (r *rig) takeOver() {
	r.t.Helper()
	r.clock.Advance(directory.LeaseTTL + time.Second)
	if _, err := r.dir.For(devB).Acquire(context.Background(), cellID); err != nil {
		r.t.Fatalf("takeover: %v", err)
	}
}

// bPublishes writes device B's head, as its own driver would after takeover.
func (r *rig) bPublishes(old string) {
	r.t.Helper()
	_, err := r.dir.For(devB).Publish(context.Background(), cellID, directory.Publish{
		Fence: 2, OldHead: old, Head: otherHead, Class: "chat",
	})
	if err != nil {
		r.t.Fatalf("B publish: %v", err)
	}
}

func (r *rig) puts() int {
	n := 0
	for _, op := range r.mem.Log() {
		if op.Kind == "put" {
			n++
		}
	}
	return n
}

func note(b *Batcher, heads ...string) {
	for _, h := range heads {
		b.Note(cellstore.Turn{ID: h})
	}
}

// flakyStore is a store that can fail, or run a hook, before each PutFrame.
type flakyStore struct {
	blobstore.Store
	mu   sync.Mutex
	err  error
	hook func(context.Context)
}

func (s *flakyStore) set(err error, hook func(context.Context)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err, s.hook = err, hook
}

func (s *flakyStore) PutFrame(ctx context.Context, frame []byte) (blobstore.FrameID, error) {
	s.mu.Lock()
	err, hook := s.err, s.hook
	s.mu.Unlock()
	if hook != nil {
		hook(ctx)
	}
	if err != nil {
		return "", err
	}
	return s.Store.PutFrame(ctx, frame)
}

// faultDir is a directory client that can be told to fail its writes.
type faultDir struct {
	directory.Client
	mu  sync.Mutex
	err error
}

func (d *faultDir) set(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.err = err
}

func (d *faultDir) fault() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.err
}

func (d *faultDir) Create(ctx context.Context, id string, in directory.CellInit) (directory.CellView, error) {
	if err := d.fault(); err != nil {
		return directory.CellView{}, err
	}
	return d.Client.Create(ctx, id, in)
}

func (d *faultDir) Publish(ctx context.Context, id string, p directory.Publish) (directory.CellView, error) {
	if err := d.fault(); err != nil {
		return directory.CellView{}, err
	}
	return d.Client.Publish(ctx, id, p)
}

func (d *faultDir) Heartbeat(ctx context.Context, id string, b directory.Beat) (directory.CellView, error) {
	if err := d.fault(); err != nil {
		return directory.CellView{}, err
	}
	return d.Client.Heartbeat(ctx, id, b)
}

func (d *faultDir) Release(ctx context.Context, id string, fence uint64) error {
	if err := d.fault(); err != nil {
		return err
	}
	return d.Client.Release(ctx, id, fence)
}

// readFiles reads every file under root as path -> content.
func readFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := os.ReadFile(p)
		rel, _ := filepath.Rel(root, p)
		out[rel] = string(raw)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// fakeSleeper is a clock for Run: sleeps end only when a test advances time.
type fakeSleeper struct {
	clock   *directorytest.FakeClock
	mu      sync.Mutex
	changed *sync.Cond
	now     time.Duration
	waiters []*sleeper
	expired bool // settleOn ran out of patience
}

type sleeper struct {
	wake time.Duration
	done chan struct{}
}

func newFakeSleeper(clock *directorytest.FakeClock) *fakeSleeper {
	s := &fakeSleeper{clock: clock}
	s.changed = sync.NewCond(&s.mu)
	return s
}

func (s *fakeSleeper) Sleep(ctx context.Context, d time.Duration) error {
	s.mu.Lock()
	w := &sleeper{wake: s.now + d, done: make(chan struct{})}
	s.waiters = append(s.waiters, w)
	s.changed.Broadcast()
	s.mu.Unlock()
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		s.forget(w)
		return ctx.Err()
	}
}

// forget drops a sleeper whose wait was cancelled, so settle counts live loops.
func (s *fakeSleeper) forget(w *sleeper) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.waiters = slices.DeleteFunc(s.waiters, func(x *sleeper) bool { return x == w })
	s.changed.Broadcast()
}

// advance moves both the directory's clock and the sleepers' time by d.
func (s *fakeSleeper) advance(d time.Duration) {
	s.clock.Advance(d)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now += d
	kept := s.waiters[:0]
	for _, w := range s.waiters {
		if w.wake <= s.now {
			close(w.done)
			continue
		}
		kept = append(kept, w)
	}
	s.waiters = kept
}

// settle waits until n loops are asleep again, that is, until the work the
// last advance woke has finished.
func (s *fakeSleeper) settle(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.waiters) != n {
		s.changed.Wait()
	}
}

// settleOn waits until some loop sleeps for exactly d from now: the loop has
// finished the work before that sleep. It gives up after a real second, so a
// loop that never gets there fails the test instead of hanging it.
func (s *fakeSleeper) settleOn(d time.Duration) {
	giveUp := time.AfterFunc(time.Second, func() {
		s.mu.Lock()
		s.expired = true
		s.mu.Unlock()
		s.changed.Broadcast()
	})
	defer giveUp.Stop()
	s.mu.Lock()
	defer s.mu.Unlock()
	for !slices.ContainsFunc(s.waiters, func(w *sleeper) bool { return w.wake-s.now == d }) {
		if s.expired {
			panic(fmt.Sprintf("no loop went to sleep for %v", d))
		}
		s.changed.Wait()
	}
}
