package directory

// This file is the only place lease logic lives. Each rule takes a cell and
// returns the changed cell or an error, and never touches a clock or a store:
// the caller passes the directory time in and keeps the result.

const ttlMs = int64(LeaseTTL / 1e6)

// Lifted is c with its lease kept live until vouchedUntil, the time a watch
// socket's sign of life lasts (0 for none). A released lease (expires 0) stays
// released, and a stored expiry already later is left alone. It is the one
// place evidence enters a rule: every reader of a lease asks Lifted first and
// then applies the pure rules to the answer.
func Lifted(c Cell, vouchedUntil int64) Cell {
	if c.Lease.Expires != 0 && vouchedUntil > c.Lease.Expires {
		c.Lease.Expires = vouchedUntil
	}
	return c
}

// Acquire takes the lease for device unless another device still holds it. The
// same device may re-take its own live lease; the fence still goes up. A forced
// acquire is a person's choice to displace a live holder: it skips the refusal,
// and the fence still goes up, so the old holder's next write fails as stale.
func Acquire(c Cell, device string, now int64, force bool) (Cell, error) {
	if !force && c.Lease.Expires > now && c.Lease.Device != device {
		return c, ErrLeaseHeld
	}
	c.Lease = Lease{Device: device, Fence: c.Lease.Fence + 1, Expires: now + ttlMs}
	return c, nil
}

// Heartbeat renews the lease. An expired lease nobody took may be renewed: the
// unchanged fence proves nobody did.
func Heartbeat(c Cell, device string, b Beat, now int64) (Cell, error) {
	if err := holder(c, device, b.Fence); err != nil {
		return c, err
	}
	c.Lease.Expires = now + ttlMs
	c.Lease.Pending = b.Pending
	return c, nil
}

// PublishTo moves the durable head and renews the lease, because a publish is
// proof that the holder is alive. The fence is checked before the head, so a
// superseded holder always learns it lost the lease first.
func PublishTo(c Cell, device string, p Publish, now int64) (Cell, error) {
	if err := holder(c, device, p.Fence); err != nil {
		return c, err
	}
	if p.OldHead != c.Head {
		return c, ErrHeadMoved
	}
	c.Head, c.Size, c.Class = p.Head, p.Size, p.Class
	// The frames describe the head they were set with, so every publish replaces
	// the list wholesale: an empty or absent list is a true statement ("the
	// publisher did not say"), a stale one is not kept.
	c.Frames = p.Frames
	c.Lease.Pending, c.DurableAt = p.Pending, now
	c.Lease.Expires = now + ttlMs
	c.Title = keepIfEmpty(p.Title, c.Title)
	return c, nil
}

// ReleaseOf gives the lease up. The fence stays, so the next acquire still
// goes up by one.
func ReleaseOf(c Cell, device string, fence uint64) (Cell, error) {
	if err := holder(c, device, fence); err != nil {
		return c, err
	}
	c.Lease.Expires, c.Lease.Pending = 0, 0
	return c, nil
}

// Created is the first record of a cell: the creating device holds the lease
// at fence 1. The caller has already checked that the id is free.
func Created(in CellInit, device string, now int64) Cell {
	return Cell{
		V: 1, Head: in.Head, DurableAt: now, Class: in.Class, Size: in.Size,
		ParentCell: in.ParentCell, Title: in.Title, Keys: in.Keys, OrphanTurns: in.OrphanTurns,
		Frames: in.Frames,
		Lease:  Lease{Device: device, Fence: 1, Expires: now + ttlMs},
	}
}

// holder says whether device still holds the lease at fence.
func holder(c Cell, device string, fence uint64) error {
	if c.Lease.Fence != fence || c.Lease.Device != device {
		return ErrFenceStale
	}
	return nil
}

func keepIfEmpty(next, current string) string {
	if next == "" {
		return current
	}
	return next
}

// RevokeOf marks target revoked on behalf of caller. A device cannot revoke
// itself: that is nearly always a slip, and a revoked device can no longer say
// so, so the mistake would be permanent. Revoking twice changes nothing.
func RevokeOf(target Device, caller, targetID string) (Device, error) {
	if caller == targetID {
		return target, ErrSelfRevoke
	}
	target.Revoked = true
	return target, nil
}
