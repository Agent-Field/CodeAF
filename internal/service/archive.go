package service

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/Agent-Field/codeaf/internal/redact"
)

// A REMOVED SERVICE LEAVES THE LIST AND KEEPS ITS OUTPUT. The board's word is
// that Remove "drops it from Processes; its output stays in the chat's notice",
// so Remove deletes the record that makes it a service and nothing that it
// wrote. What stays is the log where it always was, and one small descriptor
// beside it naming whose it was:
//
//	<dir>/archive/<id>.json   {id, owner, name, command, removed}
//	<dir>/logs/<id>.log       untouched, and <id>.log.1 before it
//
// Ids are never handed out twice — the allocator is in the store, written and
// its folder synced before an id is used — so a removed service's output can
// never be overwritten by a later service's. A descriptor is read by its id
// alone, never by listing the folder, so however long codeaf runs, opening
// one old notice costs one small file read.
const (
	archiveDir      = "archive"
	maxArchiveBytes = 16 << 10
)

var serviceID = regexp.MustCompile(`^s[1-9][0-9]{0,9}$`)

// Archived is a removed service, as its output's owner sees it.
type Archived struct {
	ID              string    `json:"id"`
	Owner           string    `json:"owner"`
	Name            string    `json:"name"`
	Command         string    `json:"command"`
	Removed         time.Time `json:"removed"`
	OutputTruncated bool      `json:"output_truncated,omitempty"`
}

func (s *Supervisor) archivePath(id string) string {
	return filepath.Join(s.dir, archiveDir, id+".json")
}

// writeArchive writes one descriptor whole and durably, private to the person.
func (s *Supervisor) writeArchive(a Archived) error {
	raw, err := json.Marshal(a)
	if err != nil {
		return err
	}
	if len(raw) > maxArchiveBytes {
		return errors.New("the removed service's description is too large")
	}
	dir := filepath.Join(s.dir, archiveDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".archive-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	_, writeErr := tmp.Write(raw)
	syncErr := tmp.Sync()
	closeErr := tmp.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), s.archivePath(a.ID)); err != nil {
		return err
	}
	return syncDir(dir)
}

// readArchive reads one descriptor by id and answers it only to its owner. An
// id that is not one this package mints, a path that is not a plain file, a
// file larger than any descriptor and an owner who is not the one recorded
// are all "no such process": a guessed id reveals nothing.
func (s *Supervisor) readArchive(owner, id string) (Archived, error) {
	if owner == "" || !serviceID.MatchString(id) {
		return Archived{}, ErrNotFound
	}
	file, err := os.Open(s.archivePath(id))
	if err != nil {
		return Archived{}, ErrNotFound
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxArchiveBytes {
		return Archived{}, ErrNotFound
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxArchiveBytes))
	if err != nil {
		return Archived{}, ErrNotFound
	}
	var a Archived
	if json.Unmarshal(raw, &a) != nil || a.ID != id || a.Owner != owner {
		return Archived{}, ErrNotFound
	}
	return a, nil
}

// Archive is one removed service of owner's, for the notice that still links
// to its output.
func (s *Supervisor) Archive(owner, id string) (Archived, error) {
	return s.readArchive(owner, id)
}

func archivedOf(r record, now time.Time) Archived {
	return Archived{ID: r.ID, Owner: r.Owner, Name: r.Spec.Name, Command: redact.Secrets(r.Spec.Command), Removed: now.UTC(), OutputTruncated: r.Truncated}
}
