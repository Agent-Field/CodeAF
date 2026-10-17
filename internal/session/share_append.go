package session

// share_append.go is the one append a share makes to the deliverables index.
//
// IT IS NOT [RecordArtifact], on purpose. That door is silent by design — a
// generator that has just made a picture has nothing to do with the news that a
// lookup row was not written — and it drops an over-size deliverable fact to
// keep the plain row. A share cannot do either: the row IS the publication, and
// a row written without its fact is a file nobody can find as shared, under an
// id the receipt would then call recorded. So this append refuses instead of
// shortening, says when it failed, and is durable before it says it succeeded.
//
// ── WHAT ONE APPEND GUARANTEES ──
//
//   - The row is exactly the planned row, with its fact, or nothing is written.
//   - ONE write of the whole line, so O_APPEND's atomic offset keeps another
//     writer's row from landing inside it.
//   - A line some earlier writer left without its newline (a crash mid-row) is
//     ended first, in the same write, so this row is never glued onto it.
//   - A short write is a failure, never a success: the tail it leaves is a
//     partial line every reader skips, and the retry's exact-id search proves
//     the row absent and appends it again, ended correctly.
//   - The file is synced before the append answers.

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// errShareAppend is an append that did not durably write the whole row.
var errShareAppend = errors.New("the shared row was not durably written")

// appendShareRow appends row to the index at path, whole and synced.
func appendShareRow(path string, row Artifact) error {
	line, err := shareRowLine(path, row)
	if err != nil {
		return err
	}
	artifactsMu.Lock()
	defer artifactsMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	ended, err := shareIndexEnded(file)
	if err != nil {
		return err
	}
	if !ended {
		line = append([]byte{'\n'}, line...)
	}
	written, err := file.Write(line)
	if err == nil && written != len(line) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return errors.Join(errShareAppend, err)
	}
	if err := file.Sync(); err != nil {
		return errors.Join(errShareAppend, err)
	}
	return nil
}

// shareRowLine is the row's line, refused rather than shortened: a row with no
// path, title or valid fact, or one past the bound every reader keeps.
func shareRowLine(path string, row Artifact) ([]byte, error) {
	if strings.TrimSpace(path) == "" || strings.TrimSpace(row.Path) == "" || strings.TrimSpace(row.Title) == "" {
		return nil, errShareAppend
	}
	if row.Deliver == nil || row.Deliver.Update || !ValidDeliverableID(row.Deliver.ID) {
		return nil, errShareAppend
	}
	line, err := json.Marshal(row)
	if err != nil || !deliverRowFits(line) {
		return nil, errShareAppend
	}
	return append(line, '\n'), nil
}

// shareIndexEnded says the index is empty or its last byte ends a line.
func shareIndexEnded(file *os.File) (bool, error) {
	info, err := file.Stat()
	if err != nil {
		return false, err
	}
	if info.Size() == 0 {
		return true, nil
	}
	last := make([]byte, 1)
	if _, err := file.ReadAt(last, info.Size()-1); err != nil {
		return false, err
	}
	return last[0] == '\n', nil
}
