package imports

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/query"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"github.com/google/uuid"
)

type importedRecord struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Version int    `json:"version"`
	Removed bool   `json:"removed,omitempty"`
}
type receipt struct {
	ID              string               `json:"import_id"`
	App             string               `json:"app"`
	Member          string               `json:"member"`
	Type            string               `json:"type"`
	Source          string               `json:"source"`
	Columns         []string             `json:"columns"`
	Rows            [][]string           `json:"rows"`
	Mapping         []bridge.Mapping     `json:"mapping"`
	Status          string               `json:"status"`
	Created         []importedRecord     `json:"created"`
	Result          *bridge.ImportResult `json:"result,omitempty"`
	ActivityText    string               `json:"activity_text,omitempty"`
	ActivityDetail  string               `json:"activity_detail,omitempty"`
	ActivityWritten bool                 `json:"activity_written,omitempty"`
}

func (s *Service) path(app, id string) (string, error) {
	if s.cfg.Root == "" {
		return "", invalid("imports root is required")
	}
	if !query.ValidName(app) {
		return "", invalid("invalid app id")
	}
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.String() != id {
		return "", invalid("invalid import id")
	}
	return filepath.Join(s.cfg.Root, app, "imports", id+".json"), nil
}
func (s *Service) save(r *receipt) error {
	path, err := s.path(r.App, r.ID)
	if err != nil {
		return err
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".import-*")
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
	if err := os.Rename(file.Name(), path); err != nil {
		return err
	}
	var copy receipt
	if err := json.Unmarshal(data, &copy); err != nil {
		return err
	}
	s.saved[r.App+"/"+r.ID] = &copy
	return nil
}
func (s *Service) load(scope svc.Scope, id string) (*receipt, error) {
	path, err := s.path(scope.App, id)
	if err != nil {
		return nil, err
	}
	var data []byte
	if r := s.saved[scope.App+"/"+id]; r != nil {
		data, err = json.Marshal(r)
	} else {
		data, err = os.ReadFile(path)
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, bridge.Errorf(bridge.CodeNotFound, "import not found")
	}
	if err != nil {
		return nil, err
	}
	var r receipt
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	if r.ID != id || r.App != scope.App {
		return nil, invalid("import receipt does not match app")
	}
	return &r, nil
}
