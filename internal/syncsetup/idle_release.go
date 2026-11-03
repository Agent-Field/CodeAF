package syncsetup

import (
	"context"
	"path/filepath"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/chatlist"
)

// idleReleaseWithin bounds the final, upload-free ownership boundary.
const idleReleaseWithin = 250 * time.Millisecond

func (d *Drive) SetIdleCheck(check func() bool) {
	d.workMu.Lock()
	d.idleCheck = check
	d.workMu.Unlock()
}

// BeginWork cancels preparation immediately. Only the short final release can
// be in flight, in which case admission must bypass its cached confirmation.
func (d *Drive) BeginWork() bool {
	d.activity.Add(1)
	d.idleMu.Lock()
	if d.idleStop != nil {
		d.idleStop()
	}
	d.idleMu.Unlock()
	return d.released.Load() || d.committing.Load()
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
	d.idleMu.Lock()
	d.idleClosed = true
	if d.idleStop != nil {
		d.idleStop()
	}
	d.idleMu.Unlock()
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
		finished, err := d.releaseIdle(ctx, activity)
		if finished {
			return
		}
		if err != nil {
			wait = min(max(wait*2, 5*time.Second), time.Minute)
		}
	}
}

func (d *Drive) idleState(activity uint64) (func() bool, bool) {
	d.mu.Lock()
	stopped := d.stopped || d.lost || d.viewer.Line != "" && !(d.released.Load() && d.viewer.Line == chatlist.Available)
	d.mu.Unlock()
	return d.idleCheck, activity != d.activity.Load() || d.releaseDone || stopped
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
	if check != nil && !check() || d.batcher.Pending() != 0 {
		return nil, false, nil
	}
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
	if err := d.syncVault(ctx); err != nil {
		return nil, false, err
	}
	return proof, false, nil
}

func (d *Drive) releaseIdle(parent context.Context, activity uint64) (bool, error) {
	notify := false
	defer func() {
		// Surface callbacks can take their own locks. They run after admission
		// and capture locks are free; the local work gate has already closed.
		if notify {
			d.renewalChanged()
			if d.opt.OnViewer != nil {
				d.opt.OnViewer(Viewer{Line: chatlist.Available})
			}
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
	head, err := cellstore.Head(d.cell)
	if err != nil || head == nil || head.Turn.ID != proof.Head || !proof.Unchanged(ctx) {
		return false, err
	}
	released, err := d.batcher.ReleaseIdle(ctx, proof.Head, func() error {
		if err := d.sync.forgetSendLeaseWithin(ctx, d.cell.ID, d.sendLeaseSnapshot()); err != nil {
			return err
		}
		d.mu.Lock()
		d.recordedFence = 0
		notify = d.viewer != (Viewer{Line: chatlist.Available})
		d.viewer = Viewer{Line: chatlist.Available}
		d.mu.Unlock()
		d.released.Store(true)
		return nil
	})
	if err != nil || !released {
		return false, err
	}
	d.releaseDone = true
	return true, nil
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
