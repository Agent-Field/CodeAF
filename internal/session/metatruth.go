package session

// metatruth.go splits a session's meta in two. The summary (title, model,
// spend, times) is a citation of the transcript and stays in meta.json, outside
// the sealed tree, where internal/cellindex rebuilds it. The truth is what no
// transcript can restate (the dials, the places, the unlanded trees, what was
// put away) and lives in .cell/session.json, so it is sealed with the
// workspace and travels with it.
//
// Trees records which worktrees exist; the worktree directories themselves are
// workspace material with absolute paths, and they stay where they are.

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// metaTruth is the part of [Meta] that lives in the sealed tree.
type metaTruth struct {
	LaunchDir     string          `json:"launchDir,omitempty"`
	Owned         bool            `json:"owned,omitempty"`
	Effort        string          `json:"effort,omitempty"`
	Approval      string          `json:"approval,omitempty"`
	Places        []PlaceRef      `json:"places,omitempty"`
	Trees         []StandingTree  `json:"trees,omitempty"`
	Archived      bool            `json:"archived,omitempty"`
	ArchivedTasks map[string]bool `json:"archivedTasks,omitempty"`
}

func (m Meta) truth() metaTruth {
	return metaTruth{m.LaunchDir, m.Owned, m.Effort, m.Approval, m.Places, m.Trees, m.Archived, m.ArchivedTasks}
}

func (m *Meta) setTruth(t metaTruth) {
	m.LaunchDir, m.Owned, m.Effort, m.Approval = t.LaunchDir, t.Owned, t.Effort, t.Approval
	m.Places, m.Trees, m.Archived, m.ArchivedTasks = t.Places, t.Trees, t.Archived, t.ArchivedTasks
}

// derived is m without its truth: what meta.json holds in the cell layout.
func (m Meta) derived() Meta {
	m.setTruth(metaTruth{})
	return m
}

// overlayTruth lays the sealed truth over meta. A folder with no truth file
// (legacy, or a cell whose truth has not been split out yet) keeps whatever
// meta.json says.
func overlayTruth(dir string, meta Meta) (Meta, error) {
	path := layoutOf(dir).metaTruth(dir)
	if path == "" {
		return meta, nil
	}
	t, err := readTruth(path)
	if errors.Is(err, fs.ErrNotExist) {
		return meta, nil
	}
	if err != nil {
		return Meta{}, err
	}
	meta.setTruth(t)
	return meta, nil
}

func readTruth(path string) (metaTruth, error) {
	var t metaTruth
	raw, err := os.ReadFile(path)
	if err != nil {
		return t, err
	}
	if json.Unmarshal(raw, &t) != nil {
		return metaTruth{}, nil // a corrupt file reads as no truth, as meta.json does
	}
	return t, nil
}

// TruthCarriers is what a legacy folder carries into .cell/ when it migrates:
// the state, task and team files, and the truth half of its meta.
func TruthCarriers() []cell.Carrier {
	return []cell.Carrier{cell.Files(truthNames...), metaCarrier{}}
}

// metaCarrier stages the truth fields of the legacy meta.json into the new
// cell, then rewrites meta.json as the summary alone.
type metaCarrier struct{}

func (metaCarrier) Stage(dir string, c cell.Cell) error {
	meta, err := readMeta(dir)
	if err != nil {
		return err
	}
	return writeJSONAtomic(filepath.Join(c.Root, cellStateDir, placeMetaTruth), meta.truth())
}

func (metaCarrier) Clear(dir string) error {
	held, err := readMeta(dir)
	if err != nil || held.ID == "" || reflect.DeepEqual(held.truth(), metaTruth{}) {
		return err // nothing left in meta.json to move
	}
	meta, err := LoadMeta(dir)
	if err != nil {
		return err
	}
	return SaveMeta(dir, meta)
}
