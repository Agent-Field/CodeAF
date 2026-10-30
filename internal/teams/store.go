package teams

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/filelock"
)

// The store's name inside the profile directory. teams.json, the file it
// replaced, is never read: a profile that has only that has no teams.
const FileName = "teams.db"

// lockWait is how long a write waits for another writer before it gives up
// with [ErrBusy]. A write holds the lock for one small read and the rows it
// changed, so anything near this long is a writer that is stuck, and waiting
// on it for good would freeze whoever called.
const lockWait = 2 * time.Second

// ErrBusy is another process holding the teams store's lock for longer than a
// write should take. Nothing was written.
var ErrBusy = errors.New("teams: the store is being written by somebody else; try again")

// Path is where the teams live. An empty profile directory is the ordinary
// launch and resolves to this process's own profile ([config.ProfilePath]).
func Path(profileDir string) string { return config.ProfilePath(profileDir, FileName) }

func lockPath(profileDir string) string { return Path(profileDir) + ".lock" }

// Load reads the teams from profileDir and puts them in order ([tidy]). It
// takes no lock and does not wait: a repair it had to make is written back
// only when the lock is free at that moment, and is otherwise made again on
// the next load. Teams the store left without a colour stay without one; the
// interface, which knows the palette, uses [LoadHued].
//
// A missing store is an empty File and no error. A store that is there but
// unreadable is an error ([ErrUnreadable]) and is left exactly as it was.
// Members come without their briefs ([WithBriefs]).
func Load(profileDir string) (*File, error) { return load(profileDir, false, nil) }

// LoadHued is [Load] that also gives every uncoloured team a colour around
// the reserved hues ([File.Colour]) and writes that back as a repair.
func LoadHued(profileDir string, reserved []float64) (*File, error) {
	return load(profileDir, true, reserved)
}

func load(profileDir string, colour bool, reserved []float64) (*File, error) {
	f, err := read(profileDir)
	if err != nil || f == nil {
		return &File{Version: Version, localIdentities: true}, err
	}
	changed := tidy(f.Teams)
	if colour && f.Colour(reserved) {
		changed = true
	}
	if !changed {
		return f, nil
	}
	// Write the repair under the lock, reading afresh so a writer that got
	// in between is not undone. A busy lock leaves the repair for next time.
	var fresh *File
	err = withLock(profileDir, 0, func() error {
		s, err := readSnapshot(profileDir)
		if err != nil || s == nil {
			return err
		}
		g := s.file()
		tidy(g.Teams)
		if colour {
			g.Colour(reserved)
		}
		if _, err := writeTeams(profileDir, s, g.Teams); err != nil {
			return err
		}
		fresh = g
		return nil
	})
	if err == nil && fresh != nil {
		return fresh, nil
	}
	return f, nil
}

// readSnapshot is the store as it stands, nil for no store at all.
func readSnapshot(profileDir string) (*snapshot, error) {
	return readPath(context.Background(), Path(profileDir))
}

// read is the store as it is, with no repair: nil for no store at all.
func read(profileDir string) (*File, error) {
	s, err := readSnapshot(profileDir)
	if err != nil || s == nil {
		return nil, err
	}
	return s.file(), nil
}

// Save writes teams as the whole list, under the lock. It puts the list in
// order first, in place (ids, parents, handles, the manager), so the caller's
// memory and the store agree afterwards. Only the rows that differ from the
// store are written.
//
// IT REPLACES WHATEVER THE STORE HOLDS. A caller that loaded the teams a while
// ago and saves its copy undoes any change another process made in between; a
// caller that shares the store should change it with [Update] instead.
func Save(profileDir string, teams []Team) error {
	tidy(teams)
	return withLock(profileDir, lockWait, func() error {
		s, err := readSnapshot(profileDir)
		if err != nil {
			return err
		}
		before := &File{Version: Version, localIdentities: true}
		if s != nil {
			before = s.file()
		}
		next := &File{Version: Version, Teams: teams, localIdentities: true}
		if err := next.checkIntroducedConversations(before); err != nil {
			return err
		}
		if err := next.CheckManagementChange(before); err != nil {
			return err
		}
		_, err = writeTeams(profileDir, s, teams)
		return err
	})
}

// Update is the one read-modify-write: under the exclusive lock it loads the
// store fresh (repaired), hands it to fn, and saves what fn left, writing only
// the rows that changed. An error from fn writes nothing and is returned. An
// unreadable store is returned as its error and fn is not called. A store that
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
// answers the teams as fn left them, tidied, which is what was written; a
// missing store that fn left with no teams is answered empty and is not
// created.
func updateLocked(profileDir string, fn func(*File) error) (*File, error) {
	s, err := readSnapshot(profileDir)
	if err != nil {
		return nil, err
	}
	missing := s == nil
	f := &File{Version: Version, localIdentities: true}
	if !missing {
		f = s.file()
	}
	tidy(f.Teams)
	previous := f.ManagementSnapshot()
	if err := fn(f); err != nil {
		return nil, err
	}
	if missing && len(f.Teams) == 0 {
		return f, nil
	}
	tidy(f.Teams)
	if err := f.checkIntroducedConversations(previous); err != nil {
		return nil, err
	}
	if err := f.CheckManagementChange(previous); err != nil {
		return nil, err
	}
	if _, err := writeTeams(profileDir, s, f.Teams); err != nil {
		return nil, err
	}
	return f, nil
}

// UpdateOpen is [Update] over the open teams only, which is what almost every
// write changes: fn is handed the open teams, and a closed team is neither
// read nor written. A team fn closes stays in the list, closed, and is
// written; a team it takes out of the list is deleted; one it adds is added.
// What it costs is what the open teams hold, however many have closed.
//
// A write that acts on a closed team (reopening, archiving or deleting one)
// uses [Update], which hands fn every team.
//
// A MEMBER IN A CLOSED TEAM TOO IS WRITTEN OVER EVERY TEAM. Whether a
// conversation is independent is written on each of its memberships, closed
// teams' too, and read from any of them ([File.AddMember], [File.RemoveMember],
// [File.Disband]). A write that adds or removes a member, or changes its home
// or independence, where that conversation also sits in a closed team, is
// made again as [Update] would make it, under the same lock: fn is then
// handed every team, so it must not count on seeing only the open ones.
func UpdateOpen(profileDir string, fn func(*File) error) error {
	return withLock(profileDir, lockWait, func() error {
		_, err := updateOpenLocked(profileDir, fn)
		return err
	})
}

// updateOpenLocked is [UpdateOpen]'s body, for a caller that holds the lock.
func updateOpenLocked(profileDir string, fn func(*File) error) (*File, error) {
	s, err := readOpenPath(context.Background(), Path(profileDir))
	if err != nil {
		return nil, err
	}
	missing := s == nil
	f := &File{Version: Version, localIdentities: true}
	var elsewhere map[string]bool
	if !missing {
		f, elsewhere = s.file(), s.elsewhere
	}
	tidyWithin(f.Teams, elsewhere)
	previous := f.ManagementSnapshot()
	marks := markMembers(f.Teams)
	if err := fn(f); err != nil {
		return nil, err
	}
	if missing && len(f.Teams) == 0 {
		return f, nil
	}
	tidyWithin(f.Teams, elsewhere)
	if keys, convs := marks.changed(f.Teams); !missing && len(keys) > 0 {
		closed, err := inClosedTeams(profileDir, keys, convs)
		if err != nil {
			return nil, err
		}
		if closed {
			return updateLocked(profileDir, fn)
		}
	}
	if err := f.checkIntroducedConversations(previous); err != nil {
		return nil, err
	}
	if err := f.CheckManagementChange(previous); err != nil {
		return nil, err
	}
	if _, err := writeScoped(profileDir, s, f.Teams, true); err != nil {
		return nil, err
	}
	return f, nil
}

// memberMark is one membership's place in a list: its home and independence
// flags, and the conversation it names ([memberConv]).
type memberMark struct {
	home, independent bool
	conv              string
}

// memberMarks is every membership in teams by team and key, for a write to
// find, after fn, the memberships it changed.
type memberMarks map[[2]string]memberMark

func markMembers(teams []Team) memberMarks {
	out := memberMarks{}
	for _, t := range teams {
		for _, m := range t.Members {
			out[[2]string{t.ID, m.Key}] = memberMark{m.Home, m.Independent, memberConv(m)}
		}
	}
	return out
}

// changed is every key whose memberships teams added, removed, or gave
// another home or independence flag since the marks, and the conversations
// those memberships name.
func (before memberMarks) changed(teams []Team) (keys, convs []string) {
	seen := map[string]bool{}
	note := func(key, conv string) {
		if !seen[key] {
			seen[key] = true
			keys = append(keys, key)
			convs = append(convs, memberConv(Member{Key: key}))
		}
		convs = append(convs, conv)
	}
	after := markMembers(teams)
	for at, m := range after {
		if old, ok := before[at]; !ok || old.home != m.home || old.independent != m.independent {
			note(at[1], m.conv)
		}
	}
	for at, m := range before {
		if _, ok := after[at]; !ok {
			note(at[1], m.conv)
		}
	}
	return keys, convs
}

// Snapshot reads references without repairs, locking or writes. Launch
// discovery must not change a profile merely to inspect conversations.
func Snapshot(profileDir string) (*File, error) {
	f, err := read(profileDir)
	if f == nil {
		f = &File{Version: Version, localIdentities: true}
	}
	return f, err
}

// Inspect reads a fresh snapshot under the writer's lock without saving it.
// Cleanup uses this door so a team assignment cannot arrive during removal.
func Inspect(profileDir string, fn func(*File) error) error {
	return strictlyLockedAt(lockPath(profileDir), lockWait, func() error {
		f, err := read(profileDir)
		if err != nil {
			return err
		}
		if f == nil {
			f = &File{Version: Version, localIdentities: true}
		}
		return fn(f)
	})
}

// SetAside moves a teams.db that is not this store's (bytes that are not a
// database, or another program's database) out of the way, to
// <name>.unreadable-<nanos> beside it with its WAL and shared-memory files, so
// that starting again does not write over a file that may hold every team a
// person made. ANY OTHER STORE IS NOT MOVED, and the answer says why: one that
// reads (the error that sent the caller here was something else, a busy
// peer), a newer build's ([ErrNewer]), which that build still reads, a damaged
// store of this one's own, and one that could not be reached for the moment.
//
// It holds the teams lock, so no writer comes between its look and its
// renames, and moves the WAL before the store: a writer that found teams.db
// gone would take a WAL left behind for a dead store's and remove it.
func SetAside(profileDir string) (string, error) {
	path := Path(profileDir)
	aside := ""
	err := withLock(profileDir, lockWait, func() error {
		forget(path)
		_, err := readPath(context.Background(), path)
		var not *notOursError
		if !errors.As(err, &not) {
			if err == nil {
				err = errors.New("teams: the store reads; nothing was set aside")
			}
			return err
		}
		forget(path)
		to := path + ".unreadable-" + strconv.FormatInt(time.Now().UnixNano(), 10)
		var moved []string
		undo := func() {
			for _, suffix := range moved {
				_ = os.Rename(to+suffix, path+suffix)
			}
		}
		for _, suffix := range []string{"-wal", "-shm"} {
			if _, err := os.Lstat(path + suffix); err != nil {
				continue
			}
			if err := os.Rename(path+suffix, to+suffix); err != nil {
				undo()
				return err
			}
			moved = append(moved, suffix)
		}
		if err := os.Rename(path, to); err != nil {
			undo()
			return err
		}
		aside = to
		return nil
	})
	return aside, err
}

// ── THE LOCK ────────────────────────────────────────────────────────────────

// withLock runs fn holding the exclusive lock on the teams store, waiting up to
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

// strictlyLockedAt never authorizes destructive cleanup without an exclusive
// lock. Ordinary writes retain their older filesystem fallback independently.
func strictlyLockedAt(path string, wait time.Duration, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	gate, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer gate.Close()
	held, err := take(gate, wait)
	if err != nil {
		return err
	}
	if !held {
		return fmt.Errorf("teams: cleanup requires an exclusive lock")
	}
	defer filelock.Unlock(gate)
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
