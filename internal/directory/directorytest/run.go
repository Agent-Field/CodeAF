package directorytest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
)

// Devices returns the Client one device would hold. Every Client it returns
// shares one directory and the clock the factory was given.
type Devices func(device string) directory.Client

// Factory builds a fresh, empty directory that reads time from clock.
type Factory func(t *testing.T, clock *FakeClock) Devices

type env struct {
	clock *FakeClock
	as    Devices
}

const (
	cell = "01J0000000000000000000000A"
	devA = "dev_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	devB = "dev_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	head = "1111111111111111111111111111111111111111111111111111111111111111"
	next = "2222222222222222222222222222222222222222222222222222222222222222"
)

var ctx = context.Background()

// Run runs every conformance case against a fresh directory from factory.
func Run(t *testing.T, factory Factory) {
	for name, fn := range cases {
		t.Run(name, func(t *testing.T) {
			clock := NewFakeClock()
			fn(t, env{clock: clock, as: factory(t, clock)})
		})
	}
}

var cases = map[string]func(*testing.T, env){
	"CreateHoldsLease":               createHoldsLease,
	"CreateExists":                   createExists,
	"CellNotFound":                   cellNotFound,
	"AcquireRefusedWhileHeld":        acquireRefusedWhileHeld,
	"AcquireAfterExpiry":             acquireAfterExpiry,
	"AcquireOwnLeaseRaisesFence":     acquireOwnLeaseRaisesFence,
	"HeartbeatRenews":                heartbeatRenews,
	"LateHeartbeatSameFence":         lateHeartbeatSameFence,
	"HeartbeatWrongFenceRefused":     heartbeatWrongFenceRefused,
	"PublishMovesHead":               publishMovesHead,
	"PublishKeepsTitleWhenEmpty":     publishKeepsTitleWhenEmpty,
	"PublishStaleOldHeadRefused":     publishStaleOldHeadRefused,
	"PublishAfterTakeoverRefused":    publishAfterTakeoverRefused,
	"ReleaseFreesLeaseKeepsFence":    releaseFreesLeaseKeepsFence,
	"ReleaseStaleRefused":            releaseStaleRefused,
	"ArchiveIsIdempotent":            archiveIsIdempotent,
	"PutDeviceUpserts":               putDeviceUpserts,
	"SetVaultIsCompareAndSwap":       setVaultIsCompareAndSwap,
	"DirectoryClockStampsEverything": directoryClockStampsEverything,
	"ConcurrentAcquireOneWins":       concurrentAcquireOneWins,
}

func start(t *testing.T, e env) directory.CellView {
	t.Helper()
	v, err := e.as(devA).Create(ctx, cell, directory.CellInit{Head: head, Class: "chat", Size: 10, Title: "t"})
	must(t, err)
	return v
}

func expire(e env) { e.clock.Advance(directory.LeaseTTL + time.Second) }

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func wantErr(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

func get(t *testing.T, e env) directory.Cell {
	t.Helper()
	v, err := e.as(devA).Cell(ctx, cell)
	must(t, err)
	return v.Cell
}

func createHoldsLease(t *testing.T, e env) {
	v := start(t, e)
	l := v.Cell.Lease
	if l.Device != devA || l.Fence != 1 || l.Expires != v.Now+directory.LeaseTTL.Milliseconds() {
		t.Fatalf("lease = %+v at now %d", l, v.Now)
	}
	if v.Cell.DurableAt != v.Now || v.Cell.Head != head {
		t.Fatalf("cell = %+v", v.Cell)
	}
}

func createExists(t *testing.T, e env) {
	start(t, e)
	_, err := e.as(devB).Create(ctx, cell, directory.CellInit{Head: next})
	wantErr(t, err, directory.ErrExists)
}

func cellNotFound(t *testing.T, e env) {
	_, err := e.as(devA).Cell(ctx, cell)
	wantErr(t, err, directory.ErrNotFound)
	_, err = e.as(devA).Acquire(ctx, cell)
	wantErr(t, err, directory.ErrNotFound)
}

func acquireRefusedWhileHeld(t *testing.T, e env) {
	start(t, e)
	_, err := e.as(devB).Acquire(ctx, cell)
	wantErr(t, err, directory.ErrLeaseHeld)
}

func acquireAfterExpiry(t *testing.T, e env) {
	start(t, e)
	expire(e)
	v, err := e.as(devB).Acquire(ctx, cell)
	must(t, err)
	if v.Cell.Lease.Device != devB || v.Cell.Lease.Fence != 2 || v.Cell.Lease.Pending != 0 {
		t.Fatalf("lease = %+v", v.Cell.Lease)
	}
}

func acquireOwnLeaseRaisesFence(t *testing.T, e env) {
	start(t, e)
	v, err := e.as(devA).Acquire(ctx, cell)
	must(t, err)
	if v.Cell.Lease.Fence != 2 {
		t.Fatalf("fence = %d, want 2", v.Cell.Lease.Fence)
	}
}

func heartbeatRenews(t *testing.T, e env) {
	start(t, e)
	e.clock.Advance(directory.HeartbeatEvery)
	v, err := e.as(devA).Heartbeat(ctx, cell, directory.Beat{Fence: 1, Pending: 3})
	must(t, err)
	if v.Cell.Lease.Expires != v.Now+directory.LeaseTTL.Milliseconds() || v.Cell.Lease.Pending != 3 {
		t.Fatalf("lease = %+v at now %d", v.Cell.Lease, v.Now)
	}
}

func lateHeartbeatSameFence(t *testing.T, e env) {
	start(t, e)
	expire(e)
	v, err := e.as(devA).Heartbeat(ctx, cell, directory.Beat{Fence: 1})
	must(t, err)
	if v.Cell.Lease.Expires <= v.Now {
		t.Fatalf("lease not renewed: %+v at now %d", v.Cell.Lease, v.Now)
	}
}

func heartbeatWrongFenceRefused(t *testing.T, e env) {
	start(t, e)
	_, err := e.as(devA).Heartbeat(ctx, cell, directory.Beat{Fence: 9})
	wantErr(t, err, directory.ErrFenceStale)
	_, err = e.as(devB).Heartbeat(ctx, cell, directory.Beat{Fence: 1})
	wantErr(t, err, directory.ErrFenceStale)
}

func publishMovesHead(t *testing.T, e env) {
	start(t, e)
	e.clock.Advance(time.Second)
	v, err := e.as(devA).Publish(ctx, cell, directory.Publish{
		Fence: 1, OldHead: head, Head: next, Size: 20, Class: "chat", Title: "u", Pending: 2,
	})
	must(t, err)
	c := v.Cell
	if c.Head != next || c.Size != 20 || c.Title != "u" || c.Lease.Pending != 2 || c.DurableAt != v.Now {
		t.Fatalf("cell = %+v at now %d", c, v.Now)
	}
}

func publishKeepsTitleWhenEmpty(t *testing.T, e env) {
	start(t, e)
	v, err := e.as(devA).Publish(ctx, cell, directory.Publish{Fence: 1, OldHead: head, Head: next})
	must(t, err)
	if v.Cell.Title != "t" {
		t.Fatalf("title = %q, want t", v.Cell.Title)
	}
}

func publishStaleOldHeadRefused(t *testing.T, e env) {
	start(t, e)
	_, err := e.as(devA).Publish(ctx, cell, directory.Publish{Fence: 1, OldHead: next, Head: next})
	wantErr(t, err, directory.ErrHeadMoved)
	if got := get(t, e).Head; got != head {
		t.Fatalf("head moved to %s", got)
	}
}

// publishAfterTakeoverRefused is law L7: a superseded holder can neither move
// the head nor renew, and the new holder can.
func publishAfterTakeoverRefused(t *testing.T, e env) {
	start(t, e)
	expire(e)
	b, err := e.as(devB).Acquire(ctx, cell)
	must(t, err)
	if b.Cell.Lease.Fence != 2 {
		t.Fatalf("fence = %d, want 2", b.Cell.Lease.Fence)
	}
	_, err = e.as(devA).Publish(ctx, cell, directory.Publish{Fence: 1, OldHead: head, Head: next})
	wantErr(t, err, directory.ErrFenceStale)
	_, err = e.as(devA).Heartbeat(ctx, cell, directory.Beat{Fence: 1})
	wantErr(t, err, directory.ErrFenceStale)
	if got := get(t, e).Head; got != head {
		t.Fatalf("head moved under a stale fence to %s", got)
	}
	_, err = e.as(devB).Publish(ctx, cell, directory.Publish{Fence: 2, OldHead: head, Head: next})
	must(t, err)
}

func releaseFreesLeaseKeepsFence(t *testing.T, e env) {
	start(t, e)
	must(t, e.as(devA).Release(ctx, cell, 1))
	l := get(t, e).Lease
	if l.Expires != 0 || l.Pending != 0 || l.Fence != 1 || l.Device != devA {
		t.Fatalf("lease = %+v", l)
	}
	v, err := e.as(devB).Acquire(ctx, cell)
	must(t, err)
	if v.Cell.Lease.Fence != 2 {
		t.Fatalf("fence = %d, want 2", v.Cell.Lease.Fence)
	}
}

func releaseStaleRefused(t *testing.T, e env) {
	start(t, e)
	wantErr(t, e.as(devA).Release(ctx, cell, 7), directory.ErrFenceStale)
	wantErr(t, e.as(devB).Release(ctx, cell, 1), directory.ErrFenceStale)
}

func archiveIsIdempotent(t *testing.T, e env) {
	start(t, e)
	must(t, e.as(devA).Archive(ctx, cell))
	must(t, e.as(devA).Archive(ctx, cell))
	if !get(t, e).Archived {
		t.Fatal("cell not archived")
	}
	wantErr(t, e.as(devA).Archive(ctx, "missing"), directory.ErrNotFound)
}

func putDeviceUpserts(t *testing.T, e env) {
	c := e.as(devA)
	must(t, c.PutDevice(ctx, devA, directory.Device{V: 1, Name: "one"}))
	must(t, c.PutDevice(ctx, devA, directory.Device{V: 1, Name: "two"}))
	l, err := c.List(ctx)
	must(t, err)
	if len(l.Devices) != 1 || l.Devices[devA].Name != "two" {
		t.Fatalf("devices = %+v", l.Devices)
	}
}

func setVaultIsCompareAndSwap(t *testing.T, e env) {
	c := e.as(devA)
	must(t, c.SetVault(ctx, "", "rid1"))
	wantErr(t, c.SetVault(ctx, "", "rid2"), directory.ErrCAS)
	must(t, c.SetVault(ctx, "rid1", "rid2"))
	l, err := c.List(ctx)
	must(t, err)
	if l.Identity.Vault != "rid2" {
		t.Fatalf("vault = %q", l.Identity.Vault)
	}
}

// directoryClockStampsEverything: devices never send a time, so every time in
// a record is the directory clock's.
func directoryClockStampsEverything(t *testing.T, e env) {
	e.clock.Advance(time.Hour)
	v := start(t, e)
	want := e.clock.Now().UnixMilli()
	l, err := e.as(devA).List(ctx)
	must(t, err)
	if v.Now != want || v.Cell.DurableAt != want || l.Now != want {
		t.Fatalf("now %d, durable_at %d, list now %d, want %d", v.Now, v.Cell.DurableAt, l.Now, want)
	}
}

func concurrentAcquireOneWins(t *testing.T, e env) {
	const racers = 50
	start(t, e)
	expire(e)
	errs := make(chan error, racers)
	var wg sync.WaitGroup
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := e.as(fmt.Sprintf("dev_%032x", i+1)).Acquire(ctx, cell)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	tally(t, errs, racers)
	if got := get(t, e).Lease.Fence; got != 2 {
		t.Fatalf("fence = %d, want exactly 2", got)
	}
}

// tally checks that exactly one racer won and every other was told the lease
// is held.
func tally(t *testing.T, errs <-chan error, racers int) {
	t.Helper()
	won, held := 0, 0
	for err := range errs {
		switch {
		case err == nil:
			won++
		case errors.Is(err, directory.ErrLeaseHeld):
			held++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if won != 1 || held != racers-1 {
		t.Fatalf("won %d, held %d of %d", won, held, racers)
	}
}
