package delegate

// The launch: one program as a child process, in its own process group, its
// stdout read as the records and its stderr kept in a file for a person, ended
// by SIGTERM with a grace and then SIGKILL when the caller's context ends
// (docs/design/delegate/PROTOCOL.md). The process is codeaf's own executable
// running the program's verb ([ChildArgs]); what it is started with is the
// caller's to say, so a test can start a script that speaks the records.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Agent-Field/codeaf/internal/processgroup"
)

// DefaultGrace is how long a SIGTERM has to work before SIGKILL follows. It is
// the job registry's own two seconds plus what a program that has to write a
// terminal record and close a database needs: senior-dev ships its frozen tree on
// the way out, and a grace that cut that short would lose the one record the
// whole protocol exists for.
const DefaultGrace = 15 * time.Second

// Launch is one run of one program.
type Launch struct {
	// Name is the program's name, for the errors this launch writes.
	Name string
	// Bin and Args are the process: codeaf's own executable and the program's
	// line ([ChildArgs]).
	Bin  string
	Args []string
	// Env is the child's whole environment ([ChildEnv]). Nil inherits this
	// process's, which only a test wants: it would hand a program every key.
	Env []string
	// Dir is the folder the process starts in.
	Dir string
	// StderrPath is the file the program's stderr is appended to. Empty
	// discards it, which no real caller wants: stderr is where a program says
	// why it could not start.
	StderrPath string
	// Grace overrides DefaultGrace, for a test that must not wait fifteen
	// seconds for a process that ignores SIGTERM.
	Grace time.Duration
}

// Result is what one launch came to.
type Result struct {
	Reading Reading
	// ExitCode is the process's own, -1 when it was ended by a signal or never
	// ran. The verdict is NOT read from it (§3): a program that failed its task
	// exits zero with a terminal saying `fail`.
	ExitCode int
	// Stopped is true when the caller's context ended the program: SIGTERM,
	// and SIGKILL when the grace passed. The reading may still hold a terminal
	// the program wrote inside the grace.
	Stopped bool
	// Killed is true when SIGKILL was needed.
	Killed bool
	// Elapsed is the process's wall time.
	Elapsed time.Duration
}

// ExitedAt is the instant the program's process was gone: the launch's own
// measure of the process's life laid on the instant the caller started it, and
// never later than returned, the instant the launch gave its answer back.
//
// THE PROGRAM'S WALL TIME IS ITS PROCESS'S, NOT THE DRAIN'S. A launch returns
// only once stdout is drained, and a helper the program left holding stdout can
// keep that drain open for the whole grace after the program itself exited. A
// conversation's run and a shell run both end the program's clock here, so the
// same program reads the same time on every surface.
func (r Result) ExitedAt(started, returned time.Time) time.Time {
	if r.Elapsed > 0 {
		if exited := started.Add(r.Elapsed); exited.Before(returned) {
			return exited
		}
	}
	return returned
}

// ErrNoTerminal is the error a launch answers when the program exited without
// a terminal record and was not stopped by the caller: the run did not finish
// in the protocol's terms, whatever the exit code said.
var ErrNoTerminal = errors.New("the program exited without a terminal record")

// Run starts the program and reads it to its end. It returns when the process
// has exited and stdout is drained, so nothing of the child outlives the call.
//
// A CONTEXT THAT ENDS ENDS THE PROGRAM, in the order the protocol promises:
// SIGTERM to the group, the grace, SIGKILL. The stdout reader keeps reading
// through the grace, so a terminal written on the way out is the reading's
// terminal. The error answered is the context's own, so a run supervisor that
// reads `context.Canceled` off a worker knows its own ending cut the task.
func Run(ctx context.Context, launch Launch, sink Sink) (Result, error) {
	cmd := exec.Command(launch.Bin, launch.Args...)
	// The marker is a last-resort Linux sweep after SIGKILL stops the engine
	// before its own subreaper can clean up its descendants.
	markerBytes := make([]byte, 16)
	if _, err := rand.Read(markerBytes); err != nil {
		return Result{ExitCode: -1}, fmt.Errorf("mark %s's descendants: %w", launch.Name, err)
	}
	marker := hex.EncodeToString(markerBytes)
	baseEnv := launch.Env
	if baseEnv == nil {
		baseEnv = os.Environ()
	}
	cmd.Env = append(append([]string(nil), baseEnv...), processgroup.RunMarkerEnv+"="+marker)
	cmd.Dir = launch.Dir
	cmd.Stdin = nil
	processgroup.Configure(cmd)
	stderr, err := openStderr(launch.StderrPath)
	if err != nil {
		return Result{ExitCode: -1}, err
	}
	defer stderr.Close()
	cmd.Stderr = stderr
	// STDOUT IS A PIPE THIS LAUNCH OWNS, not cmd.StdoutPipe: Wait closes that
	// one the moment the process exits, and bytes still in the kernel's buffer
	// — a terminal record written a millisecond before exit — would be gone
	// with it. Here the write end is the child's alone once started, the reader
	// reads to EOF, and EOF comes when every holder of the write end is gone.
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		return Result{ExitCode: -1}, err
	}
	cmd.Stdout = stdoutWrite
	started := time.Now()
	if err := cmd.Start(); err != nil {
		_ = stdoutRead.Close()
		_ = stdoutWrite.Close()
		return Result{ExitCode: -1}, fmt.Errorf("start %s: %w", launch.Name, err)
	}
	_ = stdoutWrite.Close()
	group := processgroup.CaptureGroup(cmd.Process.Pid)

	type read struct {
		reading Reading
		err     error
	}
	readDone := make(chan read, 1)
	go func() {
		reading, err := Read(stdoutRead, sink)
		readDone <- read{reading, err}
	}()

	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	result := Result{ExitCode: -1}
	grace := launch.Grace
	if grace <= 0 {
		grace = DefaultGrace
	}
	var waitErr error
	select {
	case waitErr = <-waitDone:
	case <-ctx.Done():
		result.Stopped = true
		_ = group.Terminate()
		select {
		case waitErr = <-waitDone:
		case <-time.After(grace):
			result.Killed = true
			_ = group.Kill()
			waitErr = <-waitDone
		}
	}
	result.Elapsed = time.Since(started)
	// A SIGKILL engine cannot run its own parent-link cleanup. The marker is
	// only a best-effort fallback for that ending.
	if result.Killed || killedBySignal(waitErr) {
		processgroup.CleanupRun(marker)
	}
	if waitErr == nil {
		result.ExitCode = 0
	} else {
		var exit *exec.ExitError
		if errors.As(waitErr, &exit) {
			if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Exited() {
				result.ExitCode = status.ExitStatus()
			}
		}
	}
	// THE READER IS GIVEN THE GRACE TO REACH EOF, then the pipe is closed under
	// it. EOF ordinarily arrives with the exit, but a grandchild the program
	// left holding stdout — a detached helper — would hold this launch open for
	// as long as it lived, and a launch that never returns is a run that never
	// lands.
	var r read
	select {
	case r = <-readDone:
	case <-time.After(grace):
		_ = stdoutRead.Close()
		r = <-readDone
	}
	_ = stdoutRead.Close()
	result.Reading = r.reading
	if result.Stopped {
		return result, ctx.Err()
	}
	if r.err != nil {
		return result, fmt.Errorf("read %s's stdout: %w", launch.Name, r.err)
	}
	if result.Reading.Terminal == nil {
		return result, ErrNoTerminal
	}
	return result, nil
}

func killedBySignal(err error) bool {
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return false
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	return ok && status.Signaled() && status.Signal() == syscall.SIGKILL
}

// openStderr opens the stderr file for append, creating it, or a sink when
// no path was given.
func openStderr(path string) (io.WriteCloser, error) {
	if strings.TrimSpace(path) == "" {
		return nopCloser{io.Discard}, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
}

type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }
