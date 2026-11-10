package syncsetup

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/handoff"
)

// The table enumerates the Cartesian product before filtering unreachable
// states. Each excluded cell retains its reason in the test output and notes.
type r9Cell struct {
	relay, local, service, turn, unpublished string
	impossible, want                         string
}

func r9Matrix() []r9Cell {
	var table []r9Cell
	for _, relay := range []string{"live", "revived", "lapsed", "released", "other_live", "other_released", "other_lapsed"} {
		for _, local := range []string{"holding", "expired", "self_released", "release_uncertain", "release_undone", "stopped", "acquire_uncertain"} {
			for _, service := range []string{"answering", "refusing", "hanging"} {
				for _, turn := range []string{"idle", "working"} {
					for _, unpublished := range []string{"none", "some"} {
						c := r9Cell{relay: relay, local: local, service: service, turn: turn, unpublished: unpublished}
						switch {
						case turn == "working" && local != "holding" && local != "release_undone":
							c.impossible = "A retired, releasing, stopped or acquiring writer cannot start a working turn."
						case local == "expired" && relay == "live":
							c.impossible = "A live original fence after observed expiry is the revived row."
						case local == "self_released" && relay == "live":
							c.impossible = "A live fence after confirmed release is the revived row."
						case unpublished == "some" && (local == "self_released" || local == "release_uncertain" || local == "acquire_uncertain"):
							c.impossible = "Release requires no pending turns, and released or uncertain acquisition admits no new work."
						case local == "stopped":
							c.want = "replace"
						case service != "answering" && local != "holding" && local != "release_undone":
							c.want = "unreachable"
						case service != "answering":
							c.want = "offline"
						case relay == "other_live" || relay == "other_released" || relay == "other_lapsed":
							c.want = "consent"
						case (local == "holding" || local == "release_undone") && (relay == "live" || relay == "revived"):
							c.want = "same"
						case local == "self_released" || local == "release_uncertain" || local == "acquire_uncertain":
							c.want = "acquire"
						default:
							c.want = "reopen"
						}
						table = append(table, c)
					}
				}
			}
		}
	}
	return table
}

func (c r9Cell) name() string {
	return c.relay + "/" + c.local + "/" + c.service + "/" + c.turn + "/" + c.unpublished
}

type r9Directory struct {
	directory.Client
	failure   error
	reads     int
	takes     []directory.AcquireOpts
	notices   chan string
	publishes []directory.Publish
}

func (d *r9Directory) Cell(ctx context.Context, id string) (directory.CellView, error) {
	d.reads++
	if d.failure != nil {
		return directory.CellView{}, d.failure
	}
	return d.Client.Cell(ctx, id)
}

func (d *r9Directory) Acquire(ctx context.Context, id string, opt directory.AcquireOpts) (directory.CellView, error) {
	d.takes = append(d.takes, opt)
	if d.failure != nil {
		return directory.CellView{}, d.failure
	}
	return d.Client.Acquire(ctx, id, opt)
}

func (d *r9Directory) Publish(ctx context.Context, id string, in directory.Publish) (directory.CellView, error) {
	if d.failure != nil {
		return directory.CellView{}, d.failure
	}
	d.publishes = append(d.publishes, in)
	return d.Client.Publish(ctx, id, in)
}

func r9Fixture(t *testing.T, c r9Cell) (*Drive, *r9Directory) {
	t.Helper()
	clock := directorytest.NewFakeClock()
	d, memory := sameComputerDriveWithDirectory(t, clock.Now)
	d.idleNotices.Store(true)
	d.devices = &DeviceObserver{home: d.sync.Home, binding: deviceBindingFor(Resolve(d.sync.Home).URL, d.sync.Identity, d.sync.Device), state: DeviceSnapshot{Complete: true, Devices: map[string]directory.Device{"A": {}, "B": {}}}}
	d.batcher.Now = clock.Now
	mustNil(t, os.MkdirAll(filepath.Join(d.cell.Root, ".cell"), 0700))
	engine := cellsync.NewFakeEngine(t.TempDir())
	metadata := `{"V":1,"class":"files-only","cell_key_id":"00000000000000000000000000000000"}`
	saved := engine.Seal(d.cell, map[string]string{"journal": "saved", cell.MetaPath: metadata})
	adopt := func(head string) {
		t.Helper()
		mustNil(t, os.WriteFile(filepath.Join(d.cell.Root, ".cell", "adopted.json"), []byte(fmt.Sprintf(`{"V":1,"head":%q,"turns_size":0}`, head)), 0600))
	}
	adopt(saved)
	d.batcher.Publisher.Engine, d.batcher.Publisher.Store = engine, blobstore.NewMemory()
	d.batcher.Brancher = cellsync.Brancher{Dir: d.sync.Dir, Publisher: d.batcher.Publisher, NewID: func() (string, error) { return "kept-chat", nil }}
	err := d.batcher.Publisher.Publish(t.Context(), d.batcher.Driving, saved, cellsync.PublishInfo{})
	mustNil(t, err)
	raw := d.sync.Dir
	dir := &r9Directory{Client: raw, notices: make(chan string, 8)}
	d.opt.OnNotice = func(line string) { dir.notices <- line }
	d.sync.Dir, d.batcher.Publisher.Dir = dir, dir
	switch c.local {
	case "expired":
		clock.Advance(directory.LeaseTTL + time.Second)
		mustNil(t, d.CheckLease(t.Context()))
	case "self_released", "release_uncertain", "acquire_uncertain":
		// The lower release door establishes the real renewal state without
		// invoking certification or an engine process in each matrix cell.
		lower := &r8ReleaseDirectory{Client: raw}
		if c.local != "self_released" {
			lower.failure = context.DeadlineExceeded
		}
		d.batcher.Publisher.Dir = lower
		_, _ = d.batcher.ReleaseIdle(t.Context(), saved, func() error { return nil })
		d.released.Store(true)
		d.availableAfterWithin(t.Context(), d.sendLeaseSnapshot(), directory.Lease{})
		d.batcher.Publisher.Dir = dir
		if c.local == "acquire_uncertain" {
			dir.failure = context.DeadlineExceeded
			seen, readErr := raw.Cell(t.Context(), d.cell.ID)
			mustNil(t, readErr)
			_, _ = d.batcher.Reacquire(t.Context(), seen)
			dir.failure = nil
			d.uncertainAcquire(t.Context())
		}
	case "release_undone":
		lower := &r8ReleaseDirectory{Client: raw, failure: &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}}
		d.batcher.Publisher.Dir = lower
		_, err = d.batcher.ReleaseIdle(t.Context(), saved, func() error { return nil })
		if !errors.Is(err, syscall.ECONNREFUSED) {
			t.Fatal(err)
		}
		d.batcher.Publisher.Dir = dir
	case "stopped":
		d.stopped = true
	}
	if c.relay == "revived" {
		clock.Advance(directory.LeaseTTL + time.Second)
		_, err = raw.Heartbeat(t.Context(), d.cell.ID, directory.Beat{Fence: 1})
		mustNil(t, err)
	}
	if c.relay == "lapsed" {
		if c.local == "self_released" {
			_, err = raw.Heartbeat(t.Context(), d.cell.ID, directory.Beat{Fence: 1})
			mustNil(t, err)
		}
		clock.Advance(directory.LeaseTTL + time.Second)
	}
	if c.relay == "released" {
		mustNil(t, raw.Release(t.Context(), d.cell.ID, 1))
	}
	if c.relay == "other_live" || c.relay == "other_released" || c.relay == "other_lapsed" {
		other := memory.For("B")
		v, takeErr := other.Acquire(t.Context(), d.cell.ID, directory.AcquireOpts{Force: true, Expect: &directory.LeaseExpectation{Device: d.sync.Device.ID(), Fence: 1}})
		mustNil(t, takeErr)
		if c.relay == "other_released" {
			mustNil(t, other.Release(t.Context(), d.cell.ID, v.Cell.Lease.Fence))
		} else if c.relay == "other_lapsed" {
			clock.Advance(directory.LeaseTTL + time.Second)
		}
	}
	if c.unpublished == "some" {
		// A turn admitted before the outage can finish with unpublished local work.
		head := engine.Seal(d.cell, map[string]string{"journal": "finished during the outage", cell.MetaPath: metadata})
		adopt(head)
		d.batcher.Note(cellstore.Turn{ID: head})
	}
	switch c.service {
	case "refusing":
		dir.failure = fmt.Errorf("%w: %w", directory.ErrUnreachable, &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED})
	case "hanging":
		dir.failure = context.DeadlineExceeded
	}
	d.Working(c.turn == "working")
	dir.reads, dir.takes = 0, nil
	return d, dir
}

func TestIdleRelease_R9_RevivalRequiresANewFence(t *testing.T) {
	d, dir := r9Fixture(t, r9Cell{relay: "revived", local: "expired", service: "answering", turn: "idle"})
	confirmed, err := d.ConfirmWork(t.Context())
	if err != nil || confirmed || d.Gate() == nil || d.batcher.Fence() != 1 || len(dir.takes) != 0 {
		t.Fatalf("retired drive must await reopen: confirmed=%v error=%v gate=%v fence=%d takes=%+v", confirmed, err, d.Gate(), d.batcher.Fence(), dir.takes)
	}
	if _, _, held := d.batcher.RenewalStatus(); held {
		t.Fatal("observing expiry left renewal active")
	}
}

func TestIdleRelease_R9_StateMatrix(t *testing.T) {
	for _, c := range r9Matrix() {
		t.Run(c.name(), func(t *testing.T) {
			if c.impossible != "" {
				t.Skip(c.impossible)
			}
			d, dir := r9Fixture(t, c)
			if c.want == "reopen" {
				seen, err := dir.Client.Cell(t.Context(), d.cell.ID)
				mustNil(t, err)
				if d.workOwnership(seen, 1) != workReopen {
					t.Fatal("ordinary lapse or outside release must require reopen")
				}
				confirmed, err := d.ConfirmWork(t.Context())
				if err != nil || confirmed || d.Gate() == nil {
					t.Fatalf("the existing drive admitted a reopen cell: confirmed=%v error=%v gate=%v", confirmed, err, d.Gate())
				}
			}
			err := d.ConfirmPreparedWork(t.Context())

			head, headErr := cellstore.Head(d.cell)
			mustNil(t, headErr)
			switch c.want {
			case "same", "offline", "acquire":
				if err != nil || d.Gate() != nil {
					t.Fatalf("want %s admission, got %v (gate %v)", c.want, err, d.Gate())
				}
				wantFence := uint64(1)
				if c.want == "acquire" {
					wantFence++
					if len(dir.takes) != 1 || !dir.takes[0].RequireConditional || dir.takes[0].Force || dir.takes[0].Expect == nil || dir.takes[0].Expect.Device != d.sync.Device.ID() || dir.takes[0].Expect.Fence != 1 {
						t.Fatalf("retake lost its exact expectation: %+v", dir.takes)
					}
				} else if len(dir.takes) != 0 {
					t.Fatalf("retained hold acquired again: %+v", dir.takes)
				}
				_, fence, receiptErr := d.sync.SendLease(d.cell.ID)
				if receiptErr != nil || fence != wantFence || d.batcher.Fence() != wantFence {
					t.Fatalf("admitted receipt=%d, %v; writer=%d, want %d", fence, receiptErr, d.batcher.Fence(), wantFence)
				}
			case "reopen":
				if err == nil || d.Gate() == nil || len(dir.takes) != 0 || d.batcher.Fence() != 1 {
					t.Fatalf("lapsed or outside-released drive must refuse until reopen: error=%v gate=%v takes=%+v", err, d.Gate(), dir.takes)
				}
			case "replace":
				if !errors.Is(err, cellstore.ErrReadOnly) {
					t.Fatalf("stopped writer must require replacement: %v", err)
				}
			case "consent":
				if err == nil || len(dir.takes) != 0 || d.Gate() == nil {
					t.Fatalf("another holder bypassed consent: %v, takes=%+v", err, dir.takes)
				}
				var refusal *executor.WorkRefusal
				if !errors.As(err, &refusal) || refusal.HolderID != "B" || refusal.Retryable {
					t.Fatalf("fresh takeover must name its holder instead of replaying uncertainty: %v", err)
				}
				<-dir.notices
			case "unreachable":
				if err == nil || err.Error() != chatlist.TakeBackUnreachable || len(dir.takes) != 0 {
					t.Fatalf("unheld outage needs honest retry: %v, takes=%+v", err, dir.takes)
				}
				// The next Enter uses the same writer and fresh ownership facts.
				dir.failure = nil
				retry := d.ConfirmPreparedWork(t.Context())
				other := c.relay == "other_live" || c.relay == "other_released" || c.relay == "other_lapsed"
				reopen := c.local == "expired" && !other
				if (retry != nil) != (other || reopen) {
					t.Fatalf("recovery retained a cached refusal: %v", retry)
				}
				if other {
					<-dir.notices
				}
			}
			if c.unpublished == "some" {
				// Closing exercises the real drain after admission, retry, replacement or consent.
				dir.failure = nil
				before, readErr := dir.Client.Cell(t.Context(), d.cell.ID)
				mustNil(t, readErr)
				closeErr := d.batcher.Close(t.Context())
				if c.want == "reopen" || c.local == "expired" && !strings.HasPrefix(c.relay, "other_") {
					if !errors.Is(closeErr, cellstore.ErrReadOnly) || d.batcher.Pending() != 1 {
						t.Fatalf("retired writer must honestly retain its pending save: error=%v pending=%d", closeErr, d.batcher.Pending())
					}
					r9TakePending(t, d, dir, head.Turn.ID)
				} else {
					mustNil(t, closeErr)
				}
				after, readErr := dir.Client.Cell(t.Context(), d.cell.ID)
				mustNil(t, readErr)
				other := c.relay == "other_live" || c.relay == "other_released" || c.relay == "other_lapsed"
				if other {
					kept, keptErr := dir.Client.Cell(t.Context(), "kept-chat")
					if keptErr != nil || kept.Cell.Head != head.Turn.ID || kept.Cell.ParentCell != d.cell.ID || after.Cell.Head != before.Cell.Head || after.Cell.Lease != before.Cell.Lease {
						t.Fatalf("takeover lost kept turns or changed the winner: kept=%+v error=%v after=%+v", kept, keptErr, after)
					}
				} else if c.want != "reopen" && c.local != "expired" && after.Cell.Head != head.Turn.ID {
					t.Fatalf("unpublished turns stranded: relay=%s local=%s", after.Cell.Head, head.Turn.ID)
				}
				if c.want != "reopen" && c.local != "expired" && d.batcher.Pending() != 0 {
					t.Fatal("completed preservation left turns pending")
				}
				for _, in := range dir.publishes {
					if (c.want == "acquire" || c.local == "expired" || c.local == "self_released" || c.local == "release_uncertain" || c.local == "acquire_uncertain") && in.Fence == 1 {
						t.Fatal("retired fence published pending turns")
					}
				}
			}
		})
	}
}

// r9TakeLocal supplies the fake engine's sealed-head facts to the real taker.
// There are no unsealed edits or path bindings in this fixture.
type r9TakeLocal struct{ head string }

func (l r9TakeLocal) Unpublished(_ cell.Cell, remote string) (string, uint32, error) {
	if l.head == remote {
		return "", 0, nil
	}
	return l.head, 1, nil
}

func (r9TakeLocal) Dirty(context.Context, cell.Cell) (bool, error) { return false, nil }
func (r9TakeLocal) Seal(context.Context, cell.Cell) (string, uint32, error) {
	return "", 0, errors.New("the fixture has no unsealed edits")
}
func (r9TakeLocal) Follow(context.Context, cell.Cell, cell.Cell) error { return nil }

func r9TakePending(t *testing.T, d *Drive, dir *r9Directory, head string) {
	t.Helper()
	seen, err := dir.Client.Cell(t.Context(), d.cell.ID)
	mustNil(t, err)
	pub := d.batcher.Publisher
	brancher := d.batcher.Brancher
	taker := handoff.Taker{
		Dir:     dir,
		Local:   r9TakeLocal{head: head},
		Fetch:   &cellsync.Fetcher{Engine: pub.Engine, Store: pub.Store, Dir: dir, Inbox: func(cell.Cell) string { return t.TempDir() }},
		RootFor: func(string) string { return d.cell.Root },
		Branch: func(ctx context.Context, from *cellsync.Driving, head string, turns uint32) (string, error) {
			return brancher.Branch(ctx, from, head, turns, cellsync.PublishInfo{})
		},
	}
	taken, err := taker.TakeSeen(t.Context(), d.cell.ID, &directory.LeaseExpectation{Device: seen.Cell.Lease.Device, Fence: seen.Cell.Lease.Fence})
	mustNil(t, err)
	kept, err := dir.Client.Cell(t.Context(), taken.Kept)
	mustNil(t, err)
	if kept.Cell.Head != head || kept.Cell.ParentCell != d.cell.ID || kept.Cell.OrphanTurns != 1 || taken.Driving.Fence <= seen.Cell.Lease.Fence || taken.Driving.Head != seen.Cell.Head {
		t.Fatalf("normal reopen stranded pending turns: taken=%+v kept=%+v", taken, kept)
	}
	engine := cellsync.NewFakeEngine(t.TempDir())
	copy := cell.Cell{ID: taken.Kept, Root: t.TempDir()}
	fetcher := &cellsync.Fetcher{Engine: engine, Store: pub.Store, Dir: dir, Inbox: func(cell.Cell) string { return t.TempDir() }}
	mustNil(t, fetcher.Fetch(t.Context(), copy, kept.Cell.Head))
	words, err := os.ReadFile(filepath.Join(copy.Root, "journal"))
	if err != nil || string(words) != "finished during the outage" {
		t.Fatalf("kept turns did not reach shared storage: words=%q error=%v", words, err)
	}
}

func TestIdleRelease_R9_RevivedFenceReopensThroughTheRealContinuer(t *testing.T) {
	for _, unpublished := range []string{"none", "some"} {
		t.Run(unpublished, func(t *testing.T) {
			h := newTwoHomes(t)
			chat := idleContractChat(t, h)
			d := chat.drive
			// Freeze background traffic so expiry and revival are explicit observations.
			d.cancel()
			<-d.done
			h.clock.advance(directory.LeaseTTL + time.Second)
			mustNil(t, d.CheckLease(t.Context()))
			if d.Gate() == nil {
				t.Fatal("observed expiry left work admitted")
			}
			_, err := h.a.Dir.Heartbeat(t.Context(), h.cell.ID, directory.Beat{Fence: d.batcher.Fence()})
			mustNil(t, err)
			seen, err := h.a.Dir.Cell(t.Context(), h.cell.ID)
			mustNil(t, err)
			if confirmed, err := d.ConfirmWork(t.Context()); err != nil || confirmed || d.Gate() == nil {
				t.Fatalf("revived old drive admitted work: confirmed=%v error=%v gate=%v", confirmed, err, d.Gate())
			}
			if unpublished == "some" {
				appendTo(t, transcriptOf(h.cell), "{\"type\":\"message\",\"role\":\"user\",\"content\":\"finished during the outage\"}\n")
				_, err := d.Store(h.engine).Seal(t.Context(), h.cell, cellstore.TurnInfo{Calls: []cellstore.Executed{{Call: cellstore.Call{Tool: "earlier", Started: 0}}}})
				mustNil(t, err)
			}
			closeErr := d.Close(t.Context())
			if unpublished == "some" {
				if closeErr == nil {
					t.Fatal("retired close did not report its pending save")
				}
			} else {
				mustNil(t, closeErr)
			}
			got, err := h.continuerA().TakeSeen(t.Context(), h.cell.ID, &directory.LeaseExpectation{Device: seen.Cell.Lease.Device, Fence: seen.Cell.Lease.Fence})
			mustNil(t, err)
			if got.AlreadyHere || got.Taken.Driving.Fence <= seen.Cell.Lease.Fence {
				t.Fatalf("retired own live fence reused the old writer: alreadyHere=%v fence=%d observed=%d", got.AlreadyHere, got.Taken.Driving.Fence, seen.Cell.Lease.Fence)
			}
			if unpublished == "some" {
				if got.Taken.Kept == "" || got.KeptTurns == 0 {
					t.Fatal("reopen did not keep the unpublished local turn")
				}
				kept, err := h.a.Dir.Cell(t.Context(), got.Taken.Kept)
				mustNil(t, err)
				if kept.Cell.ParentCell != h.cell.ID {
					t.Fatalf("kept turns lost their parent: %+v", kept.Cell)
				}
				// Normal continuation may seal remaining edits again before keeping them.
				// Fetch the resulting branch to verify the original words survived.
				engine := h.engB.Sync(cellstore.SyncKeys{CellKey: h.a.Identity.CellKey(), Dedup: h.a.Identity.DedupSecret()}, h.a.Ledger)
				scope := h.a.scope(got.Taken.Kept)
				defer scope.Settle()
				copy := cell.Cell{ID: got.Taken.Kept, Root: t.TempDir()}
				fetcher := cellsync.Fetcher{Engine: engine, Store: scope.Store, Dir: h.a.Dir, Inbox: engine.Inbox}
				mustNil(t, fetcher.Fetch(t.Context(), copy, kept.Cell.Head))
				words, err := os.ReadFile(transcriptOf(copy))
				if err != nil || !strings.Contains(string(words), "finished during the outage") {
					t.Fatalf("reopen lost unpublished words: journal=%q error=%v", words, err)
				}
			}
			replacement := h.openOn(h.a, h.engine, got.Taken.Cell, h.work, nameA)
			if replacement.drive == d || replacement.drive.Gate() != nil {
				t.Fatal("reopen failed to install an admitted replacement drive")
			}
			replacement.mustSay("first Enter after observed expiry")
			h.durable(h.cell.ID, got.Taken.Cell)
		})
	}
}
