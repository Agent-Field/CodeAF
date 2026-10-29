package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// heldChatRelay counts every cell write, including creation, rather than only
// the heartbeat that first exposed the second opener's ownership side effect.
func heldChatRelay(t *testing.T, id string) (*syncsetup.Sync, *atomic.Int64) {
	t.Helper()
	service := relayserve.New(relayserve.Config{Store: t.TempDir()})
	writes := &atomic.Int64{}
	path := "/v1/dir/cells/" + id
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && (r.URL.Path == path || strings.HasPrefix(r.URL.Path, path+"/")) {
			writes.Add(1)
		}
		service.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(func() { server.Close(); _ = service.Close() })
	t.Setenv(syncsetup.URLVar, server.URL)
	t.Setenv(syncsetup.IntervalVar, "3600000")
	if _, err := identity.Ensure(home.Dir()); err != nil {
		t.Fatalf("make this computer's identity: %v", err)
	}
	if syncsetup.Quiet(home.Dir()) {
		t.Fatalf("the existing non-solo identity must permit relay requests")
	}
	s, ok, err := syncsetup.Open(home.Dir())
	if err != nil || !ok {
		t.Fatalf("open relay sync = %v, %v; want true, nil", ok, err)
	}
	return s, writes
}

type heldChatReceipt struct {
	path string
	raw  []byte
	info os.FileInfo
}

func snapshotHeldChatReceipt(t *testing.T, s *syncsetup.Sync, id string) heldChatReceipt {
	t.Helper()
	key := sha256.Sum256([]byte(s.Ledger + "/" + s.Device.ID() + "/" + id))
	path := filepath.Join(home.Dir(), "v3", "sync", "holds", fmt.Sprintf("%x", key))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read P1's hold receipt: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat P1's hold receipt: %v", err)
	}
	return heldChatReceipt{path: path, raw: raw, info: info}
}

func (before heldChatReceipt) check(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile(before.path)
	if err != nil {
		t.Fatalf("P1's hold receipt after P2 quit = %v; want the original receipt", err)
	}
	if !bytes.Equal(raw, before.raw) {
		t.Fatalf("P1's hold receipt = %q; want unchanged %q", raw, before.raw)
	}
	info, err := os.Stat(before.path)
	if err != nil {
		t.Fatalf("stat P1's hold receipt after P2 quit: %v", err)
	} else if !os.SameFile(before.info, info) {
		t.Fatalf("P2 rewrote P1's hold receipt; want the same file")
	}
}

func checkHeldChatLease(t *testing.T, s *syncsetup.Sync, id string, before directory.CellView) {
	t.Helper()
	after, err := s.Dir.Cell(t.Context(), id)
	if err != nil {
		t.Fatalf("read the held chat after P2 quit: %v", err)
	}
	if after.Cell.Lease.Device != before.Cell.Lease.Device || after.Cell.Lease.Fence != before.Cell.Lease.Fence {
		t.Fatalf("lease after P2 quit = %s fence %d; want %s fence %d", after.Cell.Lease.Device, after.Cell.Lease.Fence, before.Cell.Lease.Device, before.Cell.Lease.Fence)
	}
	if after.Cell.Lease.Expires == 0 || after.Cell.Lease.Expires != before.Cell.Lease.Expires {
		t.Fatalf("lease expiry after P2 quit = %d; want unchanged, unreleased %d", after.Cell.Lease.Expires, before.Cell.Lease.Expires)
	}
	if after.Cell.Head != before.Cell.Head {
		t.Fatalf("relay head after P2 quit = %q; want %q", after.Cell.Head, before.Cell.Head)
	}
}

func refuseHeldChatOpen(t *testing.T, cfg session.Config, writes *atomic.Int64) session.Config {
	t.Helper()
	_, seated, _, err := openV3Agent(cfg, cfg.Place.Workspace, session.New)
	var held *sessionHeldElsewhere
	if !errors.As(err, &held) {
		t.Fatalf("P2 open = %v; want sessionHeldElsewhere from the real journal lock", err)
	}
	// On the unfixed product startup is asynchronous. Observe its first write
	// before quitting; closeAll then joins startup and deterministically releases it.
	deadline := time.Now().Add(2 * time.Second)
	for writes.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	syncDrives.closeAll()
	return seated
}

func TestAChatAnotherProcessHoldsIsNeitherRenewedNorReleasedFromHere(t *testing.T) {
	celltest.Available(t)
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv("HOME", t.TempDir())
	// A missing explicit binary prevents even an installed engine from being
	// used by the failed opener's background warm capture. P1 needs no seals.
	t.Setenv("CODEAF_FURROW", filepath.Join(t.TempDir(), "absent-engine"))
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatalf("create the held chat: %v", err)
	}
	place := session.Place{Dir: c.Root, Workspace: t.TempDir()}
	cfg := session.Config{Place: place, Workspace: place.Workspace, SessionFile: place.Transcript(), Model: "stub/chat", APIKey: "stub", BaseURL: "http://127.0.0.1:1"}
	journal, err := os.OpenFile(place.Transcript(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatalf("open P1's journal: %v", err)
	}
	defer journal.Close()
	if err := filelock.Lock(journal, true, true); err != nil {
		t.Fatalf("hold P1's journal with the session's exclusive nonblocking flock: %v", err)
	}
	defer filelock.Unlock(journal)
	s, writes := heldChatRelay(t, c.ID)
	if _, err := s.Dir.Create(t.Context(), c.ID, directory.CellInit{}); err != nil {
		t.Fatalf("create P1's relay record and lease: %v", err)
	}
	prior := syncDrives
	firstBook := &driveBook{}
	syncDrives = firstBook
	defer func() { syncDrives.closeAll(); syncDrives = prior }()
	// P1 resumes the relay's fence and records its receipt through the real
	// Drive. Its sleeper holds all renewal ticks until Close cancels them.
	first := newLiveDrive(func(func()) (*syncsetup.Drive, error) {
		return s.Drive(t.Context(), cellstore.Engine{}, c, syncsetup.DriveOptions{Sleep: func(ctx context.Context, _ time.Duration) error {
			<-ctx.Done()
			return ctx.Err()
		}})
	})
	firstBook.drive = map[string]*liveDrive{c.ID: first}
	defer firstBook.closeAll()
	if err := first.Gate(); err != nil {
		t.Fatalf("P1's initial gate: %v", err)
	}
	writer := first.started()
	if writer == nil || !writer.HoldsLease(t.Context()) {
		t.Fatalf("P1 must start a real writer holding the relay lease")
	}
	before, err := s.Dir.Cell(t.Context(), c.ID)
	if err != nil || before.Cell.Lease.Fence < 1 || before.Cell.Lease.Expires <= before.Now {
		t.Fatalf("P1's initial relay lease = %+v, %v; want a live fence", before, err)
	}
	receipt := snapshotHeldChatReceipt(t, s, c.ID)
	writes.Store(0)
	syncDrives = &driveBook{}
	seated := refuseHeldChatOpen(t, cfg, writes)
	// Drain only this failed opener's warm capture before removing its files.
	// Warm preparation screens locally and does not require a sealing binary.
	_ = executor.Drain(t.Context(), seated.Seat)
	if got := writes.Load(); got != 0 {
		t.Fatalf("P2 made %d lease writes while opening and quitting a held chat; want 0", got)
	}
	checkHeldChatLease(t, s, c.ID, before)
	receipt.check(t)
	if !writer.HoldsLease(t.Context()) {
		t.Fatalf("P1 lost its lease when P2 quit")
	}
	if err := writer.Gate(); err != nil {
		t.Fatalf("P1's next work gate = %v; want nil", err)
	}
	confirmed, err := writer.ConfirmWork(t.Context())
	if err != nil || !confirmed {
		t.Fatalf("P1's next work confirmation = %v, %v; want true, nil", confirmed, err)
	}
	if d := syncDrives.renewalsOf(c.ID); d != nil && d.started() != nil {
		t.Fatalf("P2 retained a started drive for a chat whose journal it never held")
	}
}

func TestAQuitAtTheMoveQuestionLeavesTheOtherWindowsChatSendable(t *testing.T) {
	cfg, c := spokenDoorChat(t)
	s, writes := heldChatRelay(t, c.ID)
	firstBook := syncDrives
	defer firstBook.closeAll()
	agent, firstSeat, _, err := openV3Agent(cfg, cfg.Place.Workspace, session.New)
	if err != nil {
		t.Fatalf("open P1's seated chat: %v", err)
	}
	defer agent.Close()
	send := func(command string) {
		t.Helper()
		events, err := agent.SubmitBash(t.Context(), command)
		if err != nil {
			t.Fatalf("P1 send %q = %v; want admitted", command, err)
		}
		for event := range events {
			if event.Kind == session.EventError {
				t.Fatalf("P1 send %q reported %q; want no EventError", command, event.Text)
			}
		}
		if err := executor.Drain(t.Context(), firstSeat.Seat); err != nil {
			t.Fatalf("settle P1 send %q: %v", command, err)
		}
	}
	send("! printf fixture")
	deadline := time.Now().Add(15 * time.Second)
	var before directory.CellView
	for {
		head, headErr := cellstore.Head(c)
		before, err = s.Dir.Cell(t.Context(), c.ID)
		_, receiptFence, receiptErr := s.SendLease(c.ID)
		if headErr == nil && head != nil && err == nil && before.Cell.Head == head.Turn.ID && before.Cell.Lease.Fence > 0 && receiptErr == nil && receiptFence == before.Cell.Lease.Fence {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("P1's spoken turn did not publish: relay = %+v, %v; local head error = %v", before, err, headErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	receipt := snapshotHeldChatReceipt(t, s, c.ID)
	first := firstBook.renewalsOf(c.ID)
	writes.Store(0)
	syncDrives = &driveBook{}
	t.Cleanup(func() { syncDrives.closeAll(); syncDrives = firstBook })
	seated := refuseHeldChatOpen(t, cfg, writes)
	if err := executor.Drain(t.Context(), seated.Seat); err != nil {
		t.Fatalf("settle P2's unused seat: %v", err)
	}
	checkHeldChatLease(t, s, c.ID, before)
	receipt.check(t)
	send("! printf again")
	turns, err := cellstore.Turns(c)
	if err != nil || len(turns) == 0 {
		t.Fatalf("read P1's newest sealed turn = %d turns, %v", len(turns), err)
	}
	// A sealed turn names its writer by the device certificate's stamp, which
	// is what the drive's store seals with, not by the relay's device id.
	newest := turns[len(turns)-1]
	if newest.Device != s.Device.Cert.Device || newest.Fence != before.Cell.Lease.Fence {
		t.Fatalf("P1's next turn = %s fence %d; want %s fence %d", newest.Device, newest.Fence, s.Device.Cert.Device, before.Cell.Lease.Fence)
	}
	if first == nil || first.started() == nil || !first.started().HoldsLease(t.Context()) {
		t.Fatalf("P1's drive must still hold its original lease after its next send")
	}
}

// waitParkedIn waits until some goroutine is blocked on a channel inside fn,
// which is how a waiter on a parked drive's startup boundary looks.
func waitParkedIn(t *testing.T, fn string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		for _, g := range strings.Split(string(buf[:n]), "\n\n") {
			head, _, _ := strings.Cut(g, "\n")
			if (strings.Contains(head, "[chan receive") || strings.Contains(head, "[select")) && strings.Contains(g, fn) {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no goroutine is waiting inside %s on the parked startup", fn)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestAParkedChatDriveWaitsForItsJournalDecision(t *testing.T) {
	celltest.Available(t)
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv(syncsetup.URLVar, "http://relay.invalid")

	fixture := func(t *testing.T) (*driveBook, *liveDrive, *atomic.Int64, func(bool)) {
		t.Helper()
		calls := &atomic.Int64{}
		d := newAdmissionDrive(func(func()) (*syncsetup.Drive, error) {
			calls.Add(1)
			return nil, nil
		})
		book := &driveBook{drive: map[string]*liveDrive{"chat": d}}
		t.Cleanup(book.closeAll)
		got, claim := book.pendingDriveOf(cell.Cell{ID: "chat"}, cellstore.Engine{}, func(error) {})
		if got != d {
			t.Fatalf("the parked drive was replaced while its journal open was in flight")
		}
		return book, d, calls, claim
	}
	claimAgain := func(t *testing.T, book *driveBook, d *liveDrive) func(bool) {
		t.Helper()
		got, claim := book.pendingDriveOf(cell.Cell{ID: "chat"}, cellstore.Engine{}, func(error) {})
		if got != d {
			t.Fatalf("a second opener must share the existing drive")
		}
		return claim
	}
	waiters := func(t *testing.T, d *liveDrive) func() {
		t.Helper()
		gate, store := make(chan error, 1), make(chan error, 1)
		entered := make(chan struct{}, 2)
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		t.Cleanup(cancel)
		go func() { entered <- struct{}{}; gate <- d.Gate() }()
		go func() {
			entered <- struct{}{}
			chosen, used, err := (lateStore{live: d, engine: cellstore.Engine{Workspace: "local"}}).readyStore(ctx)
			if err == nil {
				local, ok := chosen.(cellstore.Engine)
				if !ok || local.Workspace != "local" || used != nil {
					err = fmt.Errorf("store = %T, drive present = %v; want the local engine and no drive", chosen, used != nil)
				}
			}
			store <- err
		}()
		<-entered
		<-entered
		// Both waiters must be parked on the startup boundary, not merely
		// scheduled: the goroutine dump names each one blocked inside its wait.
		waitParkedIn(t, "(*liveDrive).Gate")
		waitParkedIn(t, "lateStore.readyStore")
		select {
		case err := <-gate:
			t.Fatalf("parked gate returned %v before journal ownership was decided", err)
		case err := <-store:
			t.Fatalf("parked store returned %v before journal ownership was decided", err)
		default:
		}
		return func() {
			t.Helper()
			for _, waiter := range []<-chan error{gate, store} {
				select {
				case err := <-waiter:
					if err != nil {
						t.Fatalf("resolved parked waiter = %v; want nil", err)
					}
				case <-ctx.Done():
					t.Fatalf("journal ownership was decided but its parked waiter did not return")
				}
			}
		}
	}

	t.Run("a successful journal open starts once and releases both waiters", func(t *testing.T) {
		book, d, calls, claim := fixture(t)
		ready := d.pending()
		if ready == nil {
			t.Fatalf("a parked drive must already have its startup channel")
		}
		finish := waiters(t, d)
		d.try()
		if got := calls.Load(); got != 0 || d.pending() != ready {
			t.Fatalf("parked try called start %d times or replaced ready; want 0 and the original channel", got)
		}
		claim(true)
		claim(false)
		claim(true)
		finish()
		if got := calls.Load(); got != 1 {
			t.Fatalf("successful claim called start %d times; want 1", got)
		}
		if book.renewalsOf("chat") != d {
			t.Fatalf("the successful journal opener lost its drive")
		}
	})

	t.Run("every failed journal open abandons without starting", func(t *testing.T) {
		book, d, calls, first := fixture(t)
		second := claimAgain(t, book, d)
		finish := waiters(t, d)
		first(false)
		first(true)
		if d.pending() == nil {
			t.Fatalf("one failed opener abandoned a drive another opener still claims")
		}
		second(false)
		second(true)
		finish()
		if got := calls.Load(); got != 0 {
			t.Fatalf("failed journal opens called start %d times; want 0", got)
		}
		if book.renewalsOf("chat") != nil {
			t.Fatalf("the book retained the abandoned drive")
		}
		if _, found := workAdmissions.Load(d); found {
			t.Fatalf("the abandoned drive retained work admission state")
		}
	})

	for _, operation := range []string{"Close", "closeAll", "take", "closeCell", "takeCell"} {
		t.Run(operation+" abandons immediately and a later success cannot start", func(t *testing.T) {
			book, d, calls, claim := fixture(t)
			finish := waiters(t, d)
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			done := make(chan error, 1)
			go func() {
				switch operation {
				case "Close":
					done <- d.Close(ctx)
				case "closeAll":
					book.closeAll()
					done <- nil
				case "take":
					book.take()
					done <- nil
				case "closeCell":
					done <- book.closeCell("chat")
				case "takeCell":
					book.takeCell("chat")
					done <- nil
				}
			}()
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("closing parked drive = %v; want nil even with a canceled context", err)
				}
			case <-time.After(driveCloseWithin / 3):
				// A close that waited for the parked startup would take the
				// whole driveCloseWithin; a third of it is generous under load.
				t.Fatalf("closing a parked drive waited for startup or driveCloseWithin")
			}
			claim(true)
			d.try()
			finish()
			if got := calls.Load(); got != 0 {
				t.Fatalf("claim after close called start %d times; want 0", got)
			}
		})
	}

	for _, successFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("two openers keep one drive when success comes first %v", successFirst), func(t *testing.T) {
			book, d, calls, first := fixture(t)
			second := claimAgain(t, book, d)
			first(successFirst)
			second(!successFirst)
			if err := d.Gate(); err != nil {
				t.Fatalf("the successful opener's gate = %v; want nil", err)
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("two openers called start %d times; want 1", got)
			}
			if book.renewalsOf("chat") != d {
				t.Fatalf("a failed opener removed the successful opener's drive")
			}
			if _, found := workAdmissions.Load(d); !found {
				t.Fatalf("a failed opener removed the successful opener's work admission")
			}
		})
	}

	t.Run("a claim on startup already begun changes nothing", func(t *testing.T) {
		book, d, calls, first := fixture(t)
		first(true)
		if err := d.Gate(); err != nil {
			t.Fatalf("initial gate = %v; want nil", err)
		}
		claimAgain(t, book, d)(false)
		claimAgain(t, book, d)(true)
		d.mu.Lock()
		closed := d.closed
		d.mu.Unlock()
		if got := calls.Load(); got != 1 || closed || book.renewalsOf("chat") != d {
			t.Fatalf("claim on begun drive: starts = %d, closed = %v, kept = %v; want 1, false, true", got, closed, book.renewalsOf("chat") == d)
		}
	})
}
