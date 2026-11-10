package syncsetup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
)

func r10OrderingMatrix() []r9Cell {
	var table []r9Cell
	for _, relay := range []string{"live", "lapsed"} {
		for _, turn := range []string{"idle", "working"} {
			for _, pending := range []string{"none", "some"} {
				for _, call := range []string{"no_call", "call_in_flight_at_expiry"} {
					for _, order := range []string{"observation_first", "own_beat_first", "restart_renew_holds_first"} {
						observers := []string{""}
						if order == "observation_first" {
							observers = []string{"check", "confirm"}
						}
						for _, observer := range observers {
							want := "same_fence_upload"
							if order == "observation_first" {
								want = "retire_keep_reopen"
								if observer == "confirm" {
									want = "retire_upload_reopen"
								}
							}
							table = append(table, r9Cell{relay: relay, local: "holding", service: "down_then_answering", turn: turn, unpublished: pending, inFlight: call, ordering: order, observer: observer, want: want})
						}
					}
				}
			}
		}
	}
	return table
}

func TestIdleRelease_R10_OrderingMatrix(t *testing.T) {
	for _, c := range r10OrderingMatrix() {
		t.Run(c.name(), func(t *testing.T) { r10Ordering(t, c) })
	}
}

// A beat runs on the real batcher while upload remains unavailable. Joining
// Run before lifting that failure chooses the revival event without a race.
type r10BeatDirectory struct {
	directory.Client
	beat chan struct{}
}

func (d r10BeatDirectory) Publish(context.Context, string, directory.Publish) (directory.CellView, error) {
	return directory.CellView{}, directory.ErrUnreachable
}

func (d r10BeatDirectory) Heartbeat(ctx context.Context, id string, in directory.Beat) (directory.CellView, error) {
	v, err := d.Client.Heartbeat(ctx, id, in)
	select {
	case d.beat <- struct{}{}:
	default:
	}
	return v, err
}

func r10OwnBeat(t *testing.T, d *Drive, dir *r9Directory) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	beat := make(chan struct{}, 1)
	tick := make(chan struct{}, 1)
	tick <- struct{}{}
	d.batcher.Publisher.Dir = r10BeatDirectory{Client: dir.Client, beat: beat}
	d.batcher.Sleep = func(ctx context.Context, duration time.Duration) error {
		if duration == directory.HeartbeatEvery {
			select {
			case <-tick:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		<-ctx.Done()
		return ctx.Err()
	}
	done := make(chan struct{})
	go func() { _ = d.batcher.Run(ctx); close(done) }()
	select {
	case <-beat:
	case <-time.After(5 * time.Second):
		cancel()
		<-done
		t.Fatal("batcher did not send its revival beat")
	}
	cancel()
	<-done
	d.batcher.Publisher.Dir, d.batcher.Sleep = dir, nil
}

func r10Ordering(t *testing.T, c r9Cell) {
	t.Helper()
	ctx := t.Context()
	clock := directorytest.NewFakeClock()
	d, dir := r9Fixture(t, r9Cell{relay: "live", local: "holding", service: "answering", turn: c.turn}, clock)
	engine := d.batcher.Publisher.Engine.(*cellsync.FakeEngine)
	first, err := dir.Client.Cell(ctx, d.cell.ID)
	mustNil(t, err)
	if c.inFlight != "no_call" {
		mustNil(t, d.ConfirmPreparedWork(ctx))
	}
	if c.relay == "lapsed" {
		clock.Advance(directory.LeaseTTL + time.Second)
	}
	dir.failure = directory.ErrUnreachable
	clock.Advance(directory.LeaseTTL + time.Second)
	mustNil(t, d.CheckLease(ctx))
	mustNil(t, d.ConfirmPreparedWork(ctx))
	if d.Gate() != nil {
		t.Fatal("an unsuccessful read locally expired the hold")
	}
	latest := first.Cell.Head
	turns := 0
	seal := func(words string) {
		latest = engine.Seal(d.cell, map[string]string{"journal": words, cell.MetaPath: `{"V":1,"class":"files-only","cell_key_id":"00000000000000000000000000000000"}`})
		mustNil(t, os.WriteFile(filepath.Join(d.cell.Root, ".cell", "adopted.json"), []byte(fmt.Sprintf(`{"V":1,"head":%q,"turns_size":0}`, latest)), 0600))
		d.batcher.Note(cellstore.Turn{ID: latest})
		turns++
	}
	if c.unpublished == "some" {
		seal("offline turn")
	}
	// The previously admitted call has not returned or sealed at the observation.
	dir.failure = nil
	if c.ordering == "observation_first" {
		if c.observer == "check" {
			mustNil(t, d.CheckLease(ctx))
		} else if ok, err := d.ConfirmWork(ctx); err != nil || ok {
			t.Fatalf("observed expiry admitted new work: %v %v", ok, err)
		}
		if d.Gate() == nil {
			t.Fatal("observed expiry did not retire admission")
		}
	} else if c.ordering == "own_beat_first" {
		r10OwnBeat(t, d, dir)
		mustNil(t, d.CheckLease(ctx))
		mustNil(t, d.ConfirmPreparedWork(ctx))
	} else {
		// The fake relay applies the same unchanged-fence renewal as restart's
		// authenticated renew_holds effect, before delivering an ownership read.
		_, err := dir.Client.Heartbeat(ctx, d.cell.ID, directory.Beat{Fence: first.Cell.Lease.Fence})
		mustNil(t, err)
		mustNil(t, d.CheckLease(ctx))
		mustNil(t, d.ConfirmPreparedWork(ctx))
	}
	if c.inFlight != "no_call" {
		seal("offline turn; late external call finished once")
	}
	mustNil(t, d.batcher.Close(ctx))
	after, err := dir.Client.Cell(ctx, d.cell.ID)
	mustNil(t, err)
	if after.Cell.Lease.Fence != first.Cell.Lease.Fence {
		t.Fatal("preservation changed the original fence")
	}
	if c.ordering != "observation_first" {
		if after.Cell.Head != latest || d.Gate() != nil {
			t.Fatal("revival did not upload at the retained fence")
		}
		if _, err := dir.Client.Cell(ctx, "kept-chat"); err == nil {
			t.Fatal("revival unexpectedly branched")
		}
	} else {
		wantHead := first.Cell.Head
		if c.observer == "confirm" {
			wantHead = latest
		}
		if after.Cell.Head != wantHead || d.Gate() == nil {
			t.Fatal("retirement overwrote the shared head or revived admission")
		}
		if turns > 0 && c.observer == "check" {
			kept, err := dir.Client.Cell(ctx, "kept-chat")
			mustNil(t, err)
			if kept.Cell.Head != latest || kept.Cell.ParentCell != d.cell.ID || kept.Cell.OrphanTurns == 0 {
				t.Fatalf("offline work not kept: %+v", kept)
			}
			listing, err := dir.Client.List(ctx)
			mustNil(t, err)
			found := false
			for _, row := range chatlist.Rows(listing, "B", nil) {
				if row.Cell == "kept-chat" {
					found = row.Status == chatlist.Branch && row.OrphanTurns > 0
				}
			}
			if !found {
				t.Fatal("B did not see the turns as kept")
			}
			copy := cell.Cell{ID: "kept-chat", Root: t.TempDir()}
			fetch := &cellsync.Fetcher{Engine: cellsync.NewFakeEngine(t.TempDir()), Store: d.batcher.Publisher.Store, Dir: dir, Inbox: func(cell.Cell) string { return t.TempDir() }}
			mustNil(t, fetch.Fetch(ctx, copy, latest))
			words, err := os.ReadFile(filepath.Join(copy.Root, "journal"))
			mustNil(t, err)
			want := "offline turn"
			if c.inFlight != "no_call" {
				want = "offline turn; late external call finished once"
			}
			if string(words) != want {
				t.Fatalf("B lost kept work: %q", words)
			}
		}
		if c.observer == "confirm" || turns == 0 {
			if _, err := dir.Client.Cell(ctx, "kept-chat"); err == nil {
				t.Fatal("cell unexpectedly branched")
			}
		}
		if ok, err := d.ConfirmWork(ctx); err != nil || ok {
			t.Fatal("retired drive admitted new work after preservation")
		}
		seen, err := dir.Client.Cell(ctx, d.cell.ID)
		mustNil(t, err)
		// The existing conditional reopen boundary installs a fresh drive; the
		// TUI matrix verifies the same Enter carries its box without a card.
		taken, err := dir.Acquire(ctx, d.cell.ID, directory.AcquireOpts{RequireConditional: true, Expect: &directory.LeaseExpectation{Device: seen.Cell.Lease.Device, Fence: seen.Cell.Lease.Fence}})
		mustNil(t, err)
		if taken.Cell.Lease.Fence != first.Cell.Lease.Fence+1 {
			t.Fatal("reopen did not advance the fence")
		}
	}
	if d.batcher.Pending() != 0 {
		t.Fatal("preservation stranded a sealed turn")
	}
}
