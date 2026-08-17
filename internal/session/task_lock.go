package session

// The lock over one repository's shared git state.
//
// A worktree add, a merge, a worktree remove and a branch delete all write the
// ROOT repository's index and its refs, and git guards those with .git/index.lock
// — a file it creates, refuses to wait on, and reports as "another git process
// seems to be running". That report is indistinguishable, from where comeHome
// stands, from a real conflict: the merge fails, the branch is kept, and the
// person is told their finished task did not land cleanly when the only thing
// that happened was that two of them tried at the same instant.
//
// A process-local mutex closed that hole for two nodes in one session and left
// it wide open for the shape people actually work in: two aforge windows on one
// repository, each running tasks. So the serialization is a FILE lock now, and
// the mutex in front of it is only the cheap first gate for this process's own
// goroutines.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

const (
	// gitRootLockName is the file every aforge on this machine flocks before it
	// touches the root repository, in the LEGACY layout. It sits with the task
	// worktrees rather than inside .git, because a repository's toplevel can be a
	// linked worktree whose .git is a FILE, and because everything else that
	// layout left in a person's repository was already under this one directory.
	gitRootLockName = ".gitroot.lock"

	// gitRootLockDir is where the lock lives once a session keeps its things in a
	// folder of its own (Decision 26): under the state root, so that NOTHING OF
	// OURS LIVES IN THE PERSON'S FOLDER. It goes through internal/home, so
	// AFORGE_HOME moves it with everything else.
	gitRootLockDir = "locks"

	// gitRootLockStem is how much of the repository path's digest names the lock
	// file. Sixteen hex characters is sixty-four bits: far past the point where
	// two of a person's repositories collide, and short enough that the directory
	// listing is readable. The digest and not the path itself, because a lock file
	// named after a path would need every separator escaped and would still be
	// unopenable at the length some checkouts reach.
	gitRootLockStem = 16

	// gitRootPoll is how often a waiter re-asks for the lock, and gitRootPatience
	// is how long it asks for before it goes ahead anyway.
	//
	// The wait is a poll rather than a blocking flock because a blocking flock
	// cannot be given a deadline: it parks the OS thread until the holder lets
	// go, and a holder that has wedged would take this node with it for the rest
	// of the hour its own leash allows. Fifty milliseconds is far below the cost
	// of the git command on the other side of the lock, and two minutes is longer
	// than any merge and shorter than a person's patience with a task that has
	// stopped saying anything.
	gitRootPoll     = 50 * time.Millisecond
	gitRootPatience = 2 * time.Minute
)

// lockGitRoot claims the root repository for one span of git commands and
// returns the release. Use it as `defer lockGitRoot(place, root)()`.
//
// THE LOCK IS THE REPOSITORY'S, NOT THE SESSION'S: place decides only WHERE the
// file sits, and the identity it is keyed on is the repository root every window
// on that repository resolves to the same string.
//
// GOING AHEAD IS ALWAYS BETTER THAN REFUSING. A filesystem that cannot flock at
// all, a lock file that cannot be created under a read-only checkout, and a
// holder that outlasts our patience are all answered the same way: the work
// proceeds unserialized, which is exactly what this code did before the lock
// existed. The downside of proceeding is git's own index lock reporting a clean
// merge as a conflict — which keeps the branch and loses nothing — and the
// downside of refusing would be a finished node with nowhere to put its work.
func lockGitRoot(place Place, root string) func() {
	gitRoot.Lock()
	file := claimGitRoot(place, root)
	return func() {
		if file != nil {
			// The unlock is belt-and-braces, as sessionfile.go's is: closing the
			// descriptor drops the flock on its own. It is stated here so a reader
			// can see the release at the place it happens.
			_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
			_ = file.Close()
		}
		gitRoot.Unlock()
	}
}

// claimGitRoot takes the cross-process half of the lock, and returns nil when it
// could not be taken for any reason at all.
//
// flock is the right primitive for the reason sessionfile.go gives: the kernel
// releases it when the holder dies, however it dies, so there is no stale lock
// to detect and nothing to clean up after a crashed aforge. What differs here is
// who loses. A second session opening the same transcript is a mistake to name
// and stop; a second session merging its own task is ordinary work that has to
// happen, just not at this instant — so the loser WAITS instead of being told
// no.
func claimGitRoot(place Place, root string) *os.File {
	file := openGitRootLock(place, root)
	if file == nil {
		return nil
	}
	deadline := time.Now().Add(gitRootPatience)
	for {
		err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return file
		}
		if !isLockHeld(err) {
			// A filesystem that answers EINVAL or ENOLCK does not do locks; there
			// is nothing to wait for and nothing to come back for.
			_ = file.Close()
			return nil
		}
		if time.Now().After(deadline) {
			_ = file.Close()
			return nil
		}
		time.Sleep(gitRootPoll)
	}
}

// openGitRootLock creates the lock file and hands back the open descriptor, or
// nil when there is nowhere to put one.
//
// A SESSION WITH A FOLDER LOCKS OUTSIDE THE PERSON'S REPOSITORY. The legacy
// layout kept the file with the worktrees under <repo>/.aforge-v3/, which is the
// litter Decision 26 removes; the folder layout keys the same lock on a digest
// of the repository root instead and keeps it under the state root. Both windows
// on one repository still meet on one file, because both derive it from the same
// resolved root — what they must not do is meet on one file in one layout and on
// two in the other, which is why the choice is made HERE and from the Place
// alone, and why the whole product moves layouts at once.
func openGitRootLock(place Place, root string) *os.File {
	directory, name := filepath.Join(root, filepath.FromSlash(tasksDirName)), gitRootLockName
	directoryMode, fileMode := os.FileMode(0o755), os.FileMode(0o644)
	if strings.TrimSpace(place.Dir) != "" {
		directory, name = home.Join("v3", gitRootLockDir), gitRootLockFile(root)
		directoryMode, fileMode = 0o700, 0o600
	}
	if err := os.MkdirAll(directory, directoryMode); err != nil {
		return nil
	}
	file, err := os.OpenFile(filepath.Join(directory, name), os.O_CREATE|os.O_RDWR, fileMode)
	if err != nil {
		return nil
	}
	return file
}

// gitRootLockFile names one repository's lock: the head of the SHA-256 of its
// resolved root path, and ".lock" so a person listing the directory can see what
// they are looking at.
func gitRootLockFile(root string) string {
	digest := sha256.Sum256([]byte(filepath.Clean(root)))
	return hex.EncodeToString(digest[:])[:gitRootLockStem] + ".lock"
}

// isLockHeld says whether the error means somebody else holds the lock, as
// against the filesystem not doing locks at all. EAGAIN on Linux and
// EWOULDBLOCK on darwin are the same value and the one answer that means held.
func isLockHeld(err error) bool {
	return errors.Is(err, unix.EWOULDBLOCK)
}
