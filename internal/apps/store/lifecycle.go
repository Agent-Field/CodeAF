package store

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

const deletedRetention = 30 * 24 * time.Hour

// Delete hides a kept app immediately and retains its folder for recovery.
// The personal owner is the only caller; workspace roles belong to wave 2.
func (s *Service) Delete(ctx context.Context, caller svc.Caller, app, confirmation string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if caller.Member != svc.Owner || caller.Agent {
		return bridge.Errorf(bridge.CodeDenied, "only the owner may delete a personal app")
	}
	m, err := s.lookup(ctx, app)
	if err != nil {
		return err
	}
	if m.State != bridge.StateKept && m.State != StateArchived {
		return bridge.Errorf(bridge.CodeConflict, "only a kept app can be deleted; discard a draft instead")
	}
	if confirmation != m.Name {
		return bridge.Errorf(bridge.CodeInvalid, "type the app name exactly to delete it")
	}
	m.State, m.DeletedAt = StateDeleted, s.now().UTC()
	if err := s.save(&m); err != nil {
		return err
	}
	s.publish(m.AppSummary)
	return nil
}

func (s *Service) RestoreDeleted(ctx context.Context, caller svc.Caller, app string) (bridge.AppSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if caller.Member != svc.Owner || caller.Agent {
		return bridge.AppSummary{}, bridge.Errorf(bridge.CodeDenied, "only the owner may restore a deleted app")
	}
	if err := s.ready(ctx); err != nil {
		return bridge.AppSummary{}, err
	}
	if !validID(app) {
		return bridge.AppSummary{}, bridge.Errorf(bridge.CodeInvalid, "invalid app id")
	}
	m, err := s.load(app)
	if errors.Is(err, os.ErrNotExist) {
		return bridge.AppSummary{}, bridge.Errorf(bridge.CodeNotFound, "no app "+app)
	}
	if err != nil {
		return bridge.AppSummary{}, err
	}
	if m.State != StateDeleted || !s.now().Before(m.DeletedAt.Add(deletedRetention)) {
		return bridge.AppSummary{}, bridge.Errorf(bridge.CodeConflict, "app is not within its deleted recovery period")
	}
	m.State, m.DeletedAt = bridge.StateKept, time.Time{}
	if err := s.save(&m); err != nil {
		return bridge.AppSummary{}, err
	}
	s.publish(m.AppSummary)
	return m.AppSummary, nil
}

// PurgeDeleted removes expired tombstones when the owner invokes maintenance.
// There is no background timer in the store; the sidecar owns scheduling.
func (s *Service) PurgeDeleted(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return err
	}
	entries, err := os.ReadDir(s.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !validID(entry.Name()) {
			continue
		}
		m, err := s.load(entry.Name())
		if err != nil {
			return err
		}
		if m.State == StateDeleted && !s.now().Before(m.DeletedAt.Add(deletedRetention)) {
			if err := os.RemoveAll(s.appDir(m.ID)); err != nil {
				return err
			}
		}
	}
	return nil
}
