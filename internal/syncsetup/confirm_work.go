package syncsetup

import (
	"context"
	"errors"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
)

// ConfirmPreparedWork bypasses admission's cached reading after task setup.
// Solo and private, unpublished chats keep their existing local door.
func (d *Drive) ConfirmPreparedWork(ctx context.Context) error {
	if !d.paired() || d.batcher == nil || d.batcher.Fence() == 0 {
		return d.Gate()
	}
	confirmed, err := d.ConfirmWork(ctx)
	if err != nil {
		return err
	}
	if !confirmed {
		if err := d.Gate(); err != nil {
			return err
		}
		return cellstore.ErrReadOnly
	}
	return d.Gate()
}

// ConfirmWork checks only this chat's directory record. Unlike CheckLease it
// never waits for a capture before reading ownership. The caller bounds ctx
// and decides how to degrade a failed read. True confirms the current fence;
// false means either a viewer or a drive that cannot confirm ownership yet.
func (d *Drive) ConfirmWork(ctx context.Context) (bool, error) {
	d.BeginWork()
	if err := lockDrive(ctx, &d.workMu); err != nil {
		return false, err
	}
	defer d.workMu.Unlock()
	d.mu.Lock()
	stopped := d.stopped
	d.mu.Unlock()
	if stopped {
		return false, cellstore.ErrReadOnly
	}
	held := d.sendLeaseSnapshot()
	if d.batcher == nil {
		return false, nil
	}
	id, fence := d.batcher.Lease()
	// Preserving orphaned work in a branch does not revive this displaced
	// window. A deliberate reopen owns that branch through a new drive.
	if id != d.directoryID {
		return false, nil
	}
	v, err := d.sync.Dir.Cell(ctx, id)
	if err != nil {
		return false, err
	}
	v, err = d.renewIdleLease(ctx, v, fence)
	if err != nil {
		return false, err
	}
	lease := v.Cell.Lease
	// A first publish or branch can change the lease during the read. That
	// answer confirms neither a takeover nor the batcher's new lease.
	if currentID, currentFence := d.batcher.Lease(); currentID != id || currentFence != fence {
		return false, nil
	}
	if lease.Expires <= v.Now {
		d.availableAfterWithin(ctx, held, lease)
		// Never resume an older tree over work saved by another computer.
		head, err := cellstore.Head(d.cell)
		if err != nil || head == nil || head.Turn.ID != v.Cell.Head {
			if err == nil && lease.Device != d.sync.Device.ID() {
				d.confirmHolder(ctx, lease)
			}
			return false, err
		}
		resumed, err := d.batcher.Reacquire(ctx, v)
		if errors.Is(err, directory.ErrLeaseHeld) || errors.Is(err, directory.ErrFenceStale) {
			// A send can lose to an automatic open after the free reading.
			// Learn the winner before returning the ordinary moved refusal.
			fresh, readErr := d.sync.Dir.Cell(ctx, id)
			if readErr != nil {
				return false, readErr
			}
			if fresh.Cell.Lease.Expires > fresh.Now {
				d.confirmHolder(ctx, fresh.Cell.Lease)
			}
			return false, nil
		}
		if err != nil || !resumed {
			return false, err
		}
		d.mu.Lock()
		d.viewer, d.lost = Viewer{}, false
		d.mu.Unlock()
		if err := d.recordSendLeaseWithin(ctx); err != nil {
			d.available()
			return false, err
		}
		d.released.Store(false)
		d.releaseDone = false
		if d.opt.OnDriving != nil {
			d.opt.OnDriving()
		}
		return true, nil
	}
	if lease.Device == d.sync.Device.ID() && lease.Fence == fence {
		if d.released.Load() && !d.batcher.ResumeHeld(ctx, v) {
			return false, nil
		}
		d.released.Store(false)
		d.releaseDone = false
		d.resumeViewer()
		if err := d.recordSendLeaseWithin(ctx); err != nil {
			d.available()
			return false, err
		}
		return true, nil
	}
	if lease.Device == d.sync.Device.ID() {
		d.availableAfterWithin(ctx, held, lease)
		return false, nil
	}

	d.confirmHolder(ctx, lease)
	return false, nil
}

func (d *Drive) confirmHolder(ctx context.Context, lease directory.Lease) {
	// Naming the holder shares the admission budget. Once the fence is known
	// lost, a slow name lookup cannot turn the refusal back into admission.
	name := chatlist.DeviceName(nil, lease.Device, nil)
	if lease.Device == "" {
		name = unknownDevice
	} else if listing, err := d.sync.Dir.List(ctx); err == nil {
		key := directory.MetadataKey(d.sync.Identity.CellKey())
		name = chatlist.DeviceName(listing.Devices, lease.Device, func(sealed string) (string, error) { return directory.OpenName(key, sealed) })
	}
	viewer := Viewer{ID: lease.Device, Name: name, Line: chatlist.Superseded(name)}
	d.mu.Lock()
	if d.viewer == viewer {
		d.mu.Unlock()
		return
	}
	d.viewer = viewer
	expected := sendLease{Cell: d.directoryID, Fence: d.recordedFence}
	d.mu.Unlock()
	if d.opt.OnViewer != nil {
		d.opt.OnViewer(viewer)
	}
	if d.opt.OnRenewalChange != nil {
		d.opt.OnRenewalChange()
	}
	// Match becomeViewer's capture-before-notice ordering in the background:
	// pending saves must settle, but admission cannot inherit their disk wait.
	go func() {
		if err := d.sync.forgetSendLease(d.cell.ID, expected); err != nil {
			d.notice(err.Error())
		}
		pending, cancel := context.WithTimeout(context.Background(), cellstore.SealWithin)
		defer cancel()
		if err := cellstore.Await(pending, d.cell); err != nil {
			d.notice(err.Error())
		}
		d.notice(viewer.Line)
	}()
}

func (d *Drive) resumeViewer() {
	d.mu.Lock()
	if d.stopped || d.released.Load() {
		d.mu.Unlock()
		return
	}
	changed := d.viewer.Line != "" || d.lost
	d.viewer, d.lost = Viewer{}, false
	d.mu.Unlock()
	if changed && d.opt.OnDriving != nil {
		d.opt.OnDriving()
	}
}

// available retains the work gate until ownership is confirmed again, without
// claiming that the last holder is still working in a released chat.
func (d *Drive) available() {
	d.availableAfter(sendLease{}, directory.Lease{})
}

// availableAfter closes the work gate before receipt I/O can block and retires
// only the receipt held before the reading, so a concurrent reacquisition survives.
// An unchanged owner and fence keeps its receipt even when released, because a
// heartbeat can still renew it. Readers hear the change after retirement finishes.
func (d *Drive) availableAfter(held sendLease, reading directory.Lease) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	d.availableAfterWithin(ctx, held, reading)
}

func (d *Drive) availableAfterWithin(ctx context.Context, held sendLease, reading directory.Lease) {
	viewer := Viewer{Line: chatlist.Available}
	d.mu.Lock()
	if held.Fence != 0 && held.Fence != d.recordedFence {
		d.mu.Unlock()
		return
	}
	changed := d.viewer != viewer
	d.viewer = viewer
	d.mu.Unlock()
	if held.Fence != 0 && (reading.Device != d.sync.Device.ID() || reading.Fence != held.Fence) {
		if err := d.sync.forgetSendLeaseWithin(ctx, d.cell.ID, held); err != nil {
			d.notice(err.Error())
		}
	}
	if changed {
		d.renewalChanged()
	}
	if changed && d.opt.OnViewer != nil {
		d.opt.OnViewer(viewer)
	}
}
