// Package bare — the four pi wire tools. See truncate.go and editdiff.go for
// the matching/truncation logic this file drives.
package bare

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/processgroup"
)

// Tool is the shared interface between this package and the bare executor.
// Name, Description, and Schema are sent verbatim on the wire; Execute
// returns the model-visible text, an isError flag (for pi's thrown-error
// semantics), and a Go error for harness-level failures only.
type Tool struct {
	Name        string
	Description string
	Schema      json.RawMessage
	Execute     func(ctx context.Context, args json.RawMessage) (text string, isError bool, err error)
}

// AllTools returns all seven pi tools in registry order: read, bash, edit,
// write, grep, find, ls. The first four are active by default (Tools returns
// them); grep, find, and ls are registered but inactive unless activated.
func AllTools(cwd string) []Tool {
	return []Tool{
		newReadTool(cwd),
		newBashTool(cwd),
		newEditTool(cwd),
		newWriteTool(cwd),
		newGrepTool(cwd),
		newFindTool(cwd),
		newLsTool(cwd),
	}
}

// Tools returns the four default active pi tools in registry order: read,
// bash, edit, write. Descriptions and schemas are the exact verbatim pi
// strings. They go on the wire, so the bytes are pinned to pi's source.
func Tools(cwd string) []Tool {
	return []Tool{
		newReadTool(cwd),
		newBashTool(cwd),
		newEditTool(cwd),
		newWriteTool(cwd),
	}
}

// ── wire schemas (verbatim from pi) ────────────────────────────────────────

// The schemas are the exact JSON pi sends on the wire. They are raw literals
// rather than constructed maps so the wire bytes are byte-for-byte identical
// and a test can pin them without ordering ambiguity.

// THE WHOLE-NUMBER ARGUMENTS BELOW ARE DECLARED "integer", NOT "number", AND
// THAT IS THE ONE THING THIS FILE DOES NOT TAKE VERBATIM. JSON has no integers,
// so `"type":"number"` on an argument decoded into a Go int invites the form
// encoding/json refuses — several providers render every whole number as a
// float, and one of them sent `{"limit":10.0}` to a tool eleven times in a
// single turn. `timeout` stays `"number"`: it is a float64 and seconds really
// can be fractional. (internal/session/toolargs.go carries the other half of
// that fix, for the tools aforge adds itself.)
const readSchemaJSON = `{"type":"object","properties":{"path":{"type":"string","description":"Path to the file to read (relative or absolute)"},"offset":{"type":"integer","description":"Line number to start reading from (1-indexed)"},"limit":{"type":"integer","description":"Maximum number of lines to read"}},"required":["path"],"additionalProperties":false}`

const bashSchemaJSON = `{"type":"object","properties":{"command":{"type":"string","description":"Bash command to execute"},"timeout":{"type":"number","description":"Timeout in seconds (optional; 600 when unset)"}},"required":["command"],"additionalProperties":false}`

// BashCeilingSeconds is the bound a foreground bash call runs under when the
// model named no usable timeout of its own — ONE NUMBER, typed once, read by
// this tool, by the session's own law (internal/session.BashCeilingSeconds)
// and by the surface that counts down against it.
//
// There used to be no default here at all — pi's choice for a bare loop, and
// the wrong one for a worker nobody is watching: a headless leaf that ran
// `find / -name "luhn*"` held its node for the whole of the task's deadline,
// two of seven workers at once, with the run's own log saying only that the
// last model call was minutes ago. Ten minutes is longer than almost every
// build, test suite and script a worker runs in one call; a call that is meant
// to outlive it is a background job, which is a decision the model makes
// rather than one the clock makes for it. With a promoter present the bound is
// a handoff (promote.go); with none, the process group is killed and the call
// says so.
const BashCeilingSeconds = 600

// bashDefaultTimeout is the ceiling as a duration, seamed so a test can prove
// the bound without waiting ten minutes for it.
var bashDefaultTimeout = time.Duration(BashCeilingSeconds) * time.Second

const editSchemaJSON = `{"type":"object","properties":{"path":{"type":"string","description":"Path to the file to edit (relative or absolute)"},"edits":{"type":"array","items":{"type":"object","properties":{"oldText":{"type":"string","description":"Exact text for one targeted replacement. It must be unique in the original file and must not overlap with any other edits[].oldText in the same call."},"newText":{"type":"string","description":"Replacement text for this targeted edit."}},"required":["oldText","newText"],"additionalProperties":false},"description":"One or more targeted replacements. Each edit is matched against the original file, not incrementally. Do not include overlapping or nested edits. If two changes touch the same block or nearby lines, merge them into one edit instead."}},"required":["path","edits"],"additionalProperties":false}`

const writeSchemaJSON = `{"type":"object","properties":{"path":{"type":"string","description":"Path to the file to write (relative or absolute)"},"content":{"type":"string","description":"Content to write to the file"}},"required":["path","content"],"additionalProperties":false}`

const grepSchemaJSON = `{"type":"object","properties":{"pattern":{"type":"string","description":"Search pattern (regex or literal string)"},"path":{"type":"string","description":"Directory or file to search (default: current directory)"},"glob":{"type":"string","description":"Filter files by glob pattern, e.g. '*.ts' or '**/*.spec.ts'"},"ignoreCase":{"type":"boolean","description":"Case-insensitive search (default: false)"},"literal":{"type":"boolean","description":"Treat pattern as literal string instead of regex (default: false)"},"context":{"type":"integer","description":"Number of lines to show before and after each match (default: 0)"},"limit":{"type":"integer","description":"Maximum number of matches to return (default: 100)"}},"required":["pattern"],"additionalProperties":false}`

const findSchemaJSON = `{"type":"object","properties":{"pattern":{"type":"string","description":"Glob pattern to match files, e.g. '*.ts', '**/*.json', or 'src/**/*.spec.ts'"},"path":{"type":"string","description":"Directory to search in (default: current directory)"},"limit":{"type":"integer","description":"Maximum number of results (default: 1000)"}},"required":["pattern"],"additionalProperties":false}`

const lsSchemaJSON = `{"type":"object","properties":{"path":{"type":"string","description":"Directory to list (default: current directory)"},"limit":{"type":"integer","description":"Maximum number of entries to return (default: 500)"}},"required":[],"additionalProperties":false}`

const readDescription = "Read the contents of a file. Supports text files and images (jpg, png, gif, webp, bmp). Images are sent as attachments. For text files, output is truncated to 2000 lines or 50KB (whichever is hit first). Use offset/limit for large files. When you need the full file, continue with offset until complete."

const bashDescription = "Execute a bash command in the current working directory. Returns stdout and stderr. Output is truncated to last 2000 lines or 50KB (whichever is hit first). If truncated, full output is saved to a temp file. Optionally provide a timeout in seconds."

const editDescription = "Edit a single file using exact text replacement. Every edits[].oldText must match a unique, non-overlapping region of the original file. If two changes affect the same block or nearby lines, merge them into one edit instead of emitting overlapping edits. Do not include large unchanged regions just to connect distant changes."

const writeDescription = "Write content to a file. Creates the file if it doesn't exist, overwrites if it does. Automatically creates parent directories."

const grepDescription = "Search file contents for a pattern. Returns matching lines with file paths and line numbers. Respects .gitignore. Output is truncated to 100 matches or 50KB (whichever is hit first). Long lines are truncated to 500 chars."

const findDescription = "Search for files by glob pattern. Returns matching file paths relative to the search directory. Respects .gitignore. Output is truncated to 1000 results or 50KB (whichever is hit first)."

const lsDescription = "List directory contents. Returns entries sorted alphabetically, with '/' suffix for directories. Includes dotfiles. Output is truncated to 500 entries or 50KB (whichever is hit first)."

// ── path resolution ────────────────────────────────────────────────────────

// ResolvePath is [resolveToCwd] for the wrappers the session fits around
// these tools: write's append mode reads the file the inner write will land
// on, and resolving that path any other way would be a second, driftable copy
// of pi's normalization.
func ResolvePath(path, cwd string) string { return resolveToCwd(path, cwd) }

// resolveToCwd mirrors pi's path-utils.js:resolveToCwd. It expands ~, strips
// a leading @, normalizes unicode spaces, and resolves the path against cwd.
// On Unix the ~ expansion and unicode-space normalization are the only
// non-obvious parts.
func resolveToCwd(path, cwd string) string {
	normalized := normalizePath(path)
	if filepath.IsAbs(normalized) {
		return filepath.Clean(normalized)
	}
	return filepath.Clean(filepath.Join(cwd, normalized))
}

// normalizePath mirrors pi's normalizePath with normalizeUnicodeSpaces and
// stripAtPrefix options: unicode spaces → regular space, strip leading @,
// expand ~ — plus one rule of this program's own, [stripFileScheme].
func normalizePath(path string) string {
	normalized := stripFileScheme(unicodeSpaces.Replace(path))
	if strings.HasPrefix(normalized, "@") {
		normalized = normalized[1:]
	}
	home, err := os.UserHomeDir()
	if err == nil {
		if normalized == "~" {
			return home
		}
		if strings.HasPrefix(normalized, "~/") {
			return filepath.Join(home, normalized[2:])
		}
	}
	return normalized
}

// stripFileScheme turns a file:// URI back into the path inside it, and leaves
// everything else exactly as it was.
//
// THIS PROGRAM HANDS THE MODEL file:// URIs AND THEN TELLS IT TO READ THEM. A
// row of the project's task record carries its transcript and its artifact as
// URIs rather than bare paths, because one of the two is sometimes a branch and
// a reader should not have to guess which kind of thing it is holding
// (internal/session/task_index.go's taskURI); the "@" pointer block spells them
// the same way. So "read the transcript URI" — which is the whole of how a
// question about work that already ran gets answered — arrives here as
// `file:///…/tasks/20260819-120133_7.jsonl`, which is not an absolute path, gets
// joined to the working directory, and fails on a file nobody named. The scheme
// comes off at the one door every path tool already passes through, so the
// address this program PRINTS is the address its own tools ACCEPT.
//
// NOTHING IS PERCENT-DECODED. Those URIs are minted by concatenation and were
// never encoded, so a `%` in them is a `%` in the filename; decoding would
// corrupt exactly the paths this exists to open.
func stripFileScheme(path string) string {
	const scheme = "file://"
	if len(path) < len(scheme) || !strings.EqualFold(path[:len(scheme)], scheme) {
		return path
	}
	rest := path[len(scheme):]
	// file://localhost/… is the same file as file:///…; any other authority
	// names another machine, and this program has no hands there — so it is left
	// spelled as it was, to fail as the path it is rather than silently reading
	// something local.
	if trimmed := strings.TrimPrefix(rest, "localhost"); strings.HasPrefix(trimmed, "/") {
		return trimmed
	}
	if !strings.HasPrefix(rest, "/") {
		return path
	}
	return rest
}

// unicodeSpaces replaces the same set pi's paths.js does: U+00A0,
// U+2000-U+200A, U+202F, U+205F, U+3000.
var unicodeSpaces = strings.NewReplacer(
	"\u00A0", " ",
	"\u2000", " ",
	"\u2001", " ",
	"\u2002", " ",
	"\u2003", " ",
	"\u2004", " ",
	"\u2005", " ",
	"\u2006", " ",
	"\u2007", " ",
	"\u2008", " ",
	"\u2009", " ",
	"\u200A", " ",
	"\u202F", " ",
	"\u205F", " ",
	"\u3000", " ",
)

// ── read tool ─────────────────────────────────────────────────────────────

func newReadTool(cwd string) Tool {
	return Tool{
		Name:        "read",
		Description: readDescription,
		Schema:      json.RawMessage(readSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var p struct {
				Path   string `json:"path"`
				Offset *int   `json:"offset"`
				Limit  *int   `json:"limit"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}
			absPath := resolveToCwd(p.Path, cwd)
			data, err := os.ReadFile(absPath)
			if err != nil {
				return "Error reading file: " + err.Error(), true, nil
			}
			textContent := string(data)
			allLines := strings.Split(textContent, "\n")
			totalFileLines := len(allLines)

			startLine := 0
			if p.Offset != nil {
				startLine = *p.Offset - 1
				if startLine < 0 {
					startLine = 0
				}
			}
			startLineDisplay := startLine + 1

			if startLine >= len(allLines) {
				offset := startLine + 1
				if p.Offset != nil {
					offset = *p.Offset
				}
				return fmt.Sprintf("Offset %d is beyond end of file (%d lines total)", offset, len(allLines)), true, nil
			}

			var selectedContent string
			userLimitedLines := -1
			if p.Limit != nil {
				endLine := startLine + *p.Limit
				if endLine > len(allLines) {
					endLine = len(allLines)
				}
				selectedContent = strings.Join(allLines[startLine:endLine], "\n")
				userLimitedLines = endLine - startLine
			} else {
				selectedContent = strings.Join(allLines[startLine:], "\n")
			}

			truncation := truncateHead(selectedContent)

			if truncation.firstLineExceedsLimit {
				firstLineSize := formatSize(byteLength(allLines[startLine]))
				return fmt.Sprintf("[Line %d is %s, exceeds %s limit. Use bash: sed -n '%dp' %s | head -c %d]", startLineDisplay, firstLineSize, formatSize(defaultMaxBytes), startLineDisplay, p.Path, defaultMaxBytes), false, nil
			}

			if truncation.truncated {
				endLineDisplay := startLineDisplay + truncation.outputLines - 1
				nextOffset := endLineDisplay + 1
				if truncation.truncatedBy == "lines" {
					return truncation.content + fmt.Sprintf("\n\n[Showing lines %d-%d of %d. Use offset=%d to continue.]", startLineDisplay, endLineDisplay, totalFileLines, nextOffset), false, nil
				}
				return truncation.content + fmt.Sprintf("\n\n[Showing lines %d-%d of %d (%s limit). Use offset=%d to continue.]", startLineDisplay, endLineDisplay, totalFileLines, formatSize(defaultMaxBytes), nextOffset), false, nil
			}

			if userLimitedLines >= 0 && startLine+userLimitedLines < len(allLines) {
				remaining := len(allLines) - (startLine + userLimitedLines)
				nextOffset := startLine + userLimitedLines + 1
				return truncation.content + fmt.Sprintf("\n\n[%d more lines in file. Use offset=%d to continue.]", remaining, nextOffset), false, nil
			}

			return truncation.content, false, nil
		},
	}
}

// ── bash tool ──────────────────────────────────────────────────────────────

const maxTimeoutMs = 2147483647

func newBashTool(cwd string) Tool {
	return Tool{
		Name:        "bash",
		Description: bashDescription,
		Schema:      json.RawMessage(bashSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var p struct {
				Command string   `json:"command"`
				Timeout *float64 `json:"timeout"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}

			// Validate timeout.
			var timeoutMs int
			timeoutSet := false
			if p.Timeout != nil {
				timeoutSet = true
				tv := *p.Timeout
				if !isFinite(tv) || tv <= 0 {
					return "Invalid timeout: must be a finite number of seconds", true, nil
				}
				ms := int(tv * 1000)
				if ms > maxTimeoutMs {
					return fmt.Sprintf("Invalid timeout: maximum is %s seconds", trimFloat(float64(maxTimeoutMs)/1000)), true, nil
				}
				timeoutMs = ms
			}
			// A CALL THAT NAMED NO TIMEOUT IS BOUNDED, NOT UNBOUNDED — see
			// BashCeilingSeconds. A figure the model did set is honoured as it
			// always was, however large.
			if !timeoutSet {
				timeoutSet = true
				timeoutMs = int(bashDefaultTimeout / time.Millisecond)
			}

			// Check cwd exists.
			if _, err := os.Stat(cwd); os.IsNotExist(err) {
				return fmt.Sprintf("Working directory does not exist: %s\nCannot execute bash commands.", cwd), true, nil
			}

			// THE OUTPUT HAS TO EXIST BEFORE THE COMMAND ENDS, which is what
			// [StreamingShell] and [StreamingEnv] buy: a command whose output is
			// held in a 4KB buffer until it exits is a command that looks dead to
			// anybody reading its log, and a long one gets killed for it
			// (streaming.go states the whole case).
			shell, shellArgs := StreamingShell(p.Command)

			// THE COMMAND IS NOT BOUND TO THE CONTEXT, and the watcher below
			// does the binding by hand. exec.CommandContext's own watcher kills
			// the group on cancel and then force-closes the output pipes after
			// WaitDelay — which is exactly right for a call that ends with its
			// turn, and fatal for one that has been PROMOTED into a job that is
			// supposed to outlive it (promote.go). The cancel semantics are
			// unchanged: SIGKILL to the whole group, the moment ctx is done.
			cmd := exec.Command(shell, shellArgs...)
			cmd.Dir = cwd
			cmd.Env = StreamingEnv()
			processgroup.Configure(cmd)
			// A COMMAND THAT LEAVES A BACKGROUND CHILD SHARING ITS STDOUT MUST
			// STILL COST ITS TIMEOUT AND NOTHING MORE. Killing the shell is not
			// enough on its own: Stdout and Stderr below are an in-process
			// writer, so Go hands the child a pipe, and Wait blocks until EVERY
			// holder of the write end is gone — a grandchild that escaped the
			// process group (its own setsid, a daemon that re-parented) holds it
			// open forever, and the caller waiting on this call waits with it.
			// The kill reaches the whole group rather than the shell alone, and
			// WaitDelay force-closes the pipes shortly after the shell itself is
			// gone for anything that survived. The defence is internal/exec's,
			// verbatim (tools.go, jobs.go), for a fault that was observed there
			// first.
			cmd.WaitDelay = 3 * time.Second

			// Interleave stdout+stderr in arrival order. Setting both
			// cmd.Stdout and cmd.Stderr to the same writer lets Go's exec
			// package copy both streams into one buffer, preserving the
			// arrival order the way pi does with a single onData callback
			// for both child.stdout and child.stderr. Using separate pipes
			// with goroutines raced: cmd.Wait closes the pipes before the
			// goroutines drain the last chunk.
			acc := newOutputAccumulator()
			cmd.Stdout = acc
			cmd.Stderr = acc

			if err := cmd.Start(); err != nil {
				return "Failed to start command: " + err.Error(), true, nil
			}

			// The call is now a thing somebody else could take (promote.go).
			// Everything that can end it goes through this handle from here on,
			// so exactly one of the four racers wins.
			call := newBashCall(p.Command, cmd, acc, ctx)
			over := make(chan struct{})
			defer close(over)
			watchCancel(ctx, call, over)

			promoter := bashPromoterFrom(ctx)
			if promoter != nil {
				if finished := promoter.Started(call); finished != nil {
					defer finished()
				}
			}

			// ONE Wait, IN A GOROUTINE, because the call may be handed over
			// while the process is still running and Go permits exactly one
			// wait per command. The exit code goes to whoever adopts the call;
			// the error comes back here for the ordinary ending.
			waitCh := make(chan error, 1)
			go func() {
				err := cmd.Wait()
				call.exit <- exitCodeFromWait(err)
				waitCh <- err
			}()

			// THE TIMEOUT IS A HANDOFF WHERE SOMEBODY IS THERE TO TAKE IT.
			// With no promoter it is what it always was — the process group is
			// killed and the call says so.
			var timer *time.Timer
			if timeoutSet {
				timer = time.AfterFunc(time.Duration(timeoutMs)*time.Millisecond, func() {
					if promoter != nil && promoter.TimedOut(call) {
						return
					}
					if !call.closeTimedOut() {
						return
					}
					killProcessGroup(cmd)
				})
			}

			var waitErr error
			select {
			case waitErr = <-waitCh:
				// A call adopted in the same breath as its own exit keeps the
				// adoption: the process is the adopter's now, and the exit code
				// is already on its way to it.
				if answer, isError, adopted := call.close(); adopted {
					if timer != nil {
						timer.Stop()
					}
					return answer, isError, nil
				}
			case <-call.promoted:
				if timer != nil {
					timer.Stop()
				}
				answer, isError, _ := call.close()
				return answer, isError, nil
			}
			if timer != nil {
				timer.Stop()
			}
			acc.finish()
			snapshot := acc.snapshot()
			acc.closeTempFile()
			text := snapshot.content
			if text == "" {
				text = "(no output)"
			}
			if snapshot.truncated {
				text += formatBashTruncationFooter(snapshot)
			}

			if call.wasTimedOut() {
				// The bound that was APPLIED, which is the model's own figure
				// or the ceiling it was given in place of one.
				timeoutSecs := timeoutMs / 1000
				return appendStatus(text, fmt.Sprintf("Command timed out after %d seconds", timeoutSecs)), true, nil
			}

			if ctx.Err() == context.Canceled {
				return appendStatus(text, "Command aborted"), true, nil
			}

			exitCode := exitCodeFromWait(waitErr)
			if exitCode != 0 && exitCode != -1 {
				return appendStatus(text, fmt.Sprintf("Command exited with code %d", exitCode)), true, nil
			}

			return text, false, nil
		},
	}
}

// appendStatus mirrors pi's bash.js:appendStatus: join text and status with
// "\n\n", or just status if text is empty.
func appendStatus(text, status string) string {
	if text == "" {
		return status
	}
	return text + "\n\n" + status
}

// exitCodeFromWait extracts the exit code from a cmd.Wait() error. Returns
// -1 if the process was killed or the code is unavailable.
func exitCodeFromWait(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return -1
}

// getShellConfig mirrors pi's shell.js:getShellConfig on Unix: /bin/bash if
// it exists, then bash on PATH, then fallback to sh. Returns the shell path
// and args (["-c"]).
func getShellConfig() (string, []string) {
	if _, err := os.Stat("/bin/bash"); err == nil {
		return "/bin/bash", []string{"-c"}
	}
	if bash, err := exec.LookPath("bash"); err == nil {
		return bash, []string{"-c"}
	}
	return "sh", []string{"-c"}
}

// killProcessGroup sends SIGKILL to the process group, mirroring pi's
// killProcessTree.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	if err := processgroup.Kill(cmd.Process.Pid); err != nil {
		_ = cmd.Process.Kill()
	}
}

// isFinite checks whether a float64 is finite (not NaN or Inf), matching
// JS Number.isFinite.
func isFinite(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

// ── edit tool ──────────────────────────────────────────────────────────────

func newEditTool(cwd string) Tool {
	return Tool{
		Name:        "edit",
		Description: editDescription,
		Schema:      json.RawMessage(editSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			// prepareEditArguments: parse the input, accepting string-encoded
			// edits arrays and legacy oldText/newText.
			edits, path, err := prepareEditArguments(args)
			if err != nil {
				return err.Error(), true, nil
			}
			if len(edits) == 0 {
				return "Edit tool input is invalid. edits must contain at least one replacement.", true, nil
			}

			absPath := resolveToCwd(path, cwd)

			return withFileMutationQueue(absPath, func() (string, bool, error) {
				if ctx.Err() != nil {
					return "Operation aborted", true, nil
				}

				// Check if file exists.
				if _, err := os.Stat(absPath); err != nil {
					if ctx.Err() != nil {
						return "Operation aborted", true, nil
					}
					errMsg := "ENOENT"
					if os.IsNotExist(err) {
						errMsg = "Error code: ENOENT"
					} else {
						errMsg = err.Error()
					}
					return fmt.Sprintf("Could not edit file: %s. %s.", path, errMsg), true, nil
				}
				if ctx.Err() != nil {
					return "Operation aborted", true, nil
				}

				// Read the file.
				rawContent, err := os.ReadFile(absPath)
				if err != nil {
					return fmt.Sprintf("Could not process edit: %s. %s", path, err.Error()), true, nil
				}
				if ctx.Err() != nil {
					return "Operation aborted", true, nil
				}

				// Strip BOM, detect EOL, normalize to LF.
				bom, content := stripBom(string(rawContent))
				originalEnding := detectLineEnding(content)
				normalizedContent := normalizeToLF(content)

				result, err := applyEditsToNormalizedContent(normalizedContent, edits, path)
				if err != nil {
					return err.Error(), true, nil
				}
				if ctx.Err() != nil {
					return "Operation aborted", true, nil
				}

				finalContent := bom + restoreLineEndings(result.newContent, originalEnding)
				if err := os.WriteFile(absPath, []byte(finalContent), 0644); err != nil {
					return fmt.Sprintf("Could not process edit: %s. %s", path, err.Error()), true, nil
				}
				if ctx.Err() != nil {
					return "Operation aborted", true, nil
				}

				return fmt.Sprintf("Successfully replaced %d block(s) in %s.", len(edits), path), false, nil
			})
		},
	}
}

// prepareEditArguments mirrors pi's edit.js:prepareEditArguments. It accepts:
//   - edits as a JSON array (normal case)
//   - edits as a JSON string (some models send this) → parse to array
//   - legacy top-level oldText/newText → merge into edits[]
//
// Returns the edits list and the path.
func prepareEditArguments(args json.RawMessage) ([]editPair, string, error) {
	// Decode into a flexible structure.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(args, &raw); err != nil {
		return nil, "", err
	}

	var path string
	if p, ok := raw["path"]; ok {
		if err := json.Unmarshal(p, &path); err != nil {
			return nil, "", err
		}
	}

	// Parse edits — may be an array or a JSON string encoding an array.
	var edits []editPair
	if e, ok := raw["edits"]; ok {
		// Try as array first.
		var arr []struct {
			OldText string `json:"oldText"`
			NewText string `json:"newText"`
		}
		if err := json.Unmarshal(e, &arr); err == nil && arr != nil {
			edits = make([]editPair, len(arr))
			for i, a := range arr {
				edits[i] = editPair{oldText: a.OldText, newText: a.NewText}
			}
		} else {
			// Try as a JSON string encoding an array.
			var s string
			if err := json.Unmarshal(e, &s); err == nil {
				if err := json.Unmarshal([]byte(s), &arr); err == nil && arr != nil {
					edits = make([]editPair, len(arr))
					for i, a := range arr {
						edits[i] = editPair{oldText: a.OldText, newText: a.NewText}
					}
				}
			}
		}
	}

	// Legacy oldText/newText at top level — merge into edits.
	var legacyOld, legacyNew string
	if v, ok := raw["oldText"]; ok {
		_ = json.Unmarshal(v, &legacyOld)
	}
	if v, ok := raw["newText"]; ok {
		_ = json.Unmarshal(v, &legacyNew)
	}
	if legacyOld != "" || legacyNew != "" {
		edits = append(edits, editPair{oldText: legacyOld, newText: legacyNew})
	}

	return edits, path, nil
}

// ── write tool ─────────────────────────────────────────────────────────────

func newWriteTool(cwd string) Tool {
	return Tool{
		Name:        "write",
		Description: writeDescription,
		Schema:      json.RawMessage(writeSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var p struct {
				Path    string `json:"path"`
				Content string `json:"content"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}

			absPath := resolveToCwd(p.Path, cwd)

			return withFileMutationQueue(absPath, func() (string, bool, error) {
				if ctx.Err() != nil {
					return "Operation aborted", true, nil
				}
				dir := filepath.Dir(absPath)
				if err := os.MkdirAll(dir, 0755); err != nil {
					return err.Error(), true, nil
				}
				if ctx.Err() != nil {
					return "Operation aborted", true, nil
				}
				if err := os.WriteFile(absPath, []byte(p.Content), 0644); err != nil {
					return err.Error(), true, nil
				}
				if ctx.Err() != nil {
					return "Operation aborted", true, nil
				}
				// pi uses content.length which is the JS string char length
				// (UTF-16 code units), NOT the byte length.
				return fmt.Sprintf("Successfully wrote %d bytes to %s", utf16Length(p.Content), p.Path), false, nil
			})
		},
	}
}

// utf16Length returns the number of UTF-16 code units in a string, matching
// JS's string.length. Runes outside the BMP count as 2 (surrogate pair).
func utf16Length(s string) int {
	count := 0
	for _, r := range s {
		if r > 0xFFFF {
			count += 2
		} else {
			count++
		}
	}
	return count
}

// ── per-realpath mutation serialization ────────────────────────────────────

// fileMutationQueues serializes file mutation operations targeting the same
// file (by resolved realpath). Operations for different files run in
// parallel. This mirrors pi's file-mutation-queue.js.
var (
	mutationMu     sync.Mutex
	mutationQueues = map[string]chan struct{}{}
)

func withFileMutationQueue(filePath string, fn func() (string, bool, error)) (string, bool, error) {
	key := mutationQueueKey(filePath)

	ch := mutationQueueFor(key)

	ch <- struct{}{}
	defer func() {
		<-ch
		releaseMutationQueue(key, ch)
	}()

	return fn()
}

// mutationQueueFor hands back the queue for one file, making it on first use.
//
// It is its own function so the map's lock is released by a defer directly
// under the acquisition: a panic between the two — a map grown while another
// goroutine reads it, anything — would otherwise leave every later writer to
// any file blocked on a mutex nobody holds.
func mutationQueueFor(key string) chan struct{} {
	mutationMu.Lock()
	defer mutationMu.Unlock()
	ch, ok := mutationQueues[key]
	if !ok {
		ch = make(chan struct{}, 1)
		mutationQueues[key] = ch
	}
	return ch
}

// releaseMutationQueue forgets a queue nobody is waiting on. The channel is
// passed back in rather than looked up again because the map may already hold a
// different one for this key, and deleting that would strand its waiters.
func releaseMutationQueue(key string, ch chan struct{}) {
	mutationMu.Lock()
	defer mutationMu.Unlock()
	if len(ch) == 0 && mutationQueues[key] == ch {
		delete(mutationQueues, key)
	}
}

// mutationQueueKey mirrors pi's getMutationQueueKey: resolve the path, then
// realpath it; if the file does not exist yet, fall back to the resolved
// path.
func mutationQueueKey(filePath string) string {
	resolved := filepath.Clean(filePath)
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return resolved
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	return abs
}

// ── bash output accumulator ────────────────────────────────────────────────

// outputAccumulator mirrors pi's OutputAccumulator: a streaming tail
// accumulator with a rolling byte buffer, line/byte caps, and temp file spill
// when the output exceeds the caps. The snapshot runs truncateTail on the
// accumulated tail text.
type outputAccumulator struct {
	maxBytes         int
	maxLines         int
	maxRollingBytes  int
	tailText         []byte
	totalDecoded     int
	totalLines       int
	completedLines   int
	currentLineBytes int
	hasOpenLine      bool
	finished         bool
	tempFilePath     string
	tempFile         *os.File
	// mirror is where a PROMOTED call's output goes (promote.go). Once it is
	// set, this accumulator stops accumulating altogether: the tool result has
	// already been answered, nobody will ask for another snapshot, and the one
	// place the rest of the output belongs is the adopter's own log. Two files
	// for one command would be two answers to "where is the rest of it".
	mirror io.Writer
	mu     sync.Mutex
}

// Write implements io.Writer so both cmd.Stdout and cmd.Stderr can be set
// to the same accumulator. Go's exec package copies each stream via its
// own goroutine, so the mutex serializes concurrent writes from both.
func (a *outputAccumulator) Write(data []byte) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.append(data)
	return len(data), nil
}

func newOutputAccumulator() *outputAccumulator {
	return &outputAccumulator{
		maxBytes:        defaultMaxBytes,
		maxLines:        defaultMaxLines,
		maxRollingBytes: defaultMaxBytes * 2,
	}
}

// mirrorTo redirects everything from here on into w, after replaying what has
// already arrived so the adopter's log opens with the output the person was
// already watching. See [BashCall.Attach] for what the replay can and cannot
// promise.
func (a *outputAccumulator) mirrorTo(w io.Writer) {
	if w == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.tailText) > 0 {
		_, _ = w.Write(a.tailText)
	}
	a.mirror = w
	// The temp spill ends here for the same reason the accumulation does: the
	// full log is the adopter's file now.
	if a.tempFile != nil {
		a.tempFile.Close()
		a.tempFile = nil
	}
}

func (a *outputAccumulator) append(data []byte) {
	if a.mirror != nil {
		_, _ = a.mirror.Write(data)
		return
	}
	if a.finished {
		return
	}
	a.totalDecoded += len(data)
	a.tailText = append(a.tailText, data...)
	if len(a.tailText) > a.maxRollingBytes*2 {
		a.trimTail()
	}

	// Counted over the bytes as they arrived. This used to run over a string
	// copy of the chunk, which bought nothing — the only questions asked of it
	// are how many newlines there are and how far the last one is from the end,
	// and both are byte questions — while paying a full copy of every pipe read
	// of a command that may be writing megabytes.
	newlines := bytes.Count(data, []byte{'\n'})
	if newlines == 0 {
		a.currentLineBytes += len(data)
		a.hasOpenLine = true
	} else {
		a.completedLines += newlines
		open := len(data) - bytes.LastIndexByte(data, '\n') - 1
		a.currentLineBytes = open
		a.hasOpenLine = open > 0
	}
	a.totalLines = a.completedLines
	if a.hasOpenLine {
		a.totalLines++
	}

	if a.shouldUseTempFile() {
		a.ensureTempFile()
		if a.tempFile != nil {
			a.tempFile.Write(data)
		}
	}
}

func (a *outputAccumulator) finish() {
	if a.finished {
		return
	}
	a.finished = true
	if a.shouldUseTempFile() {
		a.ensureTempFile()
	}
}

type accumulatorSnapshot struct {
	content         string
	truncated       bool
	truncatedBy     string
	totalLines      int
	outputLines     int
	outputBytes     int
	lastLinePartial bool
	lastLineBytes   int
	fullOutputPath  string
}

func (a *outputAccumulator) snapshot() accumulatorSnapshot {
	snapshotText := a.getSnapshotText()
	tailTrunc := truncateTail(snapshotText)

	truncated := a.totalLines > a.maxLines || a.totalDecoded > a.maxBytes
	truncatedBy := ""
	if truncated {
		truncatedBy = tailTrunc.truncatedBy
		if truncatedBy == "" {
			if a.totalDecoded > a.maxBytes {
				truncatedBy = "bytes"
			} else {
				truncatedBy = "lines"
			}
		}
	}

	if truncated {
		a.ensureTempFile()
	}

	return accumulatorSnapshot{
		content:         tailTrunc.content,
		truncated:       truncated,
		truncatedBy:     truncatedBy,
		totalLines:      a.totalLines,
		outputLines:     tailTrunc.outputLines,
		outputBytes:     tailTrunc.outputBytes,
		lastLinePartial: tailTrunc.lastLinePartial,
		lastLineBytes:   a.currentLineBytes,
		fullOutputPath:  a.tempFilePath,
	}
}

func (a *outputAccumulator) getSnapshotText() string {
	return string(a.tailText)
}

func (a *outputAccumulator) trimTail() {
	if len(a.tailText) <= a.maxRollingBytes {
		return
	}
	start := len(a.tailText) - a.maxRollingBytes
	for start < len(a.tailText) && (a.tailText[start]&0xc0) == 0x80 {
		start++
	}
	a.tailText = a.tailText[start:]
}

func (a *outputAccumulator) shouldUseTempFile() bool {
	return a.totalDecoded > a.maxBytes || a.totalLines > a.maxLines
}

func (a *outputAccumulator) ensureTempFile() {
	if a.tempFilePath != "" {
		return
	}
	id := make([]byte, 8)
	rand.Read(id)
	a.tempFilePath = filepath.Join(os.TempDir(), "pi-bash-"+hex.EncodeToString(id)+".log")
	f, err := os.Create(a.tempFilePath)
	if err == nil {
		a.tempFile = f
	}
}

func (a *outputAccumulator) closeTempFile() {
	if a.tempFile != nil {
		a.tempFile.Close()
		a.tempFile = nil
	}
}

// formatBashTruncationFooter renders the exact pi footer for a truncated bash
// snapshot. Called by the bash Execute when snapshot.truncated is true.
func formatBashTruncationFooter(snap accumulatorSnapshot) string {
	if !snap.truncated {
		return ""
	}
	startLine := snap.totalLines - snap.outputLines + 1
	endLine := snap.totalLines
	if snap.lastLinePartial {
		return fmt.Sprintf("\n\n[Showing last %s of line %d (line is %s). Full output: %s]", formatSize(snap.outputBytes), endLine, formatSize(snap.lastLineBytes), snap.fullOutputPath)
	}
	if snap.truncatedBy == "lines" {
		return fmt.Sprintf("\n\n[Showing lines %d-%d of %d. Full output: %s]", startLine, endLine, snap.totalLines, snap.fullOutputPath)
	}
	return fmt.Sprintf("\n\n[Showing lines %d-%d of %d (%s limit). Full output: %s]", startLine, endLine, snap.totalLines, formatSize(defaultMaxBytes), snap.fullOutputPath)
}

// _ keeps strconv imported for potential future use.
var _ = strconv.Itoa
