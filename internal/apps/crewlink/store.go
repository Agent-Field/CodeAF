package crewlink

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
)

// storedLink is one row of links.json. Access, the live label and the crew
// name shown on the wire are filled per caller at read time.
type storedLink struct {
	Crew     string               `json:"crew"`
	CrewName string               `json:"crew_name,omitempty"`
	AppName  string               `json:"app_name,omitempty"`
	Type     string               `json:"type"`
	ID       string               `json:"id"`
	Label    string               `json:"label"`
	By       string               `json:"by"`
	LinkedAt string               `json:"linked_at"`
	State    bridge.CrewLinkState `json:"state"`
}

type linkFile struct {
	Links []storedLink `json:"links"`
}

type crewRef struct {
	App  string `json:"app"`
	Type string `json:"type"`
	ID   string `json:"id"`
}

// byCrew is the reverse index: crew id → the apps that hold a link, including
// unlinked rows, so ForCrew does not scan every app directory.
type byCrew struct {
	Crews map[string][]crewRef `json:"crews"`
}

func (s *Service) linksPath(app string) string {
	return filepath.Join(s.root, app, "links.json")
}

func (s *Service) indexPath() string {
	// The underscore keeps this directory from being read as an app id.
	return filepath.Join(s.root, "_index", "by-crew.json")
}

func (s *Service) load(app string) (linkFile, error) {
	var file linkFile
	err := readJSON(s.linksPath(app), &file)
	if err != nil || file.Links == nil {
		file.Links = []storedLink{}
	}
	return file, err
}

func (s *Service) loadIndex() (byCrew, error) {
	var index byCrew
	err := readJSON(s.indexPath(), &index)
	if index.Crews == nil {
		index.Crews = map[string][]crewRef{}
	}
	return index, err
}

func (s *Service) save(app string, file linkFile) error {
	if file.Links == nil {
		file.Links = []storedLink{}
	}
	if err := writeJSON(s.linksPath(app), file); err != nil {
		return err
	}
	index, err := s.loadIndex()
	if err != nil {
		return err
	}
	for _, row := range file.Links {
		index.Crews[row.Crew] = putRef(index.Crews[row.Crew], crewRef{App: app, Type: row.Type, ID: row.ID})
	}
	return writeJSON(s.indexPath(), index)
}

func putRef(refs []crewRef, ref crewRef) []crewRef {
	for _, existing := range refs {
		if existing == ref {
			return refs
		}
	}
	return append(refs, ref)
}

func readJSON(path string, dest any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, dest)
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	ok = true
	return nil
}
