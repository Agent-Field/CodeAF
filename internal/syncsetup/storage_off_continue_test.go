package syncsetup

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

type storageOffStore struct {
	blobstore.Store
	writes int
	off    bool
}

func (s *storageOffStore) PutFrame(ctx context.Context, frame []byte) (blobstore.FrameID, error) {
	s.writes++
	if s.off {
		return "", wireauth.ErrStorageOff
	}
	return s.Store.PutFrame(ctx, frame)
}

// B5 preserves the entire local root and acquires through the directory without store writes.
func TestStorageOffContinuePreservesLocalTurns(t *testing.T) {
	celltest.RequireEngine(t)
	for _, mode := range []string{"two turns above head", "empty recorded head", "diverged", "head moves at acquire"} {
		t.Run(mode, func(t *testing.T) {
			home := t.TempDir()
			id, err := identity.Ensure(home)
			mustNil(t, err)
			dev, err := identity.Device(home)
			mustNil(t, err)
			dir := directory.NewMemory(time.Now)
			defer dir.Close()
			store := &storageOffStore{Store: blobstore.NewMemory(), off: true}
			s := &Sync{Home: home, Relay: ProductionRelayURL, Identity: id, Device: dev, Dir: dir.For(dev.ID()), Store: store}
			at, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.HostBound})
			mustNil(t, err)
			work := t.TempDir()
			mustNil(t, session.SaveMeta(at.Root, session.Meta{ID: at.ID, Workspace: work, Created: time.Now()}))
			eng := cellstore.EngineFor(work)
			eng.DataRoot = t.TempDir()
			t.Cleanup(cellstore.StopSpawned)
			seal := func(words string) string {
				appendTo(t, transcriptOf(at), `{"type":"message","role":"user","content":"`+words+`"}`+"\n")
				sealed, err := eng.Seal(t.Context(), at, cellstore.TurnInfo{})
				mustNil(t, err)
				return sealed.Turn.ID
			}
			base := seal("recorded turn")
			if mode == "empty recorded head" {
				base = ""
			}
			view, err := s.Dir.Create(t.Context(), at.ID, directory.CellInit{Head: base})
			mustNil(t, err)
			mustNil(t, cellstore.NotePublished(at, base))
			seal("local one")
			seal("local two")
			if mode == "diverged" {
				view, err = s.Dir.Publish(t.Context(), at.ID, directory.Publish{Fence: view.Cell.Lease.Fence, OldHead: base, Head: "missing-remote-turn"})
				mustNil(t, err)
			}
			mustNil(t, s.Dir.Release(t.Context(), at.ID, view.Cell.Lease.Fence))
			if mode == "head moves at acquire" {
				s.Dir = storageOffMovingDirectory{Client: s.Dir}
			}
			before, err := os.ReadFile(transcriptOf(at))
			mustNil(t, err)
			turns, err := cellstore.Turns(at)
			mustNil(t, err)
			cont := s.Continuer(eng, TakeOptions{RootFor: func(string) string { return at.Root }})
			got, err := cont.Take(t.Context(), at.ID)
			refused := mode == "diverged" || mode == "head moves at acquire"
			if refused {
				if err == nil || err.Error() != "cannot continue this chat here while sync storage is off because this computer is missing saved turns; your local turns stay here" {
					t.Fatalf("refusal: %v", err)
				}
			} else if err != nil {
				t.Fatalf("continue: %v", err)
			}
			after, readErr := os.ReadFile(transcriptOf(at))
			mustNil(t, readErr)
			afterTurns, readErr := cellstore.Turns(at)
			mustNil(t, readErr)
			if !bytes.Equal(before, after) || !reflect.DeepEqual(turns, afterTurns) || store.writes != 0 {
				t.Fatalf("lost local history or wrote storage: turns=%d/%d writes=%d", len(turns), len(afterTurns), store.writes)
			}
			latest, err := s.Dir.Cell(t.Context(), at.ID)
			mustNil(t, err)
			if !refused && (got.Taken.Cell.Root != at.Root || latest.Cell.Lease.Device != dev.ID() || latest.Cell.Lease.Fence <= view.Cell.Lease.Fence || got.Taken.Driving.Fence != latest.Cell.Lease.Fence) {
				t.Fatalf("directory lease not acquired: %+v", latest.Cell.Lease)
			}
			if refused && latest.Cell.Lease.Expires != 0 {
				t.Fatal("refused continuation retained its new lease")
			}
		})
	}
}

type storageOffMovingDirectory struct{ directory.Client }

func (d storageOffMovingDirectory) Acquire(ctx context.Context, id string, opts directory.AcquireOpts) (directory.CellView, error) {
	got, err := d.Client.Acquire(ctx, id, opts)
	if err != nil {
		return got, err
	}
	return d.Client.Publish(ctx, id, directory.Publish{Fence: got.Cell.Lease.Fence, OldHead: got.Cell.Head, Head: "new-remote-turn"})
}

// B5 keeps the storage-on branch upload and remote-head restore through the existing take path.
func TestStorageOnContinueStillKeepsAndFetches(t *testing.T) {
	h := newTwoHomes(t)
	a := h.openA()
	a.mustSay("recorded turn")
	h.durable(h.cell.ID, h.cell)
	mustNil(t, a.drive.Close(t.Context()))
	base := headOf(t, h.cell)
	appendTo(t, transcriptOf(h.cell), `{"type":"message","role":"user","content":"local extra"}`+"\n")
	_, err := h.engine.Seal(t.Context(), h.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	counted := &storageOffStore{Store: h.a.Store}
	h.a.Store = counted
	got, err := h.continuerA().Take(t.Context(), h.cell.ID)
	mustNil(t, err)
	if got.Taken.Kept == "" || got.Taken.Driving.Head != base || counted.writes == 0 {
		t.Fatalf("on path changed: kept=%s head=%s writes=%d", got.Taken.Kept, got.Taken.Driving.Head, counted.writes)
	}
	raw, err := os.ReadFile(transcriptOf(h.cell))
	mustNil(t, err)
	if bytes.Contains(raw, []byte("local extra")) {
		t.Fatal("root did not restore the recorded head")
	}
}

// B3 discovers a mid-session pairing once and leaves never-paired homes silent.
func TestStorageRefreshDiscoversMidSessionPairing(t *testing.T) {
	celltest.Available(t)
	for _, solo := range []bool{false, true} {
		for _, initial := range []bool{false, true} {
			name := "missing identity/"
			if solo {
				name = "solo identity/"
			}
			if initial {
				name += "engine"
			} else {
				name += "window"
			}
			t.Run(name, func(t *testing.T) {
				var requests atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests.Add(1)
					if r.URL.Path != "/v1/store/stats" {
						t.Errorf("unexpected request %s", r.URL.Path)
					}
					_, _ = w.Write([]byte(`{"storage":"on"}`))
				}))
				defer server.Close()
				t.Setenv(URLVar, server.URL)
				home := t.TempDir()
				if solo {
					_, err := identity.EnsureSolo(home)
					mustNil(t, err)
				}
				p := storageProcessFor(home)
				timers := make(chan func(), 8)
				p.after = func(_ time.Duration, fn func()) func() bool { timers <- fn; return func() bool { return true } }
				stop := StartStorageRefresh(t.Context(), home, initial)
				defer stop()
				next := func() func() {
					select {
					case fn := <-timers:
						return fn
					case <-time.After(3 * time.Second):
						t.Fatal("pairing observation stopped")
						return nil
					}
				}
				first := next()
				if requests.Load() != 0 {
					t.Fatal("never-paired home made a request")
				}
				first()
				second := next()
				if requests.Load() != 0 {
					t.Fatal("never-paired home made a request on its next check")
				}
				id, err := identity.Ensure(home)
				mustNil(t, err)
				if solo {
					mustNil(t, identity.EndSolo(home))
				}
				second()
				p.check(true)
				again := StartStorageRefresh(t.Context(), home, false)
				defer again()
				if requests.Load() != 1 || StorageState(home, server.URL, id.ID()) != "on" {
					t.Fatalf("pairing requests=%d state=%s", requests.Load(), StorageState(home, server.URL, id.ID()))
				}
			})
		}
	}
}
