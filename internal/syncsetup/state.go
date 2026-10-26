package syncsetup

import (
	"context"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/keys"
	"github.com/Agent-Field/codeaf/internal/vaultsync"
)

var stateMu sync.Mutex

// SyncState completes one encrypted home-state round even when no chat is
// open. The quiet gate is checked before any capture or request (C30.4).
func (s *Sync) SyncState(ctx context.Context) error {
	if s.isRemoved() {
		return directory.ErrRevoked
	}
	if Quiet(s.Home) {
		return nil
	}
	rid, local, err := s.memberVault(ctx)
	if err != nil {
		return err
	}
	return s.withVault(func(v vaultsync.Syncer) error {
		if err := v.Capture(); err != nil {
			return err
		}
		fp, exists := keys.Fingerprint(s.Home)
		if exists && fp != s.stateSent {
			if err := v.Push(ctx); err != nil {
				return err
			}
			s.stateVaultKnown = false
		} else if !local || !s.stateVaultKnown || rid != s.stateVault {
			if local {
				err = v.PullAt(ctx, rid)
			} else {
				err = v.Pull(ctx)
			}
			if err != nil {
				return err
			}
			s.stateVault, s.stateVaultKnown = rid, local
		}
		s.stateSent, _ = keys.Fingerprint(s.Home)
		return nil
	})
}

// RunState is owned and joined by the terminal's lifetime. Pairing while Home
// stands open starts sharing on the next round without restarting codeaf.
func (s *Sync) RunState(ctx context.Context) {
	ticker := time.NewTicker(s.Interval)
	defer ticker.Stop()
	s.runState(ctx, ticker.C)
}

// runState shares the terminal lifetime while accepting sampled host ticks.
func (s *Sync) runState(ctx context.Context, ticks <-chan time.Time) {
	defer func() { s.stopMember() }()
	for {
		if ctx.Err() != nil {
			return
		}
		next, _ := s.stateRound(ctx)
		// A newly adopted identity owns its learner even if its first vault round
		// fails. Keeping the old handle here would orphan the new lifetime.
		s = next
		select {
		case <-ctx.Done():
			return
		case <-ticks:
		}
	}
}

// stateRound notices a pairing or relay change made while Home stays open.
// Reusing the old signer would send the new state to the fleet it just left.
func (s *Sync) stateRound(ctx context.Context) (*Sync, error) {
	next, ok, err := Open(s.Home)
	if err != nil || !ok {
		s.stopMember()
		return s, err
	}
	if next.Identity.ID() == s.Identity.ID() && next.Device.ID() == s.Device.ID() && next.Relay == s.Relay {
		next = s
	} else {
		s.stopMember()
	}
	if next.isRemoved() {
		next.stopMember()
		return next, directory.ErrRevoked
	}
	next.memberRound(ctx)
	return next, next.SyncState(ctx)
}
