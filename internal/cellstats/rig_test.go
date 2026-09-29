package cellstats_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstats"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

const (
	identity = "alice"
	cellID   = "01J0000000000000000000000A"
)

// The store wire tests use the fake sign/authenticate pair of contract §16.
func fakeSign(id string) wireauth.Sign {
	return func(r *http.Request, _ []byte) { r.Header.Set("X-Test-Device", id) }
}

func fakeAuth(r *http.Request, _ []byte) (string, string, error) {
	id := r.Header.Get("X-Test-Device")
	if id == "" {
		return "", "", wireauth.ErrUnauthorized
	}
	return id, "dev_" + id, nil
}

// metered counts what passes through it the way blobstore.Counting will
// (contract §2.6): one request each, frame length up, bytes returned down.
type metered struct {
	blobstore.Store
	puts, gets, has, up, down atomic.Int64
}

func (m *metered) PutFrame(ctx context.Context, frame []byte) (blobstore.FrameID, error) {
	m.puts.Add(1)
	m.up.Add(int64(len(frame)))
	return m.Store.PutFrame(ctx, frame)
}

func (m *metered) Get(ctx context.Context, rid string) ([]byte, error) {
	m.gets.Add(1)
	b, err := m.Store.Get(ctx, rid)
	m.down.Add(int64(len(b)))
	return b, err
}

func (m *metered) Has(ctx context.Context, rids []string) ([]bool, error) {
	m.has.Add(1)
	return m.Store.Has(ctx, rids)
}

func (m *metered) Snapshot() cellstats.Counts {
	return cellstats.Counts{Puts: m.puts.Load(), Gets: m.gets.Load(), Has: m.has.Load(),
		BytesUp: m.up.Load(), BytesDown: m.down.Load()}
}

// session is one device driving one cell against a store handler over HTTP.
type session struct {
	home    string
	client  *blobstore.HTTP
	store   *metered
	engine  *cellsync.FakeEngine
	cell    cell.Cell
	batcher *cellsync.Batcher
	rec     *cellstats.Recorder
	mu      sync.Mutex
}

func newSession(t *testing.T, title string) *session {
	t.Helper()
	var mems sync.Map
	srv := httptest.NewServer(blobstore.Handler(fakeAuth, func(id string) (blobstore.Store, error) {
		m, _ := mems.LoadOrStore(id, blobstore.NewMemory())
		return m.(blobstore.Store), nil
	}))
	t.Cleanup(srv.Close)
	s := &session{home: t.TempDir()}
	s.client = blobstore.NewHTTP(srv.URL, fakeSign(identity), nil)
	s.store = &metered{Store: s.client}
	s.engine = cellsync.NewFakeEngine(t.TempDir())
	s.cell = cell.Cell{ID: cellID, Root: t.TempDir()}
	s.rec = cellstats.NewRecorder(s.home, cellID, s.store)
	dir := directory.NewMemory(directorytest.NewFakeClock().Now).For("dev_alice")
	s.batcher = &cellsync.Batcher{
		Publisher: &cellsync.Publisher{Engine: s.engine, Store: s.store, Dir: dir},
		Driving:   &cellsync.Driving{Cell: s.cell},
		Info:      func() cellsync.PublishInfo { return cellsync.PublishInfo{Class: "chat", Title: title, Size: 7} },
		OnFlush:   s.rec.OnFlush,
	}
	return s
}

// sealAndFlush seals files as one turn and publishes it.
func (s *session) sealAndFlush(t *testing.T, files map[string]string) {
	t.Helper()
	s.batcher.Note(cellstore.Turn{ID: s.engine.Seal(s.cell, files)})
	if err := s.batcher.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
