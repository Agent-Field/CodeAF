package syncsetup

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/syncwords"
)

// idleReleaseWithin bounds the final, upload-free ownership boundary.
const idleReleaseWithin = 250 * time.Millisecond

func (d *Drive) SetIdleCheck(check func() bool) {
	func() { d.workMu.Lock(); defer d.workMu.Unlock(); d.idleCheck = check }()
	// A restored terminal can already be idle without another foreground turn.
	if d.batcher != nil {
		d.idleRequested.Store(true)
		d.scheduleIdle(d.activity.Load())
	}
}

// BeginWork cancels preparation immediately. Only the short final release can
// be in flight, in which case admission must bypass its cached confirmation.
// A retired viewer also bypasses that cache to reconsider fresh ownership.
func (d *Drive) BeginWork() bool {
	d.activity.Add(1)
	d.idleMu.Lock()
	if d.idleStop != nil {
		d.idleStop()
	}
	d.idleMu.Unlock()
	return d.released.Load() || d.committing.Load() || d.displaced()
}

// SavedMetadata resumes a paired chat's requested idle certification. It does
// not trigger ordinary publication; readiness excludes settings and live work.
func (d *Drive) SavedMetadata() {
	if d.idleRequested.Load() && d.paired() {
		d.scheduleIdle(d.activity.Load())
	}
}

func (d *Drive) IdleReleased() bool { return d.released.Load() }

func lockDrive(ctx context.Context, mu *sync.Mutex) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if mu.TryLock() {
			return nil
		}
		timer := time.NewTimer(time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// stopIdle closes admission to new idle attempts before joining canceled work.
func (d *Drive) stopIdle(ctx context.Context) error {
	started := func() bool {
		d.idleMu.Lock()
		defer d.idleMu.Unlock()
		d.idleClosed = true
		if d.idleStop == nil {
			return false
		}
		d.idleStop()
		return true
	}()
	if !started {
		return nil
	}
	done := make(chan struct{})
	go func() { d.idleWorkers.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (d *Drive) releaseWhenIdle(ctx context.Context, activity uint64) {
	wait := cellstore.TailQuiet
	for {
		if !d.paired() {
			select {
			case <-ctx.Done():
				return
			case <-d.done:
				return
			case <-d.devices.Changes():
				continue
			}
		}
		if d.opt.Sleep != nil {
			if d.opt.Sleep(ctx, wait) != nil {
				return
			}
		} else {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-d.done:
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		finished, err := d.releaseIdle(ctx, activity)
		if finished {
			return
		}
		if err == nil || errors.Is(err, cellstore.ErrSavedStateChanged) || errors.Is(err, errStateBusy) {
			wait = cellstore.TailQuiet
		} else {
			wait = min(max(wait*2, 5*time.Second), time.Minute)
		}
	}
}

func (d *Drive) idleState(activity uint64) (func() bool, bool) {
	d.mu.Lock()
	stopped := d.stopped || d.lost || d.viewer.Line != "" && !(d.released.Load() && d.viewer.Line == chatlist.Available)
	d.mu.Unlock()
	return d.idleCheck, activity != d.activity.Load() || d.releaseDone.Load() || stopped
}

func (d *Drive) profileFiles() []string {
	return []string{
		filepath.Join(d.sync.profile(), "config.json"),
		filepath.Join(d.sync.profile(), "credentials.json"),
		filepath.Join(d.sync.Home, "graph.db"),
		filepath.Join(d.sync.Home, "graph.db-wal"),
	}
}

// prepareIdle performs every capture and upload without holding admission.
// Dirty metadata and workspace changes take the same ordinary sealed-turn road
// as tool work; a failed capture or publication leaves the hold intact.
func (d *Drive) prepareIdle(ctx context.Context, activity uint64) (*cellstore.SavedState, bool, error) {
	if err := lockDrive(ctx, &d.workMu); err != nil {
		return nil, false, err
	}
	check, finished := d.idleState(activity)
	d.workMu.Unlock()
	if finished || d.sync.isRemoved() || Quiet(d.sync.Home) {
		return nil, true, nil
	}
	// A private unpublished chat has no hold to release. Do not add captures
	// or vault work to its existing local save path.
	if d.batcher.Fence() == 0 && d.batcher.Pending() == 0 {
		return nil, true, nil
	}
	ready := check == nil || check()
	if !ready || d.batcher.Pending() != 0 {
		return nil, false, nil
	}
	if err := cellstore.Captured(ctx, d.cell); err != nil {
		return nil, false, err
	}
	if d.batcher.Pending() != 0 {
		d.batcher.Idle()
		return nil, false, nil
	}
	d.idlePrepared.Store(true)
	if err := lockDrive(ctx, &d.sealMu); err != nil {
		return nil, false, err
	}
	proof, err := d.engine.SavedState(ctx, d.cell, d.profileFiles()...)
	if err == nil && proof != nil && len(proof.Changes) != 0 {
		if ctx.Err() != nil || activity != d.activity.Load() || d.Gate() != nil {
			d.sealMu.Unlock()
			return nil, true, ctx.Err()
		}
		engine := d.engine
		engine.Identity = cellstore.Identity{Device: d.sync.Device.Cert.Device, Fence: d.batcher.Fence()}
		var sealed cellstore.Sealed
		sealed, err = engine.Seal(ctx, d.cell, cellstore.TurnInfo{})
		if err == nil {
			d.note(sealed)
			d.batcher.Idle()
		}
		d.sealMu.Unlock()
		return nil, false, err
	}
	d.sealMu.Unlock()
	if err != nil || proof == nil {
		return nil, false, err
	}
	if err := d.syncIdleVault(ctx); err != nil {
		return nil, false, err
	}
	return proof, false, nil
}

func (d *Drive) releaseIdle(parent context.Context, activity uint64) (bool, error) {
	notify := false
	var version uint64
	defer func() {
		// Surface callbacks can take their own locks. They run after admission
		// and capture locks are free; the local work gate has already closed.
		if notify {
			d.renewalChanged()
			d.ownershipNotice(context.Background(), version, Viewer{Line: chatlist.Available})
		}
	}()
	prepare, cancel := context.WithTimeout(parent, requestTimeout)
	proof, finished, err := d.prepareIdle(prepare, activity)
	cancel()
	if err != nil || finished || proof == nil {
		return finished, err
	}

	// No capture, upload, deferred-write join or contended publisher lock belongs
	// here. A sender invalidates preparation without waiting for any of them.
	ctx, cancel := context.WithTimeout(parent, idleReleaseWithin)
	defer cancel()
	if !d.workMu.TryLock() {
		return false, nil
	}
	defer d.workMu.Unlock()
	d.committing.Store(true)
	defer d.committing.Store(false)
	check, finished := d.idleState(activity)
	if finished {
		return true, nil
	}
	if ctx.Err() != nil || !d.paired() || check != nil && !check() {
		return false, ctx.Err()
	}
	if !d.sealMu.TryLock() {
		return false, nil
	}
	defer d.sealMu.Unlock()
	if cellstore.Unsaved(d.cell) != 0 {
		return false, nil
	}
	head, err := cellstore.Head(d.cell)
	if err != nil || head == nil || head.Turn.ID != proof.Head || !proof.Unchanged(ctx) {
		return false, err
	}
	wasReleased := d.released.Load()
	held := d.sendLeaseSnapshot()
	previous := func() Viewer { d.mu.Lock(); defer d.mu.Unlock(); return d.viewer }()
	released, err := d.batcher.ReleaseIdle(ctx, proof.Head, func() error {
		if err := d.sync.forgetSendLeaseWithin(ctx, d.cell.ID, d.sendLeaseSnapshot()); err != nil {
			return err
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		d.recordedFence = 0
		notify = d.viewer != (Viewer{Line: chatlist.Available})
		d.viewer = Viewer{Line: chatlist.Available}
		d.idleNotices.Store(true)
		version = d.noticeVersion.Load()
		if notify {
			version = d.noticeVersion.Add(1)
		}
		d.released.Store(true)
		return nil
	})
	if err != nil && !wasReleased && d.released.Load() && syncwords.RequestNotSent(err) {
		// A canceled admission budget must still put back the receipt it retired.
		// Keep the work gate closed if local restoration itself fails.
		restore, cancel := context.WithTimeout(context.WithoutCancel(parent), idleReleaseWithin)
		defer cancel()
		if restoreErr := d.undoUnsentRelease(restore, held, previous, version); restoreErr != nil {
			return false, errors.Join(err, restoreErr)
		}
		notify = false
	}
	if err != nil || !released {
		return false, err
	}
	d.releaseDone.Store(true)
	return true, nil
}

func (d *Drive) undoUnsentRelease(ctx context.Context, held sendLease, previous Viewer, version uint64) error {
	if err := lockDrive(ctx, &d.receiptMu); err != nil {
		return err
	}
	defer d.receiptMu.Unlock()
	d.mu.Lock()
	defer d.mu.Unlock()
	// Ownership news can arrive while the dial fails. It must still win over
	// the saved viewer, and no later writer's receipt may be replaced here.
	if d.stopped || d.lost || d.noticeVersion.Load() != version {
		return nil
	}
	if err := d.sync.recordSendLeaseWithin(ctx, d.cell.ID, held); err != nil {
		return err
	}
	d.viewer, d.recordedFence = previous, held.Fence
	d.released.Store(false)
	d.noticeVersion.Add(1)
	return nil
}

func (d *Drive) paired() bool {
	if d.devices == nil {
		return false
	}
	state := d.devices.Snapshot(d.sync.Identity, d.sync.Device)
	if !state.Complete || state.Refused != nil {
		return false
	}
	n := 0
	for _, dev := range state.Devices {
		if !dev.Revoked {
			n++
		}
	}
	return n >= 2
}
