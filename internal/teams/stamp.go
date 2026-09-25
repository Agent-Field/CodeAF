package teams

import (
	"errors"
	"os"
	"strconv"
	"sync"
	"time"
)

// ── STAMPS: WHETHER A FILE MOVED, ANSWERED BY A STAT ────────────────────────
//
// A reader that asks every second whether the teams file or a Traffic log has
// changed must not read either file to find out. A stamp is what one stat says
// about a file, its size and its modification time, written as a short string
// so that it can cross a wire and be handed back unchanged. Two equal stamps
// are a file nobody wrote in between.
//
// EVERY WRITE HERE MOVES THE TIME FORWARD, so the stamp is a real answer and not
// a likely one. A filesystem keeps modification times at some granularity, and
// two writes inside one tick with the same size would read as one; [write] and
// [AppendTraffic] therefore set the new file's time to one microsecond past the
// old one's whenever the clock has not moved past it. Every write of the teams
// file happens under its lock, so the times it leaves only ever increase.
//
// A stamp is never "": [MissingStamp] is the answer for a file that is not
// there, and "" is kept for a reader that has not looked yet, so a reader
// passing "" to [ChangeIf] or to a caller that compares is always told the file
// moved.

// MissingStamp is the stamp of a file that does not exist.
const MissingStamp = "-"

// ErrStale is [ChangeIf] finding the file changed since the stamp it was given.
// Nothing was written; the caller reads again and makes its change again.
var ErrStale = errors.New("teams: the file changed since it was read")

// Stamp is the teams file's stamp in profileDir.
func Stamp(profileDir string) string { return stampOf(Path(profileDir)) }

// TrafficStamp is the stamp of team teamID's current Traffic log. A rotation
// starts a new current file, so it moves this stamp as an append does.
func TrafficStamp(profileDir, teamID string) string {
	return stampOf(TrafficPath(profileDir, teamID))
}

func stampOf(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return MissingStamp
	}
	return strconv.FormatInt(info.Size(), 10) + "." + strconv.FormatInt(info.ModTime().UnixNano(), 10)
}

// advance sets path's modification time one microsecond past before when the
// write that made it did not already move past it. A zero before is a file
// that did not exist, and nothing needs moving.
func advance(path string, before time.Time) {
	if before.IsZero() {
		return
	}
	info, err := os.Stat(path)
	if err != nil || info.ModTime().After(before) {
		return
	}
	at := before.Add(time.Microsecond)
	_ = os.Chtimes(path, at, at)
}

// modTime is path's modification time, zero when it is not there.
func modTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// Change is [Update] that answers what it wrote and the file's stamp after the
// write, both taken under the lock, so the caller can hold the list and know
// which version of the file it is.
func Change(profileDir string, fn func(*File) error) (*File, string, error) {
	return change(profileDir, nil, fn)
}

// ChangeIf is [Change] only while the file is still at stamp base: the
// compare-and-swap a writer on the far side of a wire needs, because it read
// the file in one call and writes it in another and the lock cannot be held
// across the two. A file that moved in between is [ErrStale], nothing is
// written, and fn is not called.
func ChangeIf(profileDir, base string, fn func(*File) error) (*File, string, error) {
	return change(profileDir, &base, fn)
}

func change(profileDir string, base *string, fn func(*File) error) (*File, string, error) {
	var (
		wrote *File
		stamp string
	)
	err := withLock(profileDir, lockWait, func() error {
		if base != nil && Stamp(profileDir) != *base {
			return ErrStale
		}
		f, err := updateLocked(profileDir, fn)
		if err != nil {
			return err
		}
		wrote, stamp = f, Stamp(profileDir)
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	return wrote, stamp, nil
}

// ── A TRAFFIC READER THAT STATS BEFORE IT READS ─────────────────────────────

// Watch reads Traffic logs for a reader that comes back with the cursor it was
// last given, and answers "nothing new" from one stat when the log has not
// moved since. It is the reading a clock makes once a second while a managed
// team is held, locally and on the engine's side of a wire, and it is safe for
// several goroutines at once. The zero value is ready.
//
// IT REMEMBERS ONE MARK PER LOG: the stamp the log had just before the last
// read, and the cursor that read left the reader at. A read asked from that
// cursor with the stamp unchanged cannot find anything, so it is not made. The
// stamp is taken BEFORE the read, so a line written while the read was going
// on moves the stamp past the mark and is found on the next turn.
type Watch struct {
	mu    sync.Mutex
	marks map[string]watchMark
	// reads counts the reads made, for a test to see the quiet turns make none.
	reads int
}

type watchMark struct{ stamp, after string }

// Traffic is [ReadTraffic] for team teamID, after the cursor after, at most
// limit entries, with the log's stamp; a read from the cursor the last one
// left, of a log that has not moved, answers no entries without reading.
// A tail (after "") or an unlimited read is always made.
func (w *Watch) Traffic(profileDir, teamID, after string, limit int) ([]Entry, string, error) {
	if err := safeTeamID(teamID); err != nil {
		return nil, "", err
	}
	key := profileDir + "\x00" + teamID
	stamp := TrafficStamp(profileDir, teamID)
	if after != "" && limit > 0 && w.quiet(key, stamp, after) {
		return nil, stamp, nil
	}
	entries, err := ReadTraffic(profileDir, teamID, after, limit)
	if err != nil {
		return nil, stamp, err
	}
	next := after
	if len(entries) > 0 {
		next = entries[len(entries)-1].ID
	}
	// A page that came back full may have more behind it, and a tail is not a
	// cursor; neither is remembered, so the next ask reads.
	if limit > 0 && len(entries) >= limit || after == "" && len(entries) == 0 {
		next = ""
	}
	w.mark(key, stamp, next)
	return entries, stamp, nil
}

func (w *Watch) quiet(key, stamp, after string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	m, ok := w.marks[key]
	return ok && m.stamp == stamp && m.after == after
}

func (w *Watch) mark(key, stamp, after string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.reads++
	if w.marks == nil {
		w.marks = map[string]watchMark{}
	}
	if after == "" {
		delete(w.marks, key)
		return
	}
	w.marks[key] = watchMark{stamp: stamp, after: after}
}
