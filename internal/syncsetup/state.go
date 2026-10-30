package syncsetup

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/keys"
	"github.com/Agent-Field/codeaf/internal/teams"
	"github.com/Agent-Field/codeaf/internal/vaultsync"
)

var stateMu sync.Mutex

// RunHomeState keeps a serving computer's saved keys, preferences and shared
// memories current without starting a cluster member. The caller owns and joins
// its lifetime. Quiet, unpaired and removed homes end silently; other failures
// retry after the sync interval and are reported once per distinct error.
func RunHomeState(ctx context.Context, home string, report func(error)) {
	interval, err := flushInterval()
	if err != nil {
		interval = DefaultInterval
	}
	ticks := time.NewTicker(interval)
	defer ticks.Stop()
	var current *Sync
	reported := make(map[string]bool)
	for ctx.Err() == nil {
		if cell.UnavailableReason() != "" || Quiet(home) {
			return
		}
		next, ok, err := Open(home)
		if errors.Is(err, ErrNoIdentity) || (err == nil && !ok) {
			return
		}
		if err == nil {
			if current == nil || next.Identity.ID() != current.Identity.ID() || next.Device.ID() != current.Device.ID() || next.Relay != current.Relay {
				current = next
			}
			if current.isRemoved() {
				return
			}
			err = current.SyncState(ctx)
		}
		// A pairing can be removed while a request is in flight. Its quiet
		// refusal ends this lifetime without turning it into a sync warning.
		if Quiet(home) || errors.Is(err, ErrQuiet) {
			return
		}
		if errors.Is(err, directory.ErrRevoked) || (current != nil && current.isRemoved()) {
			return
		}
		if err != nil && ctx.Err() == nil && !reported[err.Error()] {
			reported[err.Error()] = true
			if report != nil {
				report(err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticks.C:
		}
	}
}

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
		// A continuation may leave only local paths to repair, with identical
		// portable values and no new vault pointer to pull. Apply those
		// bindings before capture; a failed repair waits for the next round.
		teams.SharingError(s.profile(), (teamKeys{s.profile(), s.Home, s.Device.ID(), s.teamsCache}).Refresh())
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
