package settings

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
)

var appIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,47}$`)

type preferences struct {
	Month    *float64                `json:"month,omitempty"`
	Triggers map[string]triggerState `json:"triggers,omitempty"`
}

type triggerState struct {
	Paused    bool   `json:"paused"`
	PausedBy  string `json:"paused_by,omitempty"`
	PausedAt  string `json:"paused_at,omitempty"`
	LastFired string `json:"last_fired,omitempty"`
}

func validAppID(app string) bool { return appIDPattern.MatchString(app) }

func (s *Service) read(app string) (preferences, error) {
	p := preferences{Triggers: map[string]triggerState{}}
	if !validAppID(app) {
		return p, bridge.Errorf(bridge.CodeInvalid, "invalid app id")
	}
	raw, err := os.ReadFile(filepath.Join(s.root, app, "settings.json"))
	if errors.Is(err, os.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return p, bridge.Errorf(bridge.CodeInternal, "could not read app settings")
	}
	if json.Unmarshal(raw, &p) != nil || (p.Month != nil && !validMonth(*p.Month)) {
		return p, bridge.Errorf(bridge.CodeInternal, "could not read app settings")
	}
	if p.Triggers == nil {
		p.Triggers = map[string]triggerState{}
	}
	return p, nil
}

func (s *Service) write(app string, p preferences) error {
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return bridge.Errorf(bridge.CodeInternal, "could not save app settings")
	}
	if err := atomicWrite(filepath.Join(s.root, app, "settings.json"), append(raw, '\n')); err != nil {
		return bridge.Errorf(bridge.CodeInternal, "could not save app settings")
	}
	return nil
}

func atomicWrite(path string, raw []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".settings-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.Write(raw); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
