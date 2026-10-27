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
	if _, viewer := d.Viewer(); viewer || d.batcher == nil {
		return false, nil
	}
	id, fence := d.batcher.Lease()
	v, err := d.sync.Dir.Cell(ctx, id)
	if err != nil {
		return false, err
	}
	lease := v.Cell.Lease
	// A first publish or branch can change the lease during the read. That
	// answer confirms neither a takeover nor the batcher's new lease.
	if currentID, currentFence := d.batcher.Lease(); currentID != id || currentFence != fence {
		return false, nil
	}
	if lease.Device == d.sync.Device.ID() && lease.Fence == fence {
		return true, nil
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
	d.mu.Unlock()
	if d.opt.OnViewer != nil {
		d.opt.OnViewer(viewer)
	}
	// Match becomeViewer's capture-before-notice ordering in the background:
	// pending saves must settle, but admission cannot inherit their disk wait.
	go func() {
		pending, cancel := context.WithTimeout(context.Background(), cellstore.SealWithin)
		defer cancel()
		if err := cellstore.Await(pending, d.cell); err != nil {
			d.notice(err.Error())
		}
		d.notice(viewer.Line)
	}()
	return false, nil
}
