// Package cell is the on-disk layout of a cell: one folder per conversation
// under <home>/v3/cells/<ulid>/, holding the cell's own state in .cell/:
//
//	.cell/transcript.jsonl   the journal
//	.cell/env/               environment state
//	.cell/meta.json          Meta (SCHEMAS.md §4)
//
// Every path this package persists is cell-relative; absolute paths exist
// only transiently, as the answer to Cell.Path.
//
// New chats opt in behind [Enabled] (CODEAF_CELLS=1): v3MintSession in
// cmd/codeaf/chatv3_layout.go calls [CreateIn] on the project bucket, and
// internal/session's layout.go recognises a folder holding .cell/ and keeps
// the transcript there. A legacy folder is migrated in place when it is opened
// (MigrateLegacy, hooked in openV3Agent), only while the flag is on.
package cell

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/env"
)

// Cell-relative names.
const (
	StateDir       = ".cell"
	TranscriptPath = StateDir + "/transcript.jsonl"
	EnvPath        = StateDir + "/env"
	MetaPath       = StateDir + "/meta.json"

	schemaV = 1
)

// EnvVar switches new chats onto cells. Unset or anything but "1" is off.
const EnvVar = "CODEAF_CELLS"

// Enabled reports whether new chats should be created as cells.
func Enabled() bool { return env.Get(EnvVar) == "1" }

// Options describe a new cell. KeyID is generated when empty.
type Options struct {
	Class Class
	KeyID string
	Base  *Base
}

// Cell is a handle on one cell folder.
type Cell struct {
	ID   string
	Root string
	meta Meta
}

// Dir is the directory that holds every cell under a codeaf home.
func Dir(home string) string { return filepath.Join(home, "v3", "cells") }

// Create makes a new cell folder and writes its meta.json.
func Create(home string, opts Options) (Cell, error) { return CreateIn(Dir(home), opts) }

// CreateIn makes a new cell folder directly under parent. A caller that
// already groups its conversations in a directory (a project bucket) uses it
// so the cell is found where that grouping looks.
func CreateIn(parent string, opts Options) (Cell, error) {
	m, err := opts.meta()
	if err != nil {
		return Cell{}, err
	}
	id, err := NewID(time.Now())
	if err != nil {
		return Cell{}, err
	}
	c := Cell{ID: id, Root: filepath.Join(parent, id), meta: m}
	if err := os.MkdirAll(c.Root+"/"+EnvPath, 0o700); err != nil {
		return Cell{}, fmt.Errorf("create cell: %w", err)
	}
	if err := c.WriteMeta(m); err != nil {
		return Cell{}, err
	}
	return c, nil
}

// Open loads an existing cell.
func Open(home, id string) (Cell, error) {
	if !ValidID(id) {
		return Cell{}, fmt.Errorf("open cell: invalid id %q", id)
	}
	return OpenAt(filepath.Join(Dir(home), id), id)
}

// OpenAt loads the cell whose folder is root, wherever it lives.
func OpenAt(root, id string) (Cell, error) {
	c := Cell{ID: id, Root: root}
	raw, err := os.ReadFile(c.abs(MetaPath))
	if err != nil {
		return Cell{}, fmt.Errorf("open cell %s: %w", id, err)
	}
	if err := json.Unmarshal(raw, &c.meta); err != nil {
		return Cell{}, fmt.Errorf("open cell %s: %w", id, err)
	}
	if err := c.meta.validate(); err != nil {
		return Cell{}, fmt.Errorf("open cell %s: %w", id, err)
	}
	return c, nil
}

// Meta is the cell's metadata as last written or read.
func (c Cell) Meta() Meta { return c.meta }

// Path resolves a cell-relative path to a location inside the cell. Absolute
// and escaping paths are rejected.
func (c Cell) Path(rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") {
		return "", fmt.Errorf("cell path %q: must be relative", rel)
	}
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("cell path %q: escapes the cell", rel)
	}
	return c.abs(clean), nil
}

func (c Cell) abs(rel string) string { return filepath.Join(c.Root, filepath.FromSlash(rel)) }

// WriteMeta replaces meta.json atomically (temp file, fsync, rename).
func (c *Cell) WriteMeta(m Meta) error {
	if err := m.validate(); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := writeAtomic(c.abs(MetaPath), raw); err != nil {
		return fmt.Errorf("write cell meta: %w", err)
	}
	c.meta = m
	return nil
}

func writeAtomic(path string, data []byte) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".meta-*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

var errBadMeta = errors.New("invalid cell meta")
