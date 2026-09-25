// codeaf-suite-lock holds the machine-wide heavy-suite lock while a command runs.
//
// The lock is an advisory file lock held on an open descriptor through
// internal/filelock, so it works on every platform the release ships
// (build-cross covers Windows too).
//
// THE LOCK'S LIFETIME IS THE SUITE'S. Not this wrapper's: a wrapper killed
// mid-run must not unlock a box that is still running a suite. And not any
// descendant's: an advisory lock lives on the open file description, so a
// descriptor handed to the suite is kept alive by every process the suite
// leaves behind, and a test's orphaned shell held this box for eight minutes
// past a green run on 2026-09-19. So the descriptor goes to a HOLDER beside the
// suite instead (lock_unix.go): a process of ours that execs nothing, watches
// the suite's pid, and exits when the suite does. The suite never sees the
// descriptor, and nothing can inherit it.
//
// If the holder is killed, the lock frees while the suite may still run. That
// is the failure we choose. The holder waits and does nothing else, so it dies
// only to a deliberate kill, an out-of-memory sweep or the box going down; it
// lives in its own session, so a signal aimed at the suite or at this wrapper
// misses it; the lock file names both pids so a free lock under a running suite
// can still be read back; and a lock nothing can release stops the box, where a
// lock freed early costs one concurrent suite.
//
// The lock file carries the suite's pid, when it started, and the holder's pid,
// only so a refused contender can name who holds it.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

// errNoHolder says this platform keeps the lock in the wrapper rather than
// handing it to a holder. Windows has no descriptor to hand over the way unix
// does, and it also has none of the inheritance the holder exists to avoid:
// nothing but this process holds the lock there, so the wrapper's own lifetime
// is the lock's and there is nothing to warn about.
var errNoHolder = errors.New("this platform holds the heavy-suite lock in the wrapper")

// holdFlag names the hold mode, which is this binary run as its own lock
// holder. It is not a user-facing flag: the wrapper passes it to itself.
const holdFlag = "--hold-heavy-suite-lock"

// holdPoll is how often the holder looks at the suite it is waiting for. The
// lock is held for the whole suite either way, so this is only the delay
// between the suite ending and the box opening: small enough not to be noticed,
// large enough that waiting costs nothing.
const holdPoll = 50 * time.Millisecond

func main() {
	os.Exit(dispatch(os.Args[1:]))
}

// dispatch is main's whole body, kept apart from os.Exit so a test that runs
// this binary as its own fixture reaches the hold mode as well as the wrapper.
func dispatch(args []string) int {
	if len(args) > 0 && args[0] == holdFlag {
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: codeaf-suite-lock "+holdFlag+" SUITEPID [STARTTOKEN]")
			return 2
		}
		suite, err := strconv.Atoi(args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "hold heavy-suite lock: suite pid %q: %v\n", args[1], err)
			return 2
		}
		token := ""
		if len(args) > 2 {
			token = args[2]
		}
		return holdLock(suite, token)
	}
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: codeaf-suite-lock LOCK COMMAND [ARG...]")
		return 2
	}
	return run(args[0], args[1:])
}

func run(path string, argv []string) int {
	// THE DIRECTORY LOCK IS CLAIMED FIRST, and the order is the point rather
	// than a preference. It is the only lock a checkout behind #1264 can see,
	// so a current tree that took the flock first would be invisible to a stale
	// runner for the width of that window, which is the exact collision this
	// closes (dirlock.go says why there are two). Only the REFUSAL is reported
	// later, after the flock has had its say, and the reason is beside it.
	dirPath := dirLockPath()
	dirTaken, dirHeldBy, dirErr := takeDirLock(dirPath)
	if dirErr != nil {
		fmt.Fprintf(os.Stderr, "take heavy-suite directory lock: %v\n", dirErr)
		return 2
	}
	// OWNERSHIP MOVES TO THE HOLDER AND THIS UNDOES ONLY WHAT IT STILL OWNS.
	// Every road out of this function before the holder exists must free the
	// directory, or a refused run would leave a lock nothing releases; after the
	// holder exists, freeing it here would unlock a box that is still running.
	dirOurs := dirTaken
	defer func() {
		if dirOurs {
			dropDirLock(dirPath, os.Getpid())
		}
	}()
	lock, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "take heavy-suite lock: %v\n", err)
		return 2
	}
	defer lock.Close()
	if err := filelock.Lock(lock, true, true); err != nil {
		if filelock.IsBusy(err) {
			if _, seekErr := lock.Seek(0, io.SeekStart); seekErr != nil {
				fmt.Fprintf(os.Stderr, "read heavy-suite lock: %v\n", seekErr)
				return 2
			}
			metadata, _ := io.ReadAll(lock)
			fields := strings.Fields(string(metadata))
			pid, since := "?", "?"
			if len(fields) > 0 {
				pid = fields[0]
			}
			if len(fields) > 1 {
				since = fields[1]
			}
			fmt.Fprintf(os.Stderr, "another heavy suite is already running on this box (pid %s, started %s).\n", pid, since)
			if held, convErr := strconv.Atoi(pid); convErr == nil && !pidVisibleHere(held) {
				// The pid in the lock file is meaningful only in the holder's own
				// pid namespace, so from here it may be a live suite in another
				// namespace or a holder that exited and left the lock to a process
				// it started (an inherited descriptor is not close-on-exec). Never
				// call a live suite gone: say the recorded holder is not visible and
				// name the file-level way to find whoever really holds it.
				fmt.Fprintf(os.Stderr, "pid %s is not visible from here: it may be running in another process namespace, or it may have exited and left the lock to a process it started.\n", pid)
				fmt.Fprintf(os.Stderr, "Find the real holder where every process is visible: lsof %s\n", path)
			} else {
				fmt.Fprintln(os.Stderr, "Wait for it, or run one named regression with make test-focus.")
			}
			return 1
		}
		fmt.Fprintf(os.Stderr, "take heavy-suite lock: %v\n", err)
		return 2
	}

	// THE FLOCK IS OURS AND THE DIRECTORY IS NOT, so the holder is a checkout
	// that cannot see the lock we just took. This is the case the file lock is
	// structurally unable to report: it read free and it was right to, and the
	// box is busy anyway.
	//
	// The refusal is reported HERE rather than where the directory was claimed,
	// although the claim comes first and deliberately so. Two CURRENT trees
	// collide on the flock, and the refusal above knows things this one cannot:
	// whether the recorded pid is visible from this namespace, and what to run
	// when it is not. Reporting the directory first would have replaced that
	// diagnosis with a cruder one for the common case, to describe a rarer one.
	//
	// AND ONLY A LIVE OLD CHECKOUT IS A HOLDER (#1324). This run holds the
	// flock, so no current tree is running; a directory whose pid is dead, or
	// is not an old checkout at all, is what a SIGKILLed or OOM-killed holder
	// leaves behind, and refusing on it locked the box for ever naming a pid
	// that no longer existed. It is taken back here (dirlock.go's
	// [reclaimDirLock]), and a live old checkout still turns this run away.
	if dirPath != "" && !dirTaken {
		reclaimed, heldNow, reclaimErr := reclaimDirLock(dirPath)
		if reclaimErr != nil {
			fmt.Fprintf(os.Stderr, "take back a dead heavy-suite directory lock: %v\n", reclaimErr)
			return 2
		}
		if reclaimed {
			dirTaken, dirOurs = true, true
		} else {
			dirHeldBy = heldNow
		}
	}
	if dirPath != "" && !dirTaken {
		fmt.Fprintf(os.Stderr, "another heavy suite is already running on this box (directory lock %s, pid %s).\n", dirPath, dirLockHolderName(dirHeldBy))
		fmt.Fprintln(os.Stderr, "That is the lock a checkout behind #1264 takes. Its holder cannot see the file lock this run uses, and the file lock read free, so waiting on that alone would have started a second heavy suite beside it.")
		fmt.Fprintf(os.Stderr, "Find the holder where every process is visible: cat %s/pid\n", dirPath)
		return 1
	}

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	// Without the directory lock's name, for the reason dirlock.go gives beside
	// [suiteEnviron]: the suite is not part of the locking scheme and anything
	// it inherits it passes on.
	cmd.Env = suiteEnviron()
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "start heavy suite: %v\n", err)
		return 2
	}

	// The holder takes the descriptor before anything is recorded, so the lock
	// is already the suite's by the time the file names it. A platform without a
	// holder (Windows) keeps it here, which is safe there because nothing else
	// can inherit it.
	var holder *os.Process
	holderPID := 0
	if started, err := startHolder(self(), path, lock, cmd.Process.Pid); err == nil {
		holder, holderPID = started, started.Pid
		// The holder inherited this run's environment, so it knows the directory
		// lock by the same name this process does and frees it when the suite
		// ends. From here the two locks share one lifetime, which is the whole
		// reason the holder and not this wrapper drops it.
		dirOurs = false
	} else if !errors.Is(err, errNoHolder) {
		fmt.Fprintf(os.Stderr, "hold heavy-suite lock beside the suite: %v\n", err)
		fmt.Fprintln(os.Stderr, "this wrapper is holding it instead, so killing this wrapper would unlock a box that is still running a suite.")
	}

	since := time.Now().UTC().Format(time.RFC3339)
	if err := lock.Truncate(0); err != nil {
		fmt.Fprintf(os.Stderr, "write heavy-suite lock: %v\n", err)
		return 2
	}
	if _, err := lock.Seek(0, io.SeekStart); err != nil {
		fmt.Fprintf(os.Stderr, "write heavy-suite lock: %v\n", err)
		return 2
	}
	if _, err := fmt.Fprintf(lock, "%d %s holder=%d\n", cmd.Process.Pid, since, holderPID); err != nil {
		fmt.Fprintf(os.Stderr, "write heavy-suite lock: %v\n", err)
		return 2
	}
	if err := lock.Sync(); err != nil {
		fmt.Fprintf(os.Stderr, "write heavy-suite lock: %v\n", err)
		return 2
	}
	// THE DIRECTORY IS NOT RENAMED HERE. The holder names itself in it the
	// moment it starts (lock_unix.go's holdLock), because the holder is the
	// process an old checkout must find alive and the one that drops it; this
	// wrapper naming the SUITE here is what made the lock invisible to old
	// readers (#1324), and a late write from here could land on a directory a
	// later run has already taken.
	if holderPID != 0 {
		// Dropping our own descriptor is what makes the lock the suite's rather
		// than this wrapper's: from here the holder's copy is the only one, so
		// this process can be killed without unlocking a running suite, and the
		// lock cannot outlive the holder's watch.
		_ = lock.Close()
	}

	stops := make(chan os.Signal, 2)
	signal.Notify(stops, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stops)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	for {
		select {
		case sig := <-stops:
			_ = cmd.Process.Signal(sig)
		case err := <-done:
			reportLingeringHolder(os.Stderr, path, holder)
			if err == nil {
				return 0
			}
			// ExitCode is cross-platform: it is the child's code, or -1 when a
			// signal ended it, which is a failure the caller must still see.
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				if code := exitErr.ExitCode(); code >= 0 {
					return code
				}
				return 1
			}
			fmt.Fprintf(os.Stderr, "wait for heavy suite: %v\n", err)
			return 2
		}
	}
}

// self is this binary, the one the holder runs. os.Executable follows the
// running image rather than argv[0], so a wrapper invoked through a relative
// path or a symlink still starts the same binary it is.
func self() string {
	path, err := os.Executable()
	if err != nil {
		return os.Args[0]
	}
	return path
}

// reportLingeringHolder says so when this suite's holder is still running after
// the suite has ended, and NEVER ends it: the one thing worse than a locked box
// is a process killed by someone who has not identified it. It names the
// file-level way to find the holder, which works across pid namespaces where a
// pid does not.
//
// IT ASKS ABOUT OUR HOLDER, NOT ABOUT THE LOCK. Asking whether the lock is free
// answers the wrong question twice. On a platform with no holder this wrapper
// is itself the only owner, so the lock reads held because of us, and every
// such run would wait the whole grace and then report a holder that is this
// process. And a contender that takes the lock in the moment after a clean
// release (a gate that polls, a wrapper that retries) also reads as held, which
// would name our holder for a lock that is rightfully somebody else's. Whether
// the holder we started is still alive is the only thing here that means what
// it says.
func reportLingeringHolder(report io.Writer, path string, holder *os.Process) {
	if holder == nil {
		return
	}
	// WAIT FOR IT RATHER THAN LOOK AT ITS PID. The holder is this process's own
	// child, so once it exits it stays a zombie until it is reaped, and a pid
	// that is merely unreaped answers every liveness question with yes. Waiting
	// both reaps it and answers honestly; the grace bounds the wait for the case
	// this exists to report.
	finished := make(chan struct{})
	go func() {
		_, _ = holder.Wait()
		close(finished)
	}()
	select {
	case <-finished:
		return
	case <-time.After(lingerGrace):
	}
	fmt.Fprintf(report, "this suite has ended but its lock holder, pid %d, is still running after %s.\n", holder.Pid, lingerGrace)
	fmt.Fprintf(report, "The lock is still held by it. Find who holds it, and end nothing you have not identified: lsof %s\n", path)
}

// lingerGrace is how long the holder is given to notice the suite has ended
// before the wrapper says it is still running. It is the holder's poll with
// room for a loaded box, which is exactly when this matters. It is a var only
// so a test need not spend it.
var lingerGrace = 2 * time.Second
