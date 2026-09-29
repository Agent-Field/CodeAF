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
// folder keeps its name, so a session keeps its id; tasks/, trees/ and the
// session's own files stay where they are, because the cell layout only
// moves the journal (internal/session/layout.go). What appears is .cell/ with
// the journal, an env/ directory and a meta.json that holds no absolute path.
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
// A folder that is already a cell, or holds no journal, is left as it is.
func MigrateLegacy(dir string) error {
	if isCell(dir) {
		return finish(dir)
	}
	if _, err := os.Stat(filepath.Join(dir, legacyTranscript)); err != nil {
		return ignoreMissing(err)
	}
	if err := stageCell(dir); err != nil {
		return fmt.Errorf("migrate %s: %w", filepath.Base(dir), err)
	}
	if err := os.Rename(filepath.Join(dir, stagingDir, StateDir), filepath.Join(dir, StateDir)); err != nil {
		return fmt.Errorf("migrate %s: %w", filepath.Base(dir), err)
	}
	return finish(dir)
}

// finish clears what a migration leaves once .cell is in place.
func finish(dir string) error {
	if err := os.RemoveAll(filepath.Join(dir, stagingDir)); err != nil {
		return err
	}
	return dropLegacyName(dir)
}

// stageCell builds a complete .cell for dir under the staging directory.
func stageCell(dir string) error {
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
	return os.Link(filepath.Join(dir, legacyTranscript), c.abs(TranscriptPath))
}

// dropLegacyName removes the top-level journal name once .cell holds the same
// file. Anything else at that name is not ours to remove.
func dropLegacyName(dir string) error {
	legacy := filepath.Join(dir, legacyTranscript)
	old, err := os.Stat(legacy)
	if err != nil {
		return ignoreMissing(err)
	}
	kept, err := os.Stat(filepath.Join(dir, TranscriptPath))
	if err != nil || !os.SameFile(old, kept) {
		return ignoreMissing(err)
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
