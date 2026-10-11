// Package journal is the file-backed placeorg.Journal: an append-only record
// of one organising run after another, beside the places store.
//
// THE FILE IS THE RECORD, NOT THE UNDO. Undo is the one receipt a plan makes
// in placegraph. This file is what a later read uses to see every op a run
// proposed, in order. The self-check replays those ops; it does not trust the
// plan's own list alone.
//
// ONE LINE IS ONE ENTRY. Write appends a single JSON object and a newline in
// one call, with nothing buffered across lines. A crash between lines leaves
// every finished line intact. A crash during a line leaves a torn tail after
// the last newline; the next Open drops that tail, and Replay ignores it, so
// the finished lines stay readable either way.
//
// THE FILE IS SYNCED WHEN A RUN ENDS, not on every line. A finished run is the
// one that survives a power loss. Lines written after the previous run ended
// can still be lost if the machine dies before this run's run.ended line.
//
// AT RotateAt BYTES THE LIVE FILE IS SEALED. The sealed name is
// places-journal.jsonl.N, where 1 is the most recently sealed file and a
// higher N is older. Sealed files are kept: a run's ops may sit in one of
// them, and Replay reads oldest first, then the live file. The architecture
// names the 32 MB cut and not the sealed names; this spelling is the one
// Replay understands, so a second scheme would hide ops from the self-check.
package journal

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

const (
	// FileName is the live segment beside the store.
	FileName = "places-journal.jsonl"
	// RotateAt is the size at which the live segment is sealed and a new one
	// opened. One line may land past it; the next line starts the new file.
	// ARCHITECTURE.md is the source of the 32 MB figure.
	RotateAt int64 = 32 << 20
)

// errClosed is returned by Write after Close. A closed journal cannot append,
// because the descriptor is gone and a later write would have nowhere honest
// to land.
var errClosed = errors.New("placeorg/journal: closed")

// File is the append-only journal. The zero value is not usable; Open is the
// constructor. Write is safe for concurrent callers.
type File struct {
	dir   string
	file  *os.File
	size  int64
	limit int64
	mu    sync.Mutex

	// syncFile and syncDir are the durability calls. Production uses the real
	// ones; a test replaces them to count calls without changing the order.
	syncFile func(*os.File) error
	syncDir  func(string) error
	// writeLine is the one append. A test replaces it to simulate a short
	// write, which is the in-process shape of a crash during a line.
	writeLine func(*os.File, []byte) (int, error)
}

// Open opens the live journal in dir, creating it if needed. A torn tail left
// by a crash is dropped before the first append. A live file already at
// RotateAt is sealed first, so this process does not keep extending a segment
// the previous one should have closed.
func Open(dir string) (*File, error) {
	return open(dir, RotateAt)
}

// open is Open with a chosen cut, so a test can rotate without writing 32 MB.
func open(dir string, limit int64) (*File, error) {
	if dir == "" {
		return nil, errors.New("placeorg/journal: directory is required")
	}
	if limit <= 0 {
		return nil, errors.New("placeorg/journal: rotate limit must be positive")
	}
	j := &File{
		dir:       dir,
		limit:     limit,
		syncFile:  func(f *os.File) error { return f.Sync() },
		syncDir:   syncDir,
		writeLine: func(f *os.File, p []byte) (int, error) { return f.Write(p) },
	}
	if err := j.reopen(); err != nil {
		return nil, err
	}
	if j.size > 0 && j.size >= j.limit {
		if err := j.rotate(); err != nil {
			_ = j.closeLocked()
			return nil, err
		}
	}
	return j, nil
}

// Write appends one entry. A run.ended line is synced before Write returns,
// so a finished run is durable. Any other line is only as durable as the
// kernel's writeback until the run ends. A sync failure after run.ended
// leaves the line in the file and returns the error: cutting it back out
// would open a second torn-write window, and the caller can read the line.
func (j *File) Write(e placeorg.Entry) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.file == nil {
		return errClosed
	}
	line, err := encode(e)
	if err != nil {
		return err
	}
	if err := j.rotateIfFull(); err != nil {
		return err
	}
	if err := j.append(line); err != nil {
		return err
	}
	if e.Kind != placeorg.EntryRunEnded {
		return nil
	}
	if err := j.sync(j.file); err != nil {
		return fmt.Errorf("placeorg/journal: run ended but the record was not synced: %w", err)
	}
	return nil
}

// Close releases the file. It does not sync: durability is the run.ended
// line, and syncing here would make a crash and a clean stop look the same.
func (j *File) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.closeLocked()
}

func (j *File) closeLocked() error {
	if j.file == nil {
		return nil
	}
	err := j.file.Close()
	j.file = nil
	return err
}

// Ops returns the ops journaled for run, in write order. It waits out an
// in-flight Write so the list cannot stop on a line that is half appended.
func (j *File) Ops(run string) ([]placeorg.Op, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return Replay(j.dir, run)
}

func (j *File) rotateIfFull() error {
	if j.size == 0 || j.size < j.limit {
		return nil
	}
	return j.rotate()
}

// rotate seals the live file and opens a new one. The old bytes are synced
// before the rename, and the directory is synced after it, so a crash cannot
// leave the sealed name pointing at an empty file.
func (j *File) rotate() error {
	if err := j.sync(j.file); err != nil {
		return err
	}
	if err := j.file.Close(); err != nil {
		j.file = nil
		return err
	}
	j.file = nil
	if err := shiftSealed(j.dir); err != nil {
		return errors.Join(err, j.reopen())
	}
	if err := j.syncDirectory(); err != nil {
		return errors.Join(err, j.reopen())
	}
	return j.reopen()
}

func (j *File) reopen() error {
	path := filepath.Join(j.dir, FileName)
	_, statErr := os.Stat(path)
	created := os.IsNotExist(statErr)
	if statErr != nil && !created {
		return statErr
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	size, err := trimPartial(f, info.Size())
	if err != nil {
		_ = f.Close()
		return err
	}
	if created {
		if err := j.syncDirectory(); err != nil {
			_ = f.Close()
			return err
		}
	}
	j.file = f
	j.size = size
	return nil
}

// append writes one whole line. A short write is truncated back to the last
// good line so the next append cannot glue itself onto the torn tail.
func (j *File) append(line []byte) error {
	n, err := j.writeLine(j.file, line)
	if err == nil && n == len(line) {
		j.size += int64(n)
		return nil
	}
	if n > 0 {
		if terr := j.file.Truncate(j.size); terr != nil {
			return errors.Join(err, terr)
		}
	}
	if err == nil {
		err = io.ErrShortWrite
	}
	return err
}

func (j *File) sync(f *os.File) error {
	if j.syncFile != nil {
		return j.syncFile(f)
	}
	return f.Sync()
}

func (j *File) syncDirectory() error {
	if j.syncDir != nil {
		return j.syncDir(j.dir)
	}
	return syncDir(j.dir)
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// encode is one JSON object plus the newline that marks the line finished.
// json.Marshal does not emit a raw newline, so the only newline in the
// buffer is the record boundary.
func encode(e placeorg.Entry) ([]byte, error) {
	raw, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

// trimPartial drops a trailing partial line. A file that ends on a newline
// is already a sequence of finished records and is left alone.
func trimPartial(f *os.File, size int64) (int64, error) {
	if size == 0 {
		return 0, nil
	}
	last, err := readByteAt(f, size-1)
	if err != nil {
		return 0, err
	}
	if last == '\n' {
		return size, nil
	}
	end, err := lastNewline(f, size)
	if err != nil {
		return 0, err
	}
	if err := f.Truncate(end); err != nil {
		return 0, err
	}
	return end, nil
}

func readByteAt(f *os.File, at int64) (byte, error) {
	var one [1]byte
	_, err := f.ReadAt(one[:], at)
	return one[0], err
}

// lastNewline returns the offset just after the last newline before size,
// or 0 when the file has none. The scan walks backward so a 32 MB segment
// is not loaded to repair a short torn tail.
func lastNewline(f *os.File, size int64) (int64, error) {
	const window = 4096
	pos := size
	for pos > 0 {
		n := int64(window)
		if n > pos {
			n = pos
		}
		buf := make([]byte, n)
		start := pos - n
		if _, err := f.ReadAt(buf, start); err != nil {
			return 0, err
		}
		for i := len(buf) - 1; i >= 0; i-- {
			if buf[i] == '\n' {
				return start + int64(i) + 1, nil
			}
		}
		pos = start
	}
	return 0, nil
}

// Confirm File is the journal the runner holds.
var _ placeorg.Journal = (*File)(nil)
