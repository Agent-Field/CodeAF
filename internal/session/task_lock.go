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
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

const (
	// gitRootLockName is the file every aforge on this machine flocks before it
	// touches the root repository. It sits with the task worktrees rather than
	// inside .git, because a repository's toplevel can be a linked worktree whose
	// .git is a FILE, and because everything else this harness leaves in a
	// person's repository is already under this one directory.
	gitRootLockName = ".gitroot.lock"

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
// returns the release. Use it as `defer lockGitRoot(root)()`.
//
// GOING AHEAD IS ALWAYS BETTER THAN REFUSING. A filesystem that cannot flock at
// all, a lock file that cannot be created under a read-only checkout, and a
// holder that outlasts our patience are all answered the same way: the work
// proceeds unserialized, which is exactly what this code did before the lock
// existed. The downside of proceeding is git's own index lock reporting a clean
// merge as a conflict — which keeps the branch and loses nothing — and the
// downside of refusing would be a finished node with nowhere to put its work.
func lockGitRoot(root string) func() {
	gitRoot.Lock()
	file := claimGitRoot(root)
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
func claimGitRoot(root string) *os.File {
	directory := filepath.Join(root, filepath.FromSlash(tasksDirName))
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil
	}
	file, err := os.OpenFile(filepath.Join(directory, gitRootLockName), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
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

// isLockHeld says whether the error means somebody else holds the lock, as
// against the filesystem not doing locks at all. EAGAIN on Linux and
// EWOULDBLOCK on darwin are the same value and the one answer that means held.
func isLockHeld(err error) bool {
	return err == unix.EWOULDBLOCK || err == unix.EAGAIN
}
