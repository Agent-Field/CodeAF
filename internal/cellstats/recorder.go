package cellstats

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/Agent-Field/codeaf/internal/cellsync"
)

// Counts is a snapshot of what one store client has asked of the store so far.
// Its fields match blobstore.Counters so the wiring layer can read one into
// the other without this package importing the decorator.
type Counts struct {
	Puts, Gets, Has, BytesUp, BytesDown int64
}

// Meter reads a snapshot of the running counters. Keeping the reader this
// small lets any counting store, real or fake, be measured.
type Meter interface{ Snapshot() Counts }

// MeterFunc makes a function a Meter.
type MeterFunc func() Counts

// Snapshot implements Meter.
func (f MeterFunc) Snapshot() Counts { return f() }

// Recorder turns each flush into one appended line. Request and byte figures
// are the meter's growth since the previous flush, so a line is what that one
// flush cost and the lines add up to the meter's total.
type Recorder struct {
	Path  string
	Meter Meter // nil records zero request figures

	mu   sync.Mutex
	last Counts
	err  error
}

// NewRecorder records one cell's flushes under home. A meter is optional.
func NewRecorder(home, cellID string, m Meter) *Recorder {
	return &Recorder{Path: Path(home, cellID), Meter: m}
}

// OnFlush is the cellsync.Batcher hook. It never fails the flush: a stats file
// that cannot be written is kept in Err and the sync carries on.
func (r *Recorder) OnFlush(f cellsync.Flush) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.err = appendLine(r.Path, r.line(f))
}

// Err is the outcome of the latest write, nil when it worked.
func (r *Recorder) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

func (r *Recorder) line(f cellsync.Flush) Line {
	l := Line{V: Version, Turn: f.Head, Turns: f.Turns, Frames: f.Frames, Objects: f.Objects}
	now := r.snapshot()
	l.Puts, l.Gets, l.Has = now.Puts-r.last.Puts, now.Gets-r.last.Gets, now.Has-r.last.Has
	l.BytesUp, l.BytesDown = now.BytesUp-r.last.BytesUp, now.BytesDown-r.last.BytesDown
	r.last = now
	return l
}

func (r *Recorder) snapshot() Counts {
	if r.Meter == nil {
		return r.last
	}
	return r.Meter.Snapshot()
}

// appendLine adds one line with a single write on an append-only handle, so a
// line is never split and an earlier line is never touched.
func appendLine(path string, l Line) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(Marshal(l)); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
