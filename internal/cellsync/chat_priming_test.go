package cellsync

import (
	"bytes"
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/furrow"
)

type chatFetchCounts struct {
	blobstore.Store
	frames, objects, bytes atomic.Int64
}

func (s *chatFetchCounts) GetFrame(ctx context.Context, id string) ([]byte, error) {
	s.frames.Add(1)
	raw, err := s.Store.GetFrame(ctx, id)
	s.bytes.Add(int64(len(raw)))
	return raw, err
}
func (s *chatFetchCounts) GetMany(ctx context.Context, ids []string) ([]blobstore.Object, error) {
	s.objects.Add(1)
	objects, err := s.Store.GetMany(ctx, ids)
	for _, o := range objects {
		s.bytes.Add(int64(len(o.Bytes)))
	}
	return objects, err
}

type wantCountingEngine struct {
	Engine
	wants atomic.Int64
}

func (e *wantCountingEngine) Want(ctx context.Context, c cell.Cell, h string) ([]string, error) {
	e.wants.Add(1)
	return e.Engine.Want(ctx, c, h)
}

type usedStoreRig struct {
	sender      cellstore.Engine
	pub         *Publisher
	receiver    cellstore.SyncEngine
	fetch       *Fetcher
	counts      *chatFetchCounts
	cell        cell.Cell
	source      cell.Cell
	earlier     cell.Cell
	earlierHead string
	body        []byte
	head        string
}

func newUsedStoreRig(t *testing.T) *usedStoreRig {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	celltest.RequireEngine(t)
	binary, err := furrow.ResolveOwned()
	if err != nil {
		t.Skip(err)
	}
	t.Cleanup(cellstore.StopSpawned)
	sender := cellstore.Engine{Binary: binary, DataRoot: t.TempDir(), Transport: cellstore.Spawn{Binary: binary}}
	receiver := cellstore.Engine{Binary: binary, DataRoot: t.TempDir(), Transport: cellstore.Spawn{Binary: binary}}
	keys := cellstore.SyncKeys{CellKey: make([]byte, 32), Dedup: make([]byte, 32)}
	send := sender.Sync(keys, "0123456789abcdef")
	recv := receiver.Sync(keys, "0123456789abcdef")
	r := newRig(t)
	pub := &Publisher{Engine: send, Store: r.mem, Dir: r.dirA}
	counts := &chatFetchCounts{Store: r.mem}
	f := &Fetcher{Engine: recv, Store: counts, Dir: r.dir.For(devB), Inbox: recv.Inbox}
	makeChat := func() (cell.Cell, cell.Cell) {
		c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.Sandboxed})
		if err != nil {
			t.Fatal(err)
		}
		local := cell.Cell{ID: c.ID, Root: filepath.Join(t.TempDir(), c.ID)}
		if err := os.MkdirAll(local.Root, 0700); err != nil {
			t.Fatal(err)
		}
		return c, local
	}
	publish := func(c cell.Cell, body []byte) string {
		if err := os.WriteFile(filepath.Join(c.Root, "payload"), body, 0600); err != nil {
			t.Fatal(err)
		}
		sealed, err := sender.Seal(t.Context(), c, cellstore.TurnInfo{})
		if err != nil {
			t.Fatal(err)
		}
		if err := cellstore.Await(t.Context(), c); err != nil {
			t.Fatal(err)
		}
		if err := pub.Publish(t.Context(), &Driving{Cell: c}, sealed.Turn.ID, PublishInfo{Class: "chat"}); err != nil {
			t.Fatal(err)
		}
		return sealed.Turn.ID
	}
	earlier, localEarlier := makeChat()
	first := publish(earlier, []byte("an earlier chat"))
	if err := f.Fetch(t.Context(), localEarlier, first); err != nil {
		t.Fatal(err)
	}
	source, local := makeChat()
	body := make([]byte, 2<<20)
	if _, err := rand.Read(body); err != nil {
		t.Fatal(err)
	}
	head := publish(source, body)
	counts.frames.Store(0)
	counts.objects.Store(0)
	counts.bytes.Store(0)
	return &usedStoreRig{sender: sender, pub: pub, receiver: recv, fetch: f, counts: counts, cell: local, source: source, earlier: earlier, earlierHead: first, body: body, head: head}
}
func TestANewChatInAUsedStorePrimesFromItsPlan(t *testing.T) {
	r := newUsedStoreRig(t)
	// Skipping the plan was the old used-store path. Fetch the very same source
	// through that object-only door in another already-used store for comparison.
	local := cellstore.Engine{Binary: r.sender.Binary, DataRoot: t.TempDir(), Transport: cellstore.Spawn{Binary: r.sender.Binary}}
	engine := local.Sync(r.receiver.Keys, r.receiver.Ledger)
	before := &chatFetchCounts{Store: r.counts.Store}
	plain := &Fetcher{Engine: engine, Store: before, Inbox: engine.Inbox}
	if err := plain.Fetch(t.Context(), cell.Cell{ID: r.earlier.ID, Root: t.TempDir()}, r.earlierHead); err != nil {
		t.Fatal(err)
	}
	before.frames.Store(0)
	before.objects.Store(0)
	before.bytes.Store(0)
	plainCell := cell.Cell{ID: r.cell.ID, Root: t.TempDir()}
	if err := plain.Fetch(t.Context(), plainCell, r.head); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(filepath.Join(plainCell.Root, "payload"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, r.body) || before.frames.Load() != 0 || before.objects.Load() == 0 {
		t.Fatal("object-only baseline did not complete the same bytes")
	}
	t.Logf("object-only cold baseline in used store: frame GETs=%d object POSTs=%d bytes=%d", before.frames.Load(), before.objects.Load(), before.bytes.Load())
	counted := &wantCountingEngine{Engine: r.receiver}
	r.fetch.Engine = counted
	if err := r.fetch.Fetch(t.Context(), r.cell, r.head); err != nil {
		t.Fatal(err)
	}
	t.Logf("cold chat in used store: frame GETs=%d object POSTs=%d bytes=%d", r.counts.frames.Load(), r.counts.objects.Load(), r.counts.bytes.Load())
	if r.counts.frames.Load() == 0 || r.counts.objects.Load() != 0 {
		t.Fatal("new chat skipped its frame plan")
	}
	if counted.wants.Load() < 2 {
		t.Fatal("priming skipped the completeness loop")
	}
	got, err := os.ReadFile(filepath.Join(r.cell.Root, "payload"))
	if err != nil || !bytes.Equal(got, r.body) || !bytes.Equal(got, original) {
		t.Fatal("received different bytes", err)
	}
}
func TestAWarmChatStillFetchesOnlyItsDelta(t *testing.T) {
	r := newUsedStoreRig(t)
	if err := r.fetch.Fetch(t.Context(), r.cell, r.head); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.source.Root, "small"), []byte("one file changed"), 0600); err != nil {
		t.Fatal(err)
	}
	sealed, err := r.sender.Seal(t.Context(), r.source, cellstore.TurnInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if err := cellstore.Await(t.Context(), r.source); err != nil {
		t.Fatal(err)
	}
	// Reuse the actual published head/ownership, just as the existing writer does.
	view, err := r.pub.Dir.Cell(t.Context(), r.source.ID)
	if err != nil {
		t.Fatal(err)
	}
	drv := &Driving{Cell: r.source, Fence: view.Cell.Lease.Fence, Head: view.Cell.Head, Frames: view.Cell.Frames}
	if err := r.pub.Publish(t.Context(), drv, sealed.Turn.ID, PublishInfo{Class: "chat"}); err != nil {
		t.Fatal(err)
	}
	r.counts.frames.Store(0)
	r.counts.objects.Store(0)
	r.counts.bytes.Store(0)
	if err := r.fetch.Fetch(t.Context(), r.cell, sealed.Turn.ID); err != nil {
		t.Fatal(err)
	}
	t.Logf("warm delta: frame GETs=%d object POSTs=%d bytes=%d", r.counts.frames.Load(), r.counts.objects.Load(), r.counts.bytes.Load())
	if r.counts.frames.Load() != 0 || r.counts.objects.Load() == 0 || r.counts.bytes.Load() >= int64(len(r.body)) {
		t.Fatal("warm chat redownloaded its history")
	}
	got, err := os.ReadFile(filepath.Join(r.cell.Root, "payload"))
	if err != nil || !bytes.Equal(got, r.body) {
		t.Fatal("warm history changed", err)
	}
	small, err := os.ReadFile(filepath.Join(r.cell.Root, "small"))
	if err != nil || string(small) != "one file changed" {
		t.Fatal("delta missing", err)
	}
}
