package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/env"
)

// THE SECOND LOCK EXISTS BECAUSE THE FIRST ONE IS INVISIBLE TO HALF THE BOX.
//
// Before #1264 a heavy suite took a DIRECTORY lock, /tmp/codeaf-suite-<uid>.lock,
// because mkdir is atomic. Current trees take an flock on
// /tmp/codeaf-suite-<uid>.lockfile instead. Neither mechanism can see the other,
// so a checkout still sitting on an older commit runs a heavy suite beside one
// of ours and each believes it has the box to itself (#1307).
//
// Dual-checking on the READ side would only close half of that. It stops us
// starting beside a stale holder; it cannot stop a stale runner starting beside
// US, because a runner that has never heard of the flock is exactly what makes
// it stale and it will never be taught to read one. Taking BOTH locks here
// closes the other half without changing a single old checkout: a current tree
// becomes visible to every stale reader still out there, and the remaining
// blindness runs in the safe direction only.
//
// IT IS NOT DERIVED FROM THE FLOCK'S PATH. Stripping "file" off the end would
// make two names one fact, and a fact spelled twice is a fact that can disagree
// with itself; it would also be invisible to a test harness that drives private
// paths. The configuration says both names or neither, and an empty setting
// means this tree takes no directory lock at all, which is what a platform that
// never had one wants.
const dirLockEnv = "CODEAF_SUITE_DIRLOCK_PATH"

// dirLockPath is the directory lock this run should take, or empty for none.
func dirLockPath() string { return strings.TrimSpace(env.Get(dirLockEnv)) }

// suiteEnviron is the environment the SUITE runs in, which is this one without
// the directory lock's name in it.
//
// THE SUITE IS NOT PART OF THE LOCKING SCHEME AND MUST NOT INHERIT ITS
// IDENTITY. It already never sees the file lock's descriptor, for the reason
// main.go gives: anything the suite can inherit, the suite's children keep
// alive. The name is the same hazard one level up and it bites harder, because
// it is inherited silently and forever.
//
// Measured rather than reasoned: with the name exported, `make check` ran two
// of this package's own tests INSIDE a suite that already held the box, so the
// wrapper each test started was correctly refused by the gate's own directory
// lock, wrote nothing, and the test failed reading an empty pipe. A locking
// scheme that cannot be tested from inside a locked box is a locking scheme
// nobody can gate.
func suiteEnviron() []string { return env.EnvironWithout(dirLockEnv) }

// takeDirLock claims the directory lock by creating it, which is the whole
// mechanism: mkdir either makes the directory or says somebody else already
// did, in one syscall, with no window between the asking and the taking.
//
// It reports whether it was taken and, when it was not, whatever pid the
// existing lock names, SO THE REFUSAL CAN SAY WHO. A directory that is already
// there is not judged here: whether its holder is dead is asked only once this
// run holds the flock ([reclaimDirLock]). A lock file left by an older
// build may hold nothing readable; an empty answer here means the directory is
// held and its holder did not say who it was, which is still a refusal.
func takeDirLock(path string) (taken bool, held string, err error) {
	if path == "" {
		return false, "", nil
	}
	if mkErr := os.Mkdir(path, 0o700); mkErr != nil {
		if errors.Is(mkErr, fs.ErrExist) {
			return false, readDirLockHolder(path), nil
		}
		return false, "", mkErr
	}
	// NAMED THE INSTANT IT IS CLAIMED, and named again later with the holder's
	// pid once there is a holder. The second write is not this one being
	// repeated: it replaces a placeholder with the answer somebody actually
	// wants, and between the two there is a window this process can be killed in.
	//
	// A CASE YOU CANNOT CHEAPLY PREVENT MUST AT LEAST NOT BE INDISTINGUISHABLE
	// FROM ONE YOU HANDLED. A SIGKILL here leaks the directory and no deferred
	// cleanup can cover that, because SIGKILL runs no defers. What this write
	// buys is that the leak NAMES SOMEBODY: a reader finds a pid, asks whether
	// it is alive, and gets an answer. Without it the directory is held by
	// nobody, reads as a busy box rather than a broken one, and the first
	// diagnostic anyone reaches for comes back empty.
	nameDirLockHolder(path, os.Getpid())
	return true, "", nil
}

// nameDirLockHolder writes the holder's pid where the old readers look for it.
// It is BEST EFFORT and deliberately not an error: the lock is the directory,
// not the file inside it, so a run that cannot write the pid still holds the
// box correctly and only costs a future refusal the name of who it waited for.
//
// IT ALSO WRITES `since`, because an old reader's refusal prints that file
// beside the pid, and a refusal that says `started ?` about a live holder reads
// as a lock with nobody behind it.
func nameDirLockHolder(path string, holder int) {
	if path == "" {
		return
	}
	_ = os.WriteFile(filepath.Join(path, "pid"), []byte(strconv.Itoa(holder)+"\n"), 0o600)
	_ = os.WriteFile(filepath.Join(path, "since"), []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600)
}

// dropDirLock releases the directory lock, and it is called by WHOEVER OWNS THE
// FLOCK, never by the wrapper that started them.
//
// That is the whole of the design and it is not an implementation detail: the
// flock's lifetime is the holder's (main.go says why), and a directory lock
// released on a different schedule is worse than no directory lock at all. If
// the wrapper dropped it, killing the wrapper, which this design explicitly
// permits, would free the directory while a suite still ran and still held the
// flock. The two locks must go together or a stale reader is told the box is
// free while it is not.
//
// AND IT DROPS ONLY A DIRECTORY THAT STILL NAMES `owner` (#1324). The directory
// is a name on disk and not a descriptor, so the one who created it is not
// necessarily the one holding it now: an old checkout that judged it stale has
// moved it aside and made its own under the same name. Deleting that one's pid
// file and leaving its `since` behind was a directory that no rmdir could
// remove and that named nobody, which the next old reader took as free. A
// directory naming somebody else is somebody else's and is left alone.
func dropDirLock(path string, owner int) {
	if path == "" {
		return
	}
	if readDirLockHolder(path) != strconv.Itoa(owner) {
		return
	}
	_ = os.Remove(filepath.Join(path, "pid"))
	_ = os.Remove(filepath.Join(path, "since"))
	_ = os.Remove(path)
}

// ── A DEAD DIRECTORY LOCK IS TAKEN BACK, AND ONLY UNDER THE FLOCK ───────────
//
// A SIGKILL or an out-of-memory sweep of the holder frees the flock, because
// the kernel closes a dead process's descriptors, and leaves the directory,
// because nothing closes a name. Until #1324 nothing here asked whether the
// directory's holder was alive, so from then on every run on the box was
// refused naming a pid that no longer existed: a dead lock refused exactly the
// way a live one does, which is the one thing a lock may never do.
//
// THE FLOCK IS THE TRUTH AND THE DIRECTORY IS ITS SHADOW FOR OLD READERS. So
// the question is asked only by a run that already HOLDS the flock, which
// settles every current tree at once: a current holder would hold the flock,
// and this run does. What is left is whether an OLD checkout — one that takes
// only the directory — is running, and that is answered the way the old
// checkout answers it about itself: a pid that is alive and whose command line
// carries [legacyReaderMark]. Anything else — a pid that is gone, reused by
// something else, or never written — is a directory nobody holds.

// legacyReaderMark is the one string an old checkout's liveness check accepts
// in a holder's command line (`holder_alive` in one-suite.sh before #1264: the
// pid must be alive AND its command line must contain this).
//
// THE PID IN THE DIRECTORY MUST SATISFY IT, OR THE DIRECTORY IS INVISIBLE.
// It used to name the SUITE, whose command line is `go test …`, so an old tree
// read a live lock as stale, moved it aside and ran its suite beside ours
// (#1324), which is the collision the directory exists to prevent. The
// directory now names the HOLDER, whose lifetime is the lock's, and the holder
// carries this mark in its argv[0] (lock_unix.go's startHolder). The wrapper
// carries it too, because one-suite.sh execs it under the script's own name,
// and that covers the moment between the claim and the holder naming itself.
const legacyReaderMark = "one-suite.sh"

// unnamedGrace is how long a directory that names nobody is given to name
// somebody before it is judged dead. Both kinds of claimant write the pid in
// the instant after mkdir, so a directory still unnamed after this is a claim
// that died in that instant rather than one still being made.
const unnamedGrace = 500 * time.Millisecond

// dirLockHolderAlive reports whether the pid a directory lock names is an old
// checkout still holding it, by the old checkout's own test.
func dirLockHolderAlive(named string) bool {
	pid, err := strconv.Atoi(strings.TrimSpace(named))
	if err != nil || pid <= 0 || !pidVisibleHere(pid) {
		return false
	}
	return strings.Contains(commandLine(pid), legacyReaderMark)
}

// reclaimDirLock takes back a directory lock whose holder is dead. The caller
// MUST already hold the flock (see the section comment above).
//
// It reports whether the lock is now this run's and, when it is not, whoever
// the directory names, so the refusal can say who.
//
// THE DEAD DIRECTORY IS MOVED ASIDE, NEVER REMOVED IN PLACE, which is what the
// old script does for the same reason: an old reader may judge the same
// directory stale in the same instant, only one rename of it succeeds, and the
// loser can then never delete a lock the winner has just made.
func reclaimDirLock(path string) (taken bool, held string, err error) {
	if path == "" {
		return false, "", nil
	}
	named := readDirLockHolder(path)
	for waited := time.Duration(0); named == "" && waited < unnamedGrace; waited += 50 * time.Millisecond {
		time.Sleep(50 * time.Millisecond)
		named = readDirLockHolder(path)
	}
	if dirLockHolderAlive(named) {
		return false, named, nil
	}
	aside := path + ".stale." + strconv.Itoa(os.Getpid())
	if renameErr := os.Rename(path, aside); renameErr == nil {
		_ = os.RemoveAll(aside)
	} else if !errors.Is(renameErr, fs.ErrNotExist) {
		return false, named, renameErr
	}
	return takeDirLock(path)
}

// readDirLockHolder is the pid a directory lock names, empty when it names
// nobody or cannot be read.
func readDirLockHolder(path string) string {
	raw, _ := os.ReadFile(filepath.Join(path, "pid"))
	return strings.TrimSpace(string(raw))
}

// dirLockHolderName is what a refusal calls the holder when the directory lock
// names nobody. A lock left by an older build, or one whose pid file was never
// written, is STILL HELD, and printing an empty name there would read as a lock
// with no holder, which is the one thing it is not.
func dirLockHolderName(pid string) string {
	if pid == "" {
		return "unnamed"
	}
	return pid
}
