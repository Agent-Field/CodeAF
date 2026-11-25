package store

import (
	"encoding/json"
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
	if copy.types == nil {
		copy.types = func(app string) (schema.TypeSet, error) {
			d, err := open()
			if err != nil {
				return nil, err
			}
			return d.TypeSetFor(app), nil
		}
	}
	return &copy
}

// SyncData adopts legacy snapshots once. A committed publication in the
// data store is never re-registered from the filesystem.
func (s *Store) SyncData(d *data.Store) error {
	if err := checkDir(s.root); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	unlock, err := s.lock(true, true)
	if err != nil {
		return err
	}
	defer unlock()
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return err
	}
	var pending []data.AppPublication
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), "_") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if !cell.ValidID(e.Name()) {
			if err := s.adoptSlug(d, e.Name()); err != nil {
				return err
			}
			continue
		}
		if _, ok, err := d.Publication(e.Name()); err != nil {
			return err
		} else if ok {
			continue
		}
		m, err := s.readFileMeta(e.Name())
		if err != nil {
			continue
		}
		var raw []byte
		if !m.Deleted {
			def, err := s.loadSnapshot(m, m.Version)
			if err != nil {
				continue
			}
			raw = def.App
		}
		pending = append(pending, publication(m, raw))
	}
	// A dependent app can register after the one declaring its types.
	for len(pending) > 0 {
		var left []data.AppPublication
		for _, p := range pending {
			if err := d.PublishApp(p, 0); err != nil {
				if e, ok := data.AsError(err); !ok || e.Code != data.CodeInvalid {
					return err
				}
				left = append(left, p)
			}
		}
		if len(left) == len(pending) {
			break
		}
		pending = left
	}
	return nil
}

func (s *Store) adoptSlug(d *data.Store, slug string) error {
	// Early slug saves predate numbered snapshots. Once imported their
	// data grants also come from the data file.
	root, err := os.OpenRoot(filepath.Join(s.root, slug))
	if err != nil {
		return nil
	}
	defer root.Close()
	raw, err := readRegular(root, "current/app.json")
	if err != nil {
		return nil
	}
	def, err := data.ParseApp(raw)
	if err != nil || def.ID != slug || d.HasApp(slug) {
		return nil
	}
	if err := d.RegisterApp(raw); err != nil {
		if e, ok := data.AsError(err); ok && e.Code == data.CodeInvalid {
			return nil
		}
		return err
	}
	return nil
}

func publication(m Meta, raw []byte) data.AppPublication {
	metadata, _ := json.Marshal(m)
	return data.AppPublication{App: m.ID, Version: m.Version, Archived: m.Archived, Deleted: m.Deleted,
		Metadata: metadata, Definition: raw}
}

func (s *Store) publishedMeta(id string) (Meta, bool, error) {
	if s.openData == nil {
		return Meta{}, false, nil
	}
	d, err := s.openData()
	if err != nil {
		return Meta{}, false, err
	}
	p, ok, err := d.Publication(id)
	if err != nil || !ok {
		return Meta{}, ok, err
	}
	var m Meta
	if err := json.Unmarshal(p.Metadata, &m); err != nil || m.ID != id || m.Version != p.Version ||
		m.Archived != p.Archived || m.Deleted != p.Deleted {
		return Meta{}, true, fmt.Errorf("%w: invalid publication pointer", ErrCorrupt)
	}
	return m, true, nil
}
