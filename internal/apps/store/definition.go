package store

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/fsreplace"
)

const (
	appFile    = "app.json"
	planFile   = "plan.json"
	agentFile  = "agent.md"
	sampleFile = "sample.json"
)

var definitionID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

func fileID(id string) bool {
	return len(id) <= 128 && definitionID.MatchString(id)
}

type appHeader struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Icon    string `json:"icon"`
	Version int    `json:"version"`
	Agent   *struct {
		File string `json:"file"`
	} `json:"agent"`
}

// files holds only definition files by their slash-separated names. The
// draft's sample database and store-owned version metadata stay separate.
type files map[string][]byte

func checkDir(dir string) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", filepath.Base(dir))
	}
	return nil
}

func readRegular(root *os.Root, name string) ([]byte, error) {
	fi, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", name)
	}
	return root.ReadFile(name)
}

func readDefinition(dir string) (files, error) {
	if err := checkDir(dir); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	f := files{}
	for _, name := range []string{appFile, planFile, agentFile, sampleFile} {
		raw, err := readRegular(root, name)
		if err == nil {
			f[name] = raw
		} else if !os.IsNotExist(err) || name == appFile {
			return nil, err
		}
	}
	if err := f.readGroup(root, "screens", ".json"); err != nil {
		return nil, err
	}
	if err := f.readGroup(root, "runs", ".yaml", ".yml"); err != nil {
		return nil, err
	}
	return f, nil
}

func (f files) readGroup(root *os.Root, dir string, exts ...string) error {
	info, err := root.Lstat(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	entries, err := fs.ReadDir(root.FS(), dir)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		ext := filepath.Ext(entry.Name())
		if !slices.Contains(exts, ext) {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ext)
		if !fileID(id) || seen[id] {
			return fmt.Errorf("invalid or duplicate %s filename %q", dir, entry.Name())
		}
		seen[id] = true
		name := dir + "/" + entry.Name()
		raw, err := readRegular(root, name)
		if err != nil {
			return err
		}
		f[name] = raw
	}
	return nil
}

func (f files) write(dir string) (err error) {
	if err := os.Mkdir(dir, 0o700); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(dir)
		}
	}()
	dirs := map[string]bool{dir: true}
	for name, raw := range f {
		path := filepath.Join(dir, filepath.FromSlash(name))
		parent := filepath.Dir(path)
		if !dirs[parent] {
			if err := os.Mkdir(parent, 0o700); err != nil {
				return err
			}
			dirs[parent] = true
		}
		if err := writeSynced(path, raw); err != nil {
			return err
		}
	}
	for parent := range dirs {
		if parent != dir {
			if err := fsreplace.SyncDir(parent); err != nil {
				return err
			}
		}
	}
	return fsreplace.SyncDir(dir)
}

func (f files) header() (appHeader, error) {
	var a appHeader
	if err := json.Unmarshal(f[appFile], &a); err != nil {
		return appHeader{}, err
	}
	if !fileID(a.ID) || strings.TrimSpace(a.Name) == "" || a.Version < 1 {
		return appHeader{}, fmt.Errorf("app.json needs an id, name and positive version")
	}
	if a.Agent != nil && a.Agent.File != agentFile {
		return appHeader{}, fmt.Errorf("app.json must use %s for its instructions", agentFile)
	}
	return a, nil
}

func (f files) stamp(id string, n int) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(f[appFile], &obj); err != nil {
		return err
	}
	if obj == nil {
		return fmt.Errorf("app.json must be an object")
	}
	obj["id"] = json.RawMessage(fmt.Appendf(nil, "%q", id))
	obj["version"] = json.RawMessage(fmt.Appendf(nil, "%d", n))
	raw, err := json.MarshalIndent(obj, "", "  ")
	if err == nil {
		f[appFile] = append(raw, '\n')
	}
	return err
}

func (s *Store) validateDefinition(f files) error {
	if s.validate == nil {
		return fmt.Errorf("%w: validator unavailable", ErrInvalid)
	}
	// Check the exact bytes that will be served or published.
	if err := s.validate(f); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	return nil
}

func (s *Store) loadCurrent(m Meta) (Definition, error) {
	return s.load(filepath.Join(s.root, m.ID, "current"), m)
}

func (s *Store) loadLive(m Meta) (Definition, error) {
	d, err := s.loadCurrent(m)
	if err == nil {
		return d, nil
	}
	nums, listErr := s.versionNumbers(m)
	if listErr != nil {
		return Definition{}, fmt.Errorf("%w: %w", ErrInvalid, listErr)
	}
	for _, n := range nums {
		if d, err := s.loadSnapshot(m, n); err == nil {
			return d, nil
		}
	}
	return Definition{}, err
}

func (s *Store) loadSnapshot(m Meta, n int) (Definition, error) {
	dir := s.snapshot(m.ID, n)
	if err := checkDir(filepath.Dir(dir)); err != nil {
		return Definition{}, err
	}
	v, err := readVersion(dir, n)
	if err != nil {
		return Definition{}, err
	}
	m.Version = n
	m.UpdatedAt = v.At
	return s.load(dir, m)
}

func (s *Store) load(dir string, m Meta) (Definition, error) {
	f, err := readDefinition(dir)
	if err != nil {
		return Definition{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	a, err := f.header()
	if err != nil {
		return Definition{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if a.ID != m.ID || a.Version != m.Version {
		return Definition{}, fmt.Errorf("%w: app.json does not match the app ID and version", ErrInvalid)
	}
	if err := s.validateDefinition(f); err != nil {
		return Definition{}, err
	}
	m.Name, m.Icon = a.Name, a.Icon
	d := Definition{
		Meta: m, App: f[appFile], Plan: f[planFile], AgentMD: string(f[agentFile]),
		Screens: []string{}, Runs: []string{}, files: f,
	}
	for path := range f {
		switch group, name, _ := strings.Cut(path, "/"); group {
		case "screens":
			d.Screens = append(d.Screens, strings.TrimSuffix(name, ".json"))
		case "runs":
			d.Runs = append(d.Runs, strings.TrimSuffix(name, filepath.Ext(name)))
		}
	}
	sort.Strings(d.Screens)
	sort.Strings(d.Runs)
	return d, nil
}
