package delegate

// The launch: one delegate as a child process, in its own process group, its
// stdout read as the protocol and its stderr kept in a file for a person, ended
// by SIGTERM with a grace and then SIGKILL when the caller's context ends
// (docs/DELEGATE-PROTOCOL.md §1 and §4).

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

// Fills is what the launch puts in the manifest's placeholders.
type Fills struct {
	Brief     string
	Workspace string
	// CostUSD and Hours are the ceilings handed to the program. Zero means
	// none, and a placeholder with no value is DROPPED together with the flag
	// before it (see fill), because a program handed `--max-cost 0` may read
	// that as a ceiling of nothing.
	CostUSD float64
	Hours   float64
	// Key is the person's API key, resolved by the caller.
	Key string
}

// Launch is one run of one delegate.
type Launch struct {
	Manifest Manifest
	Fills    Fills
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
	m := launch.Manifest
	bin := m.BinPath
	if bin == "" {
		bin = m.Bin
	}
	argv, err := fill(m.Argv, launch.Fills, true)
	if err != nil {
		return Result{ExitCode: -1}, err
	}
	env := os.Environ()
	for key, value := range m.Env {
		filled, err := fill([]string{value}, launch.Fills, false)
		if err != nil {
			return Result{ExitCode: -1}, err
		}
		if len(filled) == 0 {
			// A variable whose whole value was an empty fill is not set at all,
			// so a program that reads "is it set" reads the truth.
			continue
		}
		env = append(env, key+"="+filled[0])
	}

	cmd := exec.Command(bin, argv...)
	cmd.Env = env
	cmd.Dir = launch.Fills.Workspace
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
		return Result{ExitCode: -1}, fmt.Errorf("start %s: %w", m.Name, err)
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
		return result, fmt.Errorf("read %s's stdout: %w", m.Name, r.err)
	}
	if result.Reading.Terminal == nil {
		return result, ErrNoTerminal
	}
	return result, nil
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

// fill replaces placeholders in argv. AN EMPTY CEILING DROPS ITS FLAG: an
// element that is exactly a ceiling placeholder with no value is removed, and
// so is the element before it when that element is a flag (`--max-cost`), so
// a program with no ceiling set is handed no `--max-cost` at all rather than a
// zero it might read as "spend nothing". dropFlags is off for env values, where
// there is no flag to drop and an empty fill leaves the variable unset.
//
// A FILLED VALUE IS NEVER READ AGAIN. Each element is substituted in one pass
// ([strings.Replacer] does not rescan what it inserted), and the check for a
// placeholder this build does not fill reads the manifest's own text, never the
// result. Both are there because the brief is the person's words: a review of a
// Go template or a Helm chart says `{{ .Name }}`, which must reach the program
// as written rather than refuse the launch, and a brief that says `{{key}}`
// must not have the person's key spliced into a command line every process on
// the machine can read.
func fill(argv []string, fills Fills, dropFlags bool) ([]string, error) {
	values := map[string]string{
		FillBrief:            fills.Brief,
		FillWorkspace:        fills.Workspace,
		FillKey:              fills.Key,
		"{{key:openrouter}}": fills.Key,
	}
	if fills.CostUSD > 0 {
		values[FillCostUSD] = strconv.FormatFloat(fills.CostUSD, 'f', -1, 64)
	} else {
		values[FillCostUSD] = ""
	}
	if fills.Hours > 0 {
		values[FillHours] = strconv.FormatFloat(fills.Hours, 'f', -1, 64)
	} else {
		values[FillHours] = ""
	}
	pairs := make([]string, 0, 2*len(values))
	for placeholder, value := range values {
		pairs = append(pairs, placeholder, value)
	}
	replacer := strings.NewReplacer(pairs...)
	out := make([]string, 0, len(argv))
	for _, arg := range argv {
		if value, whole := values[arg]; whole && value == "" && (arg == FillCostUSD || arg == FillHours || arg == FillKey || arg == "{{key:openrouter}}") {
			if dropFlags && len(out) > 0 && strings.HasPrefix(out[len(out)-1], "-") {
				out = out[:len(out)-1]
			}
			continue
		}
		for _, placeholder := range fillShape.FindAllString(arg, -1) {
			if _, known := values[placeholder]; !known {
				return nil, fmt.Errorf("%s is not a placeholder this build fills", placeholder)
			}
		}
		out = append(out, replacer.Replace(arg))
	}
	return out, nil
}
