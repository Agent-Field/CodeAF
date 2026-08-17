// Cross-spawn process spawner — port of
// src/core/cross-spawn-spawner.ts:28-503 (swe-pro 3b25a1a). This Go surface
// retains argv/env/cwd/stdio/pipeline/group-kill behavior without Effect's
// Stream and Scope wrappers.
package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/processgroup"
)

// SystemError is the portable PlatformError.SystemError image.
type SystemError struct {
	Tag              string
	Module           string
	Method           string
	PathOrDescriptor string
	Syscall          string
	Cause            error
}

func (e *SystemError) Error() string {
	return fmt.Sprintf("%s.%s(%s): %v", e.Module, e.Method, e.PathOrDescriptor, e.Cause)
}

func (e *SystemError) Unwrap() error { return e.Cause }

// EnvVar is one environment assignment. A slice preserves JS object order.
type EnvVar struct {
	Name  string
	Value string
}

// IOConfig configures a standard stream.
type IOConfig struct {
	Mode   string // "pipe", "inherit", "ignore"
	Reader io.Reader
	Writer io.Writer
}

// FDConfig configures one fd >= 3.
type FDConfig struct {
	Type   string // "input" or "output"
	Reader io.Reader
	Writer io.Writer
}

// CommandOptions mirrors effect ChildProcess.CommandOptions.
type CommandOptions struct {
	Cwd       string
	Env       []EnvVar
	EnvSet    bool
	ExtendEnv *bool

	Stdin  IOConfig
	Stdout IOConfig
	Stderr IOConfig

	AdditionalFDs  map[int]FDConfig
	Detached       *bool
	Shell          string // "", "true", or an explicit shell path
	KillSignal     os.Signal
	ForceKillAfter time.Duration
}

// StandardCommand is one executable plus argv.
type StandardCommand struct {
	Command string
	Args    []string
	Options CommandOptions
}

// PipeOptions selects the source and destination of a pipeline edge.
type PipeOptions struct {
	From string // stdout (default), stderr, all, fdN
	To   string // stdin (default), fdN
}

// Command is a StandardCommand or PipedCommand.
type Command interface{ commandNode() }

func (StandardCommand) commandNode() {}

// PipedCommand connects Left to Right.
type PipedCommand struct {
	Left    Command
	Right   Command
	Options PipeOptions
}

func (PipedCommand) commandNode() {}

// MakeCommand constructs a standard command.
func MakeCommand(command string, args []string, options ...CommandOptions) StandardCommand {
	opt := CommandOptions{}
	if len(options) > 0 {
		opt = options[0]
	}
	return StandardCommand{Command: command, Args: append([]string(nil), args...), Options: opt}
}

// Pipe constructs a piped command.
func Pipe(left, right Command, options ...PipeOptions) PipedCommand {
	opt := PipeOptions{}
	if len(options) > 0 {
		opt = options[0]
	}
	return PipedCommand{Left: left, Right: right, Options: opt}
}

// SpawnSpec is the fully resolved command passed to os/exec.
type SpawnSpec struct {
	Path     string
	Args     []string
	Cwd      string
	Env      []string
	EnvSet   bool
	Detached bool
	Shell    string
}

// BuildSpawnSpec performs the pure argv/env/cwd construction.
func BuildSpawnSpec(command StandardCommand) (SpawnSpec, error) {
	options := command.Options
	cwd := ""
	if options.Cwd != "" {
		info, err := os.Stat(options.Cwd)
		if err != nil {
			return SpawnSpec{}, platformError("access", err, command)
		}
		if !info.IsDir() {
			return SpawnSpec{}, platformError("access", syscall.ENOTDIR, command)
		}
		cwd, err = filepathAbs(options.Cwd)
		if err != nil {
			return SpawnSpec{}, platformError("access", err, command)
		}
	}
	extend := true
	if options.ExtendEnv != nil {
		extend = *options.ExtendEnv
	}
	var environment []string
	envSet := options.EnvSet || len(options.Env) > 0
	if extend {
		environment = mergeEnvironment(os.Environ(), options.Env)
		envSet = true
	} else if envSet {
		environment = make([]string, 0, len(options.Env))
		for _, item := range options.Env {
			environment = append(environment, item.Name+"="+item.Value)
		}
	}
	detached := runtime.GOOS != "windows"
	if options.Detached != nil {
		detached = *options.Detached
	}
	path := command.Command
	args := append([]string(nil), command.Args...)
	if options.Shell != "" {
		shell := options.Shell
		if shell == "true" {
			if runtime.GOOS == "windows" {
				shell = "cmd.exe"
			} else {
				shell = "/bin/sh"
			}
		}
		line := strings.Join(append([]string{command.Command}, command.Args...), " ")
		if runtime.GOOS == "windows" {
			path, args = shell, []string{"/d", "/s", "/c", line}
		} else {
			path, args = shell, []string{"-c", line}
		}
	}
	return SpawnSpec{
		Path:     path,
		Args:     args,
		Cwd:      cwd,
		Env:      environment,
		EnvSet:   envSet,
		Detached: detached,
		Shell:    options.Shell,
	}, nil
}

func filepathAbs(path string) (string, error) {
	return filepathAbsolute(path)
}

// kept in a variable-sized helper so Windows path resolution can be tested
// without exposing an os/exec detail.
var filepathAbsolute = func(path string) (string, error) {
	return filepath.Abs(path)
}

func mergeEnvironment(base []string, overrides []EnvVar) []string {
	order := []string{}
	values := map[string]string{}
	for _, item := range base {
		name, value, ok := strings.Cut(item, "=")
		if !ok {
			name, value = item, ""
		}
		if _, exists := values[name]; !exists {
			order = append(order, name)
		}
		values[name] = value
	}
	for _, item := range overrides {
		if _, exists := values[item.Name]; !exists {
			order = append(order, item.Name)
		}
		values[item.Name] = item.Value
	}
	out := make([]string, 0, len(order))
	for _, name := range order {
		out = append(out, name+"="+values[name])
	}
	return out
}

// Spawner starts commands and pipelines.
type Spawner struct{}

// NewSpawner constructs the default spawner.
func NewSpawner() *Spawner { return &Spawner{} }

type flatPipeline struct {
	commands []StandardCommand
	options  []PipeOptions
}

func flatten(command Command) (flatPipeline, error) {
	out := flatPipeline{}
	var walk func(Command) error
	walk = func(command Command) error {
		switch value := command.(type) {
		case StandardCommand:
			out.commands = append(out.commands, value)
		case *StandardCommand:
			out.commands = append(out.commands, *value)
		case PipedCommand:
			if err := walk(value.Left); err != nil {
				return err
			}
			out.options = append(out.options, value.Options)
			return walk(value.Right)
		case *PipedCommand:
			if err := walk(value.Left); err != nil {
				return err
			}
			out.options = append(out.options, value.Options)
			return walk(value.Right)
		default:
			return fmt.Errorf("unknown command type %T", command)
		}
		return nil
	}
	if err := walk(command); err != nil {
		return out, err
	}
	if len(out.commands) == 0 {
		return out, errors.New("flatten produced empty commands array")
	}
	return out, nil
}

// Handle is a running command or pipeline. Stdout/Stderr belong to the final
// command, matching the PipedCommand handle returned by the TS spawner.
type Handle struct {
	PID    int
	Stdin  io.WriteCloser
	Stdout io.ReadCloser
	Stderr io.ReadCloser
	All    io.Reader

	mu       sync.Mutex
	commands []*exec.Cmd
	edges    [][]io.Closer
	done     chan struct{}
	waitErr  error
	exitCode int
	options  CommandOptions
}

// Spawn starts command and returns after every child has started.
func (s *Spawner) Spawn(ctx context.Context, command Command) (*Handle, error) {
	flat, err := flatten(command)
	if err != nil {
		return nil, err
	}
	commands := make([]*exec.Cmd, len(flat.commands))
	edges := make([][]io.Closer, len(flat.commands))
	specs := make([]SpawnSpec, len(flat.commands))
	for i, standard := range flat.commands {
		spec, err := BuildSpawnSpec(standard)
		if err != nil {
			return nil, err
		}
		specs[i] = spec
		cmd := exec.Command(spec.Path, spec.Args...)
		cmd.Dir = spec.Cwd
		if spec.EnvSet {
			cmd.Env = spec.Env
		}
		if spec.Detached {
			processgroup.Configure(cmd)
		}
		commands[i] = cmd
	}

	// Wire pipeline edges before stdio defaults so edge streams win.
	for i, option := range flat.options {
		from := option.From
		if from == "" {
			from = "stdout"
		}
		to := option.To
		if to == "" {
			to = "stdin"
		}
		reader, writer := io.Pipe()
		edges[i] = append(edges[i], writer)
		switch from {
		case "stderr":
			commands[i].Stderr = writer
		case "all":
			commands[i].Stdout = writer
			commands[i].Stderr = writer
		default:
			if fd, ok := parseFDName(from); ok {
				if err := setOutputFD(commands[i], fd, writer); err != nil {
					return nil, err
				}
			} else {
				commands[i].Stdout = writer
			}
		}
		if fd, ok := parseFDName(to); ok {
			if err := setInputFD(commands[i+1], fd, reader); err != nil {
				return nil, err
			}
		} else {
			commands[i+1].Stdin = reader
		}
	}

	var finalStdout io.ReadCloser
	var finalStderr io.ReadCloser
	var finalStdin io.WriteCloser
	var parentWriteEnds []*os.File
	for i, standard := range flat.commands {
		cmd := commands[i]
		for _, fd := range SortedFDs(standard.Options.AdditionalFDs) {
			if err := setExtraFile(cmd, fd, standard.Options.AdditionalFDs[fd]); err != nil {
				return nil, platformError("additionalFd", err, standard)
			}
		}
		if cmd.Stdin == nil {
			switch {
			case standard.Options.Stdin.Reader != nil:
				cmd.Stdin = standard.Options.Stdin.Reader
			case standard.Options.Stdin.Mode == "inherit":
				cmd.Stdin = os.Stdin
			case standard.Options.Stdin.Mode == "ignore":
				cmd.Stdin = strings.NewReader("")
			default:
				finalStdin, err = cmd.StdinPipe()
				if err != nil {
					return nil, platformError("stdin", err, standard)
				}
			}
		}
		if cmd.Stdout == nil {
			switch {
			case standard.Options.Stdout.Writer != nil:
				cmd.Stdout = standard.Options.Stdout.Writer
			case standard.Options.Stdout.Mode == "inherit":
				cmd.Stdout = os.Stdout
			case standard.Options.Stdout.Mode == "ignore":
				cmd.Stdout = io.Discard
			default:
				if i == len(commands)-1 {
					// Explicit os.Pipe, not StdoutPipe: cmd.Wait (run from the
					// background handle.wait goroutine) closes StdoutPipe pipes,
					// racing consumers still draining Handle.Stdout. Node streams
					// stay readable after exit, so the consumer must own the read
					// end. The parent write-end copy is closed after Start so EOF
					// arrives on child exit.
					pr, pw, err := os.Pipe()
					if err != nil {
						return nil, platformError("stdout", err, standard)
					}
					cmd.Stdout = pw
					finalStdout = pr
					parentWriteEnds = append(parentWriteEnds, pw)
				} else {
					pipe, err := cmd.StdoutPipe()
					if err != nil {
						return nil, platformError("stdout", err, standard)
					}
					_ = pipe
				}
			}
		}
		if cmd.Stderr == nil {
			switch {
			case standard.Options.Stderr.Writer != nil:
				cmd.Stderr = standard.Options.Stderr.Writer
			case standard.Options.Stderr.Mode == "inherit":
				cmd.Stderr = os.Stderr
			case standard.Options.Stderr.Mode == "ignore":
				cmd.Stderr = io.Discard
			default:
				if i == len(commands)-1 {
					pr, pw, err := os.Pipe()
					if err != nil {
						return nil, platformError("stderr", err, standard)
					}
					cmd.Stderr = pw
					finalStderr = pr
					parentWriteEnds = append(parentWriteEnds, pw)
				} else {
					pipe, err := cmd.StderrPipe()
					if err != nil {
						return nil, platformError("stderr", err, standard)
					}
					_ = pipe
				}
			}
		}
	}

	started := 0
	for i, cmd := range commands {
		if err := cmd.Start(); err != nil {
			for j := 0; j < started; j++ {
				_ = killCommand(commands[j], syscall.SIGTERM, specs[j].Detached)
			}
			for _, w := range parentWriteEnds {
				_ = w.Close()
			}
			return nil, platformError("spawn", err, flat.commands[i])
		}
		for _, file := range cmd.ExtraFiles {
			_ = file.Close()
		}
		started++
	}
	for _, w := range parentWriteEnds {
		_ = w.Close()
	}
	handle := &Handle{
		PID:      commands[len(commands)-1].Process.Pid,
		Stdin:    finalStdin,
		Stdout:   finalStdout,
		Stderr:   finalStderr,
		commands: commands,
		edges:    edges,
		done:     make(chan struct{}),
		options:  flat.commands[len(flat.commands)-1].Options,
	}
	if finalStdout != nil && finalStderr != nil {
		handle.All = &mergedReader{readers: []io.Reader{finalStdout, finalStderr}}
	} else if finalStdout != nil {
		handle.All = finalStdout
	} else {
		handle.All = finalStderr
	}
	go handle.wait()
	go func() {
		select {
		case <-ctx.Done():
			_ = handle.Kill()
		case <-handle.done:
		}
	}()
	return handle, nil
}

type mergedReader struct {
	once    sync.Once
	readers []io.Reader
	reader  *io.PipeReader
}

func (m *mergedReader) Read(p []byte) (int, error) {
	m.once.Do(func() {
		reader, writer := io.Pipe()
		m.reader = reader
		var wg sync.WaitGroup
		for _, source := range m.readers {
			wg.Add(1)
			go func(source io.Reader) {
				defer wg.Done()
				_, _ = io.Copy(writer, source)
			}(source)
		}
		go func() {
			wg.Wait()
			_ = writer.Close()
		}()
	})
	return m.reader.Read(p)
}

func (h *Handle) wait() {
	var lastErr error
	lastCode := 0
	for i, command := range h.commands {
		err := command.Wait()
		for _, closer := range h.edges[i] {
			_ = closer.Close()
		}
		if i == len(h.commands)-1 {
			lastErr = err
			if command.ProcessState != nil {
				lastCode = command.ProcessState.ExitCode()
			}
		}
	}
	h.mu.Lock()
	h.waitErr = lastErr
	h.exitCode = lastCode
	h.mu.Unlock()
	close(h.done)
}

// Wait waits for the pipeline and returns the final command's exit code.
func (h *Handle) Wait() (int, error) {
	<-h.done
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.exitCode, h.waitErr
}

// IsRunning reports whether Wait has completed.
func (h *Handle) IsRunning() bool {
	select {
	case <-h.done:
		return false
	default:
		return true
	}
}

// Kill sends the configured signal and optionally escalates to SIGKILL.
func (h *Handle) Kill() error {
	signal := h.options.KillSignal
	if signal == nil {
		signal = syscall.SIGTERM
	}
	for _, command := range h.commands {
		detached := processgroup.Configured(command)
		if err := killCommand(command, signal, detached); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
	}
	if h.options.ForceKillAfter > 0 {
		timer := time.NewTimer(h.options.ForceKillAfter)
		defer timer.Stop()
		select {
		case <-h.done:
			return nil
		case <-timer.C:
			for _, command := range h.commands {
				detached := processgroup.Configured(command)
				_ = killCommand(command, syscall.SIGKILL, detached)
			}
		}
	}
	return nil
}

func killCommand(command *exec.Cmd, signal os.Signal, detached bool) error {
	if command.Process == nil {
		return os.ErrProcessDone
	}
	force := signal == os.Kill || signal == syscall.SIGKILL
	if detached {
		if force {
			return processgroup.Kill(command.Process.Pid)
		}
		return processgroup.Terminate(command.Process.Pid)
	}
	if force {
		return command.Process.Kill()
	}
	return command.Process.Signal(signal)
}

func parseFDName(name string) (int, bool) {
	if !strings.HasPrefix(name, "fd") {
		return 0, false
	}
	value, err := strconv.Atoi(strings.TrimPrefix(name, "fd"))
	return value, err == nil && value >= 3
}

func setOutputFD(command *exec.Cmd, fd int, writer io.Writer) error {
	return setExtraFile(command, fd, FDConfig{Type: "output", Writer: writer})
}

func setInputFD(command *exec.Cmd, fd int, reader io.Reader) error {
	return setExtraFile(command, fd, FDConfig{Type: "input", Reader: reader})
}

func setExtraFile(command *exec.Cmd, fd int, config FDConfig) error {
	// os/exec ExtraFiles only accepts *os.File. A small pipe bridges arbitrary
	// readers/writers while preserving fd numbering.
	for len(command.ExtraFiles) <= fd-3 {
		null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
		if err != nil {
			return err
		}
		command.ExtraFiles = append(command.ExtraFiles, null)
	}
	read, write, err := os.Pipe()
	if err != nil {
		return err
	}
	if config.Type == "input" {
		command.ExtraFiles[fd-3] = read
		go func() {
			if config.Reader != nil {
				_, _ = io.Copy(write, config.Reader)
			}
			_ = write.Close()
		}()
	} else {
		command.ExtraFiles[fd-3] = write
		go func() {
			if config.Writer != nil {
				_, _ = io.Copy(config.Writer, read)
			} else {
				_, _ = io.Copy(io.Discard, read)
			}
			_ = read.Close()
		}()
	}
	return nil
}

func platformError(method string, err error, command StandardCommand) error {
	tag := "Unknown"
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, exec.ErrNotFound):
		tag = "NotFound"
	case errors.Is(err, fs.ErrPermission):
		tag = "PermissionDenied"
	case errors.Is(err, fs.ErrExist):
		tag = "AlreadyExists"
	case errors.Is(err, syscall.EBUSY):
		tag = "Busy"
	case errors.Is(err, syscall.EISDIR), errors.Is(err, syscall.ENOTDIR), errors.Is(err, syscall.ELOOP):
		tag = "BadResource"
	}
	return &SystemError{
		Tag:              tag,
		Module:           "ChildProcess",
		Method:           method,
		PathOrDescriptor: strings.TrimSpace(command.Command + " " + strings.Join(command.Args, " ")),
		Cause:            err,
	}
}

// SortedFDs returns valid additional fd numbers, matching Object.entries +
// parseFdName + numeric toSorted.
func SortedFDs(fds map[int]FDConfig) []int {
	out := make([]int, 0, len(fds))
	for fd := range fds {
		if fd >= 3 {
			out = append(out, fd)
		}
	}
	sort.Ints(out)
	return out
}

// Run captures stdout/stderr and waits for one command.
func (s *Spawner) Run(ctx context.Context, command StandardCommand) ([]byte, []byte, int, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Options.Stdout = IOConfig{Writer: &stdout}
	command.Options.Stderr = IOConfig{Writer: &stderr}
	handle, err := s.Spawn(ctx, command)
	if err != nil {
		return nil, nil, -1, err
	}
	code, err := handle.Wait()
	return stdout.Bytes(), stderr.Bytes(), code, err
}
