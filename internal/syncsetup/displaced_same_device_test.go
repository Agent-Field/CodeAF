package syncsetup

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/identity"
)

// Contract K2 also holds when the newer hold is this computer's own: the
// displaced window retires its receipt before anyone is told, and a newer
// writer's receipt for the same chat survives.
func TestSameComputerTakeoverRetiresOnlyTheDisplacedReceipt(t *testing.T) {
	for _, path := range []string{"becomeViewer", "ConfirmWork", "CheckLease"} {
		t.Run(path, func(t *testing.T) {
			for _, newerReceipt := range []bool{false, true} {
				name := "own receipt"
				if newerReceipt {
					name = "newer writer receipt"
				}
				t.Run(name, func(t *testing.T) {
					d := sameComputerDrive(t)
					s := d.sync
					newer, err := s.Dir.Acquire(t.Context(), d.directoryID, directory.AcquireOpts{
						Force: true, Expect: &directory.LeaseExpectation{Device: s.Device.ID(), Fence: d.recordedFence},
					})
					mustNil(t, err)
					if newer.Cell.Lease.Fence <= d.recordedFence {
						t.Fatalf("takeover fence = %d, want greater than %d", newer.Cell.Lease.Fence, d.recordedFence)
					}
					if newerReceipt {
						mustNil(t, s.recordSendLease(d.cell.ID, sendLease{Cell: d.directoryID, Fence: newer.Cell.Lease.Fence}))
					}
					assertReceipt := func() {
						t.Helper()
						remote, fence, err := s.SendLease(d.cell.ID)
						if newerReceipt {
							if err != nil || remote != d.directoryID || fence != newer.Cell.Lease.Fence {
								t.Fatalf("newer writer receipt = %q, %d, %v; want %q, %d, nil", remote, fence, err, d.directoryID, newer.Cell.Lease.Fence)
							}
						} else if !os.IsNotExist(err) {
							t.Fatalf("displaced receipt = %q, %d, %v; want no receipt", remote, fence, err)
						}
					}
					assertViewer := func() {
						t.Helper()
						assertReceipt()
						if line, viewer := d.Viewer(); !viewer || line != chatlist.Available {
							t.Fatalf("displaced viewer = %q, %v; want %q, true", line, viewer, chatlist.Available)
						}
						if _, _, held := d.RenewalStatus(); held {
							t.Fatal("displaced drive still reports a held lease")
						}
						if err := d.Gate(); err == nil {
							t.Fatal("displaced drive still admits work")
						}
					}
					renewals, viewers := 0, 0
					d.opt.OnRenewalChange = func() {
						renewals++
						assertViewer()
					}
					d.opt.OnViewer = func(viewer Viewer) {
						viewers++
						assertViewer()
						if viewer.Line != chatlist.Available || renewals != 1 {
							t.Fatalf("viewer notification = %q after %d renewal notifications; want %q after 1", viewer.Line, renewals, chatlist.Available)
						}
					}
					displace := func() {
						t.Helper()
						switch path {
						case "becomeViewer":
							d.becomeViewer(cellsync.Superseded{By: s.Device.ID()})
						case "ConfirmWork":
							confirmed, err := d.ConfirmWork(t.Context())
							if err != nil || confirmed {
								t.Fatalf("displaced admission = %v, %v; want false, nil", confirmed, err)
							}
						case "CheckLease":
							// This fixture has no pending capture, so the watch's
							// ownership check needs no staged engine.
							mustNil(t, d.CheckLease(t.Context()))
						}
					}
					displace()
					assertViewer()
					if renewals != 1 || viewers != 1 || len(d.ownership) != 1 {
						t.Fatalf("displacement notifications = %d renewal, %d viewer, %d ownership; want 1 each", renewals, viewers, len(d.ownership))
					}
					<-d.ownership
					displace()
					assertViewer()
					if renewals != 1 || viewers != 1 || len(d.ownership) != 0 {
						t.Fatalf("repeated displacement notifications = %d renewal, %d viewer, %d ownership; want 1, 1, 0", renewals, viewers, len(d.ownership))
					}
				})
			}
		})
	}
}

// A refusal that arrives after this window already reacquired the chat names
// an older fence; the receipt of the hold the relay still names survives it.
func TestStaleRefusalKeepsTheReacquiredReceipt(t *testing.T) {
	for _, named := range []bool{false, true} {
		name := "unnamed holder"
		if named {
			name = "own device holder"
		}
		t.Run(name, func(t *testing.T) {
			d := sameComputerDrive(t)
			newer, err := d.sync.Dir.Acquire(t.Context(), d.directoryID, directory.AcquireOpts{
				Force: true, Expect: &directory.LeaseExpectation{Device: d.sync.Device.ID(), Fence: d.recordedFence},
			})
			mustNil(t, err)
			if newer.Cell.Lease.Fence <= d.recordedFence {
				t.Fatalf("reacquired fence = %d, want greater than %d", newer.Cell.Lease.Fence, d.recordedFence)
			}
			d.batcher.Driving.Fence = newer.Cell.Lease.Fence
			mustNil(t, d.recordSendLease())
			assertReceipt := func() {
				t.Helper()
				remote, fence, err := d.sync.SendLease(d.cell.ID)
				if err != nil || remote != d.directoryID || fence != newer.Cell.Lease.Fence {
					t.Fatalf("reacquired receipt = %q, %d, %v; want %q, %d, nil", remote, fence, err, d.directoryID, newer.Cell.Lease.Fence)
				}
			}
			assertReceipt()
			refusal := cellsync.Superseded{}
			if named {
				refusal.By = d.sync.Device.ID()
			}
			d.becomeViewer(refusal)
			assertReceipt()
			mustNil(t, d.CheckLease(t.Context()))
			if err := d.Gate(); err != nil {
				t.Fatalf("reacquired drive refuses work after checking its current lease: %v", err)
			}
			if _, _, held := d.RenewalStatus(); !held {
				t.Fatal("reacquired drive does not report its current lease as held")
			}
			assertReceipt()
		})
	}
}

// A reacquisition completed before an older reading returns must keep its new
// receipt, even when the reading displaces the receipt held before the read.
func TestDisplacingReadingKeepsTheConcurrentlyReacquiredReceipt(t *testing.T) {
	for _, state := range []string{"released own device", "other device"} {
		t.Run(state, func(t *testing.T) {
			paths := []string{"CheckLease", "ConfirmWork"}
			if state == "released own device" {
				// Unlike admission's fence rechecks, this path reaches retirement
				// after reacquisition and exercises the receipt snapshot itself.
				paths = append(paths, "becomeViewer")
			}
			for _, path := range paths {
				t.Run(path, func(t *testing.T) {
					d, memory := sameComputerDriveWithDirectory(t)
					client := d.sync.Dir
					holder := client
					if state == "other device" {
						holder = memory.For("other")
					}
					displacing, err := holder.Acquire(t.Context(), d.directoryID, directory.AcquireOpts{Force: true})
					mustNil(t, err)
					if displacing.Cell.Lease.Fence <= d.recordedFence {
						t.Fatal("displacing fence did not advance")
					}
					if state == "released own device" {
						mustNil(t, holder.Release(t.Context(), d.directoryID, displacing.Cell.Lease.Fence))
					}
					var reacquired uint64
					reads := 0
					d.sync.Dir = &displacedReadingDirectory{Client: client, beforeReturn: func(reading directory.CellView) {
						reads++
						if reading.Cell.Lease.Device != displacing.Cell.Lease.Device || reading.Cell.Lease.Fence != displacing.Cell.Lease.Fence {
							t.Fatalf("displacing reading = %+v; want holder %q at fence %d", reading.Cell.Lease, displacing.Cell.Lease.Device, displacing.Cell.Lease.Fence)
						}
						if state == "released own device" && reading.Cell.Lease.Expires != 0 {
							t.Fatal("displacing reading was not released")
						}
						if err := d.Gate(); err != nil {
							t.Fatalf("gate closed before concurrent reacquisition: %v", err)
						}
						newer, err := client.Acquire(t.Context(), d.directoryID, directory.AcquireOpts{
							Force: true, Expect: &directory.LeaseExpectation{Device: reading.Cell.Lease.Device, Fence: reading.Cell.Lease.Fence},
						})
						mustNil(t, err)
						reacquired = newer.Cell.Lease.Fence
						if reacquired <= reading.Cell.Lease.Fence {
							t.Fatal("concurrently reacquired fence did not advance")
						}
						d.batcher.Driving.Fence = reacquired
						mustNil(t, d.recordSendLease())
					}}
					switch path {
					case "CheckLease":
						mustNil(t, d.CheckLease(t.Context()))
					case "ConfirmWork":
						// ConfirmWork's batcher lease recheck rejects the stale
						// answer before retirement or engine-free reacquisition.
						confirmed, err := d.ConfirmWork(t.Context())
						if err != nil || confirmed {
							t.Fatalf("stale confirmation = %v, %v; want false, nil", confirmed, err)
						}
					case "becomeViewer":
						d.becomeViewer(cellsync.Superseded{})
					}
					if reads != 1 || reacquired == 0 {
						t.Fatalf("reacquisition barrier ran %d times at fence %d; want once at a new fence", reads, reacquired)
					}
					remote, fence, err := d.sync.SendLease(d.cell.ID)
					if err != nil || remote != d.directoryID || fence != reacquired {
						t.Fatalf("concurrently reacquired receipt = %q, %d, %v; want %q, %d, nil", remote, fence, err, d.directoryID, reacquired)
					}
					if path == "becomeViewer" {
						if err := d.Gate(); err == nil {
							t.Fatal("stale released reading did not reach the viewer transition")
						}
					} else {
						// CheckLease also returns at its batcher lease recheck,
						// leaving the concurrently reacquired drive open.
						if err := d.Gate(); err != nil {
							t.Fatalf("stale reading changed the reacquired drive's gate: %v", err)
						}
						if _, _, held := d.RenewalStatus(); !held {
							t.Fatal("stale reading changed the reacquired drive's held status")
						}
					}
				})
			}
		})
	}
}

// A released fence can still heartbeat as its unchanged owner, so availability
// must retain its receipt until the same fence resumes.
func TestReleasedSameFenceKeepsTheReceiptItCanResume(t *testing.T) {
	d := sameComputerDrive(t)
	held := d.recordedFence
	assertReceipt := func() {
		t.Helper()
		remote, fence, err := d.sync.SendLease(d.cell.ID)
		if err != nil || remote != d.directoryID || fence != held {
			t.Fatalf("released same-fence receipt = %q, %d, %v; want %q, %d, nil", remote, fence, err, d.directoryID, held)
		}
	}
	mustNil(t, d.sync.Dir.Release(t.Context(), d.directoryID, held))
	mustNil(t, d.CheckLease(t.Context()))
	assertReceipt()
	d.becomeViewer(cellsync.Superseded{})
	assertReceipt()
	if err := d.Gate(); err == nil {
		t.Fatal("released drive admits work before its heartbeat")
	}
	if _, _, held := d.RenewalStatus(); held {
		t.Fatal("released drive reports a held lease before its heartbeat")
	}
	renewed, err := d.sync.Dir.Heartbeat(t.Context(), d.directoryID, directory.Beat{Fence: held})
	mustNil(t, err)
	if renewed.Cell.Lease.Fence != held || renewed.Cell.Lease.Expires <= renewed.Now {
		t.Fatalf("same-fence heartbeat = %+v at %d; want live fence %d", renewed.Cell.Lease, renewed.Now, held)
	}
	mustNil(t, d.CheckLease(t.Context()))
	if err := d.Gate(); err != nil {
		t.Fatalf("same-fence heartbeat did not reopen the work gate: %v", err)
	}
	if _, _, held := d.RenewalStatus(); !held {
		t.Fatal("resumed same-fence drive does not report its held lease")
	}
	assertReceipt()
}

// Receipt lock contention must close the work gate immediately while delaying
// both reader notifications until the displaced receipt has been retired.
func TestDisplacementClosesTheGateBeforeReceiptIO(t *testing.T) {
	d := sameComputerDrive(t)
	newer, err := d.sync.Dir.Acquire(t.Context(), d.directoryID, directory.AcquireOpts{Force: true})
	mustNil(t, err)
	if newer.Cell.Lease.Fence <= d.recordedFence {
		t.Fatal("replacement fence did not advance")
	}
	path, err := d.sync.sendLeasePath(d.cell.ID)
	mustNil(t, err)
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	mustNil(t, err)
	defer lock.Close()
	mustNil(t, filelock.Lock(lock, true, false))
	defer filelock.Unlock(lock)
	notifications := make(chan string, 2)
	d.opt.OnRenewalChange = func() { notifications <- "renewal" }
	d.opt.OnViewer = func(Viewer) { notifications <- "viewer" }
	type result struct {
		confirmed bool
		err       error
	}
	done := make(chan result, 1)
	go func() {
		confirmed, err := d.ConfirmWork(t.Context())
		done <- result{confirmed, err}
	}()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(time.Millisecond)
	defer poll.Stop()
	for d.Gate() == nil {
		select {
		case <-poll.C:
		case <-deadline.C:
			t.Fatal("work gate stayed open while retirement waited for the receipt lock")
		case got := <-done:
			t.Fatalf("admission returned before the gate closed: %+v", got)
		}
	}
	select {
	case notification := <-notifications:
		t.Fatalf("%s notification fired before receipt lock release", notification)
	default:
	}
	remote, fence, err := d.sync.SendLease(d.cell.ID)
	if err != nil || remote != d.directoryID || fence != d.recordedFence {
		t.Fatalf("locked receipt = %q, %d, %v; want %q, %d, nil", remote, fence, err, d.directoryID, d.recordedFence)
	}
	mustNil(t, filelock.Unlock(lock))
	select {
	case got := <-done:
		if got.err != nil || got.confirmed {
			t.Fatalf("displaced admission = %v, %v; want false, nil", got.confirmed, got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("admission did not finish after receipt lock release")
	}
	if remote, fence, err := d.sync.SendLease(d.cell.ID); !os.IsNotExist(err) {
		t.Fatalf("retired receipt = %q, %d, %v; want no receipt", remote, fence, err)
	}
	for _, want := range []string{"renewal", "viewer"} {
		select {
		case got := <-notifications:
			if got != want {
				t.Fatalf("notification = %q; want %q", got, want)
			}
		default:
			t.Fatalf("missing %s notification after retirement", want)
		}
	}
	if len(notifications) != 0 {
		t.Fatal("displacement sent extra notifications")
	}
}

// displacedReadingDirectory makes reacquisition finish after the first reading
// is captured and before the caller can act on that older reading.
type displacedReadingDirectory struct {
	directory.Client
	beforeReturn func(directory.CellView)
}

func (c *displacedReadingDirectory) Cell(ctx context.Context, id string) (directory.CellView, error) {
	v, err := c.Client.Cell(ctx, id)
	if err == nil && c.beforeReturn != nil {
		beforeReturn := c.beforeReturn
		c.beforeReturn = nil
		beforeReturn(v)
	}
	return v, err
}

// A replacement fence that has since expired or been released still means the
// displaced fence cannot resume, so its receipt is retired on every door.
func TestDisplacedReceiptRetiresWhenTheReplacementIsFree(t *testing.T) {
	for _, state := range []string{"expired", "released"} {
		t.Run(state, func(t *testing.T) {
			for _, path := range []string{"becomeViewer", "CheckLease", "ConfirmWork"} {
				t.Run(path, func(t *testing.T) {
					now := time.Unix(1_700_000_000, 0)
					d := sameComputerDrive(t, func() time.Time { return now })
					newer, err := d.sync.Dir.Acquire(t.Context(), d.directoryID, directory.AcquireOpts{
						Force: true, Expect: &directory.LeaseExpectation{Device: d.sync.Device.ID(), Fence: d.recordedFence},
					})
					mustNil(t, err)
					if newer.Cell.Lease.Fence <= d.recordedFence {
						t.Fatalf("replacement fence = %d, want greater than displaced fence %d", newer.Cell.Lease.Fence, d.recordedFence)
					}
					if state == "expired" {
						now = now.Add(directory.LeaseTTL + time.Second)
					} else {
						mustNil(t, d.sync.Dir.Release(t.Context(), d.directoryID, newer.Cell.Lease.Fence))
					}
					view, err := d.sync.Dir.Cell(t.Context(), d.directoryID)
					mustNil(t, err)
					if view.Cell.Lease.Fence != newer.Cell.Lease.Fence || view.Cell.Lease.Expires > view.Now {
						t.Fatalf("free replacement lease = %+v at %d; want fence %d expired or released", view.Cell.Lease, view.Now, newer.Cell.Lease.Fence)
					}
					// An unsealed fixture stops ConfirmWork before Reacquire, so
					// retirement and notification need no publisher or engine.
					head, err := cellstore.Head(d.cell)
					mustNil(t, err)
					if head != nil {
						t.Fatal("free replacement fixture unexpectedly has a sealed head")
					}
					assertRetired := func() {
						t.Helper()
						remote, fence, err := d.sync.SendLease(d.cell.ID)
						if !os.IsNotExist(err) {
							t.Fatalf("displaced receipt after replacement became %s = %q, %d, %v; want no receipt", state, remote, fence, err)
						}
						if line, viewer := d.Viewer(); !viewer || line != chatlist.Available {
							t.Fatalf("free replacement viewer = %q, %v; want %q, true", line, viewer, chatlist.Available)
						}
						if err := d.Gate(); err == nil {
							t.Fatal("displaced drive admits work after its replacement became free")
						}
						if _, _, held := d.RenewalStatus(); held {
							t.Fatal("displaced drive reports a held lease after its replacement became free")
						}
					}
					renewals, viewers := 0, 0
					d.opt.OnRenewalChange = func() {
						renewals++
						assertRetired()
					}
					d.opt.OnViewer = func(viewer Viewer) {
						viewers++
						assertRetired()
						if viewer.Line != chatlist.Available || renewals != 1 {
							t.Fatalf("free replacement notification = %q after %d renewal notifications; want %q after 1", viewer.Line, renewals, chatlist.Available)
						}
					}
					for range 2 {
						switch path {
						case "becomeViewer":
							d.becomeViewer(cellsync.Superseded{})
						case "CheckLease":
							mustNil(t, d.CheckLease(t.Context()))
						case "ConfirmWork":
							confirmed, err := d.ConfirmWork(t.Context())
							if err != nil || confirmed {
								t.Fatalf("free replacement admission = %v, %v; want false, nil for unsealed chat", confirmed, err)
							}
						}
						assertRetired()
						if renewals != 1 || viewers != 1 {
							t.Fatalf("free replacement notifications = %d renewal, %d viewer; want 1 each", renewals, viewers)
						}
					}
				})
			}
		})
	}
}

// A lapse that may still renew the same fence keeps the receipt, so the window
// that resumes can send without being asked to continue its own chat.
func TestAvailableAfterALapseKeepsTheReceiptItMayResume(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	d := sameComputerDrive(t, func() time.Time { return now })
	assertReceipt := func() {
		t.Helper()
		remote, fence, err := d.sync.SendLease(d.cell.ID)
		if err != nil || remote != d.directoryID || fence != d.recordedFence {
			t.Fatalf("resumable receipt = %q, %d, %v; want %q, %d, nil", remote, fence, err, d.directoryID, d.recordedFence)
		}
	}
	renewals, viewers := 0, 0
	d.opt.OnRenewalChange = func() {
		renewals++
		assertReceipt()
		if _, _, held := d.RenewalStatus(); held {
			t.Fatal("lapsed drive still reports a held lease during notification")
		}
		if err := d.Gate(); err == nil {
			t.Fatal("lapsed drive admits work during notification")
		}
	}
	d.opt.OnViewer = func(viewer Viewer) {
		viewers++
		assertReceipt()
		if viewer.Line != chatlist.Available || renewals != 1 {
			t.Fatalf("lapsed viewer notification = %q after %d renewal notifications; want %q after 1", viewer.Line, renewals, chatlist.Available)
		}
	}
	// The ownership loop may already have a pending wake when its check lapses.
	d.ownership <- struct{}{}
	now = now.Add(directory.LeaseTTL + time.Second)
	d.becomeViewer(cellsync.Superseded{})
	assertReceipt()
	if line, viewer := d.Viewer(); !viewer || line != chatlist.Available {
		t.Fatalf("lapsed own-fence viewer = %q, %v; want %q, true", line, viewer, chatlist.Available)
	}
	if renewals != 1 || viewers != 1 {
		t.Fatalf("lapse notifications = %d renewal, %d viewer; want 1 each", renewals, viewers)
	}
	<-d.ownership
	d.available()
	if renewals != 1 || viewers != 1 || len(d.ownership) != 0 {
		t.Fatalf("repeated lapse notifications = %d renewal, %d viewer, %d ownership; want 1, 1, 0", renewals, viewers, len(d.ownership))
	}
	d.resumeViewer()
	mustNil(t, d.recordSendLease())
	assertReceipt()
	if _, _, held := d.RenewalStatus(); !held {
		t.Fatal("resumed drive does not report its held lease")
	}
	if err := d.Gate(); err != nil {
		t.Fatalf("resumed drive refuses work: %v", err)
	}
}

// sameComputerDrive is one window driving a chat at its first fence over an
// in-memory directory, with its receipt recorded and no engine staged.
func sameComputerDrive(t *testing.T, clock ...func() time.Time) *Drive {
	t.Helper()
	d, _ := sameComputerDriveWithDirectory(t, clock...)
	return d
}

// sameComputerDriveWithDirectory also exposes the shared memory directory so
// another device can displace the fixture through the real lease rules.
func sameComputerDriveWithDirectory(t *testing.T, clock ...func() time.Time) (*Drive, *directory.Memory) {
	t.Helper()
	s := &Sync{Home: t.TempDir(), Ledger: "fixture"}
	var err error
	s.Identity, err = identity.Ensure(s.Home)
	mustNil(t, err)
	s.Device, err = identity.LoadDevice(s.Home)
	mustNil(t, err)
	if s.Device.ID() == "" {
		t.Fatal("fixture device has no identity")
	}
	now := time.Now
	if len(clock) != 0 {
		now = clock[0]
	}
	memory := directory.NewMemory(now)
	t.Cleanup(func() { _ = memory.Close() })
	s.Dir = memory.For(s.Device.ID())
	c := cell.Cell{ID: "chat", Root: t.TempDir()}
	created, err := s.Dir.Create(t.Context(), c.ID, directory.CellInit{})
	mustNil(t, err)
	d := &Drive{sync: s, cell: c, directoryID: c.ID, ownership: make(chan struct{}, 1),
		batcher: &cellsync.Batcher{Driving: &cellsync.Driving{Cell: c, Fence: created.Cell.Lease.Fence}}}
	mustNil(t, d.recordSendLease())
	return d, memory
}
