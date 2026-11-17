package syncsetup

import (
	"context"
	"errors"

	"github.com/Agent-Field/codeaf/internal/cellstore"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/syncwords"
)

// ConfirmSettingsWork leaves unpaired and unreleased choices local as before.
// A released writer must retake its hold before acknowledging a durable choice.
func (d *Drive) ConfirmSettingsWork(ctx context.Context) error {
	if !d.released.Load() && !d.committing.Load() {
		return nil
	}
	if !d.BeginWork() {
		d.SavedMetadata()
		return nil
	}
	confirmed, err := d.ConfirmWork(ctx)
	if err != nil || !confirmed {
		return d.refusedSetting()
	}
	// The session counts this setting until its save or refusal completes.
	// A failed setting must also return an otherwise idle retaken hold.
	d.SavedMetadata()
	if d.Gate() != nil {
		return d.refusedSetting()
	}
	return nil
}

// Every refused idle setting carries ownership facts across legacy RPC doors.
// Ordinary transport failures keep their pre-existing client behavior.
func (d *Drive) refusedSetting() error {
	err := d.Gate()
	var refusal *executor.WorkRefusal
	if errors.As(err, &refusal) {
		return refusal
	}
	line := chatlist.Available
	if err != nil {
		line = err.Error()
	}
	return &executor.WorkRefusal{Line: line}
}

// ConfirmPreparedWork bypasses admission's cached reading after task setup.
// Solo and private, unpublished chats keep their existing local door.
func (d *Drive) ConfirmPreparedWork(ctx context.Context) error {
	if !d.paired() || d.batcher == nil || d.batcher.Fence() == 0 {
		if d.released.Load() {
			return d.Gate()
		}
		return nil
	}
	confirmed, err := d.ConfirmWork(ctx)
	if err != nil {
		// A failed read retains ordinary offline admission for a held writer.
		// Released, displaced and stopped writers remain closed by Gate.
		return d.Gate()
	}
	if !confirmed {
		if err := d.Gate(); err != nil {
			return err
		}
		return cellstore.ErrReadOnly
	}
	return d.Gate()
}

// ConfirmWork checks only this chat's directory record without waiting for
// capture before reading ownership. The caller bounds ctx and decides how to
// degrade a failed read. True confirms the current fence; false means either a
// viewer or a drive that cannot confirm ownership yet.
func (d *Drive) ConfirmWork(ctx context.Context) (bool, error) {
	idleAdmission := d.BeginWork()
	if idleAdmission {
		var pending []ownershipNotice
		ctx = context.WithValue(ctx, ownershipNoticesKey{}, &pending)
		defer func() {
			for _, n := range pending {
				d.deliverOwnership(n)
			}
		}()
		if err := lockDrive(ctx, &d.workMu); err != nil {
			return false, err
		}
		defer d.workMu.Unlock()
	}
	held := d.sendLeaseSnapshot()
	stopped := func() bool {
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.stopped
	}()
	if stopped {
		return false, cellstore.ErrReadOnly
	}
	if d.batcher == nil {
		return false, nil
	}
	id, fence := d.batcher.Lease()
	// Preserving orphaned work in a branch does not revive this displaced
	// window. A deliberate reopen owns that branch through a new drive.
	if id != d.directoryID {
		v, err := d.sync.Dir.Cell(ctx, d.directoryID)
		d.retakeOffline.Store(syncwords.NetworkFailure(err) || errors.Is(err, context.DeadlineExceeded))
		if err != nil {
			return false, err
		}
		if v.Cell.Lease.Device == d.sync.Device.ID() {
			d.availableAfterWithin(ctx, held, v.Cell.Lease)
		} else {
			d.confirmHolder(ctx, v.Cell.Lease)
		}
		return false, nil
	}
	v, err := d.sync.Dir.Cell(ctx, id)
	d.retakeOffline.Store(syncwords.NetworkFailure(err) || errors.Is(err, context.DeadlineExceeded))
	if err != nil {
		return false, err
	}
	lease := v.Cell.Lease
	// A first publish or branch can change the lease during the read. That
	// answer confirms neither a takeover nor the batcher's new lease.
	if currentID, currentFence := d.batcher.Lease(); currentID != id || currentFence != fence {
		return false, nil
	}
	switch d.workOwnership(v, fence) {
	case workUncertain:
		return false, d.Gate()
	case workConsent:
		// A free higher fence still records a takeover. Retiring renewal beats
		// here prevents a subsequent heartbeat from reviving that writer.
		if lease.Device == d.sync.Device.ID() {
			d.availableAfter(held, lease)
		} else {
			d.confirmHolder(ctx, lease)
		}
		d.batcher.RetireFence(id, fence)
		return false, nil
	case workReopen:
		if d.canReacquireWorking(v, fence) {
			if ok, err := d.reacquireWorking(ctx, v); err != nil || ok {
				return ok, err
			}
		}
		if !d.released.Load() {
			d.retireHold(ctx, held)
		}
		d.availableAfterWithin(ctx, held, lease)
		d.batcher.RetireFence(id, fence)
		return false, nil
	case workAcquire:
		d.batcher.RetireFence(id, fence)
		d.availableAfterWithin(ctx, held, lease)
		// Only a self-initiated release can retake in place. Its saved head
		// must still match, and release requires all local turns published.
		if err := cellstore.Captured(ctx, d.cell); err != nil {
			return false, err
		}
		head, err := cellstore.Head(d.cell)
		if err != nil || head == nil || head.Turn.ID != v.Cell.Head {
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
			if d.batcher.AcquireUncertain() {
				return false, d.Gate()
			}
			if fresh.Cell.Lease.Expires > fresh.Now {
				d.confirmHolder(ctx, fresh.Cell.Lease)
			}
			return false, nil
		}
		if err != nil || !resumed {
			if d.batcher.AcquireUncertain() {
				d.uncertainAcquire(ctx)
			}
			return false, err
		}
		return d.finishIdleAcquire(ctx)
	default:
		d.releaseDone.Store(false)
		d.resumeViewerWithin(ctx)
		return true, nil
	}
}

func (d *Drive) canReacquireWorking(v directory.CellView, fence uint64) bool {
	if d == nil || d.batcher == nil || !d.working.Load() || d.released.Load() || d.displaced() {
		return false
	}
	if d.run == nil || d.run.Err() != nil {
		return false
	}
	lease := v.Cell.Lease
	return lease.Device == d.sync.Device.ID() && lease.Fence == fence && lease.Expires > 0 && lease.Expires <= v.Now
}

func (d *Drive) reacquireWorking(ctx context.Context, v directory.CellView) (bool, error) {
	if err := cellstore.Captured(ctx, d.cell); err != nil {
		return false, err
	}
	extends, err := cellstore.Extends(d.cell, v.Cell.Head)
	if err == nil && v.Cell.Head == "" && v.Cell.Lease.Device == d.sync.Device.ID() {
		extends = true
	}
	if err != nil || !extends {
		return false, err
	}
	resumed, err := d.batcher.ReacquireWorking(ctx, v)
	if errors.Is(err, directory.ErrLeaseHeld) || errors.Is(err, directory.ErrFenceStale) || errors.Is(err, directory.ErrConditionalTake) {
		return false, nil
	}
	if err != nil || !resumed {
		return false, err
	}
	if err := d.confirmSendLease(ctx); err != nil {
		return false, err
	}
	d.resumeViewerWithin(ctx)
	d.ownershipNotice(ctx, d.noticeVersion.Add(1), Viewer{})
	return true, nil
}

type workOwnership uint8

const (
	workHeld workOwnership = iota
	workAcquire
	workConsent
	workUncertain
	workReopen
)

// workOwnership is the drive's admission decision for both direct engine work
// and the terminal's CheckWork door. A fresh listing alone cannot restore a
// retired fence, and a higher fence remembers a takeover even after release.
func (d *Drive) workOwnership(v directory.CellView, fence uint64) workOwnership {
	lease := v.Cell.Lease
	if lease.Device != d.sync.Device.ID() {
		return workConsent
	}
	if lease.Fence != fence {
		if d.released.Load() && d.batcher.AcquireUncertain() {
			if lease.Expires > v.Now {
				return workUncertain
			}
			return workAcquire
		}
		return workConsent
	}
	if d.released.Load() {
		return workAcquire
	}
	if lease.Expires <= v.Now || d.displaced() {
		return workReopen
	}
	return workHeld
}

func (d *Drive) confirmHolder(ctx context.Context, lease directory.Lease) {
	// Naming the holder shares the admission budget. Once the fence is known
	// lost, a slow name lookup cannot turn the refusal back into admission.
	name := chatlist.DeviceName(nil, lease.Device, nil)
	if lease.Device == "" {
		name = unknownDevice
	}
	if l, err := d.sync.Dir.List(ctx); err == nil {
		key := directory.MetadataKey(d.sync.Identity.CellKey())
		name = chatlist.DeviceName(l.Devices, lease.Device, func(sealed string) (string, error) {
			return directory.OpenName(key, sealed)
		})
	}
	viewer := Viewer{ID: lease.Device, Name: name, Line: chatlist.Superseded(name)}
	d.mu.Lock()
	d.lost = true
	if !d.idleNotices.Load() && d.viewer.Line != "" {
		d.mu.Unlock()
		return
	}
	d.viewer = viewer
	version := d.noticeVersion.Add(1)
	expected := sendLease{Cell: d.directoryID, Fence: d.recordedFence}
	d.mu.Unlock()
	d.ownershipNotice(ctx, version, viewer)
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

func (d *Drive) resumeViewer() { d.resumeViewerWithin(context.Background()) }

func (d *Drive) resumeViewerWithin(ctx context.Context) {
	d.mu.Lock()
	if d.stopped || d.released.Load() {
		d.mu.Unlock()
		return
	}
	changed := d.viewer.Line != "" || d.lost
	d.viewer, d.lost = Viewer{}, false
	version := d.noticeVersion.Load()
	if changed {
		version = d.noticeVersion.Add(1)
	}
	d.mu.Unlock()
	if changed {
		d.ownershipNotice(ctx, version, Viewer{})
	}
}

// available retains the work gate until ownership is confirmed again, without
// claiming that the last holder is still working in a released chat.
func (d *Drive) available() {
	d.availableAfter(sendLease{}, directory.Lease{})
}

// availableAfter closes the work gate before receipt I/O can block and retires
// only the receipt held before the reading, so a concurrent reacquisition survives.
// Expiry and release retire the receipt too. Readers hear the change after
// retirement finishes; continuation must acquire a new hold.
func (d *Drive) availableAfter(held sendLease, reading directory.Lease) {
	if d.idleNotices.Load() {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		d.availableAfterWithin(ctx, held, reading)
		return
	}
	viewer := Viewer{Line: chatlist.Available}
	d.mu.Lock()
	changed := d.viewer != viewer
	d.viewer = viewer
	d.mu.Unlock()
	if d.batcher != nil && held.Fence != 0 && reading.Device == d.sync.Device.ID() && reading.Fence == held.Fence {
		d.batcher.RetireFence(d.directoryID, held.Fence)
	}
	if held.Fence != 0 {
		if err := d.sync.forgetSendLease(d.cell.ID, held); err != nil {
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

func (d *Drive) availableAfterWithin(ctx context.Context, held sendLease, reading directory.Lease) {
	if d.released.Load() && d.batcher != nil && d.batcher.AcquireUncertain() {
		return
	}
	viewer := Viewer{Line: chatlist.Available}
	d.mu.Lock()
	// A zero receipt belongs to the released viewer. Reacquisition clears
	// that viewer before receipt I/O, so a late observation cannot close its
	// gate in the gap. An empty snapshot intentionally forces a refusal.
	if held.Cell != "" && (held.Fence != d.recordedFence || held.Fence == 0 && d.viewer.Line == "") {
		d.mu.Unlock()
		return
	}
	changed := d.viewer != viewer
	d.viewer = viewer
	version := d.noticeVersion.Load()
	if changed {
		version = d.noticeVersion.Add(1)
	}
	d.mu.Unlock()
	if d.batcher != nil && held.Fence != 0 && reading.Device == d.sync.Device.ID() && reading.Fence == held.Fence {
		d.batcher.RetireFence(d.directoryID, held.Fence)
	}
	if held.Fence != 0 {
		if err := d.sync.forgetSendLeaseWithin(ctx, d.cell.ID, held); err != nil {
			d.notice(err.Error())
		}
	}
	if changed {
		d.renewalChanged()
	}
	if changed {
		d.ownershipNotice(ctx, version, viewer)
	}
}

// confirmSendLease uses bounded receipt I/O for idle-certified writers.
func (d *Drive) confirmSendLease(ctx context.Context) error {
	if d.idleNotices.Load() {
		return d.recordSendLeaseWithin(ctx)
	}
	return d.recordSendLease()
}

func (d *Drive) uncertainAcquire(ctx context.Context) {
	viewer := Viewer{Line: executor.TakeBackRetryLine}
	version := func() uint64 {
		d.mu.Lock()
		defer d.mu.Unlock()
		d.viewer = viewer
		return d.noticeVersion.Add(1)
	}()
	d.ownershipNotice(ctx, version, viewer)
}

// finishIdleAcquire records authority before opening the work door.
// The caller holds workMu for this self-initiated retake.
func (d *Drive) finishIdleAcquire(ctx context.Context) (bool, error) {
	version := func() uint64 {
		d.mu.Lock()
		defer d.mu.Unlock()
		d.viewer, d.lost = Viewer{}, false
		return d.noticeVersion.Add(1)
	}()
	if err := d.confirmSendLease(ctx); err != nil {
		d.availableAfterWithin(ctx, sendLease{}, directory.Lease{})
		return false, err
	}
	d.released.Store(false)
	d.releaseDone.Store(false)
	d.ownershipNotice(ctx, version, Viewer{})
	return true, nil
}
