// Package store keeps app definitions and their history under the engine home.
// Personal records stay in their data store. Publication updates its grants.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/home"
)

var (
	ErrBadInput = errors.New("invalid app, draft, screen or version")
	ErrNotFound = errors.New("app, draft, screen or version not found")
	ErrConflict = errors.New("the app changed since this draft was opened")
	ErrInvalid  = errors.New("app definition is invalid")
	ErrCorrupt  = errors.New("app metadata is unreadable")
)

// TypeCatalog reads the person's record definitions for publication. It omits
// fields already contributed by appID so an edit can keep them. It is called
// under the app store lock, so it must not call back into Store.
type TypeCatalog func(appID string) (schema.TypeSet, error)

// Store is a handle to one engine home's definitions. Handles and processes
// sharing that home take the same filesystem lock.
type Store struct {
	root     string
	types    TypeCatalog
	openData func() (*data.Store, error)
}

// New opens the engine home's app store. Saved versions carry the type
// definitions needed to validate them on read.
func New() *Store {
	return NewAt(home.Dir(), nil)
}

// NewAt chooses the engine home and can supply the person's catalog of types
// for definitions that use types owned by other apps.
func NewAt(engineHome string, types TypeCatalog) *Store {
	return &Store{root: filepath.Join(engineHome, "apps"), types: types}
}

// Meta remains after archive or delete so an old app ID is never reused.
type Meta struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Icon      string    `json:"icon"`
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Archived  bool      `json:"archived"`
	Deleted   bool      `json:"deleted,omitempty"`
	digest    string
}

// Definition is one validated live version. Meta describes the served version
// even when the definition has fallen back to an older snapshot.
type Definition struct {
	Meta            Meta                         `json:"meta"`
	App             json.RawMessage              `json:"app"`
	Plan            json.RawMessage              `json:"plan"`
	Screens         []string                     `json:"screens"`
	Runs            []string                     `json:"runs"`
	AgentMD         string                       `json:"agent_md"`
	ResolvedRecords map[string]schema.RecordType `json:"resolved_records,omitempty"`

	files files
	types schema.TypeSet
}

// Files returns a copy of the definition so an editing job can capture its
// base without reading anything from the personal record store.
func (d Definition) Files() map[string]string {
	out := make(map[string]string, len(d.files))
	for path, raw := range d.files {
		out[path] = string(raw)
	}
	return out
}

// Screen returns a screen from the same version as App and Plan.
func (d Definition) Screen(id string) (json.RawMessage, bool) {
	screen, ok := d.files["screens/"+id+".json"]
	return screen, ok
}

// ScreenParts compiles custom parts from the same snapshot as the screen.
func (d Definition) ScreenParts(screen []byte) (map[string]schema.PartProgram, error) {
	return schema.ScreenParts(schema.Definition{Files: d.files}, screen)
}

// CustomStep returns the manifest and source from this app version.
func (d Definition) CustomStep(id string) ([]byte, bool) {
	raw, ok := d.files["custom/"+id+".step"]
	return raw, ok
}

// Run returns YAML from the same version as App and Plan.
func (d Definition) Run(id string) ([]byte, bool) {
	run, ok := d.files["runs/"+id+".yaml"]
	if !ok {
		run, ok = d.files["runs/"+id+".yml"]
	}
	return run, ok
}

// Summary is the shelf's row for an app. Broken means the current definition
// could not be served and the engine will use a valid snapshot if available.
type Summary struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Icon      string    `json:"icon"`
	Version   int       `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
	Status    string    `json:"status" enum:"active,archived,broken"`
}

// Version records why a snapshot was made. Reverts keep the old entries.
type Version struct {
	N       int       `json:"n"`
	At      time.Time `json:"at"`
	Request string    `json:"request"`
	Summary string    `json:"summary"`
}

// A receipt saves the type definitions used to validate a version. This lets
// the same checks run when a reader opens that version without a data store.
type versionReceipt struct {
	Version
	Types schema.TypeSet `json:"types,omitempty"`
	// A revert restores the original manifest bytes under a new publication number.
	ManifestVersion int `json:"manifest_version,omitempty"`
}

// lock keeps a reader from seeing a partial publication and a writer from
// assigning the same version as another process. A fresh read creates nothing.
func (s *Store) lock(write, create bool) (func(), error) {
	if create {
		if err := os.MkdirAll(s.root, 0o700); err != nil {
			return nil, err
		}
	}
	if err := checkDir(s.root); err != nil {
		return nil, err
	}
	flags := os.O_RDWR
	if create {
		flags |= os.O_CREATE
	}
	f, err := os.OpenFile(filepath.Join(s.root, ".lock"), flags, 0o600)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: the app store lock is missing", ErrCorrupt)
	}
	if err != nil {
		return nil, err
	}
	if err := filelock.Lock(f, write, false); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() {
		_ = filelock.Unlock(f)
		_ = f.Close()
	}, nil
}

func (s *Store) readMeta(id string) (Meta, error) {
	if !cell.ValidID(id) {
		return Meta{}, ErrBadInput
	}
	if m, ok, err := s.publishedMeta(id); ok || err != nil {
		return m, err
	}
	return s.readFileMeta(id)
}

func (s *Store) readFileMeta(id string) (Meta, error) {
	if !cell.ValidID(id) {
		return Meta{}, ErrBadInput
	}
	dir := filepath.Join(s.root, id)
	if err := checkDir(dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Meta{}, ErrNotFound
		}
		return Meta{}, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return Meta{}, err
	}
	defer root.Close()
	raw, err := readRegular(root, "meta.json")
	if err != nil {
		return Meta{}, fmt.Errorf("%w: %w", ErrCorrupt, err)
	}
	var m Meta
	if err := json.Unmarshal(raw, &m); err != nil {
		return Meta{}, fmt.Errorf("%w: %w", ErrCorrupt, err)
	}
	if m.ID != id || strings.TrimSpace(m.Name) == "" || m.Version < 1 || m.CreatedAt.IsZero() || m.UpdatedAt.Before(m.CreatedAt) {
		return Meta{}, ErrCorrupt
	}
	return m, nil
}

func (s *Store) liveMeta(id string) (Meta, error) {
	m, err := s.readMeta(id)
	if err == nil && m.Deleted {
		return Meta{}, ErrNotFound
	}
	return m, err
}

// Prepare creates the publication lock before the Builder creates drafts.
func (s *Store) Prepare() error {
	unlock, err := s.lock(true, true)
	if err != nil {
		return err
	}
	unlock()
	return nil
}

// List leaves out deleted apps and isolates corrupt metadata to its own app.
func (s *Store) List() ([]Summary, error) {
	out := []Summary{}
	unlock, err := s.lock(false, false)
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	defer unlock()
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !cell.ValidID(entry.Name()) {
			continue
		}
		m, err := s.liveMeta(entry.Name())
		if errors.Is(err, ErrCorrupt) || errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		status := "active"
		if m.Archived {
			status = "archived"
		} else if _, err := s.loadCurrent(m); err != nil {
			status = "broken"
		}
		out = append(out, Summary{m.ID, m.Name, m.Icon, m.Version, m.UpdatedAt, status})
	}
	return out, nil
}

// Get reads one complete validated definition, falling back to the newest
// valid committed snapshot if current is missing or invalid.
func (s *Store) Get(id string) (Definition, error) {
	unlock, err := s.lockExisting(id, false)
	if err != nil {
		return Definition{}, err
	}
	defer unlock()
	m, err := s.liveMeta(id)
	if err != nil {
		return Definition{}, err
	}
	return s.loadLive(m)
}

// Screen opens a screen only after validating the whole live definition.
func (s *Store) Screen(app, screen string) (json.RawMessage, error) {
	if !fileID(screen) {
		return nil, ErrBadInput
	}
	d, err := s.Get(app)
	if err != nil {
		return nil, err
	}
	if raw, ok := d.Screen(screen); ok {
		return raw, nil
	}
	return nil, ErrNotFound
}

// Versions lists committed snapshots newest first, including snapshots whose
// definition has since become unreadable.
func (s *Store) Versions(id string) ([]Version, error) {
	unlock, err := s.lockExisting(id, false)
	if err != nil {
		return nil, err
	}
	defer unlock()
	m, err := s.liveMeta(id)
	if err != nil {
		return nil, err
	}
	nums, err := s.versionNumbers(m)
	if err != nil {
		return nil, err
	}
	out := make([]Version, 0, len(nums))
	for _, n := range nums {
		v, err := s.readReceipt(id, n)
		if err != nil {
			return nil, fmt.Errorf("version %d: %w", n, err)
		}
		out = append(out, v.Version)
	}
	return out, nil
}

func (s *Store) lockExisting(id string, write bool) (func(), error) {
	if !cell.ValidID(id) {
		return nil, ErrBadInput
	}
	unlock, err := s.lock(write, false)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	return unlock, err
}

func (s *Store) snapshot(id string, n int) string {
	return filepath.Join(s.root, id, "versions", strconv.Itoa(n))
}

func (s *Store) versionNumbers(m Meta) ([]int, error) {
	dir := filepath.Join(s.root, m.ID, "versions")
	if err := checkDir(dir); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var nums []int
	for _, e := range entries {
		n, err := strconv.Atoi(e.Name())
		if e.IsDir() && err == nil && n > 0 && n <= m.Version && e.Name() == strconv.Itoa(n) {
			nums = append(nums, n)
		}
	}
	slices.Sort(nums)
	slices.Reverse(nums)
	return nums, nil
}
