//go:build linux

package sessionhost

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const registryVersion = 1

type sessionRow struct {
	ID         string    `json:"id"`
	Person     string    `json:"person"`
	UID        int       `json:"uid"`
	Created    time.Time `json:"created"`
	LastActive time.Time `json:"last_active"`
	// Title is the name the chat's engine last stated for it (title_linux.go), kept here so a list can
	// show it for a chat that is not running. Empty until the chat has one, and never sent as empty.
	Title   string `json:"title,omitempty"`
	Doomed  bool   `json:"doomed,omitempty"`
	Missing bool   `json:"missing,omitempty"`
}

type quarantineRow struct {
	UID   int       `json:"uid"`
	Until time.Time `json:"until"`
}

type registryFile struct {
	Version    int             `json:"version"`
	Sessions   []sessionRow    `json:"sessions"`
	Quarantine []quarantineRow `json:"quarantine"`
}

// The supervisor holds its mutex for every registry operation after startup.
type registry struct {
	path       string
	sessions   map[string]sessionRow
	quarantine map[int]time.Time
}

func loadRegistry(cfg Config, now time.Time) (*registry, error) {
	r := &registry{
		path:       filepath.Join(cfg.Data, "registry.json"),
		sessions:   make(map[string]sessionRow),
		quarantine: make(map[int]time.Time),
	}
	info, err := os.Lstat(r.path)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || ownerUID(info) != os.Geteuid() {
		return nil, errors.New("The chat registry is unavailable.")
	}
	b, err := os.ReadFile(r.path)
	if err != nil {
		return nil, err
	}
	var disk registryFile
	if json.Unmarshal(b, &disk) != nil || disk.Version != registryVersion {
		return nil, errors.New("The chat registry could not be read.")
	}
	used := make(map[int]bool)
	for _, row := range disk.Sessions {
		_, duplicate := r.sessions[row.ID]
		if duplicate || used[row.UID] || !cfg.ownsUID(row.UID) ||
			!sessionID.MatchString(row.ID) || !personHandle.MatchString(row.Person) {
			return nil, errors.New("The chat registry contains an invalid entry.")
		}
		r.sessions[row.ID] = row
		used[row.UID] = true
	}
	for _, row := range disk.Quarantine {
		if !row.Until.After(now) {
			continue
		}
		if used[row.UID] || !cfg.ownsUID(row.UID) {
			return nil, errors.New("The chat registry contains an invalid entry.")
		}
		r.quarantine[row.UID] = row.Until
		used[row.UID] = true
	}
	return r, nil
}

func (r *registry) put(row sessionRow) error {
	old, exists := r.sessions[row.ID]
	r.sessions[row.ID] = row
	if err := r.store(); err != nil {
		if exists {
			r.sessions[row.ID] = old
		} else {
			delete(r.sessions, row.ID)
		}
		return err
	}
	return nil
}

func (r *registry) remove(id string, now time.Time) error {
	row := r.sessions[id]
	delete(r.sessions, id)
	r.quarantine[row.UID] = now.Add(uidQuarantine)
	if err := r.store(); err != nil {
		r.sessions[id] = row
		delete(r.quarantine, row.UID)
		return err
	}
	return nil
}

func (r *registry) store() error {
	disk := registryFile{
		Version:    registryVersion,
		Sessions:   make([]sessionRow, 0, len(r.sessions)),
		Quarantine: make([]quarantineRow, 0, len(r.quarantine)),
	}
	for _, row := range r.sessions {
		disk.Sessions = append(disk.Sessions, row)
	}
	slices.SortFunc(disk.Sessions, func(a, b sessionRow) int { return strings.Compare(a.ID, b.ID) })
	for uid, until := range r.quarantine {
		disk.Quarantine = append(disk.Quarantine, quarantineRow{UID: uid, Until: until})
	}
	slices.SortFunc(disk.Quarantine, func(a, b quarantineRow) int { return a.UID - b.UID })

	f, err := os.CreateTemp(filepath.Dir(r.path), ".registry-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := f.Chmod(0o600); err != nil {
		return err
	}
	if err := json.NewEncoder(f).Encode(disk); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), r.path)
}
