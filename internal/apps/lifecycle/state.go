package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/query"
)

type entry struct {
	App     bridge.AppSummary `json:"app"`
	State   bridge.AppState   `json:"state"`
	At      string            `json:"at"`
	By      string            `json:"by"`
	PurgeAt string            `json:"purge_at,omitempty"`
}

func (e *entry) summary() bridge.AppSummary {
	app := e.App
	app.State, app.UpdatedAt = e.State, e.At
	return app
}
func (e *entry) expired(at time.Time) bool {
	purge, err := time.Parse(time.RFC3339Nano, e.PurgeAt)
	return err == nil && !at.Before(purge)
}
func (s *Service) path(app string) (string, error) {
	if !query.ValidName(app) {
		return "", bridge.Errorf(bridge.CodeInvalid, "invalid app id")
	}
	if s.cfg.Root == "" {
		return "", bridge.Errorf(bridge.CodeInvalid, "apps root is required")
	}
	return filepath.Join(s.cfg.Root, app, "lifecycle.json"), nil
}
func (s *Service) load(app string) (*entry, error) {
	path, err := s.path(app)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state entry
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	if state.App.ID != app || (state.State != bridge.StateArchived && state.State != bridge.StateDeleted) {
		return nil, fmt.Errorf("invalid lifecycle state for %s", app)
	}
	if _, err := time.Parse(time.RFC3339Nano, state.At); err != nil {
		return nil, err
	}
	if state.State == bridge.StateDeleted {
		if _, err := time.Parse(time.RFC3339Nano, state.PurgeAt); err != nil {
			return nil, err
		}
	}
	return &state, nil
}
func (s *Service) save(app string, state *entry) error {
	path, err := s.path(app)
	if err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".lifecycle-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(file.Name(), path)
}
func (s *Service) clear(app string) error {
	path, err := s.path(app)
	if err != nil {
		return err
	}
	return os.Remove(path)
}

// Purge uses the underlying store so deleted apps remain inaccessible during cleanup.
func (s *Service) Purge(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.Root == "" {
		return nil
	}
	dirs, err := os.ReadDir(s.cfg.Root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var failures []error
	for _, dir := range dirs {
		if !dir.IsDir() || !query.ValidName(dir.Name()) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return errors.Join(append(failures, err)...)
		}
		state, err := s.load(dir.Name())
		if err == nil && state != nil && state.State == bridge.StateDeleted && state.expired(s.clock.Now()) {
			err = s.store.Discard(ctx, dir.Name())
			if err == nil {
				err = os.RemoveAll(filepath.Join(s.cfg.Root, dir.Name()))
			}
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("purge %s: %w", dir.Name(), err))
		}
	}
	return errors.Join(failures...)
}
