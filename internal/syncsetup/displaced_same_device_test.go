package syncsetup

import (
	"os"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
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
	return d
}
