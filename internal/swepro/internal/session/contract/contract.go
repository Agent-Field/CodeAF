// Package contract ports src/session/contract.ts:1-206 from swe-pro commit
// 3b25a1a. It validates a workspace acceptance command, executes it in a fresh
// bash subprocess, and renders independent harness evidence.
//
// Fidelity notes:
//   - command validation counts UTF-16 code units and uses JavaScript trim and
//     ASCII regex semantics;
//   - the destructive-command blocklist is intentionally narrow and retains
//     the TS gaps documented in BUGS-KEPT.md;
//   - stdout and stderr are captured separately and concatenated as complete
//     streams (stdout, then stderr), not in operating-system arrival order.
package contract

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

const (
	// ContractPath is the workspace-relative registered contract path.
	ContractPath = ".codeaf/contract.json"

	maxCommandLen    = 500
	defaultTimeoutMs = 90000
	outputTailChars  = 2000
)

var dangerousPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\brm[\x09-\x0d \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]+-[a-z]*r[a-z]*f\b`),
	regexp.MustCompile(`\brm[\x09-\x0d \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]+-[a-z]*f[a-z]*r\b`),
	regexp.MustCompile(`\bgit[\x09-\x0d \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]+push\b`),
}

// Contract is the validated acceptance contract. Optional fields are omitted
// when validateContract's TS object literal does not add them.
//
// Paths and AssertedPaths partition the files a contract touches, and the base
// contract check depends on the distinction:
//
//   - Paths are the files the check LIVES in — the test file or script that
//     encodes the reproduction. These are copied into the detached base
//     worktree so a newly written test can be run against unmodified sources.
//   - AssertedPaths are the files the check asserts ABOUT — the deliverable,
//     the fix target. These are NEVER copied into the base worktree; copying
//     one there hands the base check the answer and makes it pass by
//     construction, which reads as "the bug never reproduced" and re-scopes a
//     real fix into a no-op report. See basecontractcheck.PlanContractCopies.
//
// AssertedPaths is optional and additive: a contract.json written before the
// field existed (or by an agent that ignores it) parses and behaves exactly as
// before, which keeps mid-run resume over a persisted checkpoint working.
type Contract struct {
	Command string `json:"command"`
	// Paths holds the files the check lives in (copied to the base worktree).
	Paths []string `json:"paths,omitempty"`
	// AssertedPaths holds the files the check asserts about (never copied).
	AssertedPaths []string `json:"asserted_paths,omitempty"`
	Note          string   `json:"note,omitempty"`
}

// ContractResult is the subprocess result and captured output tail.
type ContractResult struct {
	Pass       bool    `json:"pass"`
	ExitCode   float64 `json:"exitCode"`
	TimedOut   bool    `json:"timedOut"`
	TailOutput string  `json:"tailOutput"`
	DurationMs float64 `json:"durationMs"`
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

func utf16Len(s string) int {
	return len(utf16.Encode([]rune(s)))
}

func isBackgrounded(command string) bool {
	trimmed := jscompat.Trim(command)
	if !strings.HasSuffix(trimmed, "&") {
		return false
	}
	if len(trimmed) == 1 {
		return true
	}
	return trimmed[len(trimmed)-2] != '&'
}

// ValidateContract performs the pure shape and safety validation. Nil is the
// TS null result.
func ValidateContract(parsed any) *Contract {
	obj, ok := parsed.(map[string]any)
	if !ok || obj == nil {
		return nil
	}
	command, ok := obj["command"].(string)
	if !ok {
		return nil
	}
	trimmed := jscompat.Trim(command)
	if utf16Len(trimmed) == 0 || utf16Len(trimmed) > maxCommandLen {
		return nil
	}
	folded := asciiLower(command)
	for _, pattern := range dangerousPatterns {
		if pattern.MatchString(folded) {
			return nil
		}
	}
	if isBackgrounded(command) {
		return nil
	}

	contract := &Contract{Command: command}
	contract.Paths = stringArrayField(obj, "paths")
	contract.AssertedPaths = stringArrayField(obj, "asserted_paths")
	if note, ok := obj["note"].(string); ok && jscompat.Trim(note) != "" {
		contract.Note = note
	}
	return contract
}

// stringArrayField reads an optional array-of-strings field, dropping
// non-string members and collapsing an empty result to nil so the field stays
// omitted (matching the TS object literal, which never adds an empty array).
func stringArrayField(obj map[string]any, key string) []string {
	rawValues, ok := obj[key].([]any)
	if !ok {
		return nil
	}
	values := []string{}
	for _, raw := range rawValues {
		if value, ok := raw.(string); ok {
			values = append(values, value)
		}
	}
	if len(values) == 0 {
		return nil
	}
	return values
}

// ReadContract reads and validates ContractPath. Filesystem and JSON failures
// return nil and never escape into caller control flow.
func ReadContract(workspace string) *Contract {
	raw, err := os.ReadFile(filepath.Join(workspace, ContractPath))
	if err != nil {
		return nil
	}
	var parsed any
	if err := json.Unmarshal([]byte(decodeUTF8Lossy(raw)), &parsed); err != nil {
		return nil
	}
	return ValidateContract(parsed)
}

type commandOutput struct {
	stdout   string
	stderr   string
	exitCode int
	timedOut bool
	err      error
}

// commandRunner is the minimal process/clock side-effect seam. The pure result
// shaping below is tested independently from the real OS shell.
type commandRunner interface {
	run(workspace, command string, timeout time.Duration) commandOutput
}

type osCommandRunner struct{}

func (osCommandRunner) run(workspace, command string, timeout time.Duration) commandOutput {
	cmd := exec.Command("bash", "-lc", command)
	cmd.Dir = workspace
	cmd.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		return commandOutput{err: err}
	}

	var mu sync.Mutex
	timedOut := false
	timer := time.AfterFunc(timeout, func() {
		mu.Lock()
		timedOut = true
		mu.Unlock()
		_ = cmd.Process.Signal(syscall.SIGTERM)
	})
	err := cmd.Wait()
	timer.Stop()
	mu.Lock()
	didTimeout := timedOut
	mu.Unlock()

	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
			if code < 0 {
				if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
					code = 128 + int(status.Signal())
				}
			}
		} else {
			return commandOutput{err: err, timedOut: didTimeout}
		}
	}
	return commandOutput{
		stdout:   decodeUTF8Lossy(stdout.Bytes()),
		stderr:   decodeUTF8Lossy(stderr.Bytes()),
		exitCode: code,
		timedOut: didTimeout,
	}
}

func tail(s string, limit int) string {
	units := utf16.Encode([]rune(s))
	if len(units) <= limit {
		return s
	}
	return "…" + string(utf16.Decode(units[len(units)-limit:]))
}

func runContractWithRunner(
	workspace string,
	registered Contract,
	timeoutMs int,
	runner commandRunner,
) ContractResult {
	start := time.Now()
	output := runner.run(workspace, registered.Command, time.Duration(timeoutMs)*time.Millisecond)
	duration := float64(time.Since(start).Milliseconds())
	if output.err != nil {
		message := output.err.Error()
		if utf16Len(message) > 200 {
			message = string(utf16.Decode(utf16.Encode([]rune(message))[:200]))
		}
		return ContractResult{
			Pass:       false,
			ExitCode:   1,
			TimedOut:   false,
			TailOutput: "contract command failed to spawn: " + message,
			DurationMs: duration,
		}
	}
	parts := []string{}
	if output.stdout != "" {
		parts = append(parts, output.stdout)
	}
	if output.stderr != "" {
		parts = append(parts, output.stderr)
	}
	combined := strings.Join(parts, "\n")
	code := output.exitCode
	return ContractResult{
		Pass:       code == 0 && !output.timedOut,
		ExitCode:   float64(code),
		TimedOut:   output.timedOut,
		TailOutput: tail(combined, outputTailChars),
		DurationMs: duration,
	}
}

// RunContract executes registered in bash. The optional timeout is in
// milliseconds; omission selects 90 seconds.
func RunContract(workspace string, registered Contract, timeoutMs ...int) ContractResult {
	timeout := defaultTimeoutMs
	if len(timeoutMs) > 0 {
		timeout = timeoutMs[0]
	}
	return runContractWithRunner(workspace, registered, timeout, osCommandRunner{})
}

// ContractEvidenceBlock renders independent harness evidence byte-for-byte.
func ContractEvidenceBlock(registered Contract, result ContractResult) string {
	lines := []string{
		"## Independent contract check (run by the harness, not the agent)",
		"",
		"Command: `" + registered.Command + "`",
		"Result: " + map[bool]string{true: "PASS", false: "FAIL"}[result.Pass] +
			" (exit " + jscompat.FormatNumber(result.ExitCode) +
			map[bool]string{true: ", TIMED OUT", false: ""}[result.TimedOut] + ")",
		"Duration: " + jscompat.FormatNumber(result.DurationMs) + "ms",
	}
	if len(registered.Paths) > 0 {
		lines = append(lines, "Check lives in: "+strings.Join(registered.Paths, ", "))
	}
	lines = append(lines,
		"",
		"Output tail:",
		"```",
		map[bool]string{true: "(no output)", false: result.TailOutput}[result.TailOutput == ""],
		"```",
	)
	return strings.Join(lines, "\n")
}

func decodeUTF8Lossy(b []byte) string {
	var out strings.Builder
	out.Grow(len(b))
	for i := 0; i < len(b); {
		c := b[i]
		if c < 0x80 {
			out.WriteByte(c)
			i++
			continue
		}
		var need int
		var lo, hi byte
		switch {
		case c >= 0xC2 && c <= 0xDF:
			need, lo, hi = 1, 0x80, 0xBF
		case c == 0xE0:
			need, lo, hi = 2, 0xA0, 0xBF
		case c >= 0xE1 && c <= 0xEC:
			need, lo, hi = 2, 0x80, 0xBF
		case c == 0xED:
			need, lo, hi = 2, 0x80, 0x9F
		case c >= 0xEE && c <= 0xEF:
			need, lo, hi = 2, 0x80, 0xBF
		case c == 0xF0:
			need, lo, hi = 3, 0x90, 0xBF
		case c >= 0xF1 && c <= 0xF3:
			need, lo, hi = 3, 0x80, 0xBF
		case c == 0xF4:
			need, lo, hi = 3, 0x80, 0x8F
		default:
			out.WriteRune(utf8.RuneError)
			i++
			continue
		}
		var cp rune
		switch need {
		case 1:
			cp = rune(c & 0x1F)
		case 2:
			cp = rune(c & 0x0F)
		default:
			cp = rune(c & 0x07)
		}
		j, valid := 1, true
		for ; j <= need; j++ {
			l, h := byte(0x80), byte(0xBF)
			if j == 1 {
				l, h = lo, hi
			}
			if i+j >= len(b) || b[i+j] < l || b[i+j] > h {
				valid = false
				break
			}
			cp = cp<<6 | rune(b[i+j]&0x3F)
		}
		if !valid {
			out.WriteRune(utf8.RuneError)
			i += j
			continue
		}
		out.WriteRune(cp)
		i += need + 1
	}
	return out.String()
}
