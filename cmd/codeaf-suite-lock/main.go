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

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "start heavy suite: %v\n", err)
		return 2
	}

	// The holder takes the descriptor before anything is recorded, so the lock
	// is already the suite's by the time the file names it. A platform without a
	// holder (Windows) keeps it here, which is safe there because nothing else
	// can inherit it.
	holderPID := 0
	if holder, err := startHolder(self(), path, lock, cmd.Process.Pid); err == nil {
		holderPID = holder.Pid
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
			reportLingeringHolder(path, holderPID)
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

// reportLingeringHolder says so when the lock is still held after the suite has
// ended, and NEVER ends whoever holds it: the one thing worse than a locked box
// is a person, or a wrapper, killing a process it has not identified. It names
// the file-level way to find the holder, which works across pid namespaces
// where a pid does not.
func reportLingeringHolder(path string, holderPID int) {
	deadline := time.Now().Add(lingerGrace)
	for {
		free, err := lockIsFree(path)
		if err != nil || free {
			return
		}
		if !time.Now().Before(deadline) {
			break
		}
		time.Sleep(holdPoll)
	}
	fmt.Fprintf(os.Stderr, "the heavy-suite lock is still held %s after this suite ended", lingerGrace)
	if holderPID != 0 {
		fmt.Fprintf(os.Stderr, ", and this suite's holder was pid %d", holderPID)
	}
	fmt.Fprintf(os.Stderr, ".\nFind who holds it, and end nothing you have not identified: lsof %s\n", path)
}

// lingerGrace is how long the holder is given to notice the suite has ended
// before the wrapper says the lock is still held. It is the holder's poll with
// room for a loaded box, which is exactly when this matters.
const lingerGrace = 2 * time.Second

// lockIsFree answers whether the lock can be taken right now, and takes nothing:
// it opens its own descriptor, tries the lock without blocking, and closes it.
func lockIsFree(path string) (bool, error) {
	probe, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return false, err
	}
	defer probe.Close()
	if err := filelock.Lock(probe, true, true); err != nil {
		if filelock.IsBusy(err) {
			return false, nil
		}
		return false, err
	}
	_ = filelock.Unlock(probe)
	return true, nil
}
