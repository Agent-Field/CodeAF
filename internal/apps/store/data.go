package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/cell"
)

// WithData connects this handle's publish and delete operations to the
// engine's main data store. The opener must not call back into definitions,
// since a publication holds the app store lock while using it.
func (s *Store) WithData(open func() (*data.Store, error)) *Store {
	copy := *s
	copy.openData = open
	copy.types = func(string) (schema.TypeSet, error) {
		d, err := open()
		if err != nil {
			return nil, err
		}
		return d.TypeSet(), nil
	}
	return &copy
}

// SyncData reads the committed definitions under the same lock as publish
// and delete. The data file survives restarts, so a committed deletion also
// removes grants from data files written by earlier builds.
func (s *Store) SyncData(d *data.Store) error {
	if err := checkDir(s.root); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	unlock, err := s.lock(false, true)
	if err != nil {
		return err
	}
	defer unlock()
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return err
	}
	var pending [][]byte
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), "_") {
			continue
		}
		if !cell.ValidID(e.Name()) {
			// Early saves used app slugs and had no definition receipt.
			root, err := os.OpenRoot(filepath.Join(s.root, e.Name()))
			if err != nil {
				continue
			}
			raw, err := readRegular(root, "current/app.json")
			_ = root.Close()
			if err == nil {
				pending = append(pending, raw)
			}
			continue
		}
		m, err := s.readMeta(e.Name())
		if err != nil {
			continue
		}
		if m.Deleted {
			if d.HasApp(m.ID) {
				if err := d.ForgetApp(m.ID); err != nil {
					return err
				}
			}
			continue
		}
		if def, err := s.loadLive(m); err == nil {
			pending = append(pending, def.App)
		}
	}
	// A dependent app can register after the one declaring its types.
	for len(pending) != 0 {
		var left [][]byte
		for _, raw := range pending {
			if err := d.RegisterApp(raw); err != nil {
				left = append(left, raw)
			}
		}
		if len(left) == len(pending) {
			break
		}
		pending = left
	}
	return nil
}

// changeData updates the grants before the definition's commit point, so a
// refused schema never goes live. If the filesystem commit fails, restore
// the previous grants and declaration.
func (s *Store) changeData(m Meta, raw []byte) (func(error) error, error) {
	if s.openData == nil {
		return func(err error) error { return err }, nil
	}
	d, err := s.openData()
	if err != nil {
		return nil, err
	}
	var previous []byte
	if d.HasApp(m.ID) {
		before, err := s.loadLive(m)
		if err != nil {
			return nil, fmt.Errorf("read previous app data definition: %w", err)
		}
		previous = before.App
	}
	if raw == nil {
		if err := d.ForgetApp(m.ID); err != nil {
			return nil, err
		}
	} else if err := d.RegisterApp(raw); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	return func(err error) error {
		if err == nil {
			return nil
		}
		if previous == nil {
			return errors.Join(err, d.ForgetApp(m.ID))
		}
		return errors.Join(err, d.RegisterApp(previous))
	}, nil
}
