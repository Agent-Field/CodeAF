package syncsetup

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstats"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/furrow"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/relayserve"
)

// skewedClock is the relay's clock: the wall clock plus an offset a test moves
// to let a lease run out without waiting for it. The offset stays well inside
// the five minutes a signed request may be off by.
type skewedClock struct{ offsetMs atomic.Int64 }

func (c *skewedClock) Now() time.Time {
	return time.Now().Add(time.Duration(c.offsetMs.Load()) * time.Millisecond)
}

func (c *skewedClock) advance(d time.Duration) { c.offsetMs.Add(d.Milliseconds()) }

// driveRig is one identity on two machines and a real relay with a store, in
// this process. Machine A is the one that chats.
type driveRig struct {
	t      *testing.T
	bin    string
	clock  *skewedClock
	store  string // the relay's --store directory
	homeA  string
	a, b   *Sync
	engine cellstore.Engine // A's engine: its own data root, the real binary over spawn
	cell   cell.Cell
	work   string
}

func newDriveRig(t *testing.T) *driveRig {
	t.Helper()
	bin, err := furrow.ResolveOwned()
	if err != nil {
		t.Skipf("no engine binary: %v", err)
	}
	clock := &skewedClock{}
	storeDir := t.TempDir()
	svc := relayserve.New(relayserve.Config{Store: storeDir, Now: clock.Now})
	t.Cleanup(func() { _ = svc.Close() })
	srv := httptestServer(t, svc.Handler)
	t.Setenv(cell.EnvVar, "1")
	t.Setenv(home.EnvVar, t.TempDir())
	homeA := machine(t, srv.URL)
	t.Setenv(IntervalVar, "50")
	r := &driveRig{t: t, bin: bin, clock: clock, store: storeDir, homeA: homeA}
	r.a = openSync(t, homeA)
	homeB := t.TempDir()
	if _, err := identity.Adopt(homeB, r.a.Identity, false); err != nil {
		t.Fatal(err)
	}
	r.b = openSync(t, homeB)
	r.work = t.TempDir()
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	r.cell = c
	r.engine = cellstore.EngineFor(r.work)
	r.engine.DataRoot, r.engine.Binary = t.TempDir(), bin
	r.engine.Transport = cellstore.Spawn{Binary: bin}
	return r
}

func httptestServer(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func branchMapOf(home string) (*cellsync.BranchMap, error) { return cellsync.OpenBranchMap(home) }

// noticeLog collects the lines the drive side shows the person, from the
// Batcher's goroutine, for the test goroutine to read.
type noticeLog struct {
	mu    sync.Mutex
	lines []string
}

func (n *noticeLog) add(line string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.lines = append(n.lines, line)
}

func (n *noticeLog) last() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.lines) == 0 {
		return ""
	}
	return n.lines[len(n.lines)-1]
}

func openSync(t *testing.T, home string) *Sync {
	t.Helper()
	s, ok, err := Open(home)
	if err != nil || !ok {
		t.Fatalf("Open = %v, %v", ok, err)
	}
	return s
}

// chat is A's conversation: a seat whose every tool call seals through the
// drive side, gated the way the door gates it, and a scripted model that writes
// one file per call.
type chat struct {
	r     *driveRig
	drive *Drive
	seat  executor.Seat
	calls int
}

func (r *driveRig) startChat(notices *noticeLog) *chat {
	r.t.Helper()
	drive, err := r.a.Drive(context.Background(), r.engine, r.cell, DriveOptions{
		DeviceName: "spark",
		OnNotice:   notices.add,
	})
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { _ = drive.Close(context.Background()) })
	seat, err := cellstore.SeatOver(executor.HostBound, r.cell, r.work, nil, nil,
		func(cellstore.Engine) cellstore.Store { return drive.Store(r.engine) })
	if err != nil {
		r.t.Fatal(err)
	}
	return &chat{r: r, drive: drive, seat: executor.Gated(seat, drive.Gate)}
}

// say is one scripted model turn: it writes a file and the seat seals the call.
func (c *chat) say() error {
	c.calls++
	name := filepath.Join(c.r.work, "note"+string(rune('a'+c.calls))+".txt")
	call := executor.Call{Tool: "bash", Args: []byte(`{"command":"echo"}`)}
	return c.seat.Around(context.Background(), call, func() ([]byte, bool) {
		return nil, os.WriteFile(name, []byte("turn "+name), 0o600) != nil
	})
}

func (c *chat) mustSay() {
	c.r.t.Helper()
	if err := c.say(); err != nil {
		c.r.t.Fatal(err)
	}
}

func (r *driveRig) head() string {
	r.t.Helper()
	h, err := cellstore.Head(r.cell)
	if err != nil || h == nil {
		r.t.Fatalf("no head: %v", err)
	}
	return h.Turn.ID
}

func (r *driveRig) directoryHead(id string) string {
	v, err := r.a.Dir.Cell(context.Background(), id)
	if err != nil {
		return ""
	}
	return v.Cell.Head
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestDriveSidePublishesTurns is the heart of the drive side: two sealed turns
// reach the relay's store, the directory head is the chat's head, and what the
// stats file says was sent is what the relay says it received.
func TestDriveSidePublishesTurns(t *testing.T) {
	r := newDriveRig(t)
	var notices noticeLog
	c := r.startChat(&notices)
	c.mustSay()
	c.mustSay()
	waitFor(t, "the directory head to reach the chat's head", func() bool { return r.directoryHead(r.cell.ID) == r.head() })

	if n := countFiles(t, filepath.Join(r.store, "blobs")); n == 0 {
		t.Fatal("the store holds no objects")
	}
	v, err := r.a.Dir.Cell(context.Background(), r.cell.ID)
	if err != nil || v.Cell.Lease.Device != r.a.Device.ID() || v.Cell.Lease.Fence != 1 {
		t.Fatalf("directory cell = %+v, %v; want A holding fence 1", v.Cell, err)
	}
	turns, _ := cellstore.Turns(r.cell)
	if got := turns[len(turns)-1].Device; got != r.a.Device.Cert.Device {
		t.Fatalf("turn device = %q, want this machine's key", got)
	}
	assertStatsMatchRelay(t, r)
	// One name for the ledger: the file the engine wrote is the one Sync names.
	if _, err := os.Stat(filepath.Join(r.engine.LocalDir(r.cell), "published."+r.a.Ledger)); err != nil {
		t.Fatalf("the engine's ledger is not the one Sync names: %v", err)
	}
}

func assertStatsMatchRelay(t *testing.T, r *driveRig) {
	t.Helper()
	waitFor(t, "a stats line", func() bool {
		lines, _ := cellstats.Read(r.homeA, r.cell.ID)
		return len(lines) > 0
	})
	lines, err := cellstats.Read(r.homeA, r.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	total := cellstats.Total(lines)
	got, err := r.a.Store.(blobstore.Counting).Inner.(*blobstore.HTTP).Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if total.Puts != got.Puts || total.BytesUp != got.BytesIn || total.Gets != got.Gets || total.Has != got.Has {
		t.Fatalf("stats file totals %+v, relay counted %+v", total, got)
	}
}

func countFiles(t *testing.T, dir string) (n int) {
	t.Helper()
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}

// TestDriveSideSupersededStopsTools is L7 and L12 end to end: B takes the lease
// once A's has run out, A's next publish is refused, A says so in the frozen
// words, its unpublished turn becomes a branch, and its tool calls stop.
func TestDriveSideSupersededStopsTools(t *testing.T) {
	r := newDriveRig(t)
	ctx := context.Background()
	var notices noticeLog
	c := r.startChat(&notices)
	c.mustSay()
	c.mustSay()
	durable := r.head()
	waitFor(t, "the first two turns to be durable", func() bool { return r.directoryHead(r.cell.ID) == durable })

	if err := r.b.putDevice(ctx, "blackmac"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.b.Dir.Acquire(ctx, r.cell.ID); !errors.Is(err, directory.ErrLeaseHeld) {
		t.Fatalf("B acquired a live lease: %v", err)
	}
	r.clock.advance(directory.LeaseTTL + time.Second)
	if _, err := r.b.Dir.Acquire(ctx, r.cell.ID); err != nil {
		t.Fatalf("B could not acquire after LeaseTTL: %v", err)
	}

	c.mustSay() // A does not know yet; this turn is unpublished
	want := chatlist.Superseded("blackmac")
	waitFor(t, "A to learn it was superseded", func() bool { _, viewer := c.drive.Viewer(); return viewer })
	if line, _ := c.drive.Viewer(); line != want {
		t.Fatalf("line = %q, want %q", line, want)
	}
	waitFor(t, "the superseded line to be shown", func() bool { return notices.last() == want })

	v, err := r.a.Dir.Cell(ctx, r.cell.ID)
	if err != nil || v.Cell.Head != durable {
		t.Fatalf("the old chat's head moved to %q (%v); it must stay %q", v.Cell.Head, err, durable)
	}
	assertBranch(t, r, 1)

	before := r.head()
	if err := c.say(); err == nil || err.Error() != want {
		t.Fatalf("a viewer's tool call answered %v, want the superseded line", err)
	}
	if r.head() != before {
		t.Fatal("a refused tool call still sealed a turn")
	}
}

// assertBranch checks the orphaned turns became a child cell and the local map
// points the chat at it.
func assertBranch(t *testing.T, r *driveRig, turns uint32) {
	t.Helper()
	l, err := r.a.Dir.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for id, c := range l.Cells {
		if c.ParentCell == r.cell.ID {
			if c.OrphanTurns != turns || c.Head != r.head() {
				t.Fatalf("branch %s = %+v; want %d orphan turns at the local head", id, c, turns)
			}
			m, err := branchMapOf(r.homeA)
			if err != nil || m.Resolve(r.cell.ID) != id {
				t.Fatalf("branch map resolves %q to %q (%v), want %q", r.cell.ID, m.Resolve(r.cell.ID), err, id)
			}
			return
		}
	}
	t.Fatal("no branch cell records the orphaned turns")
}

// TestDriveSideCloseReleasesTheLease is the quit path: what is sealed is
// published, then the lease is given back so another machine takes over at once.
func TestDriveSideCloseReleasesTheLease(t *testing.T) {
	r := newDriveRig(t)
	ctx := context.Background()
	var notices noticeLog
	c := r.startChat(&notices)
	c.mustSay()
	if err := c.drive.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if got := r.directoryHead(r.cell.ID); got != r.head() {
		t.Fatalf("Close left the directory at %q, want the chat's head", got)
	}
	if _, err := r.b.Dir.Acquire(ctx, r.cell.ID); err != nil {
		t.Fatalf("B could not take a released chat at once: %v", err)
	}
	if err := c.drive.Close(ctx); err != nil {
		t.Fatalf("a second Close = %v", err)
	}
}

// TestDriveSideOffIsStageZero: with no relay set nothing about sync exists. Open
// says so, a seat built over the plain engine seals under the zero device and
// fence 0, and nothing is written under the home's sync directory.
func TestDriveSideOffIsStageZero(t *testing.T) {
	bin, err := furrow.ResolveOwned()
	if err != nil {
		t.Skipf("no engine binary: %v", err)
	}
	t.Setenv(URLVar, "")
	t.Setenv(cell.EnvVar, "1")
	t.Setenv(home.EnvVar, t.TempDir())
	h := t.TempDir()
	if s, ok, err := Open(h); s != nil || ok || err != nil {
		t.Fatalf("Open = %v, %v, %v; want nothing", s, ok, err)
	}
	work := t.TempDir()
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	engine := cellstore.EngineFor(work)
	engine.DataRoot, engine.Binary, engine.Transport = t.TempDir(), bin, cellstore.Spawn{Binary: bin}
	seat, err := cellstore.SeatOver(executor.HostBound, c, work, nil, nil, func(cellstore.Engine) cellstore.Store { return engine })
	if err != nil {
		t.Fatal(err)
	}
	call := executor.Call{Tool: "bash", Args: []byte(`{"command":"echo"}`)}
	if err := seat.Around(context.Background(), call, func() ([]byte, bool) { return nil, false }); err != nil {
		t.Fatal(err)
	}
	turns, _ := cellstore.Turns(c)
	if len(turns) != 1 || turns[0].Fence != 0 || turns[0].Device != strings.Repeat("0", 64) {
		t.Fatalf("turns = %+v, want one Stage 0 turn", turns)
	}
	if _, err := os.Stat(filepath.Join(h, "v3", "sync")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("sync off wrote under the home: %v", err)
	}
}

// A chat that another machine is driving right now opens as a viewer, naming
// that machine, and nothing of it runs here: no batcher, no lease taken.
func TestDriveSideStartsAsViewerWhenHeld(t *testing.T) {
	r := newDriveRig(t)
	var notices noticeLog
	c := r.startChat(&notices)
	c.mustSay()
	waitFor(t, "the first turn to be durable", func() bool { return r.directoryHead(r.cell.ID) == r.head() })

	viewer, err := r.b.Drive(context.Background(), r.engine, r.cell, DriveOptions{DeviceName: "blackmac"})
	if err != nil {
		t.Fatal(err)
	}
	if line, ok := viewer.Viewer(); !ok || line != chatlist.Superseded("spark") {
		t.Fatalf("viewer = %q, %v; want the line naming spark", line, ok)
	}
	if err := viewer.Gate(); err == nil {
		t.Fatal("a viewer's tool calls are not refused")
	}
	if err := viewer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	v, _ := r.a.Dir.Cell(context.Background(), r.cell.ID)
	if v.Cell.Lease.Device != r.a.Device.ID() || v.Cell.Lease.Fence != 1 {
		t.Fatalf("a viewer disturbed the lease: %+v", v.Cell.Lease)
	}
}
