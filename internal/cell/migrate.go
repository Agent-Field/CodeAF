package cell

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// legacyTranscript is where a legacy session folder keeps its journal.
const legacyTranscript = "transcript.jsonl"

// stagingDir is where a migration builds .cell before it appears. It is
// inside the folder, so the final rename never crosses a filesystem.
const stagingDir = ".cell-staging"

// MigrateLegacy turns a legacy session folder into a cell in place. The
// folder keeps its name, so a session keeps its id. The truth moves into .cell/
// with the journal (internal/session/layout.go): state.json, tasks.json,
// team.json and the truth half of the session meta. tasks/ (node journals),
// trees/ (git worktrees, which record absolute paths) and the derived files
// (meta.json summary, card.json) stay where they are. What appears is .cell/
// with those, an env/ directory and a meta.json that holds no absolute path.
//
// Every step leaves a folder that opens, and a second call finishes whatever a
// first one left:
//
//  1. .cell is built under [stagingDir]. The folder is still legacy: a crash
//     leaves a directory nobody reads, and the next call replaces it.
//  2. The journal is hard-linked into the staged .cell, so both names are one
//     file: a window still holding the old name keeps writing the same bytes.
//  3. One rename moves the staged .cell into place. The folder is a cell now
//     and its journal is where the cell layout looks.
//  4. The legacy name is removed. A crash before this leaves a second name for
//     the same file, which the next call removes.
//
// Each Carrier does the same for one more kind of truth (state, tasks, the
// truth half of meta): it stages a copy in step 2, and clears the legacy
// original in step 4, so every crash point opens and a second call finishes.
//
// A folder that is already a cell, or holds no journal, is left as it is.
func MigrateLegacy(dir string, carry ...Carrier) error {
	carry = append([]Carrier{Files(legacyTranscript)}, carry...)
	if isCell(dir) {
		return finish(dir, carry)
	}
	if _, err := os.Stat(filepath.Join(dir, legacyTranscript)); err != nil {
		return ignoreMissing(err)
	}
	if err := stageCell(dir, carry); err != nil {
		return fmt.Errorf("migrate %s: %w", filepath.Base(dir), err)
	}
	if err := os.Rename(filepath.Join(dir, stagingDir, StateDir), filepath.Join(dir, StateDir)); err != nil {
		return fmt.Errorf("migrate %s: %w", filepath.Base(dir), err)
	}
	return finish(dir, carry)
}

// finish clears what a migration leaves once .cell is in place.
func finish(dir string, carry []Carrier) error {
	if err := os.RemoveAll(filepath.Join(dir, stagingDir)); err != nil {
		return err
	}
	for _, c := range carry {
		if err := c.Clear(dir); err != nil {
			return err
		}
	}
	return nil
}

// stageCell builds a complete .cell for dir under the staging directory.
func stageCell(dir string, carry []Carrier) error {
	stage := filepath.Join(dir, stagingDir)
	if err := os.RemoveAll(stage); err != nil {
		return err
	}
	// A legacy session already ran its tools on the host; declaring it
	// anything narrower would refuse or cut off the work it was doing.
	m, err := Options{Class: HostBound}.meta()
	if err != nil {
		return err
	}
	c := Cell{Root: stage}
	if err := os.MkdirAll(c.abs(EnvPath), 0o700); err != nil {
		return err
	}
	if err := c.WriteMeta(m); err != nil {
		return err
	}
	for _, k := range carry {
		if err := k.Stage(dir, c); err != nil {
			return err
		}
	}
	return nil
}

// Carrier moves one kind of session truth from a legacy folder into its cell.
type Carrier interface {
	// Stage puts a copy of the truth into the staged cell c. The legacy folder
	// is untouched.
	Stage(dir string, c Cell) error
	// Clear removes what the legacy folder still holds once .cell is in place.
	// It is safe to call again, and on a folder that never had the truth.
	Clear(dir string) error
}

// Files carries whole files: each is hard-linked into .cell so both names are
// one file, and the legacy name goes once .cell holds it. A window still
// holding the old name keeps writing the same bytes; anything else at that
// name is not ours to remove. A file the folder does not have is skipped.
func Files(names ...string) Carrier { return files(names) }

type files []string

func (f files) Stage(dir string, c Cell) error {
	for _, name := range f {
		err := os.Link(filepath.Join(dir, name), filepath.Join(c.Root, StateDir, name))
		if err := ignoreMissing(err); err != nil {
			return err
		}
	}
	return nil
}

func (f files) Clear(dir string) error {
	for _, name := range f {
		if err := dropLegacyName(dir, name); err != nil {
			return err
		}
	}
	return nil
}

// dropLegacyName settles one legacy name. A cell that lacks the file (made
// before it was carried) adopts it; one that holds the same file loses the
// legacy name.
func dropLegacyName(dir, name string) error {
	legacy, kept := filepath.Join(dir, name), filepath.Join(dir, StateDir, name)
	old, err := os.Stat(legacy)
	if err != nil {
		return ignoreMissing(err)
	}
	held, err := os.Stat(kept)
	if errors.Is(err, fs.ErrNotExist) {
		return os.Rename(legacy, kept)
	}
	if err != nil || !os.SameFile(old, held) {
		return err
	}
	return ignoreMissing(os.Remove(legacy))
}

func isCell(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, StateDir))
	return err == nil && info.IsDir()
}

func ignoreMissing(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
