//go:build !windows

package clipboard

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"
)

func goos() string { return runtime.GOOS }

// run starts the helper once and maps how it ended. Standard output and error
// are nil, which is /dev/null, so nothing the helper says is kept and a child
// it forks holds no pipe of ours. The helper has its own process group so a
// timeout reaps what it forked; WaitDelay bounds a stdin copy that a stuck
// helper never reads.
func run(ctx context.Context, c Command) error {
	return runWith(ctx, c, helperTimeout, helperWaitDelay)
}

func runWith(ctx context.Context, c Command, timeout, waitDelay time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Path, c.Args...)
	// A nil Env would inherit this process's whole environment, secrets and
	// all, so the allowlist is always a non-nil slice.
	cmd.Env = append([]string{}, c.Env...)
	cmd.Stdin = strings.NewReader(c.Stdin)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = waitDelay
	if err := cmd.Start(); err != nil {
		return ErrUnavailable
	}
	err := cmd.Wait()
	if ctx.Err() != nil {
		return ErrUnconfirmed
	}
	var exit *exec.ExitError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &exit) && exit.ExitCode() > 0:
		return ErrFailed
	}
	return ErrUnconfirmed
}
