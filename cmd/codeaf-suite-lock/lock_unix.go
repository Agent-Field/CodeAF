//go:build unix

package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// startHolder hands the locked descriptor to a small process of our own that
// outlives this wrapper and holds the lock for exactly as long as the suite
// runs.
//
// THE SUITE ITSELF NEVER GETS THE DESCRIPTOR. An advisory lock lives on the
// open file description, so every process that inherits the descriptor keeps
// the lock alive, and an inherited fd is not close-on-exec: handing it to the
// suite means any shell a test leaves behind holds the whole box's lock after
// the suite is finished. That is not a hypothetical. On 2026-09-19 a test's
// orphaned shell held it for eight minutes past a green run, with its working
// directory already deleted. The holder has no children and execs nothing, so
// nothing can inherit the lock from it.
//
// It runs in its own session so a signal aimed at the wrapper's process group,
// or at the suite's, does not take the lock down with it.
func startHolder(self, path string, lock *os.File, suite int) (*os.Process, error) {
	holder := exec.Command(self, holdFlag, strconv.Itoa(suite), procStartToken(suite))
	// AND IT CARRIES THE ONE MARK AN OLD CHECKOUT READS AS ALIVE. The holder is
	// the pid the directory lock names (dirlock.go's [legacyReaderMark]), and an
	// old reader accepts a live pid only when its command line says
	// one-suite.sh. argv[0] is only a name; the binary run is still `self`.
	holder.Args[0] = legacyReaderMark + " heavy-suite lock holder"
	// Position 3 in the holder, the first descriptor after standard input,
	// output and error: the holder reads it back with os.NewFile(3, ...).
	holder.ExtraFiles = []*os.File{lock}
	holder.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	holder.Stderr = os.Stderr
	if err := holder.Start(); err != nil {
		return nil, err
	}
	return holder.Process, nil
}

// holdLock is the holder: it owns the locked descriptor and does nothing else
// until the suite it was told to watch is gone, then exits and lets the kernel
// drop the lock.
//
// Watching a pid is honest HERE in a way reading one from a file is not: this
// process was started beside the suite, in the same pid namespace, by the
// wrapper that started the suite. The start token guards the one remaining
// misreading, a pid reused by a different process after the suite exits.
func holdLock(suite int, token string) int {
	lock := os.NewFile(3, "heavy-suite-lock")
	if lock == nil {
		return 2
	}
	defer lock.Close()
	// THE DIRECTORY LOCK GOES WHEN THIS DESCRIPTOR GOES. The holder is the only
	// process whose lifetime is the suite's, so it is the only honest place to
	// free the second lock: freeing it in the wrapper would open the box to a
	// stale reader while the suite still ran and still held the flock.
	//
	// AND THE HOLDER NAMES ITSELF IN IT, first, because it is the process whose
	// lifetime the lock has. It used to be named with the SUITE's pid, which an
	// old checkout's liveness test cannot accept (dirlock.go's
	// [legacyReaderMark]), and the release below drops the directory only while
	// it still names this process.
	me := os.Getpid()
	nameDirLockHolder(dirLockPath(), me)
	defer dropDirLock(dirLockPath(), me)
	for {
		if !pidVisibleHere(suite) {
			return 0
		}
		if token != "" && procStartToken(suite) != token {
			return 0
		}
		time.Sleep(holdPoll)
	}
}

// procStartToken is the pid's start time as the kernel records it, the one
// field that tells a reused pid from the process that was there before. It is
// empty where the system does not publish it, and then the holder watches the
// pid alone.
func procStartToken(pid int) string {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return ""
	}
	// The command name sits in parentheses and may hold spaces, so fields are
	// counted from after the last ')': start time is the 22nd field overall,
	// which is the 20th after it.
	tail := raw[strings.LastIndexByte(string(raw), ')')+1:]
	fields := strings.Fields(string(tail))
	if len(fields) < 20 {
		return ""
	}
	return fields[19]
}

// commandLine is a pid's command line with its arguments joined by spaces,
// empty when it cannot be read. It answers exactly as the old checkout's
// `holder_alive` does: /proc where there is one, and ps elsewhere.
func commandLine(pid int) string {
	if raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline"); err == nil {
		return strings.ReplaceAll(string(raw), "\x00", " ")
	}
	out, err := exec.Command("ps", "-o", "args=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// pidVisibleHere reports whether a process with this pid exists in THIS pid
// namespace. kill(pid, 0) signals nothing: nil or EPERM means it is visible
// here, ESRCH means it is not, which says nothing about whether it is alive in
// another namespace or gone, only that its pid cannot be read from here.
func pidVisibleHere(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
