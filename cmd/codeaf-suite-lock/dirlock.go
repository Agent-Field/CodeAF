package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
func dirLockPath() string { return strings.TrimSpace(os.Getenv(dirLockEnv)) }

// takeDirLock claims the directory lock by creating it, which is the whole
// mechanism: mkdir either makes the directory or says somebody else already
// did, in one syscall, with no window between the asking and the taking.
//
// It reports whether it was taken and, when it was not, whatever pid the
// existing lock names, SO THE REFUSAL CAN SAY WHO. A lock file left by an older
// build may hold nothing readable; an empty answer here means the directory is
// held and its holder did not say who it was, which is still a refusal.
func takeDirLock(path string) (taken bool, held string, err error) {
	if path == "" {
		return false, "", nil
	}
	if mkErr := os.Mkdir(path, 0o700); mkErr != nil {
		if errors.Is(mkErr, fs.ErrExist) {
			raw, _ := os.ReadFile(filepath.Join(path, "pid"))
			return false, strings.TrimSpace(string(raw)), nil
		}
		return false, "", mkErr
	}
	return true, "", nil
}

// nameDirLockHolder writes the suite's pid where the old readers look for it.
// It is BEST EFFORT and deliberately not an error: the lock is the directory,
// not the file inside it, so a run that cannot write the pid still holds the
// box correctly and only costs a future refusal the name of who it waited for.
func nameDirLockHolder(path string, suite int) {
	if path == "" {
		return
	}
	_ = os.WriteFile(filepath.Join(path, "pid"), []byte(strconv.Itoa(suite)+"\n"), 0o600)
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
func dropDirLock(path string) {
	if path == "" {
		return
	}
	_ = os.Remove(filepath.Join(path, "pid"))
	_ = os.Remove(path)
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
