package syncsetup

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/Agent-Field/codeaf/internal/clusterctl"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftdirectory"
	"go.etcd.io/raft/v3"
)

// LeaderQuitWithin bounds the extra time a clean terminal quit can spend
// returning coordination to the anchors. Unreachable anchors retain fallback.
const LeaderQuitWithin = 5 * time.Second

const leaderQuitRetryEvery = 50 * time.Millisecond
const leaderQuitTransferEvery = time.Second

// Coordinating is a local lifetime hint, never authority for a directory read
// or write. It does not wait behind a learner's ongoing replication work.
func (s *Sync) Coordinating() bool {
	if !s.memberAllowed() {
		return false
	}
	m := s.displayMember()
	return m != nil && m.coordinating.Load()
}

// HandBack runs after the terminal has let its chats go and before its state
// owner stops. A local learner or a home without a running member stays quiet.
// A command-scoped member and an unclean disconnection keep their old lifetime.
func (s *Sync) HandBack(ctx context.Context) error {
	if !s.memberAllowed() {
		return nil
	}
	m := s.displayMember()
	if m == nil || !m.coordinating.Load() {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), LeaderQuitWithin)
	defer cancel()
	ticker := time.NewTicker(leaderQuitRetryEvery)
	defer ticker.Stop()
	lock := func() error {
		for !m.mu.TryLock() {
			select {
			case <-ticker.C:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}
	if err := lock(); err != nil {
		return err
	}
	if m.closed || m.status.Role != raft.StateLeader {
		m.mu.Unlock()
		return nil
	}
	h, err := raftdirectory.LeadershipState(ctx, m.store)
	term := m.status.Term
	m.mu.Unlock()
	if err != nil {
		return err
	}
	var transferred time.Time
	next := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := lock(); err != nil {
			return err
		}
		if m.closed || m.failure != nil {
			err := m.failure
			m.mu.Unlock()
			if err == nil {
				err = clusterctl.ErrUnavailable
			}
			return err
		}
		anchor := m.status.Term > term && slices.Contains(h.Anchors, m.status.Leader)
		if m.status.Role == raft.StateLeader && time.Since(transferred) >= leaderQuitTransferEvery {
			// The core checks catch-up and current voting membership. Cycling
			// eligible anchors lets a surviving voter take over if another is lost.
			for range h.Anchors {
				target := h.Anchors[next%len(h.Anchors)]
				next++
				err = m.step(ctx, raftcore.Transfer{Target: target})
				if errors.Is(err, raftcore.ErrTransfer) {
					continue
				}
				if err == nil {
					m.sleeping = false
					err = m.flushPeer(ctx)
					transferred = time.Now()
				}
				break
			}
		}
		changed := m.changed
		m.mu.Unlock()
		if err != nil && !errors.Is(err, raftcore.ErrTransfer) {
			return err
		}
		if anchor {
			// Observing a higher term alone does not prove the new leader can
			// serve the next home. Its ordinary fresh majority read does.
			status, e := s.clusterClient().Status(ctx)
			if e == nil && slices.Contains(h.Anchors, status.Leader) {
				return nil
			}
		}
		select {
		case <-changed:
		case <-ticker.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
