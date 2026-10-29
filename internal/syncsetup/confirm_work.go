package syncsetup

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
)

// ConfirmWork checks only this chat's directory record. Unlike CheckLease it
// never waits for a capture before reading ownership. The caller bounds ctx
// and decides how to degrade a failed read. True confirms the current fence;
// false means either a viewer or a drive that cannot confirm ownership yet.
func (d *Drive) ConfirmWork(ctx context.Context) (bool, error) {
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
		d.retireSendLease(lease)
		d.available()
		// Never resume an older tree over work saved by another computer.
		head, err := cellstore.Head(d.cell)
		if err != nil || head == nil || head.Turn.ID != v.Cell.Head {
			return false, err
		}
		resumed, err := d.batcher.Reacquire(ctx, v)
		if err != nil || !resumed {
			return false, err
		}
		d.mu.Lock()
		d.viewer = Viewer{}
		d.mu.Unlock()
		if err := d.recordSendLease(); err != nil {
			d.available()
			return false, err
		}
		if d.opt.OnDriving != nil {
			d.opt.OnDriving()
		}
		return true, nil
	}
	if lease.Device == d.sync.Device.ID() && lease.Fence == fence {
		d.resumeViewer()
		return true, nil
	}
	if lease.Device == d.sync.Device.ID() {
		d.retireSendLease(lease)
		d.available()
		return false, nil
	}

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
	if d.viewer.Line != "" {
		d.mu.Unlock()
		return false, nil
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
	return false, nil
}

func (d *Drive) resumeViewer() {
	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		return
	}
	changed := d.viewer.Line != ""
	d.viewer = Viewer{}
	d.mu.Unlock()
	if changed && d.opt.OnDriving != nil {
		d.opt.OnDriving()
	}
}

// retireSendLease keeps the recorded receipt when the relay still names this
// device at that fence with a nonzero expiry, even if it has lapsed, because
// this drive may renew it. Otherwise it conditionally removes that receipt
// before notifying readers, preserving any newer writer's receipt. A zero
// recorded fence has no receipt to retire. Removal errors are reported as notices.
func (d *Drive) retireSendLease(lease directory.Lease) {
	d.mu.Lock()
	expected := sendLease{Cell: d.directoryID, Fence: d.recordedFence}
	d.mu.Unlock()
	if expected.Fence == 0 || lease.Device == d.sync.Device.ID() && lease.Fence == expected.Fence && lease.Expires != 0 {
		return
	}
	if err := d.sync.forgetSendLease(d.cell.ID, expected); err != nil {
		d.notice(err.Error())
	}
}

// available retains the work gate until admission acquires a new fence, without
// claiming that the last holder is still working in a released chat.
func (d *Drive) available() {
	viewer := Viewer{Line: chatlist.Available}
	d.mu.Lock()
	changed := d.viewer != viewer
	d.viewer = viewer
	d.mu.Unlock()
	if changed {
		d.renewalChanged()
	}
	if changed && d.opt.OnViewer != nil {
		d.opt.OnViewer(viewer)
	}
}
