package exec

import (
	"context"
	"io"
	"os/exec"
	"time"

	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/processgroup"
	commandshell "github.com/Agent-Field/codeaf/internal/shell"
)

const (
	serviceTerminateGrace = 2 * time.Second
	serviceStopPoll       = 20 * time.Millisecond
)

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
	logFile, err := commandshell.OpenLog(logPath, 0o644)
	if err != nil {
		return 0, time.Time{}, err
	}
	cmd, err := detachedCommand(executor.Host, context.Background(), dir, command, nil, logFile, 0)
	if err == nil {
		err = processgroup.Start(cmd)
	}
	if err != nil {
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

// detachedCommand builds the shell command a background job or service runs
// as: a login bash in dir, in a session of its own, writing to one shared
// output. A new session is also a new process group; it preserves group-wide
// job teardown and lets an adopted service survive the chat terminal closing.
func detachedCommand(
	seat executor.Seat, ctx context.Context, dir, command string, environment []string, output io.Writer, wait time.Duration,
) (*exec.Cmd, error) {
	bash, err := commandshell.Bash()
	if err != nil {
		return nil, err
	}
	cmd, err := seat.In(dir).Command(ctx, executor.ExecRequest{
		Argv: append([]string{bash}, commandshell.PipefailArgs(bash, []string{"-lc"}, command)...), Env: environment,
		Group: executor.GroupSession, WaitDelay: wait,
	})
	if err != nil {
		return nil, err
	}
	cmd.Stdout, cmd.Stderr = output, output
	return cmd, nil
}

// StopServiceProcess terminates the whole detached session, preserving the
// job registry's TERM-then-KILL contract without requiring its waiter channel.
//
// IT REFUSES A RECYCLED PID. A service is addressed by a pid recorded at start;
// under pid pressure that number can name somebody else by the time a stop runs,
// and signalling it would tear down an unrelated process group. So the recorded
// start time is checked against the live process first, and a process that is no
// longer the one that was started — or whose identity cannot be read — is left
// alone. A missed stop leaks one service; a wrong stop destroys someone else's
// work.
func StopServiceProcess(pid int, startedAt time.Time) error {
	if pid <= 0 {
		return nil
	}
	if matched, err := ProcessIdentityMatches(pid, startedAt); err != nil || !matched {
		return unverifiedServiceStop(pid)
	}
	if err := serviceTerminate(pid); err != nil {
		return err
	}
	deadline := time.Now().Add(serviceTerminateGrace)
	for serviceTreeAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(serviceStopPoll)
	}
	if serviceTreeAlive(pid) {
		if err := processgroup.Kill(pid); err != nil {
			return err
		}
	}
	return finishServiceStop(pid)
}

// serviceLeaderAlive asks about the leader rather than the group: a group
// probe can be refused while the process is plainly still running. A leader
// this process spawned is reaped here first, because an unreaped zombie still
// answers a liveness signal and would otherwise read as running forever. A
// re-adopted orphan is not our child, so the signal probe decides it.
func serviceLeaderAlive(pid int) bool {
	if processgroup.ReapExitedChild(pid) {
		return false
	}
	return processgroup.ProcessAlive(pid)
}
