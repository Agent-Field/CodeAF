package syncsetup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/syncwords"
	"github.com/Agent-Field/codeaf/internal/vaultsync"
)

type l4CloseEngine struct {
	*cellsync.FakeEngine
	fail string
}

func (e l4CloseEngine) Export(ctx context.Context, c cell.Cell, head string) (cellsync.Export, error) {
	if e.fail == "export" {
		return cellsync.Export{}, os.ErrPermission
	}
	ex, err := e.FakeEngine.Export(ctx, c, head)
	if e.fail == "frame read" && err == nil {
		for _, f := range ex.Frames {
			_ = os.Remove(f.Path)
		}
	}
	return ex, err
}
func (e l4CloseEngine) Published(ctx context.Context, c cell.Cell, paths []string) error {
	if e.fail == "ledger" {
		return os.ErrPermission
	}
	return e.FakeEngine.Published(ctx, c, paths)
}

// L4-R3: Drive.Close preserves local export, frame, ledger and branch-map errors; only service failures mean upload pending.
func TestL4FixDriveCloseOrigins(t *testing.T) {
	for _, fail := range []string{"export", "frame read", "ledger", "branch map", "service"} {
		t.Run(fail, func(t *testing.T) {
			ctx := t.Context()
			memory := directory.NewMemory(time.Now)
			dir := memory.For("dev_a")
			store := blobstore.NewMemory()
			s := &Sync{Home: t.TempDir(), Store: store, Dir: dir}
			c := cell.Cell{ID: "01J0000000000000000000000A", Root: t.TempDir()}
			engine := cellsync.NewFakeEngine(t.TempDir())
			head := engine.Seal(c, map[string]string{"a": "one"})
			pub := &cellsync.Publisher{Engine: l4CloseEngine{engine, fail}, Store: store, Dir: dir}
			driving := &cellsync.Driving{Cell: c}
			branches, err := cellsync.OpenBranchMap(s.Home)
			if err != nil {
				t.Fatal(err)
			}
			b := &cellsync.Batcher{Publisher: pub, Driving: driving, Brancher: cellsync.Brancher{Dir: dir, Publisher: pub, Map: branches, NewID: func() (string, error) { return "01J0000000000000000000000B", nil }}}
			if fail == "branch map" {
				created, err := dir.Create(ctx, c.ID, directory.CellInit{Head: head})
				if err != nil {
					t.Fatal(err)
				}
				driving.Fence = created.Cell.Lease.Fence + 1
				path := filepath.Join(s.Home, "v3", "sync")
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("blocks map write"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if fail == "service" {
				store.FailAfter(0, blobstore.ErrUnreachable)
			}
			b.Note(cellstore.Turn{ID: head})
			done := make(chan struct{})
			close(done)
			d := &Drive{sync: s, cell: c, batcher: b, scope: s.scope(c.ID), files: vaultsync.Files{Withheld: func() ([]string, error) { return nil, nil }}, done: done, cancel: func() {}}
			err = d.Close(ctx)
			if err == nil || syncwords.UploadPending(err) != (fail == "service") {
				t.Fatalf("Close=%v upload pending=%v", err, syncwords.UploadPending(err))
			}
			if again := d.Close(ctx); again != err {
				t.Fatalf("Close lost its result: %v != %v", again, err)
			}
			if fail == "export" || fail == "ledger" {
				if !errors.Is(err, os.ErrPermission) {
					t.Fatal(err)
				}
			}
		})
	}
}

type l4ClosingUpload struct {
	blobstore.Store
	entered chan struct{}
}

func (s *l4ClosingUpload) PutFrame(ctx context.Context, raw []byte) (string, error) {
	close(s.entered)
	<-ctx.Done()
	return "", ctx.Err()
}

type l4CloseHeartbeat struct {
	directory.Client
	calls atomic.Int32
}

func (d *l4CloseHeartbeat) Heartbeat(ctx context.Context, id string, beat directory.Beat) (directory.CellView, error) {
	d.calls.Add(1)
	return d.Client.Heartbeat(ctx, id, beat)
}

// L4-R3: Drive.Close retains service provenance when its budget ends behind an in-flight upload.
func TestL4FixDriveCloseJoinsUpload(t *testing.T) {
	ctx := t.Context()
	memory := directory.NewMemory(time.Now)
	dir := &l4CloseHeartbeat{Client: memory.For("dev_a")}
	store := &l4ClosingUpload{Store: blobstore.NewMemory(), entered: make(chan struct{})}
	s := &Sync{Home: t.TempDir(), Store: store, Dir: dir}
	c := cell.Cell{ID: "01J0000000000000000000000A", Root: t.TempDir()}
	created, err := dir.Create(ctx, c.ID, directory.CellInit{Head: strings.Repeat("b", 64)})
	if err != nil {
		t.Fatal(err)
	}
	engine := cellsync.NewFakeEngine(t.TempDir())
	newest := engine.Seal(c, map[string]string{"a": "one"})
	pub := &cellsync.Publisher{Engine: engine, Store: store, Dir: dir}
	ticks := make(chan chan struct{})
	b := &cellsync.Batcher{Publisher: pub, Driving: &cellsync.Driving{Cell: c, Fence: created.Cell.Lease.Fence, Head: created.Cell.Head}, Sleep: func(ctx context.Context, d time.Duration) error {
		if d != directory.HeartbeatEvery {
			<-ctx.Done()
			return ctx.Err()
		}
		step := make(chan struct{})
		select {
		case ticks <- step:
		case <-ctx.Done():
			return ctx.Err()
		}
		select {
		case <-step:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	b.Note(cellstore.Turn{ID: newest})
	run, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	done := make(chan struct{})
	go func() { defer close(done); _ = b.Run(run) }()
	<-store.entered
	next := <-ticks
	closing, cancelClose := context.WithCancel(ctx)
	defer cancelClose()
	d := &Drive{sync: s, cell: c, batcher: b, scope: s.scope(c.ID), files: vaultsync.Files{Withheld: func() ([]string, error) { return nil, nil }}, done: done, cancel: cancelRun}
	result := make(chan error, 1)
	go func() { result <- d.Close(closing) }()
	// The next requested interval acknowledges the previous tick. With a live
	// fence and the first upload still blocked, only closing suppresses its beat.
	for {
		before := dir.calls.Load()
		close(next)
		next = <-ticks
		if dir.calls.Load() == before {
			break
		}
	}
	cancelClose()
	err = <-result
	if !syncwords.UploadPending(err) || !errors.Is(err, context.Canceled) {
		t.Fatalf("Close lost upload origin: %v", err)
	}
	if again := d.Close(ctx); again != err {
		t.Fatalf("Close lost its retained result: %v", again)
	}
}
