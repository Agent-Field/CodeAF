//go:build !windows

// The bash tool: runs a command in the workspace shell, enforces its timeout,
// and turns the exit into a tool result with the output capped and spilled.
package tool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
	"github.com/Agent-Field/codeaf/internal/seniordev/session/outputoffload"
)

const (
	defaultBashTimeoutMS = 120000
	maxBashOutputBytes   = 30000
)

var bashAfter = time.After
var bashOffloader = outputoffload.DefaultOffloader
var bashReadOOMCount = readShellOOMCount
var bashReadMemoryLimit = readShellMemoryLimit

type bashOutput struct {
	mu         sync.Mutex
	buffer     bytes.Buffer
	wrote      bool
	lastOutput time.Time
}

func (o *bashOutput) Write(data []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.wrote = true
	o.lastOutput = time.Now()
	return o.buffer.Write(data)
}

func (o *bashOutput) bytes() []byte {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]byte(nil), o.buffer.Bytes()...)
}

func (o *bashOutput) lastOutputAt() (time.Time, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.lastOutput, o.wrote
}

func (r *Registry) executeBash(ctx context.Context, call steploop.ToolCall) (steploop.ToolResult, error) {
	var input bashInput
	if err := decodeInput(call.Input, &input, "command"); err != nil {
		return steploop.ToolResult{}, err
	}
	timeoutMS := defaultBashTimeoutMS
	if input.TimeoutMS != nil {
		timeoutMS = *input.TimeoutMS
	}
	if timeoutMS < 1 || timeoutMS > 600000 {
		return steploop.ToolResult{}, fmt.Errorf("timeout_ms must be between 1 and 600000")
	}
	if err := ctx.Err(); err != nil {
		return steploop.ToolResult{}, err
	}
	cwd := r.workDir
	if input.Workdir != "" {
		resolved, err := r.resolvePath(input.Workdir)
		if err != nil {
			return steploop.ToolResult{}, err
		}
		cwd = resolved
		info, statErr := os.Stat(cwd)
		if statErr != nil {
			return steploop.ToolResult{}, statErr
		}
		if !info.IsDir() {
			return steploop.ToolResult{}, fmt.Errorf("workdir must be a directory: %s", cwd)
		}
		if err := r.askExternalDirectory(ctx, call, cwd, "directory"); err != nil {
			return steploop.ToolResult{}, err
		}
	}
	shell, err := r.executionShell()
	if err != nil {
		return steploop.ToolResult{}, err
	}
	scan := ScanShellPermissions(input.Command, ShellScanOptions{
		CWD: cwd, Workspace: r.worktree(), Shell: shell,
		IsDir: func(path string) bool {
			info, err := os.Stat(path)
			return err == nil && info.IsDir()
		},
	})
	if r.hardConfineShell && len(scan.Dirs) > 0 {
		return steploop.ToolResult{}, fmt.Errorf("path escapes workspace: %s", scan.Dirs[0])
	}
	if len(scan.Dirs) > 0 {
		globs := make([]string, 0, len(scan.Dirs))
		for _, dir := range scan.Dirs {
			globs = append(globs, filepath.Join(dir, "*"))
		}
		if err := r.askWithAlways(ctx, call, "external_directory", globs, globs, map[string]any{}); err != nil {
			return steploop.ToolResult{}, err
		}
	}
	if len(scan.Patterns) > 0 {
		if err := r.askWithAlways(ctx, call, "bash", scan.Patterns, scan.Always, map[string]any{}); err != nil {
			return steploop.ToolResult{}, err
		}
	}
	command := shellExecCommand(shell, input.Command)
	command.Dir = cwd
	command.Env = shellEnvironment(call.SessionID)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// A plain `sleep 300 &` leaves stdout open after bash exits. Without a
	// pipe-close bound, exec.Wait waits for that background job and turns a
	// successful shell command into a timeout before the run can clean it up.
	command.WaitDelay = 100 * time.Millisecond
	var output bashOutput
	command.Stdout = &output
	command.Stderr = &output
	envSignalsOn := os.Getenv("SENIOR_DEV_ENV_SIGNALS") != "0"
	startedAt := time.Now()
	var oomBefore *int64
	if envSignalsOn {
		oomBefore = bashReadOOMCount()
	}
	if err := command.Start(); err != nil {
		return steploop.ToolResult{}, fmt.Errorf("start shell command: %w", err)
	}
	r.registerShellGroup(command.Process.Pid)
	defer r.pruneShellGroup(command.Process.Pid)

	done := make(chan error, 1)
	go func() {
		done <- command.Wait()
	}()

	var runErr error
	expired := false
	select {
	case runErr = <-done:
	case <-bashAfter(time.Duration(timeoutMS) * time.Millisecond):
		expired = true
		killProcessGroup(command.Process.Pid)
		<-done
		appendOutputLine(&output, fmt.Sprintf("command timed out after %dms", timeoutMS))
	case <-ctx.Done():
		killProcessGroup(command.Process.Pid)
		<-done
		return steploop.ToolResult{}, ctx.Err()
	}
	if errors.Is(runErr, exec.ErrWaitDelay) {
		// The shell itself succeeded; only a background child held its output
		// pipe open past the bounded drain after the shell exited.
		runErr = nil
	}

	var exitCode *int
	if runErr == nil && !expired {
		code := 0
		exitCode = &code
	} else if !expired {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			return steploop.ToolResult{}, fmt.Errorf("wait for shell command: %w", runErr)
		}
		code := normalizedExitCode(exitErr)
		exitCode = &code
		appendOutputLine(&output, fmt.Sprintf("exit status %d", code))
	}

	if envSignalsOn {
		duration := time.Since(startedAt)
		oomAfter := bashReadOOMCount()
		var oomDelta *int64
		if oomBefore != nil && oomAfter != nil {
			delta := *oomAfter - *oomBefore
			oomDelta = &delta
		}
		var sinceLast *time.Duration
		if lastOutput, ok := output.lastOutputAt(); ok {
			quiet := time.Since(lastOutput)
			sinceLast = &quiet
		}
		metadata := []string{}
		if death := classifyShellDeath(shellDeathInput{
			ExitCode: exitCode, Expired: expired, OOMDelta: oomDelta,
			MemoryLimitBytes: bashReadMemoryLimit(), SinceLastOutput: sinceLast,
			Timeout: time.Duration(timeoutMS) * time.Millisecond, CommandDuration: duration,
		}); death != "" {
			metadata = append(metadata, death)
		}
		failed := exitCode == nil || *exitCode != 0
		if repeat := registerShellOutcome(call.SessionID, input.Command, duration, failed); repeat != "" {
			metadata = append(metadata, repeat)
		}
		if len(metadata) > 0 {
			appendOutputLine(&output, "\n<shell_metadata>\n"+strings.Join(metadata, "\n")+"\n</shell_metadata>")
		}
	}

	fullOutput := output.bytes()
	code := -1
	if exitCode != nil {
		code = *exitCode
	}
	return r.bashResult(call, input.Command, fullOutput, code, exitCode != nil), nil
}

func (r *Registry) bashResult(
	call steploop.ToolCall, command string, fullOutput []byte, exitCode int, hasExitCode bool,
) steploop.ToolResult {
	inline := truncateMiddle(fullOutput, maxBashOutputBytes)
	if len(fullOutput) > maxBashOutputBytes {
		offloaded := bashOffloader.OffloadLargeOutput(
			outputoffload.OutputOffloadInput{
				Output: string(fullOutput), Workspace: r.workDir,
				ToolName: "bash", CallID: call.ID, SessionID: call.SessionID,
			},
			outputoffload.OutputOffloadOptions{Force: true},
		)
		if offloaded.OffloadPath != nil {
			inline += "\n\nThe tool call succeeded but the output was truncated. Full output saved to: " + *offloaded.OffloadPath +
				"\nUse Grep to search the full content or Read with offset/limit to view specific sections."
		} else if fallback := strings.TrimSpace(offloaded.Inline); fallback != "" {
			if index := strings.LastIndex(fallback, "\n"); index >= 0 {
				fallback = fallback[index+1:]
			}
			inline += "\n\n" + fallback
		}
	}
	metadata := msgmodel.RawObject("{}")
	if hasExitCode {
		metadata = msgmodel.RawObject(fmt.Sprintf(`{"exitCode":%d}`, exitCode))
	}
	return steploop.ToolResult{Title: firstRunes(command, 60), Metadata: metadata, Output: inline}
}

func shellExecCommand(shell, command string) *exec.Cmd {
	switch ShellName(shell) {
	case "cmd":
		return exec.Command(shell, "/c", command)
	case "powershell", "pwsh":
		return exec.Command(shell, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", command)
	default:
		return exec.Command(shell, "-c", command)
	}
}

func normalizedExitCode(exitErr *exec.ExitError) int {
	if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal())
	}
	return exitErr.ExitCode()
}

func killProcessGroup(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}

func (r *Registry) registerShellGroup(pid int) {
	processes := r.shellProcesses
	processes.mu.Lock()
	defer processes.mu.Unlock()
	if processes.closed {
		killProcessGroup(pid)
		return
	}
	processes.groups[pid] = struct{}{}
}

func (r *Registry) pruneShellGroup(pid int) {
	processes := r.shellProcesses
	processes.mu.Lock()
	defer processes.mu.Unlock()
	if err := syscall.Kill(-pid, 0); err == syscall.ESRCH {
		delete(processes.groups, pid)
	}
}

// CloseShellProcesses ends background jobs left in the process groups that
// the run's bash tool started before the run releases its workspace.
func (r *Registry) CloseShellProcesses() {
	processes := r.shellProcesses
	processes.mu.Lock()
	defer processes.mu.Unlock()
	processes.closed = true
	for pid := range processes.groups {
		killProcessGroup(pid)
	}
}

func appendOutputLine(output *bashOutput, line string) {
	output.mu.Lock()
	defer output.mu.Unlock()
	if output.buffer.Len() > 0 && output.buffer.Bytes()[output.buffer.Len()-1] != '\n' {
		output.buffer.WriteByte('\n')
	}
	output.buffer.WriteString(line)
}

func firstRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func truncateMiddle(data []byte, limit int) string {
	if len(data) <= limit {
		return string(data)
	}

	removed := len(data) - limit
	var marker string
	var kept int
	for {
		marker = fmt.Sprintf("[... %d bytes truncated ...]", removed)
		kept = limit - len(marker)
		if kept < 0 {
			return marker[:limit]
		}
		actualRemoved := len(data) - kept
		if actualRemoved == removed {
			break
		}
		removed = actualRemoved
	}

	head := kept / 2
	tail := kept - head
	result := make([]byte, 0, limit)
	result = append(result, data[:head]...)
	result = append(result, marker...)
	result = append(result, data[len(data)-tail:]...)
	return string(result)
}
