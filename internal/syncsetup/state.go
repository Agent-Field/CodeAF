package syncsetup

import (
	"context"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/keys"
	"github.com/Agent-Field/codeaf/internal/vaultsync"
)

var stateMu sync.Mutex

// SyncState completes one encrypted home-state round even when no chat is
// open. The quiet gate is checked before any capture or request (C30.4).
func (s *Sync) SyncState(ctx context.Context) error {
	if Quiet(s.Home) {
		return nil
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
		} else if err := v.Pull(ctx); err != nil {
			return err
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
	for {
		if ctx.Err() != nil {
			return
		}
		if next, err := s.stateRound(ctx); err == nil {
			s = next
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// stateRound notices a pairing or relay change made while Home stays open.
// Reusing the old signer would send the new state to the fleet it just left.
func (s *Sync) stateRound(ctx context.Context) (*Sync, error) {
	next, ok, err := Open(s.Home)
	if err != nil || !ok {
		return s, err
	}
	if next.Identity.ID() == s.Identity.ID() && next.Device.ID() == s.Device.ID() && next.Relay == s.Relay {
		next = s
	}
	return next, next.SyncState(ctx)
}
