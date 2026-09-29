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
	"strings"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// SessionTruth is the part of [Meta] that lives in the sealed tree, as
// .cell/session.json holds it. Every path in it is spelled against a named
// base (pathcodec.go), never absolute.
type SessionTruth struct {
	V             uint16          `json:"V"`
	LaunchDir     string          `json:"launchDir,omitempty"`
	Owned         bool            `json:"owned,omitempty"`
	Effort        string          `json:"effort,omitempty"`
	Approval      string          `json:"approval,omitempty"`
	Places        []PlaceRef      `json:"places,omitempty"`
	Trees         []StandingTree  `json:"trees,omitempty"`
	Archived      bool            `json:"archived,omitempty"`
	ArchivedTasks map[string]bool `json:"archivedTasks,omitempty"`
}

const truthVersion = 1

func (m Meta) truth() SessionTruth {
	return SessionTruth{truthVersion, m.LaunchDir, m.Owned, m.Effort, m.Approval, m.Places, m.Trees, m.Archived, m.ArchivedTasks}
}

func (m *Meta) setTruth(t SessionTruth) {
	m.LaunchDir, m.Owned, m.Effort, m.Approval = t.LaunchDir, t.Owned, t.Effort, t.Approval
	m.Places, m.Trees, m.Archived, m.ArchivedTasks = t.Places, t.Trees, t.Archived, t.ArchivedTasks
}

// derived is m without its truth: what meta.json holds in the cell layout.
func (m Meta) derived() Meta {
	m.setTruth(SessionTruth{})
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
	meta.setTruth(mapTruth(t, codecOf(dir, meta).resolve))
	return meta, nil
}

func readTruth(path string) (SessionTruth, error) {
	var t SessionTruth
	raw, err := os.ReadFile(path)
	if err != nil {
		return t, err
	}
	if json.Unmarshal(raw, &t) != nil {
		return SessionTruth{}, nil // a corrupt file reads as no truth, as meta.json does
	}
	return t, nil
}

// writeTruth seals meta's truth at path, every path spelled against the bases
// of the folder dir and the workspace meta names.
func writeTruth(dir, path string, meta Meta) error {
	return writeJSONAtomic(path, mapTruth(meta.truth(), codecOf(dir, meta).encode))
}

// codecOf is the codec of the folder dir for a session whose summary is meta:
// its workspace is the one the summary names, or its own work/ when it owns
// one, and unknown when neither is on this machine yet.
func codecOf(dir string, meta Meta) pathCodec {
	workspace := strings.TrimSpace(meta.Workspace)
	if workspace == "" && meta.Owned {
		workspace = (Place{Dir: dir, Owned: true}).Work()
	}
	return codecFor(dir, workspace)
}

// TruthCarriers is what a legacy folder carries into .cell/ when it migrates:
// the state, task and team files, the node journals, and the truth half of
// its meta.
func TruthCarriers() []cell.Carrier {
	return []cell.Carrier{cell.Files(truthNames...), cell.Tree(truthTrees...), metaCarrier{}}
}

// metaCarrier stages the truth fields of the legacy meta.json into the new
// cell, then rewrites meta.json as the summary alone.
type metaCarrier struct{}

func (metaCarrier) Stage(dir string, c cell.Cell) error {
	meta, err := readMeta(dir)
	if err != nil {
		return err
	}
	return writeTruth(dir, filepath.Join(c.Root, cellStateDir, placeMetaTruth), meta)
}

func (metaCarrier) Clear(dir string) error {
	held, err := readMeta(dir)
	if err != nil || held.ID == "" || reflect.DeepEqual(held.truth(), Meta{}.truth()) {
		return err // nothing left in meta.json to move
	}
	meta, err := LoadMeta(dir)
	if err != nil {
		return err
	}
	return SaveMeta(dir, meta)
}
