package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/processgroup"
)

// settleDelay bounds how long Exec waits for output pipes after the process
// exits, so a child that keeps them open cannot hold the call.
const settleDelay = 200 * time.Millisecond

// Jail is the single seam for confinement: it may rewrite the command (wrap it,
// set attributes) before it starts. See jail_linux.go for the Linux jail; other systems keep no confinement.
type Jail interface {
	Confine(cmd *exec.Cmd, req ExecRequest) error
}

// Degrader is optionally implemented by a jail that may run with fewer
// limits than asked for on this kernel; the result then says so.
type Degrader interface {
	Degraded(req ExecRequest) bool
}

type noJail struct{}

func (noJail) Confine(*exec.Cmd, ExecRequest) error { return nil }

// Local spawns processes on this device, rooted at one workspace directory.
type Local struct {
	Root  string
	Class Class // the workspace's declared class; the zero value is Sandboxed
	Jail  Jail  // nil means no confinement
	Setup bool  // every call is a setup turn's (docs/ARCHITECTURE.md 8.4)

	// Observer, when set, sees each call that ran to a result.
	Observer Observer
}

// resolve stamps the request with the workspace's class and the network policy
// that class allows for it: the one place a policy is chosen.
func (l Local) resolve(req ExecRequest) ExecRequest {
	req.Class = l.Class
	req.Setup = req.Setup || l.Setup
	req.Net = PolicyFor(l.Class, req.Setup)
	return req
}

// Exec implements Executor.
func (l Local) Exec(ctx context.Context, req ExecRequest, onOutput func(Chunk)) (ExecResult, error) {
	req = l.resolve(req)
	ctx, cancel := withTimeout(ctx, req.Timeout)
	defer cancel()
	if req.WaitDelay == 0 {
		req.WaitDelay = settleDelay
	}
	cmd, err := l.Command(ctx, req)
	if err != nil {
		return ExecResult{}, err
	}
	res, err := run(ctx, cmd, req, l.Root, onOutput)
	res.JailDegraded = degraded(l.Jail, req)
	if err == nil && l.Observer != nil {
		l.Observer.Observe(req, res)
	}
	return res, err
}

func degraded(j Jail, req ExecRequest) bool {
	d, ok := j.(Degrader)
	return ok && req.Class == Sandboxed && d.Degraded(req)
}

// Command builds the one confined, unstarted command for a request: class and
// directory checked, environment, group and cancellation set, the jail applied.
// The caller wires the streams it needs, starts it and waits. It is for
// processes whose lifetime the caller owns (background jobs, pipelines,
// interactive children); Timeout is not applied here, and ctx ends the process
// only when it is cancelled.
func (l Local) Command(ctx context.Context, req ExecRequest) (*exec.Cmd, error) {
	req = l.resolve(req)
	dir, err := l.check(req)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, req.Argv[0], req.Argv[1:]...) //codeaf:plumbing the executor is the spawn seam
	cmd.Dir, cmd.Env, cmd.Stdin = dir, req.Env, req.Stdin
	configureGroup(cmd, req.Group)
	cmd.Cancel = canceller(cmd, req.Group)
	cmd.WaitDelay = req.WaitDelay
	if err := l.confine(cmd, req); err != nil {
		return nil, err
	}
	return cmd, nil
}

func configureGroup(cmd *exec.Cmd, g Group) {
	switch g {
	case GroupOwn:
		processgroup.Configure(cmd)
	case GroupSession:
		processgroup.ConfigureDetached(cmd)
	}
}

// canceller ends the whole group the child leads, or the child alone when it
// shares the harness's group.
func canceller(cmd *exec.Cmd, g Group) func() error {
	return func() error {
		switch {
		case cmd.Process == nil:
			return os.ErrProcessDone
		case g == GroupInherit:
			return cmd.Process.Kill()
		}
		return processgroup.Kill(cmd.Process.Pid)
	}
}

// check validates a request and returns the directory it runs in.
func (l Local) check(req ExecRequest) (string, error) {
	if !req.Class.rule().Spawns {
		return "", errors.New("executor: a files-only workspace runs no process")
	}
	if len(req.Argv) == 0 {
		return "", errors.New("executor: empty argv")
	}
	if arg, hit := cellArg(req.Argv); hit {
		return "", fmt.Errorf("executor: argument %q reaches into the harness-owned .cell directory", arg)
	}
	return resolve(l.Root, req.Dir)
}

// confine applies the jail to sandboxed calls only.
func (l Local) confine(cmd *exec.Cmd, req ExecRequest) error {
	if req.Class != Sandboxed {
		return nil
	}
	return l.jail().Confine(cmd, req)
}

// cellRef matches ".cell" as a whole path segment anywhere in an argument,
// including inside a shell command line or a --flag=path.
var cellRef = regexp.MustCompile(`(^|[/=\s'"])\.cell($|[/\s'"])`)

// cellArg returns the first argument that names the .cell directory.
func cellArg(argv []string) (string, bool) {
	for _, a := range argv {
		if cellRef.MatchString(filepath.ToSlash(a)) {
			return a, true
		}
	}
	return "", false
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

func run(ctx context.Context, cmd *exec.Cmd, req ExecRequest, root string, onOutput func(Chunk)) (ExecResult, error) {
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = sinks(req, &out, &errb, serialized(onOutput))
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return ExecResult{}, err
	}
	waitErr := cmd.Wait()
	res := ExecResult{
		Exit: cmd.ProcessState.ExitCode(), Status: cmd.ProcessState.String(),
		Stdout: out.Bytes(), Stderr: errb.Bytes(),
		Wall: time.Since(start), SideEffect: Classify(req.Net),
		TimedOut:    errors.Is(ctx.Err(), context.DeadlineExceeded),
		PipesForced: errors.Is(waitErr, exec.ErrWaitDelay),
		Services:    services(ctx, cmd, req, root),
	}
	return res, outcome(ctx, waitErr)
}

// sinks picks the writers a run's streams go to: the chunk callback always,
// the result's buffers unless the call streams, and one shared writer when
// stderr is combined into stdout.
func sinks(req ExecRequest, out, errb *bytes.Buffer, emit func(Chunk)) (io.Writer, io.Writer) {
	stdout, stderr := io.Writer(&chunkWriter{Stdout, emit}), io.Writer(&chunkWriter{Stderr, emit})
	if !req.Stream {
		stdout, stderr = io.MultiWriter(out, stdout), io.MultiWriter(errb, stderr)
	}
	if req.Combined {
		return stdout, stdout
	}
	return stdout, stderr
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

// chunkWriter turns writes on one stream into Chunks.
type chunkWriter struct {
	stream Stream
	emit   func(Chunk)
}

func (w *chunkWriter) Write(p []byte) (int, error) {
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
