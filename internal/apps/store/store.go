// Package store keeps app definitions and their history under the engine home.
// Personal records belong to a different store and have no path through here.
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

// Store is a handle to one engine home's definitions. Handles and processes
// sharing that home take the same filesystem lock.
type Store struct {
	root     string
	validate func(map[string][]byte) error
}

// New uses the same home as the rest of the engine.
func New(validate func(map[string][]byte) error) *Store {
	return NewAt(home.Dir(), validate)
}

// NewAt lets a caller choose the engine home before opening any files.
func NewAt(engineHome string, validate func(map[string][]byte) error) *Store {
	return &Store{root: filepath.Join(engineHome, "apps"), validate: validate}
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
}

// Definition is one validated live version. Meta describes the served version
// even when the definition has fallen back to an older snapshot.
type Definition struct {
	Meta    Meta            `json:"meta"`
	App     json.RawMessage `json:"app"`
	Plan    json.RawMessage `json:"plan"`
	Screens []string        `json:"screens"`
	Runs    []string        `json:"runs"`
	AgentMD string          `json:"agent_md"`

	files files
}

// Screen returns a screen from the same version as App and Plan.
func (d Definition) Screen(id string) (json.RawMessage, bool) {
	screen, ok := d.files["screens/"+id+".json"]
	return screen, ok
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
		v, err := readVersion(s.snapshot(id, n), n)
		if err != nil {
			return nil, fmt.Errorf("version %d: %w", n, err)
		}
		out = append(out, v)
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
