package cellsync

import (
	"errors"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// RenewalStatus reads synchronized local health without contacting the relay.
func (b *Batcher) RenewalStatus() (time.Time, time.Duration, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	held := b.Driving.Fence != 0 && !b.stale && !b.renewalEnded
	return b.renewalSince, directory.HeartbeatEvery, held
}

// renewalResult belongs only to the hold the request actually renewed.
// Late replies from a displaced writer cannot report recovery for a new hold.
func (b *Batcher) renewalResult(seen Driving, err error) {
	b.mu.Lock()
	changed := false
	if b.Driving.ID() == seen.ID() && b.Driving.Fence == seen.Fence && !b.stale && !b.renewalEnded {
		switch {
		case errors.Is(err, directory.ErrFenceStale), errors.Is(err, wireauth.ErrRevoked), errors.Is(err, wireauth.ErrGone), errors.Is(err, wireauth.ErrRotated):
			b.renewalEnded, b.renewalSince, changed = true, time.Time{}, true
		case err == nil:
			changed = !b.renewalSince.IsZero()
			b.renewalSince = time.Time{}
		case Unreachable(err):
			if seen.Fence != 0 && b.renewalSince.IsZero() {
				b.renewalSince, changed = b.now(), true
			}
		}
	}
	b.mu.Unlock()
	if changed {
		b.renewalChanged()
	}
}

func (b *Batcher) endRenewals() {
	b.mu.Lock()
	changed := !b.renewalEnded
	b.renewalEnded, b.renewalSince = true, time.Time{}
	b.mu.Unlock()
	if changed {
		b.renewalChanged()
	}
}

func (b *Batcher) renewalChanged() {
	if b.OnRenewalChange != nil {
		b.OnRenewalChange()
	}
}
