package syncsetup

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/session"
)

// B5 permits stored-history reads when there is no local work to keep.
func TestStorageOffContinueReadsRecordedHistory(t *testing.T) {
	for _, behind := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent root", true: "clean behind"}[behind], func(t *testing.T) {
			h := newTwoHomes(t)
			a := h.openA()
			a.mustSay("first recorded turn")
			h.durable(h.cell.ID, h.cell)
			mustNil(t, a.drive.Close(t.Context()))
			if behind {
				got, err := h.continuerB().Take(t.Context(), h.cell.ID)
				mustNil(t, err)
				mustNil(t, h.b.Dir.Release(t.Context(), h.cell.ID, got.Taken.Driving.Fence))
				a = h.openA()
				a.mustSay("newer recorded turn")
				h.durable(h.cell.ID, h.cell)
				mustNil(t, a.drive.Close(t.Context()))
			}
			want, err := os.ReadFile(transcriptOf(h.cell))
			mustNil(t, err)
			if behind {
				local := cell.Cell{ID: h.cell.ID, Root: filepath.Join(h.rootsB, h.cell.ID)}
				view, err := h.b.Dir.Cell(t.Context(), h.cell.ID)
				mustNil(t, err)
				head, _, err := cellstore.Unpublished(local, view.Cell.Head)
				mustNil(t, err)
				dirty, err := h.continuerB().eng.Dirty(t.Context(), local)
				mustNil(t, err)
				if head != "" || dirty || headOf(t, local) == view.Cell.Head {
					t.Fatal("fixture must be clean and behind the recorded history")
				}
			}
			counted := &storageOffStore{Store: h.b.Store, off: true}
			h.b.Store = counted
			mustNil(t, SaveStorage(h.homeB, h.b.Relay, h.b.Identity.ID(), "off"))
			got, err := h.continuerB().Take(t.Context(), h.cell.ID)
			mustNil(t, err)
			actual, err := os.ReadFile(transcriptOf(got.Taken.Cell))
			mustNil(t, err)
			if !bytes.Equal(actual, want) || counted.writes != 0 {
				t.Fatalf("history=%q want=%q store writes=%d", actual, want, counted.writes)
			}
			// Fetching history while off must not make the window wait for an uploader either.
			storageOffRequireLocalReady(t, got)
		})
	}
}

// B5 carries every preserved turn through quit, expiry, approval and publication.
func TestContinueOffThenApprovePublishes(t *testing.T) {
	celltest.RequireEngine(t)
	for _, empty := range []bool{true, false} {
		t.Run(map[bool]string{true: "empty head", false: "extended head"}[empty], func(t *testing.T) {
			home := t.TempDir()
			id, err := identity.Ensure(home)
			mustNil(t, err)
			dev, err := identity.Device(home)
			mustNil(t, err)
			now := time.Now()
			dir := directory.NewMemory(func() time.Time { return now })
			defer dir.Close()
			store := &storageOffStore{Store: blobstore.NewMemory()}
			s := &Sync{Home: home, Relay: "http://127.0.0.1:1", Identity: id, Device: dev, Dir: dir.For(dev.ID()), Store: store, Ledger: "0123456789abcdef"}
			t.Setenv(URLVar, s.Relay)
			at, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.HostBound})
			mustNil(t, err)
			work := t.TempDir()
			mustNil(t, session.SaveMeta(at.Root, session.Meta{ID: at.ID, Workspace: work, Created: now}))
			eng := cellstore.EngineFor(work)
			eng.DataRoot = t.TempDir()
			t.Cleanup(cellstore.StopSpawned)
			seal := func(words string) string {
				appendTo(t, transcriptOf(at), `{"type":"message","role":"user","content":"`+words+`"}`+"\n")
				got, err := eng.Seal(t.Context(), at, cellstore.TurnInfo{})
				mustNil(t, err)
				return got.Turn.ID
			}
			base := ""
			if !empty {
				base = seal("recorded")
			}
			view, err := s.Dir.Create(t.Context(), at.ID, directory.CellInit{Head: base})
			mustNil(t, err)
			mustNil(t, cellstore.NotePublished(at, base))
			seal("preserved one")
			seal("preserved two")
			mustNil(t, s.Dir.Release(t.Context(), at.ID, view.Cell.Lease.Fence))
			mustNil(t, SaveStorage(home, s.Relay, id.ID(), "off"))
			store.off = true
			continued, err := s.Continuer(eng, TakeOptions{RootFor: func(string) string { return at.Root }}).Take(t.Context(), at.ID)
			mustNil(t, err)
			preserved, err := os.ReadFile(transcriptOf(at))
			mustNil(t, err)
			again, err := s.Continuer(eng, TakeOptions{RootFor: func(string) string { return at.Root }}).Take(t.Context(), at.ID)
			mustNil(t, err)
			if !again.AlreadyHere {
				t.Fatal("current local hold was not reused")
			}
			storageOffRequireLocalReady(t, again)
			if store.writes != 0 {
				t.Fatalf("off writes=%d", store.writes)
			}
			// No drive runs while off, so quitting leaves the directory hold to expire.
			now = now.Add(directory.LeaseTTL + time.Second)
			store.off = false
			mustNil(t, SaveStorage(home, s.Relay, id.ID(), "on"))
			opened, err := cell.OpenAt(continued.Taken.Cell.Root, at.ID)
			mustNil(t, err)
			// A fresh owner reads the durable continuation result after the old process quits.
			reopened := &Sync{Home: home, Relay: s.Relay, Identity: id, Device: dev, Dir: dir.For(dev.ID()), Store: store, Ledger: s.Ledger}
			drive, err := reopened.Drive(t.Context(), eng, opened, DriveOptions{})
			mustNil(t, err)
			mustNil(t, drive.Close(t.Context()))
			view, err = s.Dir.Cell(t.Context(), at.ID)
			mustNil(t, err)
			if view.Cell.Head != headOf(t, at) || store.writes == 0 {
				t.Fatalf("approval left head=%s local=%s writes=%d", view.Cell.Head, headOf(t, at), store.writes)
			}
			restored := cell.Cell{ID: at.ID, Root: filepath.Join(t.TempDir(), at.ID)}
			receiver := eng
			receiver.DataRoot = t.TempDir()
			synced := receiver.Sync(cellstore.SyncKeys{CellKey: id.CellKey(), Dedup: id.DedupSecret()}, "fedcba9876543210")
			fetch := cellsync.Fetcher{Store: store.Store, Engine: synced, Inbox: synced.Inbox}
			mustNil(t, fetch.Fetch(t.Context(), restored, view.Cell.Head))
			raw, err := os.ReadFile(transcriptOf(restored))
			mustNil(t, err)
			if !bytes.Equal(raw, preserved) {
				t.Fatalf("publication changed preserved history: got %s want %s", raw, preserved)
			}
			for _, words := range []string{"preserved one", "preserved two"} {
				if !bytes.Contains(raw, []byte(words)) {
					t.Fatalf("published history lacks %q: %s", words, raw)
				}
			}
		})
	}
}

func storageOffRequireLocalReady(t *testing.T, got Continued) {
	t.Helper()
	raw, err := json.Marshal(got)
	mustNil(t, err)
	var readiness struct{ LocalOnly bool }
	mustNil(t, json.Unmarshal(raw, &readiness))
	if !readiness.LocalOnly {
		t.Fatal("storage-off history waits for an uploading writer")
	}
}

// B5 does not grant another computer's expired empty-head chat to this writer.
func TestAnotherDevicesEmptyHeadStaysViewer(t *testing.T) {
	r := newDriveRig(t)
	r.spokenFixture()
	_, err := r.b.Dir.Create(t.Context(), r.cell.ID, directory.CellInit{})
	mustNil(t, err)
	r.clock.advance(directory.LeaseTTL + time.Second)
	d, err := r.a.Drive(t.Context(), r.engine, r.cell, DriveOptions{})
	mustNil(t, err)
	defer d.Close(t.Context())
	if d.Gate() == nil {
		t.Fatal("another device's empty-head chat acquired a writer")
	}
}
