package syncsetup

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/directory"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestStorageOptInRealBatcherMissedOffOnAndLocalGate(t *testing.T) {
	home := t.TempDir()
	const account = "id_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	var revoked atomic.Bool
	revoked.Store(true)
	var writes atomic.Int32
	memory := blobstore.NewMemory()
	handler := blobstore.Handler(func(*http.Request, []byte) (string, string, error) { return account, "device", nil }, func(string) (blobstore.Store, error) { return memory, nil })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/store/frames" && r.Method == "POST" {
			writes.Add(1)
			if revoked.Load() {
				w.WriteHeader(403)
				w.Write([]byte(`{"err":"storage_off"}`))
				return
			}
		}
		handler.ServeHTTP(w, r)
	}))
	defer srv.Close()
	store := GateStore(blobstore.NewHTTP(srv.URL, func(*http.Request, []byte) {}, srv.Client()), home, ProductionRelayURL, account)
	if err := SaveStorage(home, ProductionRelayURL, account, "on"); err != nil {
		t.Fatal(err)
	}
	dir := directory.NewMemory(time.Now)
	defer dir.Close()
	client := dir.For("device")
	c := cell.Cell{ID: "01J0000000000000000000000A", Root: t.TempDir()}
	view, err := client.Create(t.Context(), c.ID, directory.CellInit{})
	if err != nil {
		t.Fatal(err)
	}
	engine := cellsync.NewFakeEngine(t.TempDir())
	head := engine.Seal(c, map[string]string{"a": "pending"})
	pendingWait := make(chan time.Duration, 10)
	retry := make(chan struct{})
	uploaded := make(chan struct{}, 1)
	b := &cellsync.Batcher{Publisher: &cellsync.Publisher{Engine: engine, Store: store, Dir: client}, Driving: &cellsync.Driving{Cell: c, Fence: view.Cell.Lease.Fence}, Interval: 5 * time.Second, OnFlush: func(cellsync.Flush) { uploaded <- struct{}{} }}
	b.Sleep = func(ctx context.Context, d time.Duration) error {
		if d != directory.HeartbeatEvery {
			pendingWait <- d
			select {
			case <-retry:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		<-ctx.Done()
		return ctx.Err()
	}
	b.Note(cellstore.Turn{ID: head})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); b.Run(ctx) }()
	defer func() { cancel(); <-done }()
	wait := <-pendingWait
	if wait > cellsync.MaxBackoff {
		t.Fatalf("halt %v", wait)
	}
	if StorageState(home, ProductionRelayURL, account) != "off" {
		t.Fatal("wire revoke not saved")
	}
	// An ordinary retry while the local file is off makes no second put.
	retry <- struct{}{}
	wait = <-pendingWait
	if writes.Load() != 1 || b.Pending() != 1 {
		t.Fatal("closed local gate reached relay or lost pending turn")
	}
	// Another process replaces the file directly, without notifying this one.
	// The whole off/on change occurred between permission-waiter samples.
	revoked.Store(false)
	raw := []byte(`{"relay":"` + ProductionRelayURL + `","identity":"` + account + `","storage":"on"}`)
	if err := os.WriteFile(filepath.Join(home, StorageFile), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if wait > cellsync.MaxBackoff {
		t.Fatalf("retry %v", wait)
	}
	retry <- struct{}{}
	select {
	case <-uploaded:
	case <-time.After(5 * time.Second):
		t.Fatal("missed edge stranded real publication")
	}
	if b.Pending() != 0 || writes.Load() != 2 {
		t.Fatalf("pending=%d writes=%d", b.Pending(), writes.Load())
	}
	if _, err := client.Cell(t.Context(), c.ID); err != nil {
		t.Fatal(err)
	}
}
