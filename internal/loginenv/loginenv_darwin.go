package loginenv

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/processgroup"
)

const (
	startupTimeout = 3 * time.Second
	pipeGrace      = 100 * time.Millisecond
	pathCommand    = `printf "\n` + pathStart + `%s` + pathEnd + `\n" "$PATH"`
)

func loginPath(parent context.Context) (string, error) {
	// Reserve pipe cleanup inside the startup budget, even when a profile leaves
	// a child holding stdout after the shell has exited or been killed.
	ctx, cancel := context.WithTimeout(parent, startupTimeout-pipeGrace)
	defer cancel()
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = passwdShell(ctx)
	}
	if shell == "" {
		shell = "/bin/zsh"
	}
	output, err := shellOutput(ctx, shell, "-l", "-c", pathCommand)
	if err != nil {
		return "", err
	}
	return parsePath(output)
}

func passwdShell(ctx context.Context) string {
	// macOS accounts live in Directory Services, rather than /etc/passwd.
	// Both lookups share the profile deadline and need no cgo account lookup.
	output, err := shellOutput(ctx, "/usr/bin/dscl", ".", "-search", "/Users", "UniqueID", strconv.Itoa(os.Getuid()))
	if err != nil {
		return ""
	}
	fields := strings.Fields(output)
	if len(fields) < 2 {
		return ""
	}
	output, err = shellOutput(ctx, "/usr/bin/dscl", ".", "-read", "/Users/"+fields[0], "UserShell")
	if err != nil {
		return ""
	}
	key, value, found := strings.Cut(strings.TrimSpace(output), ":")
	if !found || key != "UserShell" {
		return ""
	}
	return strings.TrimSpace(value)
}

func shellOutput(ctx context.Context, shell string, args ...string) (string, error) {
	input, err := os.Open(os.DevNull)
	if err != nil {
		return "", err
	}
	defer input.Close()
	command := exec.CommandContext(ctx, shell, args...)
	command.Stdin = input
	processgroup.Configure(command)
	command.Cancel = func() error {
		return processgroup.Kill(command.Process.Pid)
	}
	command.WaitDelay = pipeGrace
	output, err := command.Output()
	if errors.Is(err, exec.ErrWaitDelay) {
		_ = processgroup.Kill(command.Process.Pid)
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return string(output), err
}
