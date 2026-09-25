package teams

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/filelock"
)

// The file's name inside the profile directory, and the first build's.
const (
	FileName       = "teams.json"
	LegacyFileName = "spaces.json"
)

// lockWait is how long a write waits for another writer before it gives up
// with [ErrBusy]. A write holds the lock for one small read and one small
// write, so anything near this long is a writer that is stuck, and waiting on
// it for good would freeze whoever called.
const lockWait = 2 * time.Second

// ErrBusy is another process holding the teams file's lock for longer than a
// write should take. Nothing was written.
var ErrBusy = errors.New("teams: the file is being written by somebody else; try again")

// disk is the file's whole shape. Legacy is the first build's list, read and
// never written.
type disk struct {
	Version int    `json:"version"`
	Teams   []Team `json:"teams"`
	Legacy  []Team `json:"spaces,omitempty"`
}

// Path is where the teams live. An empty profile directory is the ordinary
// launch and resolves to this process's own profile ([config.ProfilePath]).
func Path(profileDir string) string { return config.ProfilePath(profileDir, FileName) }

func legacyPath(profileDir string) string { return config.ProfilePath(profileDir, LegacyFileName) }

func lockPath(profileDir string) string { return Path(profileDir) + ".lock" }

// Load reads the teams from profileDir and puts them in order ([tidy]). It
// takes no lock and does not wait: a repair it had to make is written back
// only when the lock is free at that moment, and is otherwise made again on
// the next load. Teams the file left without a colour stay without one; the
// interface, which knows the palette, uses [LoadHued].
//
// A missing file is an empty File and no error. A file that is there but
// unreadable is an error and is left exactly as it was.
//
// THE FIRST BUILD'S FILE IS MIGRATED HERE, once. With no teams.json and a
// spaces.json beside it, the old list is read, written as teams.json, and only
// then is spaces.json renamed to spaces.json.migrated.
func Load(profileDir string) (*File, error) { return load(profileDir, false, nil) }

// LoadHued is [Load] that also gives every uncoloured team a colour around
// the reserved hues ([File.Colour]) and writes that back as a repair.
func LoadHued(profileDir string, reserved []float64) (*File, error) {
	return load(profileDir, true, reserved)
}

func load(profileDir string, colour bool, reserved []float64) (*File, error) {
	f, legacy, stale, err := read(profileDir)
	if err != nil || f == nil {
		return &File{Version: Version}, err
	}
	changed := tidy(f.Teams)
	if colour && f.Colour(reserved) {
		changed = true
	}
	if !legacy && !changed && !stale {
		return f, nil
	}
	// Write the repair under the lock, reading afresh so a writer that got
	// in between is not undone. A busy lock leaves the repair for next time.
	var fresh *File
	err = withLock(profileDir, 0, func() error {
		g, gLegacy, _, err := read(profileDir)
		if err != nil || g == nil {
			return err
		}
		tidy(g.Teams)
		if colour {
			g.Colour(reserved)
		}
		if err := write(profileDir, g.Teams); err != nil {
			return err
		}
		if gLegacy {
			_ = os.Rename(legacyPath(profileDir), legacyPath(profileDir)+".migrated")
		}
		fresh = g
		return nil
	})
	if err == nil && fresh != nil {
		return fresh, nil
	}
	return f, nil
}

// read is the file as it is on disk, with no repair: nil for no file at all,
// legacy when it came from spaces.json, stale when it is an older version.
func read(profileDir string) (f *File, legacy, stale bool, err error) {
	raw, err := os.ReadFile(Path(profileDir))
	if errors.Is(err, os.ErrNotExist) {
		raw, err = os.ReadFile(legacyPath(profileDir))
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, false, nil
		}
		legacy = true
	}
	if err != nil {
		return nil, false, false, err
	}
	var d disk
	if err := json.Unmarshal(raw, &d); err != nil {
		name := FileName
		if legacy {
			name = LegacyFileName
		}
		return nil, false, false, fmt.Errorf("teams: %s is unreadable: %w", name, err)
	}
	teams := d.Teams
	if legacy || (d.Version < Version && len(teams) == 0) {
		teams = d.Legacy
	}
	return &File{Version: Version, Teams: teams}, legacy, !legacy && d.Version < Version, nil
}

// Save writes teams as the whole file, under the lock. It puts the list in
// order first, in place (ids, parents, handles, the manager), so the caller's
// memory and the disk agree afterwards.
//
// IT REPLACES WHATEVER IS ON DISK. A caller that loaded the file a while ago
// and saves its copy undoes any change another process made in between; a
// caller that shares the file should change it with [Update] instead.
func Save(profileDir string, teams []Team) error {
	tidy(teams)
	return withLock(profileDir, lockWait, func() error { return write(profileDir, teams) })
}

// Update is the one read-modify-write: under the exclusive lock it loads the
// file fresh (repaired, and migrated if it has not been), hands it to fn, and
// saves what fn left. An error from fn writes nothing and is returned. An
// unreadable file is returned as its error and fn is not called. A file that
// did not exist and that fn left with no teams is not created.
//
// Teams stay as coloured as they were; a team fn adds without [Team.SetHue]
// is coloured by the next [LoadHued].
func Update(profileDir string, fn func(*File) error) error {
	return withLock(profileDir, lockWait, func() error {
		_, err := updateLocked(profileDir, fn)
		return err
	})
}

// updateLocked is [Update]'s body, for a caller that holds the lock. It
// answers the file as fn left it, tidied, which is what was written; a missing
// file that fn left with no teams is answered empty and is not created.
func updateLocked(profileDir string, fn func(*File) error) (*File, error) {
	f, legacy, _, err := read(profileDir)
	if err != nil {
		return nil, err
	}
	missing := f == nil
	if missing {
		f = &File{Version: Version}
	}
	tidy(f.Teams)
	if err := fn(f); err != nil {
		return nil, err
	}
	if missing && len(f.Teams) == 0 {
		return f, nil
	}
	tidy(f.Teams)
	if err := write(profileDir, f.Teams); err != nil {
		return nil, err
	}
	if legacy {
		_ = os.Rename(legacyPath(profileDir), legacyPath(profileDir)+".migrated")
	}
	return f, nil
}

// SetAside moves an unreadable teams file out of the way, to
// <name>.unreadable-<nanos> beside it, so that starting again does not
// overwrite a file that may hold every team a person made. It moves teams.json
// when there is one and spaces.json otherwise, and returns the new path.
func SetAside(profileDir string) (string, error) {
	path := Path(profileDir)
	if _, err := os.Stat(path); err != nil {
		path = legacyPath(profileDir)
	}
	aside := path + ".unreadable-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	return aside, os.Rename(path, aside)
}

// write puts the teams on disk all at once or not at all: the bytes go to a
// temporary file beside the real one and are renamed over it, so a crash
// mid-write leaves the previous file whole. The caller holds the lock.
func write(profileDir string, teams []Team) error {
	path := Path(profileDir)
	dir := filepath.Dir(path)
	if teams == nil {
		teams = []Team{}
	}
	raw, err := json.MarshalIndent(disk{Version: Version, Teams: teams}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, FileName+".writing-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	if _, err := temp.Write(raw); err != nil {
		_ = temp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	// The time moves past the file this replaces, so its stamp moves
	// (stamp.go); the caller holds the lock, so no other write is between.
	before := modTime(path)
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	advance(path, before)
	return nil
}

// ── THE LOCK ────────────────────────────────────────────────────────────────

// withLock runs fn holding the exclusive lock on the teams file, waiting up to
// wait for it (0 is one try).
//
// A LOCK THAT CANNOT BE TAKEN IS NOT A REASON TO REFUSE A WRITE. A read-only
// directory or a filesystem with no advisory locking: fn runs unlocked, which
// is what a lone process has always done. A lock that is BUSY past the wait is
// the other answer, [ErrBusy], and fn does not run. The wait is non-blocking
// tries with backoff, never a blocking lock, so no caller can hang on a writer
// that never lets go.
func withLock(profileDir string, wait time.Duration, fn func() error) error {
	return lockedAt(lockPath(profileDir), wait, fn)
}

func lockedAt(path string, wait time.Duration, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fn()
	}
	gate, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fn()
	}
	defer gate.Close()
	held, err := take(gate, wait)
	if err != nil {
		return err
	}
	if held {
		defer filelock.Unlock(gate)
	}
	return fn()
}

// take tries for the lock until wait runs out. It answers held, not held with
// no error when the filesystem does not lock, or [ErrBusy].
func take(gate *os.File, wait time.Duration) (bool, error) {
	deadline := time.Now().Add(wait)
	for pause := 2 * time.Millisecond; ; pause = min(pause*2, 50*time.Millisecond) {
		err := filelock.Lock(gate, true, true)
		if err == nil {
			return true, nil
		}
		if !filelock.IsBusy(err) {
			return false, nil
		}
		if !time.Now().Add(pause).Before(deadline) {
			return false, ErrBusy
		}
		time.Sleep(pause)
	}
}
