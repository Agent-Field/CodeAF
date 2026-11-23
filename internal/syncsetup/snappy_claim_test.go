package syncsetup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The tests here are the contract of the open path's claim, written from what a
// person with two computers sees: taking a chat that fails to arrive never costs
// the other computer its chat; the window that is working here is never told to
// leave for a take that was refused; and the old order stays one switch away.

// unreadableStore is a relay store whose chunks cannot be fetched: the network,
// a bad chunk, a full disk. Uploads still work, so the other computer is unharmed.
type unreadableStore struct{ blobstore.Store }

var errNoChunks = fmt.Errorf("%w: the chunks cannot be read", blobstore.ErrNotFound)

func (unreadableStore) Get(context.Context, string) ([]byte, error)      { return nil, errNoChunks }
func (unreadableStore) GetFrame(context.Context, string) ([]byte, error) { return nil, errNoChunks }
func (unreadableStore) GetFrameBounded(context.Context, string, int64) ([]byte, error) {
	return nil, errNoChunks
}
func (unreadableStore) Has(context.Context, []string) ([]bool, error) { return nil, errNoChunks }
func (unreadableStore) GetMany(context.Context, []string) ([]blobstore.Object, error) {
	return nil, errNoChunks
}
func (unreadableStore) Locate(context.Context, []string) (map[string]blobstore.Location, error) {
	return nil, errNoChunks
}

// Contract S1: a take that fails before the chat opens leaves the computer that
// is driving the chat exactly as it was. Its lease is still its own, at the same
// fence, and its next turn is published and accepted. The take here is the
// kind that opens early (the chat shows before its files arrive), whose lease
// request used to be sent beside the fetch.
func TestS1FailedFetchLeavesTheDrivingComputerTheWriter(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	h := newTwoHomes(t)
	seedTree(t, h.work)
	a := h.openA()
	a.mustSay("S1 first turn on A")
	h.durable(h.cell.ID, h.cell)
	a.mustSay("S1 second turn on A, in the middle of the chat")
	h.durable(h.cell.ID, h.cell)

	before, err := h.a.Dir.Cell(t.Context(), h.cell.ID)
	mustNil(t, err)
	if before.Cell.Lease.Device != h.a.Device.ID() || before.Cell.Lease.Expires <= before.Now {
		t.Fatalf("setup: A should be driving the chat, lease %+v at %d", before.Cell.Lease, before.Now)
	}

	h.b.Store = unreadableStore{Store: h.b.Store}
	cont := h.continuerB()
	cont.opt.AsyncFiles = true // the take that shows the chat before its files
	_, err = cont.Take(t.Context(), h.cell.ID)
	if err == nil {
		t.Fatal("S1: the take succeeded although no chunk could be read")
	}

	after, err := h.a.Dir.Cell(t.Context(), h.cell.ID)
	mustNil(t, err)
	if after.Cell.Lease.Device != h.a.Device.ID() || after.Cell.Lease.Fence != before.Cell.Lease.Fence || after.Cell.Lease.Expires <= after.Now {
		t.Fatalf("S1: lease after B's failed take = %+v, want A still driving at fence %d", after.Cell.Lease, before.Cell.Lease.Fence)
	}
	// A's next turn is still accepted: it can publish.
	mustNil(t, a.say("S1 third turn on A, after B's failed take"))
	h.durable(h.cell.ID, h.cell)
	if err := a.drive.CheckLease(t.Context()); err != nil {
		t.Fatalf("S1: A lost its writer role to a take that never opened: %v", err)
	}
}

// Contract S1 (cost): a chat another computer is driving is fetched first and
// acquired after, so its take costs one round more than a free chat's: two
// serial rounds from a fresh view in hand (the fetch, then the lease), three when
// the directory has to be read first. No lease request is on the wire while the
// fetch runs.
func TestS1LiveHolderIsAcquiredAfterTheFetchInOneMoreRound(t *testing.T) {
	for _, cold := range []bool{false, true} {
		name := map[bool]string{false: "fresh view in hand", true: "directory read first"}[cold]
		t.Run(name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("CODEAF_TELEMETRY", "off")
			h := newTwoHomes(t)
			seedTree(t, h.work)
			a := h.openA()
			a.mustSay("S1 cost first turn on A")
			h.durable(h.cell.ID, h.cell)

			view, err := h.b.Dir.Cell(t.Context(), h.cell.ID)
			mustNil(t, err)
			if !cold {
				h.b.SupplyCellView(h.cell.ID, view)
			}

			rec := newRecordedRelay(t, h.b.Dir, h.b.Store, 400*time.Millisecond)
			rec.blockBackground()
			h.b.Dir = recordedDir{Client: h.b.Dir, rec: rec}
			h.b.Store = recordedStore{Store: h.b.Store, rec: rec}
			cont := h.continuerB()
			var shownRounds int
			var shownEvents []string
			cont.opt.OnShown = func(Continued) {
				shownRounds, _, _, _, shownEvents = rec.snapshot()
			}
			seen := &directory.LeaseExpectation{Device: view.Cell.Lease.Device, Fence: view.Cell.Lease.Fence}
			got, err := cont.TakeSeen(t.Context(), h.cell.ID, seen)
			mustNil(t, err)
			t.Cleanup(func() {
				rec.unblock()
				if got.Wait != nil {
					_ = got.Wait(context.Background())
				}
			})

			want := 2
			if cold {
				want = 3
			}
			t.Logf("S1 live holder, %s: shown after %d rounds; events=%v", name, shownRounds, shownEvents)
			if shownRounds != want {
				t.Fatalf("S1: take of a live chat (%s) showed after %d rounds, want %d; events=%v", name, shownRounds, want, shownEvents)
			}
			waves := rec.roundsOfEvents()
			acquire := -1
			for i, e := range shownEvents {
				if e == "Dir.Acquire" {
					acquire = i
					break
				}
			}
			if acquire <= 0 {
				t.Fatalf("S1: the lease of a live chat was asked for before anything was fetched, or not at all; events=%v", shownEvents)
			}
			for i := 0; i < acquire; i++ {
				if waves[i] >= waves[acquire] {
					t.Fatalf("S1: %s (round %d) was on the wire beside the lease request (round %d) of a chat somebody is driving; events=%v rounds=%v",
						shownEvents[i], waves[i], waves[acquire], shownEvents, waves)
				}
			}
		})
	}
}

// Contract S1 (speed): a chat nobody holds still shows after at most two serial
// rounds from a fresh view in hand, and its lease request travels beside the
// fetch, not after it.
func TestS1FreeChatShowsInTwoRoundsWithTheLeaseBesideTheFetch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	h := newTwoHomes(t)
	seedTree(t, h.work)
	a := h.openA()
	a.mustSay("S1 free first turn on A")
	h.durable(h.cell.ID, h.cell)
	mustNil(t, a.drive.Close(t.Context())) // A lets go: the chat is free

	view, err := h.b.Dir.Cell(t.Context(), h.cell.ID)
	mustNil(t, err)
	if view.Cell.Lease.Expires != 0 {
		t.Fatalf("setup: the chat should be released, lease %+v", view.Cell.Lease)
	}
	h.b.SupplyCellView(h.cell.ID, view)

	rec := newRecordedRelay(t, h.b.Dir, h.b.Store, 400*time.Millisecond)
	rec.blockBackground()
	h.b.Dir = recordedDir{Client: h.b.Dir, rec: rec}
	h.b.Store = recordedStore{Store: h.b.Store, rec: rec}
	cont := h.continuerB()
	var shownRounds int
	var shownEvents []string
	cont.opt.OnShown = func(Continued) {
		shownRounds, _, _, _, shownEvents = rec.snapshot()
	}
	got, err := cont.Take(t.Context(), h.cell.ID)
	mustNil(t, err)
	t.Cleanup(func() {
		rec.unblock()
		if got.Wait != nil {
			_ = got.Wait(context.Background())
		}
	})

	t.Logf("S1 free chat, fresh view in hand: shown after %d rounds; events=%v", shownRounds, shownEvents)
	if shownRounds == 0 || shownRounds > 2 {
		t.Fatalf("S1: take of a free chat from a fresh view showed after %d rounds, want 1..2; events=%v", shownRounds, shownEvents)
	}
	waves := rec.roundsOfEvents()
	acquire, beside := -1, false
	for i, e := range shownEvents {
		if e == "Dir.Acquire" {
			acquire = i
		}
	}
	if acquire < 0 {
		t.Fatalf("S1: no lease request before the chat showed; events=%v", shownEvents)
	}
	for i, e := range shownEvents {
		if strings.HasPrefix(e, "Store.") && waves[i] == waves[acquire] {
			beside = true
		}
	}
	if !beside {
		t.Fatalf("S1: the lease request of a free chat did not travel beside the fetch; events=%v rounds=%v", shownEvents, waves)
	}
}

// localWindow is another window of this computer holding the chat's journal the
// way a running engine does: the transcript's lock held, presence.json saying
// what it is doing, and a watch for anybody asking it to leave.
type localWindow struct {
	root  string
	asked atomic.Bool
	stop  chan struct{}
	file  *os.File
}

func holdLocalWindow(t *testing.T, root string, state session.PresenceState) *localWindow {
	t.Helper()
	f, err := os.Open(transcriptOf(cell.Cell{Root: root}))
	mustNil(t, err)
	mustNil(t, filelock.Lock(f, true, true))
	presence := fmt.Appendf(nil, `{"schema":1,"sessionId":%q,"pid":%d,"state":%q,"updatedAt":%q}`+"\n",
		filepath.Base(root), os.Getpid(), state, time.Now().UTC().Format(time.RFC3339Nano))
	mustNil(t, os.WriteFile(filepath.Join(root, "presence.json"), presence, 0o600))
	w := &localWindow{root: root, stop: make(chan struct{}), file: f}
	go func() {
		for {
			select {
			case <-w.stop:
				return
			case <-time.After(time.Millisecond):
				if _, err := os.Stat(session.TakeoverPath(root)); err == nil {
					w.asked.Store(true)
				}
			}
		}
	}()
	t.Cleanup(func() {
		close(w.stop)
		_ = filelock.Unlock(f)
		_ = f.Close()
		_ = os.Remove(filepath.Join(root, "presence.json"))
	})
	return w
}

// seesRequest notes a takeover request on the window's chat at the moment the
// take reads the directory, which on the open path is right after the keypress.
type seesRequest struct {
	directory.Client
	w      *localWindow
	cellFn func(context.Context, string) (directory.CellView, error)
}

func (d seesRequest) Cell(ctx context.Context, id string) (directory.CellView, error) {
	if _, err := os.Stat(session.TakeoverPath(d.w.root)); err == nil {
		d.w.asked.Store(true)
	}
	if d.cellFn != nil {
		return d.cellFn(ctx, id)
	}
	return d.Client.Cell(ctx, id)
}

// Contract S6: a take that is refused leaves the window that is working on this
// computer working. It is not asked to wind down for a take that never happened.
// A window that is only open (idle) may be asked early, so its hand-over overlaps
// the read, but the request is withdrawn the moment the take is refused.
func TestS6RefusedTakeNeverAsksAWorkingWindowToLeave(t *testing.T) {
	type refusal struct {
		name string
		prep func(h *twoHomes, w *localWindow)
		seen *directory.LeaseExpectation
		want func(error) bool
	}
	staleCard := &directory.LeaseExpectation{Device: "a-computer-that-no-longer-holds-it", Fence: 99}
	refusals := []refusal{
		{name: "the card is stale", seen: staleCard, want: func(err error) bool { return errors.Is(err, directory.ErrLeaseHeld) }},
		{
			name: "the relay cannot be reached",
			prep: func(h *twoHomes, w *localWindow) {
				h.a.Dir = seesRequest{Client: h.a.Dir, w: w, cellFn: func(context.Context, string) (directory.CellView, error) {
					return directory.CellView{}, directory.ErrUnreachable
				}}
			},
			want: func(err error) bool { return errors.Is(err, directory.ErrUnreachable) },
		},
	}
	for _, filesFirst := range []bool{false, true} {
		for _, state := range []session.PresenceState{session.PresenceWorking, session.PresenceWaiting, session.PresenceIdle} {
			for _, r := range refusals {
				name := fmt.Sprintf("%s/%s/switch=%v", state, r.name, filesFirst)
				t.Run(name, func(t *testing.T) {
					t.Setenv("HOME", t.TempDir())
					t.Setenv("CODEAF_TELEMETRY", "off")
					if filesFirst {
						t.Setenv(MoveFilesFirstVar, "1")
					}
					h := newTwoHomes(t)
					seedTree(t, h.work)
					a := h.openA()
					a.mustSay("S6 turn on A")
					h.durable(h.cell.ID, h.cell)
					mustNil(t, a.drive.Close(t.Context()))
					_ = os.Remove(filepath.Join(h.a.Home, "send-leases", h.cell.ID))

					w := holdLocalWindow(t, h.cell.Root, state)
					if r.prep != nil {
						r.prep(h, w)
					} else {
						h.a.Dir = seesRequest{Client: h.a.Dir, w: w}
					}
					_, err := h.continuerA().TakeSeen(t.Context(), h.cell.ID, r.seen)
					if err == nil || !r.want(err) {
						t.Fatalf("S6: take = %v, want the refusal %q", err, r.name)
					}

					// Whatever the window's state, nothing is left asking it to leave.
					if _, statErr := os.Stat(session.TakeoverPath(h.cell.Root)); statErr == nil {
						t.Fatalf("S6: a request to leave was still standing on the window after the refusal")
					}
					early := state == session.PresenceIdle && !filesFirst
					if w.asked.Load() && !early {
						t.Fatalf("S6: a %s window on this computer was asked to leave for a take that was refused", state)
					}
					holder, ok := session.ReadHolder(h.cell.Root, time.Now())
					if !ok || holder.State != state {
						t.Fatalf("S6: the window's own record is %+v (known %v), want it still %s", holder, ok, state)
					}
					if !session.InUse(transcriptOf(h.cell)) {
						t.Fatalf("S6: the window no longer holds its journal")
					}
				})
			}
		}
	}
}

// Contract S7: with CODEAF_MOVE_FILES_FIRST=1 the open takes the old road
// entirely. A view in hand, supplied by the list or cached from a listing, is not
// trusted: the take reads the directory first, then fetches, then asks for the
// lease, each after the one before it finished.
func TestS7MoveFilesFirstReadsFetchesAcquiresInSeries(t *testing.T) {
	for _, how := range []string{"supplied", "cached"} {
		t.Run(how, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("CODEAF_TELEMETRY", "off")
			t.Setenv(MoveFilesFirstVar, "1")
			h := newTwoHomes(t)
			seedTree(t, h.work)
			a := h.openA()
			a.mustSay("S7 turn on A")
			h.durable(h.cell.ID, h.cell)
			mustNil(t, a.drive.Close(t.Context())) // free, so a fast take would overlap its acquire

			switch how {
			case "supplied":
				view, err := h.b.Dir.Cell(t.Context(), h.cell.ID)
				mustNil(t, err)
				h.b.SupplyCellView(h.cell.ID, view)
			case "cached":
				listing, err := h.b.Dir.List(t.Context())
				mustNil(t, err)
				h.b.RememberViews(listing)
			}

			rec := newRecordedRelay(t, h.b.Dir, h.b.Store, 0)
			h.b.Dir = recordedDir{Client: h.b.Dir, rec: rec}
			h.b.Store = recordedStore{Store: h.b.Store, rec: rec}
			got, err := h.continuerB().Take(t.Context(), h.cell.ID)
			mustNil(t, err)
			if got.Wait != nil {
				mustNil(t, got.Wait(t.Context()))
			}

			_, _, _, _, events := rec.snapshot()
			waves := rec.roundsOfEvents()
			if len(events) == 0 || events[0] != "Dir.Cell" {
				t.Fatalf("S7: with the switch on, the first request is %v, want the directory read of the old road; events=%v", events, events)
			}
			acquire := -1
			for i, e := range events {
				if e == "Dir.Acquire" {
					acquire = i
					break
				}
			}
			if acquire < 0 {
				t.Fatalf("S7: no lease request; events=%v", events)
			}
			fetched := false
			for i := 0; i < acquire; i++ {
				if strings.HasPrefix(events[i], "Store.") {
					fetched = true
				}
				if waves[i] >= waves[acquire] {
					t.Fatalf("S7: %s (round %d) was on the wire beside the lease request (round %d); events=%v rounds=%v", events[i], waves[i], waves[acquire], events, waves)
				}
			}
			if !fetched {
				t.Fatalf("S7: the lease was asked for before anything was fetched; events=%v", events)
			}
		})
	}
}

// Contract S12: the person's choice to take a chat covers the holder and fence
// they were shown and nobody else. When the chat changed hands after the record
// the take acted on, the take is refused as a lost race, whether or not a card
// named the holder: the computer that took the chat meanwhile (a third computer,
// or another window of this one) keeps it, and its lease is not forced.
func TestS12ChatThatChangedHandsAfterTheReadingIsNeverForced(t *testing.T) {
	for _, who := range []string{"a third computer", "another window of this computer"} {
		t.Run(who, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("CODEAF_TELEMETRY", "off")
			h := newTwoHomes(t)
			seedTree(t, h.work)
			a := h.openA()
			a.mustSay("S12 turn on A")
			h.durable(h.cell.ID, h.cell)
			mustNil(t, a.drive.Close(t.Context())) // free: this is what B's reading shows

			view, err := h.b.Dir.Cell(t.Context(), h.cell.ID)
			mustNil(t, err)
			if view.Cell.Lease.Expires != 0 {
				t.Fatalf("setup: the chat should be released, lease %+v", view.Cell.Lease)
			}
			h.b.SupplyCellView(h.cell.ID, view)

			// The chat changes hands after that reading.
			var taker directory.Client
			var takerID string
			if who == "a third computer" {
				homeC := t.TempDir()
				_, err = identity.Adopt(homeC, h.a.Identity, false)
				mustNil(t, err)
				syncC := openSync(t, homeC)
				taker, takerID = syncC.Dir, syncC.Device.ID()
			} else {
				taker, takerID = h.b.Dir, h.b.Device.ID()
			}
			claimed, err := taker.Acquire(t.Context(), h.cell.ID, directory.AcquireOpts{})
			mustNil(t, err)

			cont := h.continuerB()
			_, err = cont.Take(t.Context(), h.cell.ID)
			if !errors.Is(err, directory.ErrLeaseHeld) {
				t.Fatalf("S12: take after %s took the chat = %v, want ErrLeaseHeld", who, err)
			}
			after, err := h.a.Dir.Cell(t.Context(), h.cell.ID)
			mustNil(t, err)
			if after.Cell.Lease.Device != takerID || after.Cell.Lease.Fence != claimed.Cell.Lease.Fence || after.Cell.Lease.Expires <= after.Now {
				t.Fatalf("S12: lease after the refused take = %+v, want %s still holding at fence %d", after.Cell.Lease, who, claimed.Cell.Lease.Fence)
			}
		})
	}
}
