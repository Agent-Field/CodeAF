// codeaf-suite-lock holds the machine-wide heavy-suite lock while a command runs.
//
// The lock is an advisory file lock held on an open descriptor through
// internal/filelock, so it works on every platform the release ships (build-cross
// covers Windows too) and, being tied to the descriptor, it dies with the holder:
// a suite killed mid-run leaves the box unlocked with no pid bookkeeping to go
// stale. The holder writes its pid and start time into the lock file only so a
// refused contender can name who holds it.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: codeaf-suite-lock LOCK COMMAND [ARG...]")
		os.Exit(2)
	}
	os.Exit(run(os.Args[1], os.Args[2:]))
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
			fmt.Fprintln(os.Stderr, "Wait for it, or run one named regression with make test-focus.")
			return 1
		}
		fmt.Fprintf(os.Stderr, "take heavy-suite lock: %v\n", err)
		return 2
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
	if _, err := fmt.Fprintf(lock, "%d %s\n", os.Getpid(), since); err != nil {
		fmt.Fprintf(os.Stderr, "write heavy-suite lock: %v\n", err)
		return 2
	}
	if err := lock.Sync(); err != nil {
		fmt.Fprintf(os.Stderr, "write heavy-suite lock: %v\n", err)
		return 2
	}

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "start heavy suite: %v\n", err)
		return 2
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
