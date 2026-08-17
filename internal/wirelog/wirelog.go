// Package wirelog counts what the surface actually puts on the wire.
//
// A TUI over SSH is not slow because it renders slowly. It is slow because
// every frame it renders becomes bytes on a link with a round trip in it, and
// nobody who has ever tuned one could say from memory how many bytes a second
// of streaming costs, or whether an idle prompt costs anything at all. This
// package answers that with a number instead of a hunch: it wraps the writer
// the terminal program paints through, counts bytes and write calls, and
// appends one line per second to a file.
//
// It is a DEVELOPER'S instrument and nothing else. There is no settings row,
// no flag and no slash command, because there is no question a person using
// aforge would ask that this answers — the audience is whoever is holding the
// SSH story and needs to know whether a change made it cheaper. It turns on
// only when AFORGE_WIRE_LOG names a file, and when it does not, the surface
// never learns it exists: [FromEnv] returns a nil *Meter, the caller leaves
// the output writer nil, and Bubble Tea paints straight into os.Stdout exactly
// as it did before. The cost of the meter when it is off is one LookupEnv at
// boot.
//
// The log is deliberately dumb — four space-separated integers per line, one
// line per wall-clock second, silent seconds included as zeros so a reader can
// tell "nothing was drawn" from "the process was gone":
//
//	# unix_ms bytes writes total_bytes
//	1755400000000 41230 60 41230
//	1755400001000 0 0 41230
//
// A second's bytes and writes are what left the program during THAT second;
// total_bytes is cumulative since the meter opened. Seconds are wall-clock
// seconds and not "seconds since start" so that a harness driving the surface
// from outside can line its own phase boundaries up against them without the
// two sides having to agree on when zero was.
package wirelog

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// EnvVar names the file the meter appends to. Empty or unset means no meter.
//
// It is an environment variable rather than a flag on purpose: a flag is a
// promise to a user, and this is a wire we tap while developing. Undocumented
// in the help output, in the settings screen and in the user-facing docs by
// design — the day it becomes something a person should reach for, it earns a
// name there and stops being this.
const EnvVar = "AFORGE_WIRE_LOG"

// tickInterval is how often an idle meter checks whether a second has closed.
// Only silent seconds need it — a second with any output in it is closed by
// the next write. Well under a second so an idle row lands in the log at
// roughly the moment it describes.
const tickInterval = 200 * time.Millisecond

// Meter is the terminal's writer with a counter on it.
//
// It satisfies charmbracelet/x/term.File (ReadWriteCloser plus Fd) because
// Bubble Tea type-asserts its output to exactly that before it will put the
// terminal in raw mode, ask for the window size, or believe the terminal has
// color. A plain io.Writer wrapper measures a surface that is no longer the
// surface — cooked mode, no size, ASCII profile — so the wrapper carries the
// file descriptor through and the measurement stays about the real thing.
//
// The zero value is not usable; a nil *Meter is, in the sense that the caller
// checks for it and never wraps at all.
type Meter struct {
	// out is the real terminal. Every Write reaches it before the counter is
	// touched, so a meter that is somehow broken costs the frame nothing but
	// the counting itself.
	out *os.File
	log io.WriteCloser
	now func() time.Time

	stop chan struct{}
	done chan struct{}
	once sync.Once

	mu     sync.Mutex
	open   bool  // has a bucket been started (i.e. has anything been written)
	sec    int64 // unix second the open bucket covers
	bytes  int64 // bytes in the open bucket
	writes int64 // write calls in the open bucket
	total  int64 // bytes since the meter opened
}

// FromEnv returns a meter on out when [EnvVar] names a file, and nil when it
// does not — which is the whole opt-in.
//
// A nil *Meter is returned as a nil *Meter and not as an io.Writer, because a
// typed nil inside an interface is not nil and the caller's `if meter != nil`
// would then wrap stdout in a meter that panics on first paint.
//
// A file that cannot be opened returns nil too, with the reason on stderr. The
// alternative is refusing to start a chat because a debugging aid could not
// write its log, and no measurement is worth a door that will not open.
func FromEnv(out *os.File) *Meter {
	path := os.Getenv(EnvVar)
	if path == "" {
		return nil
	}
	meter, err := Open(path, out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aforge: %s: %v\n", EnvVar, err)
		return nil
	}
	return meter
}

// Open starts a meter on out that appends to path.
//
// Appending rather than truncating: a harness that runs the surface twice
// against one log gets both runs, and the gap between them is visible as the
// missing seconds it actually was.
func Open(path string, out *os.File) (*Meter, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintln(file, "# unix_ms bytes writes total_bytes"); err != nil {
		_ = file.Close()
		return nil, err
	}
	meter := newMeter(out, file, time.Now)
	meter.stop, meter.done = make(chan struct{}), make(chan struct{})
	go meter.sweep()
	return meter, nil
}

// newMeter builds a meter with no sweeper. Tests use it with a clock they own
// so a second can pass without one passing.
func newMeter(out *os.File, log io.WriteCloser, now func() time.Time) *Meter {
	return &Meter{out: out, log: log, now: now}
}

// sweep closes silent seconds. Without it a surface that draws nothing for a
// minute would log nothing for a minute and then, on the next frame, sixty
// zero rows at once — true, but only after the fact, and useless to anyone
// watching the file as it fills.
func (m *Meter) sweep() {
	defer close(m.done)
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-ticker.C:
			m.mu.Lock()
			m.rollLocked(m.now().Unix())
			m.mu.Unlock()
		}
	}
}

// Write paints and counts, in that order.
func (m *Meter) Write(p []byte) (int, error) {
	n, err := m.out.Write(p)
	m.mu.Lock()
	m.recordLocked(n, m.now())
	m.mu.Unlock()
	return n, err
}

// Read is the other half of term.File. Bubble Tea reads its input from stdin
// and never from here, but the interface asks and an honest answer is one line.
func (m *Meter) Read(p []byte) (int, error) { return m.out.Read(p) }

// Fd is why this type exists rather than a bare io.Writer wrapper: it is the
// descriptor raw mode, the window size and the color probe are all asked about.
func (m *Meter) Fd() uintptr { return m.out.Fd() }

// Close writes the last, partial second and closes the log.
//
// It does NOT close the terminal. A meter is a tap on a wire that belongs to
// the process, and closing the process's stdout on the way out of a chat would
// take the shell prompt with it.
func (m *Meter) Close() error {
	var err error
	m.once.Do(func() {
		if m.stop != nil {
			close(m.stop)
			<-m.done
		}
		m.mu.Lock()
		if m.open {
			m.emitLocked()
			m.open = false
		}
		m.mu.Unlock()
		err = m.log.Close()
	})
	return err
}

// Total reports bytes written since the meter opened. For tests and for
// anything that wants the number without parsing the file back.
func (m *Meter) Total() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.total
}

// recordLocked adds one write to the bucket that owns at.
func (m *Meter) recordLocked(n int, at time.Time) {
	sec := at.Unix()
	if !m.open {
		m.open, m.sec = true, sec
	}
	m.rollLocked(sec)
	m.bytes += int64(n)
	m.total += int64(n)
	m.writes++
}

// rollLocked closes every bucket that now lies in the past. The seconds
// between the last write and now get their own rows with zeros in them,
// because a reader asking "what does idle cost" is asking about exactly those
// rows and an absent row is not an answer.
func (m *Meter) rollLocked(sec int64) {
	if !m.open {
		return
	}
	for m.sec < sec {
		m.emitLocked()
		m.sec++
		m.bytes, m.writes = 0, 0
	}
}

// emitLocked writes the open bucket's row. A log that cannot be written to is
// ignored on purpose: the surface is mid-frame and the person at the keyboard
// did not ask for any of this.
func (m *Meter) emitLocked() {
	fmt.Fprintf(m.log, "%d %d %d %d\n", m.sec*1000, m.bytes, m.writes, m.total) //nolint:errcheck
}
