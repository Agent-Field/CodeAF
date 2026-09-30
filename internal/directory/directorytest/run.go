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
	clock Clock
	as    Devices
	id    func(name string) string // the device id the directory knows a named device by
}

// Rig is one fresh, empty directory as a suite sees it. A live relay decides
// device ids from keys and keeps its own clock, so a Rig carries both.
type Rig struct {
	Clock   Clock
	Devices Devices
	// ID answers the device id a named device acts as; nil means the name is the id.
	ID func(name string) string
}

// RigFactory builds a fresh Rig for one test.
type RigFactory func(t *testing.T) Rig

const (
	cell = "01J0000000000000000000000A"
	devA = "dev_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	devB = "dev_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	head = "1111111111111111111111111111111111111111111111111111111111111111"
	next = "2222222222222222222222222222222222222222222222222222222222222222"
)

var ctx = context.Background()

// Run runs every conformance case against a fresh directory from factory, on a
// clock that moves only when a case waits.
func Run(t *testing.T, factory Factory) {
	RunRigs(t, func(t *testing.T) Rig {
		clock := NewFakeClock()
		return Rig{Clock: clock, Devices: factory(t, clock)}
	})
}

// RunRigs runs every conformance case against a fresh Rig each.
func RunRigs(t *testing.T, factory RigFactory) {
	for name, fn := range cases {
		t.Run(name, func(t *testing.T) { fn(t, envOf(factory(t))) })
	}
}

func envOf(r Rig) env {
	id := r.ID
	if id == nil {
		id = func(name string) string { return name }
	}
	return env{clock: r.Clock, as: r.Devices, id: id}
}

var cases = map[string]func(*testing.T, env){
	"CreateHoldsLease":               createHoldsLease,
	"CreateExists":                   createExists,
	"CellNotFound":                   cellNotFound,
	"AcquireRefusedWhileHeld":        acquireRefusedWhileHeld,
	"AcquireAfterExpiry":             acquireAfterExpiry,
	"AcquireOwnLeaseRaisesFence":     acquireOwnLeaseRaisesFence,
	"ForcedAcquireBeatsLiveLease":    forcedAcquireBeatsLiveLease,
	"HeartbeatRenews":                heartbeatRenews,
	"LateHeartbeatSameFence":         lateHeartbeatSameFence,
	"HeartbeatWrongFenceRefused":     heartbeatWrongFenceRefused,
	"PublishMovesHead":               publishMovesHead,
	"PublishRenewsLease":             publishRenewsLease,
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
	"DeviceRevokedIsRefused":         deviceRevokedIsRefused,
	"RevokeRefusesSelfAndUnknown":    revokeRefusesSelfAndUnknown,
	"RevokeIsIdempotent":             revokeIsIdempotent,
	"RotationStatus":                 rotationStatus,
	"RotationFreezeRefusesWrites":    rotationFreezeRefusesWrites,
	"RotationThawRestores":           rotationThawRestores,
	"RotationRetireNeedsFreeze":      rotationRetireNeedsFreeze,
	"RotationRetireBounds":           rotationRetireBounds,
	"RotationFirstFreezerWins":       rotationFirstFreezerWins,
	"RotationRetiredIsForever":       rotationRetiredIsForever,
	"RevokedCannotRotate":            revokedCannotRotate,
}

func start(t *testing.T, e env) directory.CellView {
	t.Helper()
	v, err := e.as(devA).Create(ctx, cell, directory.CellInit{Head: head, Class: "chat", Size: 10, Title: "t"})
	must(t, err)
	return v
}

func expire(e env) { e.clock.Wait(directory.LeaseTTL + time.Second) }

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
	if l.Device != e.id(devA) || l.Fence != 1 || l.Expires != v.Now+directory.LeaseTTL.Milliseconds() {
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
	_, err = e.as(devA).Acquire(ctx, cell, directory.AcquireOpts{})
	wantErr(t, err, directory.ErrNotFound)
}

func acquireRefusedWhileHeld(t *testing.T, e env) {
	start(t, e)
	_, err := e.as(devB).Acquire(ctx, cell, directory.AcquireOpts{})
	wantErr(t, err, directory.ErrLeaseHeld)
}

func acquireAfterExpiry(t *testing.T, e env) {
	start(t, e)
	expire(e)
	v, err := e.as(devB).Acquire(ctx, cell, directory.AcquireOpts{})
	must(t, err)
	if v.Cell.Lease.Device != e.id(devB) || v.Cell.Lease.Fence != 2 || v.Cell.Lease.Pending != 0 {
		t.Fatalf("lease = %+v", v.Cell.Lease)
	}
}

func acquireOwnLeaseRaisesFence(t *testing.T, e env) {
	start(t, e)
	v, err := e.as(devA).Acquire(ctx, cell, directory.AcquireOpts{})
	must(t, err)
	if v.Cell.Lease.Fence != 2 {
		t.Fatalf("fence = %d, want 2", v.Cell.Lease.Fence)
	}
}

// forcedAcquireBeatsLiveLease: a person who continues a chat where it runs now
// takes the lease at once, and the displaced holder is refused as in L7.
func forcedAcquireBeatsLiveLease(t *testing.T, e env) {
	start(t, e)
	v, err := e.as(devB).Acquire(ctx, cell, directory.AcquireOpts{Force: true})
	must(t, err)
	if l := v.Cell.Lease; l.Device != e.id(devB) || l.Fence != 2 || l.Pending != 0 {
		t.Fatalf("lease = %+v", l)
	}
	_, err = e.as(devA).Publish(ctx, cell, directory.Publish{Fence: 1, OldHead: head, Head: next})
	wantErr(t, err, directory.ErrFenceStale)
	_, err = e.as(devA).Heartbeat(ctx, cell, directory.Beat{Fence: 1})
	wantErr(t, err, directory.ErrFenceStale)
}

func heartbeatRenews(t *testing.T, e env) {
	start(t, e)
	e.clock.Wait(directory.HeartbeatEvery)
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
	e.clock.Wait(time.Second)
	v, err := e.as(devA).Publish(ctx, cell, directory.Publish{
		Fence: 1, OldHead: head, Head: next, Size: 20, Class: "chat", Title: "u", Pending: 2,
	})
	must(t, err)
	c := v.Cell
	if c.Head != next || c.Size != 20 || c.Title != "u" || c.Lease.Pending != 2 || c.DurableAt != v.Now {
		t.Fatalf("cell = %+v at now %d", c, v.Now)
	}
}

// publishRenewsLease: a publish is proof of life, so a holder that publishes
// often never needs a heartbeat.
func publishRenewsLease(t *testing.T, e env) {
	start(t, e)
	e.clock.Wait(directory.HeartbeatEvery)
	v, err := e.as(devA).Publish(ctx, cell, directory.Publish{Fence: 1, OldHead: head, Head: next})
	must(t, err)
	if want := v.Now + directory.LeaseTTL.Milliseconds(); v.Cell.Lease.Expires != want {
		t.Fatalf("expires = %d, want %d", v.Cell.Lease.Expires, want)
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
	b, err := e.as(devB).Acquire(ctx, cell, directory.AcquireOpts{})
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
	if l.Expires != 0 || l.Pending != 0 || l.Fence != 1 || l.Device != e.id(devA) {
		t.Fatalf("lease = %+v", l)
	}
	v, err := e.as(devB).Acquire(ctx, cell, directory.AcquireOpts{})
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
	must(t, c.PutDevice(ctx, e.id(devA), directory.Device{V: 1, Name: "one"}))
	must(t, c.PutDevice(ctx, e.id(devA), directory.Device{V: 1, Name: "two"}))
	l, err := c.List(ctx)
	must(t, err)
	if len(l.Devices) != 1 || l.Devices[e.id(devA)].Name != "two" {
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
// a record is the directory clock's. The clock is read before and after, so a
// clock that keeps moving is held to "between the two" and a fake to "exactly".
func directoryClockStampsEverything(t *testing.T, e env) {
	e.clock.Wait(time.Second)
	before := e.clock.Now().UnixMilli()
	v := start(t, e)
	l, err := e.as(devA).List(ctx)
	must(t, err)
	after := e.clock.Now().UnixMilli()
	if v.Cell.DurableAt != v.Now || v.Now < before || l.Now < v.Now || l.Now > after {
		t.Fatalf("now %d, durable_at %d, list now %d, want %d..%d", v.Now, v.Cell.DurableAt, l.Now, before, after)
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
			_, err := e.as(fmt.Sprintf("dev_%032x", i+1)).Acquire(ctx, cell, directory.AcquireOpts{})
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

// twoDevices records devA and devB as devices of one identity.
func twoDevices(t *testing.T, e env) {
	t.Helper()
	for _, d := range []string{devA, devB} {
		must(t, e.as(d).PutDevice(ctx, e.id(d), directory.Device{V: 1, Name: d}))
	}
}

// deviceRevokedIsRefused: once A revokes B, B is refused on every verb, A goes
// on as before, and the record and the listing both say B is stopped.
func deviceRevokedIsRefused(t *testing.T, e env) {
	start(t, e)
	twoDevices(t, e)
	must(t, e.as(devA).Revoke(ctx, e.id(devB)))

	b := e.as(devB)
	_, listErr := b.List(ctx)
	_, cellErr := b.Cell(ctx, cell)
	_, createErr := b.Create(ctx, "01J0000000000000000000000B", directory.CellInit{Head: head})
	_, acquireErr := b.Acquire(ctx, cell, directory.AcquireOpts{})
	_, beatErr := b.Heartbeat(ctx, cell, directory.Beat{Fence: 1})
	_, publishErr := b.Publish(ctx, cell, directory.Publish{Fence: 1, OldHead: head, Head: next})
	for verb, err := range map[string]error{
		"List": listErr, "Cell": cellErr, "Create": createErr, "Acquire": acquireErr, "Heartbeat": beatErr,
		"Publish":   publishErr,
		"Release":   b.Release(ctx, cell, 1),
		"Archive":   b.Archive(ctx, cell),
		"SetVault":  b.SetVault(ctx, "", "rid"),
		"PutDevice": b.PutDevice(ctx, e.id(devB), directory.Device{V: 1, Name: "back"}),
		"Revoke":    b.Revoke(ctx, e.id(devA)),
	} {
		if !errors.Is(err, directory.ErrRevoked) {
			t.Errorf("%s by a revoked device: %v, want ErrRevoked", verb, err)
		}
	}

	l, err := e.as(devA).List(ctx)
	must(t, err)
	if !l.Devices[e.id(devB)].Revoked || l.Devices[e.id(devA)].Revoked {
		t.Fatalf("listing shows devices %+v, want only B revoked", l.Devices)
	}
	if got := get(t, e); got.Head != head || got.Lease.Device != e.id(devA) {
		t.Fatalf("a refused device changed the cell: %+v", got)
	}
	_, err = e.as(devA).Heartbeat(ctx, cell, directory.Beat{Fence: 1})
	must(t, err)
}

// revokeRefusesSelfAndUnknown: a device cannot stop itself, and an id nobody
// recorded is not found.
func revokeRefusesSelfAndUnknown(t *testing.T, e env) {
	twoDevices(t, e)
	wantErr(t, e.as(devA).Revoke(ctx, e.id(devA)), directory.ErrSelfRevoke)
	wantErr(t, e.as(devA).Revoke(ctx, "dev_cccccccccccccccccccccccccccccccc"), directory.ErrNotFound)
	l, err := e.as(devA).List(ctx)
	must(t, err)
	if l.Devices[e.id(devA)].Revoked {
		t.Fatal("a refused self-revoke still stopped the device")
	}
}

// revokeIsIdempotent: stopping a stopped device is not an error, so a retry
// after a lost answer is safe.
func revokeIsIdempotent(t *testing.T, e env) {
	twoDevices(t, e)
	must(t, e.as(devA).Revoke(ctx, e.id(devB)))
	must(t, e.as(devA).Revoke(ctx, e.id(devB)))
}

func move(t *testing.T, e env, device string, req directory.RotationReq) (directory.RotationView, error) {
	t.Helper()
	return e.as(device).Rotate(ctx, req)
}

// rotationStatus: a live identity has no rotation and says what grace it accepts.
func rotationStatus(t *testing.T, e env) {
	v, err := e.as(devA).Rotation(ctx)
	must(t, err)
	if v.Rotation != nil || v.MinMS <= 0 || v.MinMS > v.DefaultMS || v.DefaultMS > v.MaxMS {
		t.Fatalf("view of a live identity = %+v", v)
	}
}

// rotationFreezeRefusesWrites: once frozen, every verb that changes a record is
// refused for every device, and reads are not.
func rotationFreezeRefusesWrites(t *testing.T, e env) {
	start(t, e)
	twoDevices(t, e)
	_, err := move(t, e, devA, directory.RotationReq{Op: directory.OpFreeze})
	must(t, err)
	for _, dev := range []string{devA, devB} {
		c := e.as(dev)
		_, createErr := c.Create(ctx, "01J0000000000000000000000B", directory.CellInit{Head: head})
		_, acquireErr := c.Acquire(ctx, cell, directory.AcquireOpts{})
		_, beatErr := c.Heartbeat(ctx, cell, directory.Beat{Fence: 1})
		_, publishErr := c.Publish(ctx, cell, directory.Publish{Fence: 1, OldHead: head, Head: next})
		for verb, err := range map[string]error{
			"Create": createErr, "Acquire": acquireErr, "Heartbeat": beatErr, "Publish": publishErr,
			"Release":   c.Release(ctx, cell, 1),
			"Archive":   c.Archive(ctx, cell),
			"SetVault":  c.SetVault(ctx, "", "rid"),
			"PutDevice": c.PutDevice(ctx, e.id(dev), directory.Device{V: 1, Name: "x"}),
			"Revoke":    c.Revoke(ctx, e.id(devB)),
		} {
			if !errors.Is(err, directory.ErrRotated) {
				t.Errorf("%s by %s on a frozen identity: %v, want ErrRotated", verb, dev, err)
			}
		}
		if _, err := c.List(ctx); err != nil {
			t.Errorf("List on a frozen identity: %v", err)
		}
		if _, err := c.Cell(ctx, cell); err != nil {
			t.Errorf("Cell on a frozen identity: %v", err)
		}
	}
	v, err := e.as(devB).Rotation(ctx)
	must(t, err)
	if v.Rotation == nil || v.Rotation.State != directory.StateFrozen || v.Rotation.By != e.id(devA) {
		t.Fatalf("rotation = %+v, want frozen by A", v.Rotation)
	}
}

// rotationThawRestores: a thaw makes the identity writable as it was, and thawing
// a live one is not an error.
func rotationThawRestores(t *testing.T, e env) {
	start(t, e)
	_, err := move(t, e, devA, directory.RotationReq{Op: directory.OpFreeze})
	must(t, err)
	_, err = move(t, e, devA, directory.RotationReq{Op: directory.OpThaw})
	must(t, err)
	_, err = move(t, e, devA, directory.RotationReq{Op: directory.OpThaw})
	must(t, err)
	must(t, e.as(devA).Archive(ctx, cell))
}

// rotationRetireNeedsFreeze: retiring a live identity is refused.
func rotationRetireNeedsFreeze(t *testing.T, e env) {
	_, err := move(t, e, devA, directory.RotationReq{Op: directory.OpRetire})
	wantErr(t, err, directory.ErrRotationStep)
}

// rotationRetireBounds: a grace outside what the relay says it accepts is
// refused, none at all means the relay's default, and the deadline is the
// directory's time plus the grace.
func rotationRetireBounds(t *testing.T, e env) {
	_, err := move(t, e, devA, directory.RotationReq{Op: directory.OpFreeze})
	must(t, err)
	limits, err := e.as(devA).Rotation(ctx)
	must(t, err)
	for _, ms := range []int64{limits.MinMS - 1, limits.MaxMS + 1} {
		_, err := move(t, e, devA, directory.RotationReq{Op: directory.OpRetire, GraceMS: ms})
		wantErr(t, err, directory.ErrBadGrace)
	}
	v, err := move(t, e, devA, directory.RotationReq{Op: directory.OpRetire})
	must(t, err)
	if v.Rotation.State != directory.StateRetired || v.Rotation.RetireAt != v.Now+limits.DefaultMS {
		t.Fatalf("retired view = %+v at %d, default %d", v.Rotation, v.Now, limits.DefaultMS)
	}
}

// rotationFirstFreezerWins: a second device cannot freeze what another froze, and
// only the device that froze may retire; any device may thaw.
func rotationFirstFreezerWins(t *testing.T, e env) {
	twoDevices(t, e)
	_, err := move(t, e, devA, directory.RotationReq{Op: directory.OpFreeze})
	must(t, err)
	_, err = move(t, e, devB, directory.RotationReq{Op: directory.OpFreeze})
	wantErr(t, err, directory.ErrRotated)
	_, err = move(t, e, devB, directory.RotationReq{Op: directory.OpRetire})
	wantErr(t, err, directory.ErrRotated)
	_, err = move(t, e, devB, directory.RotationReq{Op: directory.OpThaw})
	must(t, err)
}

// rotationRetiredIsForever: a retired identity cannot be thawed or frozen by
// another, and retiring again changes nothing.
func rotationRetiredIsForever(t *testing.T, e env) {
	twoDevices(t, e)
	_, err := move(t, e, devA, directory.RotationReq{Op: directory.OpFreeze})
	must(t, err)
	first, err := move(t, e, devA, directory.RotationReq{Op: directory.OpRetire})
	must(t, err)
	_, err = move(t, e, devA, directory.RotationReq{Op: directory.OpThaw})
	wantErr(t, err, directory.ErrRotated)
	_, err = move(t, e, devB, directory.RotationReq{Op: directory.OpFreeze})
	wantErr(t, err, directory.ErrRotated)
	again, err := move(t, e, devA, directory.RotationReq{Op: directory.OpRetire})
	must(t, err)
	if *again.Rotation != *first.Rotation {
		t.Fatalf("a second retire changed %+v to %+v", first.Rotation, again.Rotation)
	}
}

// revokedCannotRotate: a stopped device cannot ask for anything, rotation included.
func revokedCannotRotate(t *testing.T, e env) {
	twoDevices(t, e)
	must(t, e.as(devA).Revoke(ctx, e.id(devB)))
	_, err := move(t, e, devB, directory.RotationReq{Op: directory.OpFreeze})
	wantErr(t, err, directory.ErrRevoked)
	_, err = e.as(devB).Rotation(ctx)
	wantErr(t, err, directory.ErrRevoked)
}
