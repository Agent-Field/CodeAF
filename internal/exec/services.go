package exec

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const (
	serviceTerminateGrace = 2 * time.Second
	serviceStopPoll       = 20 * time.Millisecond
)

// ProcessStartTime is the platform identity helper used both when a service
// is recorded and when it is re-adopted. Darwin and the other Unix targets
// supported by aforge expose lstart through ps; callers degrade safely when a
// platform does not.
func ProcessStartTime(pid int) (time.Time, error) {
	if pid <= 0 {
		return time.Time{}, errors.New("invalid pid")
	}
	command := exec.Command("ps", "-o", "lstart=", "-p", fmt.Sprint(pid))
	// ps renders lstart in the caller's locale, which reorders month and day.
	// Asking for C makes the one format we depend on the one we get; the
	// day-first layouts below keep a platform that ignores the request usable.
	command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C", "LC_TIME=C")
	output, err := command.Output()
	if err != nil {
		return time.Time{}, err
	}
	raw := strings.Join(strings.Fields(string(output)), " ")
	for _, layout := range []string{
		"Mon Jan 2 15:04:05 2006", "Mon Jan _2 15:04:05 2006",
		"Mon 2 Jan 15:04:05 2006", "Jan 2 15:04:05 2006", "2 Jan 15:04:05 2006",
	} {
		if parsed, parseErr := time.ParseInLocation(layout, raw, time.Local); parseErr == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("parse process start time %q", raw)
}

// ProcessIdentityMatches protects re-adoption from PID reuse. A platform that
// cannot expose start time reports an error; callers can preserve the process
// without pretending identity was proven.
func ProcessIdentityMatches(pid int, startedAt time.Time) (bool, error) {
	actual, err := ProcessStartTime(pid)
	if err != nil {
		return false, err
	}
	return actual.Equal(startedAt.UTC().Truncate(time.Second)), nil
}

// StartDetachedService uses the same shell, log, session/process-group shape
// as background jobs. No pipe or resident goroutine is created.
func StartDetachedService(command, dir, logPath string) (int, time.Time, error) {
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, time.Time{}, err
	}
	cmd := exec.Command("bash", "-lc", command)
	configureDetachedCommand(cmd, dir, logFile)
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return 0, time.Time{}, err
	}
	_ = logFile.Close()
	started := time.Now().UTC().Truncate(time.Second)
	if identity, identityErr := ProcessStartTime(cmd.Process.Pid); identityErr == nil {
		started = identity
	}
	pid := cmd.Process.Pid
	// The supervisor reaps runtime-started children on its ordinary tick;
	// releasing the Go handle avoids retaining one handle per restart.
	_ = cmd.Process.Release()
	return pid, started, nil
}

func configureDetachedCommand(cmd *exec.Cmd, dir string, output io.Writer) {
	cmd.Dir = dir
	cmd.Stdout = output
	cmd.Stderr = output
	// A new session is also a new process group. It preserves group-wide job
	// teardown and lets an adopted service survive the chat terminal closing.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// StopServiceProcess terminates the whole detached session, preserving the
// job registry's TERM-then-KILL contract without requiring its waiter channel.
func StopServiceProcess(pid int) error {
	if pid <= 0 {
		return nil
	}
	if err := signalService(pid, syscall.SIGTERM); err != nil {
		return err
	}
	deadline := time.Now().Add(serviceTerminateGrace)
	for serviceLeaderAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(serviceStopPoll)
	}
	if serviceLeaderAlive(pid) {
		if err := signalService(pid, syscall.SIGKILL); err != nil {
			return err
		}
	}
	// One last sweep of anything the session leader left behind. Best effort:
	// the group is commonly already gone, and a stop must not fail for saying
	// so twice.
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	return nil
}

// signalService prefers the whole process group and falls back to the session
// leader. Darwin refuses a group signal outright once a member has become a
// zombie, and a service that cannot be stopped conversationally is exactly the
// orphan this feature exists to prevent.
func signalService(pid int, signal syscall.Signal) error {
	groupErr := syscall.Kill(-pid, signal)
	if groupErr == nil || errors.Is(groupErr, syscall.ESRCH) {
		return nil
	}
	leaderErr := syscall.Kill(pid, signal)
	if leaderErr == nil || errors.Is(leaderErr, syscall.ESRCH) {
		return nil
	}
	return leaderErr
}

// serviceLeaderAlive asks about the leader rather than the group: a group
// probe can be refused while the process is plainly still running. A leader
// this process spawned is reaped here first, because an unreaped zombie still
// answers a liveness signal and would otherwise read as running forever. A
// re-adopted orphan is not our child, so the signal probe decides it.
func serviceLeaderAlive(pid int) bool {
	var status syscall.WaitStatus
	if waited, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil); err == nil && waited == pid {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
