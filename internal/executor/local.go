package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/processgroup"
)

// settleDelay bounds how long Exec waits for output pipes after the process
// exits, so a child that keeps them open cannot hold the call.
const settleDelay = 200 * time.Millisecond

// Jail is the single seam for confinement: it may rewrite the command (wrap it,
// set attributes) before it starts. Jails are not built yet.
type Jail interface {
	Confine(cmd *exec.Cmd, req ExecRequest) error
}

type noJail struct{}

func (noJail) Confine(*exec.Cmd, ExecRequest) error { return nil }

// Local spawns processes on this device, rooted at one workspace directory.
type Local struct {
	Root string
	Jail Jail // nil means no confinement
}

// Exec implements Executor.
func (l Local) Exec(ctx context.Context, req ExecRequest, onOutput func(Chunk)) (ExecResult, error) {
	dir, err := l.check(req)
	if err != nil {
		return ExecResult{}, err
	}
	ctx, cancel := withTimeout(ctx, req.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, req.Argv[0], req.Argv[1:]...) //codeaf:plumbing the executor is the spawn seam
	cmd.Dir, cmd.Env = dir, req.Env
	processgroup.Configure(cmd)
	cmd.Cancel = func() error { return processgroup.Kill(cmd.Process.Pid) }
	cmd.WaitDelay = settleDelay
	if err := l.jail().Confine(cmd, req); err != nil {
		return ExecResult{}, err
	}
	return run(ctx, cmd, req, onOutput)
}

// check validates a request and returns the directory it runs in.
func (l Local) check(req ExecRequest) (string, error) {
	if req.Class == FilesOnly {
		return "", errors.New("executor: a files-only workspace runs no process")
	}
	if len(req.Argv) == 0 {
		return "", errors.New("executor: empty argv")
	}
	return resolve(l.Root, req.Dir)
}

func (l Local) jail() Jail {
	if l.Jail == nil {
		return noJail{}
	}
	return l.Jail
}

// resolve joins a workspace-relative directory to the root, refusing anything
// that escapes it or reaches into the harness-owned .cell directory.
func resolve(root, rel string) (string, error) {
	clean := filepath.Clean(rel)
	first, _, _ := strings.Cut(filepath.ToSlash(clean), "/")
	if filepath.IsAbs(clean) || first == ".." || first == ".cell" {
		return "", fmt.Errorf("executor: directory %q is outside the workspace", rel)
	}
	return filepath.Join(root, clean), nil
}

func withTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, d)
}

func run(ctx context.Context, cmd *exec.Cmd, req ExecRequest, onOutput func(Chunk)) (ExecResult, error) {
	var out, errb bytes.Buffer
	emit := serialized(onOutput)
	cmd.Stdout = io.MultiWriter(&out, chunkWriter{Stdout, emit})
	cmd.Stderr = io.MultiWriter(&errb, chunkWriter{Stderr, emit})
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return ExecResult{}, err
	}
	waitErr := cmd.Wait()
	res := ExecResult{
		Exit: cmd.ProcessState.ExitCode(), Stdout: out.Bytes(), Stderr: errb.Bytes(),
		Wall: time.Since(start), SideEffect: classify(req.Net),
		TimedOut: errors.Is(ctx.Err(), context.DeadlineExceeded),
		Services: services(ctx, cmd),
	}
	return res, outcome(ctx, waitErr)
}

// outcome separates "the process exited" (a result) from "the call failed".
func outcome(ctx context.Context, waitErr error) error {
	var exited *exec.ExitError
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		return ctx.Err()
	case waitErr == nil, errors.As(waitErr, &exited), errors.Is(waitErr, exec.ErrWaitDelay):
		return nil
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return nil
	}
	return waitErr
}

// services reports the process group when something in it outlived the leader.
// A call that was killed leaves none.
func services(ctx context.Context, cmd *exec.Cmd) []Service {
	pid := cmd.Process.Pid
	if ctx.Err() != nil || !processgroup.Alive(pid) {
		return nil
	}
	return []Service{{PGID: pid}}
}

// chunkWriter turns writes on one stream into Chunks.
type chunkWriter struct {
	stream Stream
	emit   func(Chunk)
}

func (w chunkWriter) Write(p []byte) (int, error) {
	w.emit(Chunk{Stream: w.stream, Data: append([]byte(nil), p...)})
	return len(p), nil
}

// serialized makes a nil or concurrent-unsafe callback safe to call from both
// output goroutines.
func serialized(f func(Chunk)) func(Chunk) {
	if f == nil {
		return func(Chunk) {}
	}
	var mu sync.Mutex
	return func(c Chunk) {
		mu.Lock()
		defer mu.Unlock()
		f(c)
	}
}
