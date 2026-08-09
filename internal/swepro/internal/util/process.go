// Process helpers — port of src/util/process.ts:6-174
// (swe-pro 3b25a1a).
package util

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

type ProcessOptions struct {
	Cwd string
	Env map[string]string
	// ClearEnv distinguishes TS env:null from an absent env.
	ClearEnv bool
	Stdin    string
	Stdout   string
	Stderr   string
	Shell    string
	Kill     os.Signal
	Timeout  time.Duration
}

type RunOptions struct {
	ProcessOptions
	NoThrow bool
}

type ProcessResult struct {
	Code   int    `json:"code"`
	Stdout []byte `json:"-"`
	Stderr []byte `json:"-"`
}

type TextResult struct {
	ProcessResult
	Text string `json:"text"`
}

type RunFailedError struct {
	Cmd    []string
	Code   int
	Stdout []byte
	Stderr []byte
}

func (e *RunFailedError) Error() string {
	message := fmt.Sprintf("Command failed with code %d: %s", e.Code, strings.Join(e.Cmd, " "))
	if text := jscompat.Trim(string(e.Stderr)); text != "" {
		message += "\n" + text
	}
	return message
}

func (e *RunFailedError) ErrorName() string { return "ProcessRunFailedError" }

type Child struct {
	Cmd    *exec.Cmd
	Stdin  io.WriteCloser
	Stdout io.ReadCloser
	Stderr io.ReadCloser
	Exited <-chan int

	exit   chan int
	done   chan struct{}
	mu     sync.Mutex
	closed bool
}

func SpawnProcess(ctx context.Context, command []string, options ...ProcessOptions) (*Child, error) {
	if len(command) == 0 {
		return nil, errors.New("Command is required")
	}
	opt := ProcessOptions{}
	if len(options) > 0 {
		opt = options[0]
	}
	name := command[0]
	args := command[1:]
	if opt.Shell != "" {
		shell := opt.Shell
		if shell == "true" {
			if runtime.GOOS == "windows" {
				shell = "cmd.exe"
			} else {
				shell = "/bin/sh"
			}
		}
		line := strings.Join(command, " ")
		name, args = shell, []string{"-c", line}
		if runtime.GOOS == "windows" {
			args = []string{"/d", "/s", "/c", line}
		}
	}
	cmd := exec.Command(name, args...)
	cmd.Dir = opt.Cwd
	switch {
	case opt.ClearEnv:
		cmd.Env = []string{}
	case opt.Env != nil:
		values := map[string]string{}
		order := []string{}
		for _, item := range os.Environ() {
			key, value, _ := strings.Cut(item, "=")
			if _, ok := values[key]; !ok {
				order = append(order, key)
			}
			values[key] = value
		}
		for key, value := range opt.Env {
			if _, ok := values[key]; !ok {
				order = append(order, key)
			}
			values[key] = value
		}
		for _, key := range order {
			cmd.Env = append(cmd.Env, key+"="+values[key])
		}
	}
	child := &Child{Cmd: cmd, exit: make(chan int, 1), done: make(chan struct{})}
	child.Exited = child.exit
	var err error
	child.Stdin, err = configureInput(cmd, opt.Stdin)
	if err != nil {
		return nil, err
	}
	var outWrite, errWrite *os.File
	child.Stdout, outWrite, err = configureOutput(cmd, opt.Stdout, os.Stdout)
	if err != nil {
		return nil, err
	}
	child.Stderr, errWrite, err = configureOutput(cmd, opt.Stderr, os.Stderr)
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		for _, f := range []*os.File{outWrite, errWrite} {
			if f != nil {
				_ = f.Close()
			}
		}
		close(child.done)
		close(child.exit)
		return nil, err
	}
	for _, f := range []*os.File{outWrite, errWrite} {
		if f != nil {
			_ = f.Close()
		}
	}
	go func() {
		err := cmd.Wait()
		code := 0
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		} else if err != nil {
			code = 1
		}
		if code < 0 {
			code = 1
		}
		child.exit <- code
		close(child.exit)
		close(child.done)
	}()
	go func() {
		select {
		case <-ctx.Done():
			child.abort(opt)
		case <-child.done:
		}
	}()
	return child, nil
}

func configureInput(cmd *exec.Cmd, mode string) (io.WriteCloser, error) {
	switch mode {
	case "inherit":
		cmd.Stdin = os.Stdin
		return nil, nil
	case "pipe":
		return cmd.StdinPipe()
	default:
		cmd.Stdin = strings.NewReader("")
		return nil, nil
	}
}

func configureOutput(cmd *exec.Cmd, mode string, inherit io.Writer) (io.ReadCloser, *os.File, error) {
	switch mode {
	case "inherit":
		if inherit == os.Stdout {
			cmd.Stdout = inherit
		} else {
			cmd.Stderr = inherit
		}
		return nil, nil, nil
	case "pipe":
		// Explicit os.Pipe, not StdoutPipe/StderrPipe: the exit goroutine calls
		// cmd.Wait immediately after Start, and Wait auto-closes exec-managed
		// pipes while consumers may still be draining them (truncating output).
		// The caller closes the parent's write-end copy after Start so readers
		// see EOF when the child exits.
		pr, pw, err := os.Pipe()
		if err != nil {
			return nil, nil, err
		}
		if inherit == os.Stdout {
			cmd.Stdout = pw
		} else {
			cmd.Stderr = pw
		}
		return pr, pw, nil
	default:
		if inherit == os.Stdout {
			cmd.Stdout = io.Discard
		} else {
			cmd.Stderr = io.Discard
		}
		return nil, nil, nil
	}
}

func (c *Child) abort(options ProcessOptions) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.mu.Unlock()
	signal := options.Kill
	if signal == nil {
		signal = syscall.SIGTERM
	}
	_ = c.Cmd.Process.Signal(signal)
	timeout := options.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	if timeout <= 0 {
		return
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-c.done:
	case <-timer.C:
		_ = c.Cmd.Process.Kill()
	}
}

func RunProcess(ctx context.Context, command []string, options ...RunOptions) (ProcessResult, error) {
	opt := RunOptions{}
	if len(options) > 0 {
		opt = options[0]
	}
	spawnOptions := opt.ProcessOptions
	spawnOptions.Stdout = "pipe"
	spawnOptions.Stderr = "pipe"
	child, err := SpawnProcess(ctx, command, spawnOptions)
	if err != nil {
		if !opt.NoThrow {
			return ProcessResult{}, err
		}
		return ProcessResult{Code: 1, Stdout: []byte{}, Stderr: []byte(ErrorMessage(err))}, nil
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = io.Copy(&stdout, child.Stdout) }()
	go func() { defer wg.Done(); _, _ = io.Copy(&stderr, child.Stderr) }()
	code := <-child.Exited
	wg.Wait()
	result := ProcessResult{Code: code, Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if code == 0 || opt.NoThrow {
		return result, nil
	}
	return ProcessResult{}, &RunFailedError{
		Cmd: append([]string(nil), command...), Code: code,
		Stdout: result.Stdout, Stderr: result.Stderr,
	}
}

func TextProcess(ctx context.Context, command []string, options ...RunOptions) (TextResult, error) {
	result, err := RunProcess(ctx, command, options...)
	if err != nil {
		return TextResult{}, err
	}
	return TextResult{ProcessResult: result, Text: string(result.Stdout)}, nil
}

func ProcessLines(ctx context.Context, command []string, options ...RunOptions) ([]string, error) {
	result, err := TextProcess(ctx, command, options...)
	if err != nil {
		return nil, err
	}
	lines := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(result.Text, "\r\n", "\n"), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines, nil
}

func StopProcess(ctx context.Context, child *Child) {
	select {
	case <-child.done:
		return
	default:
	}
	if runtime.GOOS != "windows" || child.Cmd.Process == nil {
		_ = child.Cmd.Process.Kill()
		return
	}
	result, _ := RunProcess(ctx, []string{"taskkill", "/pid", fmt.Sprint(child.Cmd.Process.Pid), "/T", "/F"},
		RunOptions{NoThrow: true})
	if result.Code != 0 {
		_ = child.Cmd.Process.Kill()
	}
}
