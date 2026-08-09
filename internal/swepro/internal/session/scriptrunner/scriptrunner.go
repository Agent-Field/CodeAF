// Package scriptrunner ports src/session/script-runner.ts:1-133 from swe-pro
// commit 3b25a1a. Process execution and the out-of-scope output-offload sibling
// are both narrow injectable seams.
package scriptrunner

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// ScriptLanguage is one of the three emitted-script runtimes.
type ScriptLanguage string

const (
	LanguageTypeScript ScriptLanguage = "ts"
	LanguageJavaScript ScriptLanguage = "js"
	LanguageBash       ScriptLanguage = "bash"
)

const DefaultTimeoutMS = 120_000

var extensions = map[ScriptLanguage]string{
	LanguageTypeScript: ".ts",
	LanguageJavaScript: ".js",
	LanguageBash:       ".sh",
}

// ScriptExecOptions are the process settings observable by an injected exec.
type ScriptExecOptions struct {
	CWD       string
	TimeoutMS float64
	Env       *jscompat.OrderedMap[string, string]
}

// ScriptExecResult is the raw process result.
type ScriptExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode float64
	TimedOut bool
}

// ScriptExec is the process execution seam.
type ScriptExec interface {
	Exec(command []string, options ScriptExecOptions) (ScriptExecResult, error)
}

// ScriptExecFunc adapts a function to ScriptExec.
type ScriptExecFunc func(command []string, options ScriptExecOptions) (ScriptExecResult, error)

func (f ScriptExecFunc) Exec(command []string, options ScriptExecOptions) (ScriptExecResult, error) {
	return f(command, options)
}

// OutputOffloadInput is the exact projection script-runner passes to its
// output-offload sibling.
type OutputOffloadInput struct {
	Output    string
	Workspace string
	ToolName  string
	CallID    string
}

// OutputOffloadResult is the exact projection script-runner consumes.
type OutputOffloadResult struct {
	Inline      string
	OffloadPath string
}

// OutputOffloader is the unported output-offload seam.
type OutputOffloader interface {
	OffloadLargeOutput(input OutputOffloadInput) OutputOffloadResult
}

// OutputOffloaderFunc adapts a function to OutputOffloader.
type OutputOffloaderFunc func(input OutputOffloadInput) OutputOffloadResult

func (f OutputOffloaderFunc) OffloadLargeOutput(input OutputOffloadInput) OutputOffloadResult {
	return f(input)
}

// OffloadedOutput is the handle returned when the output sibling saved a file.
type OffloadedOutput struct {
	Path string `json:"path"`
	Note string `json:"note"`
}

// ScriptResult is the caller-visible result.
type ScriptResult struct {
	ExitCode  jscompat.JSNumber `json:"exitCode"`
	TimedOut  bool              `json:"timedOut"`
	Stdout    string            `json:"stdout"`
	Stderr    string            `json:"stderr"`
	Offloaded *OffloadedOutput  `json:"offloaded,omitempty"`
}

// RunEmittedScriptOptions configures one emitted script.
type RunEmittedScriptOptions struct {
	Language  ScriptLanguage
	Source    string
	CWD       string
	TimeoutMS *float64
	Env       *jscompat.OrderedMap[string, string]
	Exec      ScriptExec
	Offloader OutputOffloader
	// RuntimePath is process.execPath. Empty resolves "bun" from PATH.
	RuntimePath string
}

type defaultExec struct{}

func mergedEnvironment(overrides *jscompat.OrderedMap[string, string]) []string {
	order := []string{}
	values := map[string]string{}
	for _, pair := range os.Environ() {
		for i := 0; i < len(pair); i++ {
			if pair[i] == '=' {
				key := pair[:i]
				if _, exists := values[key]; !exists {
					order = append(order, key)
				}
				values[key] = pair[i+1:]
				break
			}
		}
	}
	for _, entry := range overrides.Entries() {
		if _, exists := values[entry.Key]; !exists {
			order = append(order, entry.Key)
		}
		values[entry.Key] = entry.Val
	}
	out := make([]string, 0, len(order))
	for _, key := range order {
		out = append(out, key+"="+values[key])
	}
	return out
}

func (defaultExec) Exec(command []string, options ScriptExecOptions) (ScriptExecResult, error) {
	if len(command) == 0 {
		return ScriptExecResult{}, errors.New("script-runner: empty command")
	}
	duration := time.Duration(options.TimeoutMS * float64(time.Millisecond))
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = options.CWD
	if options.Env != nil {
		cmd.Env = mergedEnvironment(options.Env)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return ScriptExecResult{}, err
	}
	var timedOut atomic.Bool
	timer := time.AfterFunc(duration, func() {
		timedOut.Store(true)
		// Bun.Subprocess.kill() defaults to SIGTERM. A process that ignores it
		// can keep the call pending; the TS implementation has the same shape.
		_ = cmd.Process.Signal(syscall.SIGTERM)
	})
	err := cmd.Wait()
	timer.Stop()
	exitCode := float64(0)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = float64(exitErr.ExitCode())
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				exitCode = float64(128 + int(status.Signal()))
			}
		} else if timedOut.Load() {
			exitCode = 1
		} else {
			return ScriptExecResult{}, err
		}
	}
	return ScriptExecResult{
		Stdout: stdout.String(), Stderr: stderr.String(),
		ExitCode: exitCode, TimedOut: timedOut.Load(),
	}, nil
}

func runtimePath(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	return exec.LookPath("bun")
}

func buildCommand(language ScriptLanguage, file, runtime string) []string {
	if language == LanguageBash {
		return []string{"bash", file}
	}
	return []string{runtime, "run", file}
}

// RunEmittedScript writes, executes, shapes, and removes one temporary script.
// Offloader must be supplied by the eventual output-offload port; this keeps
// the dependency seam explicit instead of silently changing large-output
// behavior.
func RunEmittedScript(options RunEmittedScriptOptions) (ScriptResult, error) {
	if options.Offloader == nil {
		return ScriptResult{}, errors.New("script-runner: output offloader is required")
	}
	timeoutMS := float64(DefaultTimeoutMS)
	if options.TimeoutMS != nil {
		timeoutMS = *options.TimeoutMS
	}
	executor := options.Exec
	if executor == nil {
		executor = defaultExec{}
	}
	runtime, err := runtimePath(options.RuntimePath)
	if err != nil {
		return ScriptResult{}, err
	}
	dir, err := os.MkdirTemp("", "codeaf-script-")
	if err != nil {
		return ScriptResult{}, err
	}
	defer os.RemoveAll(dir)

	extension, ok := extensions[options.Language]
	if !ok {
		extension = "undefined"
	}
	file := filepath.Join(dir, "script"+extension)
	if err := os.WriteFile(file, []byte(options.Source), 0o666); err != nil {
		return ScriptResult{}, err
	}
	raw, err := executor.Exec(buildCommand(options.Language, file, runtime), ScriptExecOptions{
		CWD: options.CWD, TimeoutMS: timeoutMS, Env: options.Env,
	})
	if err != nil {
		return ScriptResult{}, err
	}
	parts := []string{}
	if raw.Stdout != "" {
		parts = append(parts, raw.Stdout)
	}
	if raw.Stderr != "" {
		parts = append(parts, raw.Stderr)
	}
	combined := ""
	for i, part := range parts {
		if i > 0 {
			combined += "\n"
		}
		combined += part
	}
	offloaded := options.Offloader.OffloadLargeOutput(OutputOffloadInput{
		Output: combined, Workspace: options.CWD,
		ToolName: "emitted-script", CallID: "emitted-script",
	})
	if offloaded.OffloadPath != "" {
		note := "Full output saved to " + offloaded.OffloadPath +
			" — read it only if the extract is insufficient."
		return ScriptResult{
			ExitCode: jscompat.JSNumber(raw.ExitCode), TimedOut: raw.TimedOut,
			Stdout: offloaded.Inline, Stderr: offloaded.Inline,
			Offloaded: &OffloadedOutput{Path: offloaded.OffloadPath, Note: note},
		}, nil
	}
	return ScriptResult{
		ExitCode: jscompat.JSNumber(raw.ExitCode), TimedOut: raw.TimedOut,
		Stdout: raw.Stdout, Stderr: raw.Stderr,
	}, nil
}

// BuildScriptGuidance returns the byte-stable model-visible prompt block.
func BuildScriptGuidance() string {
	return "## Emit a script (one turn) instead of many tool calls\n" +
		"Use a script when you face a mechanical sequence of ≥3 steps with no decision between them:\n" +
		"setup, batch renames, run-and-filter pipelines, install-then-test, generate-then-move.\n" +
		"Prefer TypeScript (runs natively on Bun — use for any logic, parsing, or branching).\n" +
		"Use bash only for plain command chains with no real logic.\n" +
		"One script = one turn: write the full program, run it once, read the result.\n" +
		"Print only what the next decision needs; write bulk output to files and print their paths.\n" +
		"Do not emit a script for exploratory reads or any step that needs mid-sequence judgment."
}
