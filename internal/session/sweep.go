// sweep.go is the idle reaper (docs/CHAT-V3.md, Decision 26): one pass over the
// session folders, once per launch, that expires droppings and removes litter.
//
// IT HAS EXACTLY THREE RULES, and the third one outranks the other two.
//
//  1. DROPPINGS EXPIRE. A file under a session's logs/ that nothing has touched
//     for a week is a job log, a stub or a frame nobody is going to read. It is
//     re-creatable by definition — that is what makes logs/ logs/ — so it goes.
//
//  2. LITTER IS REAPED. A session whose recorded workspace or launch directory
//     was under a temp directory is a conversation opened in a place the machine
//     itself considers disposable, and once a week has passed since the person
//     last said anything to it, the whole folder goes. This is the rule that
//     stops the flat layout's failure repeating: nineteen dead /tmp workspace
//     directories on the author's own machine, none of them wanted, none of them
//     removable without reading each one.
//
//  3. NOTHING ELSE IS EVER TOUCHED. transcript.jsonl is FOREVER, work/ is a
//     person's own content, and a session whose transcript is flocked is a live
//     conversation in another window. A sweep that could delete a real
//     transcript would make every one of them provisional, and the whole value
//     of a journal a person can cat, grep and rsync is that it is not.
//
// The two removals are DIFFERENT ACTS and the third rule reads differently
// against each. Rule 1 reaches into logs/ and nowhere else, so no expiry can
// ever reach a transcript or a person's work/ whatever its age. Rule 2 removes a
// whole folder — its work/ and its transcript with it — and is allowed to only
// because of what it proved first: a temp-rooted session is the disposable kind
// by construction (Decision 26: "ephemeral" is not a mode a person picks, it is
// what an owned session in a temp directory already is), and a week has passed
// since the person last spoke to it. Everything that fails either half of that
// proof keeps its transcript forever.
//
// The pass is a pure function of a directory and a clock so that it can be
// proved rather than argued about: [SweepPlaces] takes both, and every failure
// is one line to the caller's note and a move to the next folder. A sweep that
// stopped on the first unreadable directory would be a sweep that never reached
// the litter.
package session

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

const (
	// sweepTTL is how long a dropping lives and how long a temp-rooted session
	// sits idle before it is litter. A week is the span Decision 26 names, and
	// it is one number for both because they are one judgement: nothing here has
	// been wanted for longer than a person's working memory of it.
	sweepTTL = 7 * 24 * time.Hour

	// placesDirName is where the session folders live under the state root:
	// v3/projects/<encoded-workspace>/<session-id>/ — the same word
	// cmd/aforge's layout spells when it creates a bucket (chatv3_layout.go).
	// The sweep needs only the root and never the encoder.
	placesDirName = "projects"
)

// SweepHome runs one pass over this machine's session folders. It is the launch
// door's call ([SweepPlaces] is the testable one underneath it).
func SweepHome(note func(string)) {
	SweepPlaces(home.Join("v3", placesDirName), time.Now(), note)
}

// SweepPlaces applies the three rules over one projects root.
//
// A root that is not there is not a failure — it is a machine that has not held
// a conversation yet — and answers silently.
func SweepPlaces(root string, now time.Time, note func(string)) {
	if note == nil {
		note = func(string) {}
	}
	buckets, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		note(fmt.Sprintf("sweep: could not read %s: %v", root, err))
		return
	}
	for _, bucket := range buckets {
		if !bucket.IsDir() {
			continue
		}
		path := filepath.Join(root, bucket.Name())
		sessions, err := os.ReadDir(path)
		if err != nil {
			note(fmt.Sprintf("sweep: could not read %s: %v", path, err))
			continue
		}
		for _, entry := range sessions {
			if !entry.IsDir() {
				continue
			}
			sweepSession(filepath.Join(path, entry.Name()), now, note)
		}
	}
}

// sweepSession is the three rules over one session folder, in the order that
// makes rule 3 unconditional: the live check first, and nothing at all happens
// to a folder that fails it.
func sweepSession(dir string, now time.Time, note func(string)) {
	if sessionIsOpen(dir) {
		// Somebody is talking to it. Not its logs, not its folder, nothing.
		return
	}
	sweepLogs(dir, now, note)
	meta, err := LoadMeta(dir)
	if err != nil {
		note(fmt.Sprintf("sweep: could not read the identity of %s: %v", dir, err))
		return
	}
	// A SESSION THAT CANNOT SAY WHAT IT IS, STAYS. LoadMeta answers a zero Meta
	// for a file that is missing or will not parse, and a missing identity is
	// exactly the case where deleting would be a guess. The rule reaps what is
	// PROVABLY litter and leaves everything else alone forever.
	if strings.TrimSpace(meta.ID) == "" || !sweepIsLitter(meta, dir, now) {
		return
	}
	reapSession(dir, meta, note)
}

// sessionIsOpen reports whether another aforge holds this session's transcript.
//
// It asks the way sessionfile.go's own claim asks — a non-blocking exclusive
// flock, dropped the instant it is taken — because a held flock IS a live
// writer: the kernel releases it when the holder dies however it dies, so there
// is no staleness to second-guess and no pid file to go wrong. Taking it here
// costs the live session nothing, since the lock rides our own open file
// description and closing it releases only ours.
//
// EVERY UNCERTAINTY ANSWERS "OPEN". A transcript that cannot be opened, a
// filesystem that does not do locks, a directory this process may not read: each
// of them is a reason to leave the folder alone, because the sweep's whole
// licence is that it only removes what it is sure about.
func sessionIsOpen(dir string) bool {
	path := filepath.Join(dir, placeTranscript)
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		// No transcript at all: a folder that never held a conversation, and
		// there is nothing for anybody to be holding.
		return false
	}
	if err != nil {
		return true
	}
	defer file.Close()
	err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err != nil {
		// Held by another window, or a filesystem with no locks to offer. Both
		// answer "leave it".
		return true
	}
	_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
	return false
}

// sweepLogs expires the droppings and NOTHING ELSE: it walks logs/ and only
// logs/, removes files past the TTL, and leaves every directory standing. A
// directory removed here would be one a live job's next line could not recreate.
func sweepLogs(dir string, now time.Time, note func(string)) {
	logs := filepath.Join(dir, placeLogs)
	cutoff := now.Add(-sweepTTL)
	err := filepath.WalkDir(logs, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			return nil
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			note(fmt.Sprintf("sweep: could not expire %s: %v", path, err))
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		note(fmt.Sprintf("sweep: could not read %s: %v", logs, err))
	}
}

// sweepIsLitter decides whether a session is the disposable kind, and it takes
// BOTH halves: the place says the conversation was never meant to be kept, and
// the clock says nobody has come back to it.
//
// The idle stamp is [Meta.LastUserAt] — when the PERSON last said something —
// and the folder's own mtime only when there is no such stamp. That order is
// v2's resume law (internal/store/session_rooms.go): a background write touching
// a file is not a person returning to a conversation, and a sweep that read
// mtime first would keep a dead session alive forever because something wrote a
// log line into it.
func sweepIsLitter(meta Meta, dir string, now time.Time) bool {
	if !underTempDir(meta.Workspace) && !underTempDir(meta.LaunchDir) {
		return false
	}
	idle := meta.LastUserAt
	if idle.IsZero() {
		info, err := os.Stat(dir)
		if err != nil {
			return false
		}
		idle = info.ModTime()
	}
	return idle.Before(now.Add(-sweepTTL))
}

// underTempDir reports whether a path sits inside a directory the machine
// itself considers disposable. Both the configured temp directory and /tmp are
// asked, because TMPDIR moves the first without making the second any less of a
// temp directory to the person who typed it.
func underTempDir(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	path = filepath.Clean(path)
	for _, temp := range []string{os.TempDir(), "/tmp"} {
		temp = filepath.Clean(strings.TrimSpace(temp))
		if temp == "" || temp == string(filepath.Separator) {
			continue
		}
		if path == temp || strings.HasPrefix(path, temp+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// reapSession removes one litter session, GIT FIRST.
//
// A worktree is two things: a directory, and a registration in the repository
// it was cut from. Removing the directory alone leaves the person's repository
// holding a registration for a path that is gone — `git worktree list` names it,
// `git worktree add` refuses to reuse it, and the mess is in THEIR repository
// rather than ours. So every entry under trees/ is unregistered against the
// recorded workspace before the folder goes, and the prune sweeps up whatever
// the removes could not name.
//
// It is best-effort by design and the outcome is never checked: the workspace
// may have moved, been deleted, or stopped being a repository since the session
// last ran, and none of those is a reason to leave the litter standing. When the
// workspace is gone there is nothing holding a registration either, which is why
// the git commands are skipped entirely rather than run into an error.
func reapSession(dir string, meta Meta, note func(string)) {
	if root, ok := repositoryRoot(meta.Workspace); ok {
		trees, err := os.ReadDir(filepath.Join(dir, placeTrees))
		if err == nil && len(trees) > 0 {
			release := lockGitRoot(Place{Dir: dir}, root)
			for _, tree := range trees {
				if !tree.IsDir() {
					continue
				}
				_, _ = git(root, "worktree", "remove", "--force", filepath.Join(dir, placeTrees, tree.Name()))
			}
			_, _ = git(root, "worktree", "prune")
			release()
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		note(fmt.Sprintf("sweep: could not remove %s: %v", dir, err))
	}
}
