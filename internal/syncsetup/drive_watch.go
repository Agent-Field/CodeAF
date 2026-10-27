package syncsetup

import "context"

// followOwnership turns directory change news into a fresh reading of this
// writer's hold. Losing ownership cannot wait for a heartbeat or another turn.
func (d *Drive) followOwnership(ctx context.Context) {
	feed := d.sync.Follow()
	if feed == nil {
		return
	}
	defer feed.Close()
	var version uint64
	for {
		state := feed.State()
		if state.Version > version && d.batcher.Fence() != 0 {
			version = state.Version
			read, cancel := context.WithTimeout(ctx, requestTimeout)
			_ = d.CheckLease(read)
			cancel()
		}
		if _, viewer := d.Viewer(); viewer || state.Refused != nil {
			return
		}
		select {
		case _, open := <-feed.Changes():
			if !open {
				return
			}
		case <-ctx.Done():
			return
		case <-d.ownership:
		}
	}
}

// renewalChanged also wakes a watch reading deferred until the first publish
// established its fence. Ownership news must not be spent before that reply.
func (d *Drive) renewalChanged() {
	select {
	case d.ownership <- struct{}{}:
	default:
	}
	if d.opt.OnRenewalChange != nil {
		d.opt.OnRenewalChange()
	}
}
