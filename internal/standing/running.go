package standing

// running.go is the one thing about an item that is true of THIS INSTANT and
// therefore cannot live in its document: whether some process, anywhere on this
// machine, has that item in its hands right now.
//
// WHY IT IS A FILE AND NOT A FIELD. A pass runs in whichever of a live window
// or the operating system's timer got to the lock first, and the person is very
// often sitting in a different window from the one doing the work — or in no
// window at all, with `aforge tick` doing it. So "firing now" is knowledge one
// process has and every other process needs, which on this side of the product
// means a small file: [Store.markRunning] raises it before the pass looks at an
// item, [Store.clearRunning] takes it down when that item's pass ends, and
// [Store.Running] is what a surface asks.
//
// IT IS NOT THE ITEM'S FLOCK. The `<id>.lock` file beside the document orders
// three writers around one temp+rename (store.go) and is held for microseconds;
// it says nothing about what is happening and it is unlocked long before a
// firing finishes. A marker that tried to be both would be a lock nobody could
// take while a two-minute run was in flight.
//
// EVERY MARKER IS DOUBTED. A process that was killed mid-firing cannot take its
// own marker down, and a `●` that breathed forever after a crash would be the
// screen asserting something it cannot derive — the one thing this product's
// surfaces may not do. So a reader believes a marker only while the process
// that wrote it is alive AND the marker is younger than the longest a pass may
// last ([TickWindow]). Anything else is a leftover, and a leftover is a no.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/processgroup"
)

// RunningPath is the marker's place: <root>/<id>/running, inside the item's own
// folder beside its runs and its log. It is NOT a `.json` beside the document,
// because [Store.List] reads that directory by suffix and a second document
// shape there would be one more thing every reader has to skip.
func (s *Store) RunningPath(id string) string {
	return filepath.Join(s.ItemDir(id), RunningFile)
}

// markRunning says that this process is on this item, and what it is doing.
//
// IT IS BEST EFFORT AND SAYS NOTHING WHEN IT FAILS, for [writeCameTo]'s reason
// and with the failure falling the same safe way: a marker that could not be
// written costs a glyph on somebody's screen, where a pass that refused to run
// because a full disk would not take a marker would cost the work itself.
func (s *Store) markRunning(id, what string) {
	if s == nil || checkID(id) != nil || what == "" {
		return
	}
	data, err := json.Marshal(RunningMark{PID: os.Getpid(), Since: s.now(), What: what})
	if err != nil {
		return
	}
	if err := os.MkdirAll(s.ItemDir(id), 0o700); err != nil {
		return
	}
	// Temp+rename, exactly as a document is written: a surface asks this file
	// every few seconds, and half a marker read mid-write would be a glyph
	// flickering off for one frame every pass.
	_ = writeAtomic(s.RunningPath(id), append(data, '\n'))
}

// clearRunning takes the marker down. A marker that is not there is the state
// it is trying to reach, so a missing file is not an error.
func (s *Store) clearRunning(id string) {
	if s == nil || checkID(id) != nil {
		return
	}
	_ = os.Remove(s.RunningPath(id))
}

// Running answers whether a pass has this item in its hands at this instant,
// and what it is doing with it.
//
// FALSE IS THE ANSWER TO EVERY DOUBT: no marker, a marker that cannot be read,
// a marker with nothing to say, a marker whose process is gone, and a marker
// older than one pass may last. A surface draws `●` on a true and nothing at
// all on a false, so every uncertainty here is a glyph that stays still.
func (s *Store) Running(id string) (RunningMark, bool) {
	if s == nil || checkID(id) != nil {
		return RunningMark{}, false
	}
	raw, err := os.ReadFile(s.RunningPath(id))
	if err != nil {
		return RunningMark{}, false
	}
	var mark RunningMark
	if err := json.Unmarshal(raw, &mark); err != nil {
		return RunningMark{}, false
	}
	if mark.What == "" || mark.Since.IsZero() || !markLive(mark, s.now()) {
		return RunningMark{}, false
	}
	return mark, true
}

// markLive is the whole of doubting a marker: it is younger than the longest a
// pass may last, and the process that wrote it is still there.
//
// THE AGE IS CHECKED FIRST AND IT IS THE ONE THAT MATTERS. Process ids are
// reused, so a marker left by a crash can name a pid that is alive again and
// belongs to somebody else entirely; [TickWindow] is what makes that stop
// mattering, because a pass older than its own ceiling is over however healthy
// the process wearing its number looks.
func markLive(mark RunningMark, now time.Time) bool {
	if now.Sub(mark.Since) > TickWindow {
		return false
	}
	return pidAlive(mark.PID)
}

// pidAlive asks the kernel whether a process is still there, through the one
// package that already knows how to ask on every platform this program builds
// for (internal/processgroup): signal zero where there are signals, an
// open-process handle where there are not, and "may not look" read as "exists"
// on both.
func pidAlive(pid int) bool {
	return pid > 0 && processgroup.ProcessAlive(pid)
}
