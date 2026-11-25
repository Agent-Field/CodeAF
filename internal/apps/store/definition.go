package store

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/fsreplace"
)

const (
	appFile    = schema.AppFile
	planFile   = schema.PlanFile
	agentFile  = "agent.md"
	sampleFile = schema.SampleFile
)

var definitionID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

func fileID(id string) bool {
	return len(id) <= 128 && definitionID.MatchString(id)
}

type appHeader struct {
	ID      string           `json:"id"`
	Name    string           `json:"name"`
	Icon    string           `json:"icon"`
	Version int              `json:"version"`
	Agent   *schema.AgentRef `json:"agent"`
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
	for dir := filepath.Dir(name); dir != "."; dir = filepath.Dir(dir) {
		info, err := root.Lstat(dir)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%s is not a directory", dir)
		}
	}
	fi, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", name)
	}
	return root.ReadFile(name)
}

func readDefinition(dir string, extra ...string) (files, error) {
	if err := checkDir(dir); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	f := files{}
	for _, name := range []string{appFile, planFile, sampleFile} {
		raw, err := readRegular(root, name)
		if err == nil {
			f[name] = raw
		} else if !os.IsNotExist(err) || name == appFile {
			return nil, err
		}
	}
	a, err := f.header()
	if err != nil {
		return nil, err
	}
	name, err := a.agentPath()
	if err != nil {
		return nil, err
	}
	if raw, err := readRegular(root, name); err == nil {
		f[name] = raw
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err := f.readGroup(root, schema.ScreensDir, ".json"); err != nil {
		return nil, err
	}
	if err := f.readGroup(root, schema.RunsDir, ".yaml", ".yml"); err != nil {
		return nil, err
	}
	if err := f.checkEntries(root.FS(), extra); err != nil {
		return nil, err
	}
	return f, nil
}

// checkEntries refuses unsupported output without reading it. The caller
// names the files that belong to the store or draft instead of the definition.
func (f files) checkEntries(tree fs.FS, extra []string) error {
	dirs := map[string]bool{".": true, schema.ScreensDir: true, schema.RunsDir: true}
	for name := range f {
		for dir := path.Dir(name); !dirs[dir]; dir = path.Dir(dir) {
			dirs[dir] = true
		}
	}
	return fs.WalkDir(tree, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && dirs[name] {
			return nil
		}
		if _, ok := f[name]; ok || (entry.Type().IsRegular() && slices.Contains(extra, name)) {
			return nil
		}
		return fmt.Errorf("unsupported definition file %q", name)
	})
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
			if err := os.MkdirAll(parent, 0o700); err != nil {
				return err
			}
			for !dirs[parent] {
				dirs[parent] = true
				parent = filepath.Dir(parent)
			}
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
	return a, nil
}

func (a appHeader) agentPath() (string, error) {
	if a.Agent == nil {
		return agentFile, nil
	}
	name, ok := schema.AgentPath(a.Agent.File)
	if !ok || !fs.ValidPath(name) || !filepath.IsLocal(filepath.FromSlash(name)) {
		return "", fmt.Errorf("the agent file must be Markdown inside the app folder")
	}
	return name, nil
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

func validateDefinition(f files, existing schema.TypeSet) error {
	// Check the exact bytes that will be served or published.
	report := schema.Validate(schema.Definition{Files: f}, existing)
	if !report.OK() {
		return fmt.Errorf("%w: %s", ErrInvalid, strings.TrimSpace(report.String()))
	}
	return nil
}

func (s *Store) loadCurrent(m Meta) (Definition, error) {
	v, err := s.readReceipt(m.ID, m.Version)
	if err != nil {
		return Definition{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	return load(filepath.Join(s.root, m.ID, "current"), m, v.Types)
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
	v, err := s.readReceipt(m.ID, n)
	if err != nil {
		return Definition{}, err
	}
	m.Version = n
	m.UpdatedAt = v.At
	return load(s.snapshot(m.ID, n), m, v.Types, versionFile)
}

func load(dir string, m Meta, existing schema.TypeSet, extra ...string) (Definition, error) {
	f, err := readDefinition(dir, extra...)
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
	if err := validateDefinition(f, existing); err != nil {
		return Definition{}, err
	}
	m.Name, m.Icon = a.Name, a.Icon
	agent, err := a.agentPath()
	if err != nil {
		return Definition{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	shape := schema.Definition{Files: f}
	return Definition{
		Meta: m, App: f[appFile], Plan: f[planFile], AgentMD: string(f[agent]),
		Screens: idsOf(shape.ScreenFiles()), Runs: idsOf(shape.RunFiles()), files: f, types: existing,
	}, nil
}

func idsOf(paths []string) []string {
	ids := make([]string, len(paths))
	for i, name := range paths {
		ids[i] = schema.FileID(name)
	}
	return ids
}
