package syncsetup

import "context"

type ownershipNotice struct {
	version uint64
	viewer  Viewer
}

type ownershipNoticesKey struct{}

// Idle transitions and their later acquisitions share one notification order.
// Admission collects callbacks until its writer locks have been released.
func (d *Drive) ownershipNotice(ctx context.Context, version uint64, viewer Viewer) {
	n := ownershipNotice{version, viewer}
	if d.idleNotices.Load() {
		if pending, ok := ctx.Value(ownershipNoticesKey{}).(*[]ownershipNotice); ok {
			*pending = append(*pending, n)
			return
		}
	}
	d.deliverOwnership(n)
}

func (d *Drive) deliverOwnership(n ownershipNotice) {
	if d.idleNotices.Load() {
		d.noticeMu.Lock()
		defer d.noticeMu.Unlock()
		if n.version != d.noticeVersion.Load() {
			return
		}
	}
	if n.viewer.Line != "" {
		if d.opt.OnViewer != nil {
			d.opt.OnViewer(n.viewer)
		}
	} else if d.opt.OnDriving != nil {
		d.opt.OnDriving()
	}
}
