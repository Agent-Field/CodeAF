// Package verification keeps the exit of a check apart from the shell pipeline
// that presents its output. A grep finding no failures is not a failing test.
package verification

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/shell"
)

// FailurePrefix marks a guard rejection in persisted stage notes.
const FailurePrefix = "Unresolved verification command failed:"

// SetupFailurePrefix identifies a tool call that never ran because its check
// observer could not be fitted. A model's later success claim cannot erase it.
const SetupFailurePrefix = "Could not record verification:"

const originalCommandPrefix = "# codeaf-check-original:"
const observerShellGuard = `if [ -n "${BASH_VERSION-}" ]; then`

// Check is an observed invocation, not a verdict inferred from its output.
// Context participates in identity: passing another package, directory, flag
// set or target platform must never erase an unresolved failure.
type Check struct {
	Before             Snapshot   `json:"before,omitempty"`
	After              Snapshot   `json:"after,omitempty"`
	Command            string     `json:"command"`
	Cwd                string     `json:"cwd"`
	Args               []string   `json:"args"`
	Context            []string   `json:"context"`
	Exit               *int       `json:"exit"`
	ID                 string     `json:"id,omitempty"`
	CallID             string     `json:"call_id,omitempty"`
	Source             string     `json:"source,omitempty"`
	OwnerRoot          string     `json:"owner_root,omitempty"`
	Diagnostic         bool       `json:"diagnostic,omitempty"`
	Problem            string     `json:"problem,omitempty"`
	StartedAt          time.Time  `json:"started_at,omitempty"`
	CompletedAt        *time.Time `json:"completed_at,omitempty"`
	Inherited          bool       `json:"inherited,omitempty"`
	SupersededBy       string     `json:"superseded_by,omitempty"`
	SupersessionReason string     `json:"supersession_reason,omitempty"`
	order              int64
}

// CurrentCheck is bounded status metadata, never a command or checkout path.
type CurrentCheck struct {
	ID          string
	CallID      string
	Purpose     string
	StartedAt   time.Time
	CompletedAt *time.Time
	Outcome     string
}

func Current(transcript string) ([]CurrentCheck, error) {
	checks, err := Read(transcript)
	var current []CurrentCheck
	for _, c := range checks {
		if c.Diagnostic || c.Inherited {
			continue
		}
		purpose := "verification"
		words := checkProgramWords(c.Args)
		if len(words) > 0 {
			purpose = filepath.Base(words[0])
			if len(words) > 1 && !strings.HasPrefix(words[1], "-") {
				purpose += " " + words[1]
			}
		}
		outcome := "running"
		if c.Problem != "" {
			outcome = "instrumentation_error"
		} else if c.Exit != nil {
			outcome = "failed"
			if *c.Exit == 0 {
				outcome = "passed"
			}
		}
		current = append(current, CurrentCheck{ID: c.ID, CallID: c.CallID, Purpose: purpose, StartedAt: c.StartedAt, CompletedAt: c.CompletedAt, Outcome: outcome})
	}
	return current, err
}

func (c Check) Key() string {
	if len(c.Args) == 0 {
		return "incomplete:" + c.Source + ":" + c.ID
	}
	args := c.Args
	if len(args) > 1 && args[0] == "go" && args[1] == "test" {
		args = make([]string, 0, len(c.Args))
		for i, arg := range c.Args {
			if arg == "-args" {
				args = append(args, c.Args[i:]...)
				break
			}
			// Verbose output does not change which tests or target ran.
			verbose := arg == "-v" || arg == "-v=true" || arg == "-v=false"
			// A value such as -run '-v' is a test selector, not verbosity.
			// Ambiguous flag/value pairs stay exact rather than weakening scope.
			value := i > 2 && strings.HasPrefix(c.Args[i-1], "-") && !strings.Contains(c.Args[i-1], "=") && c.Args[i-1] != "-v"
			if !verbose || value {
				args = append(args, arg)
			}
		}
	}
	raw, _ := json.Marshal([]any{c.Cwd, c.Context, args})
	return string(raw)
}

func (c Check) Label() string {
	parts := make([]string, len(c.Args))
	for i, arg := range c.Args {
		parts[i] = quote(arg)
	}
	return strings.Join(parts, " ") + " (in " + c.Cwd + ")"
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

// Prepare fits observers only to factory stage calls. The original command is
// retained outside the workspace; each real invocation gets its own file, so
// concurrent pipeline children cannot interleave records. Shell functions use
// command to invoke the original executable and return its ORIGINAL status.
// They do not change pipefail, filter output, or convert failures to success.
func Prepare(transcript, command string) (string, error) {
	return PrepareIn(transcript, "", command)
}

// PrepareIn assigns an explicit canonical worktree boundary to this tool call.
// Descendant fixtures may still emit diagnostics, but cannot create obligations
// for a different worktree. Compatibility callers without an owner retain every
// old obligation rather than guessing ownership from a temporary path's name.
func PrepareIn(transcript, ownerDir, command string) (string, error) {
	return PrepareCallIn(transcript, ownerDir, command, "")
}

// PrepareCallIn binds observer metadata to the native tool invocation that owns
// it. CallID is provenance, not physical liveness: status consumers must match
// it against their executor's currently owned work before advertising running.
func PrepareCallIn(transcript, ownerDir, command, callID string) (string, error) {
	if transcript == "" {
		return command, nil
	}
	root := transcript + ".checks"
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp(root, "call-")
	if err != nil {
		return "", err
	}
	if callID != "" {
		if err := atomicWrite(filepath.Join(dir, "call-id"), []byte(callID)); err != nil {
			return "", err
		}
	}
	raw, _ := json.Marshal(command)
	if err := atomicWrite(filepath.Join(dir, "command.json"), raw); err != nil {
		return "", err
	}
	owner := ""
	if ownerDir != "" {
		owner, err = canonicalOwner(ownerDir)
		if err != nil {
			return "", err
		}
		if err = atomicWrite(filepath.Join(dir, "owner"), []byte(owner)); err != nil {
			return "", err
		}
	}
	// The small context is execution scope, never the environment wholesale:
	// secrets must not be copied into command receipts. Flags and target/runtime
	// settings distinguish checks that happen to have identical argv.
	// The Unix shell resolver also permits sh without Bash. That shell keeps
	// its original execution path and journal evidence; export -f is Bash-only.
	prefix := observerShellGuard + "\n" + shell.CargoPathSetup() + worktreeStateFunction + `
_codeaf_check() {
 local _codeaf_file _codeaf_status _codeaf_cwd _codeaf_root _codeaf_scope
 _codeaf_file=$(command mktemp ` + quote(shell.ListPath(filepath.Join(dir, "check-XXXXXX"))) + `) || return 125
 _codeaf_cwd=$(pwd -P) || return 125
 _codeaf_scope=owned
 if [ -n ` + quote(owner) + ` ]; then
  case "$_codeaf_cwd/" in ` + quote(owner+"/") + `*) ;; *) _codeaf_scope=diagnostic;; esac
  _codeaf_root=$(command git rev-parse --show-toplevel 2>/dev/null) || _codeaf_root=
  if [ -n "$_codeaf_root" ]; then
   _codeaf_root=$(cd "$_codeaf_root" && pwd -P) || return 125
   if [ "$_codeaf_root" != ` + quote(owner) + ` ]; then _codeaf_scope=diagnostic; fi
  fi
 fi
 printf '%s\n' "$_codeaf_scope" > "$_codeaf_file.scope.tmp" && ` + atomicMoveCommand() + ` "$_codeaf_file.scope.tmp" "$_codeaf_file.scope" || return 125
 printf '%s\0' "$_codeaf_cwd" "${GOOS-}" "${GOARCH-}" "${GOFLAGS-}" "${CGO_ENABLED-}" "${GOWORK-}" "${GORACE-}" "${NODE_ENV-}" "${PYTEST_ADDOPTS-}" "$@" > "$_codeaf_file.tmp" && ` + atomicMoveCommand() + ` "$_codeaf_file.tmp" "$_codeaf_file" || return 125
 if ! _codeaf_worktree_state > "$_codeaf_file.before.tmp"; then : > "$_codeaf_file.before.tmp"; fi
 ` + atomicMoveCommand() + ` "$_codeaf_file.before.tmp" "$_codeaf_file.before" || return 125
 if ( export -nf ` + observerNames() + `; command "$@" ); then _codeaf_status=0; else _codeaf_status=$?; fi
 if ! _codeaf_worktree_state > "$_codeaf_file.after.tmp"; then : > "$_codeaf_file.after.tmp"; fi
 ` + atomicMoveCommand() + ` "$_codeaf_file.after.tmp" "$_codeaf_file.after" || return 125
 printf '%s\n' "$_codeaf_status" > "$_codeaf_file.exit.tmp" && ` + atomicMoveCommand() + ` "$_codeaf_file.exit.tmp" "$_codeaf_file.exit" || return 125
 return "$_codeaf_status"
}
` + observerFunctions() + `fi
`
	return originalCommandPrefix + base64.StdEncoding.EncodeToString([]byte(command)) + "\n" + prefix + command, nil
}

// OriginalCommand removes our observer metadata for display only. The command
// held for execution and rerun remains untouched. Ordinary or malformed inputs
// are preserved rather than interpreting their first comment as executable data.
func OriginalCommand(command string) string {
	if !strings.HasPrefix(command, originalCommandPrefix) {
		return command
	}
	encoded, rest, ok := strings.Cut(strings.TrimPrefix(command, originalCommandPrefix), "\n")
	if !ok || !strings.HasPrefix(rest, observerShellGuard+"\n") {
		return command
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return command
	}
	return string(raw)
}

// Read returns checks in completion order. A command killed before its status
// was written remains unknown, rather than becoming an accidental pass. A
// stage resumed from before these receipts existed still uses journal evidence.
func Read(transcript string) ([]Check, error) {
	root := transcript + ".checks"
	var inherited []Check
	raw, importErr := os.ReadFile(filepath.Join(root, "inherited.json"))
	if importErr == nil {
		importErr = json.Unmarshal(raw, &inherited)
		for i := range inherited {
			inherited[i].Inherited = true
			inherited[i].order = inherited[i].StartedAt.UnixNano()
			if inherited[i].CompletedAt != nil {
				inherited[i].order = inherited[i].CompletedAt.UnixNano()
			}
		}
	} else if os.IsNotExist(importErr) {
		importErr = nil
	}
	dirs, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return inherited, importErr
	}
	if err != nil {
		return inherited, err
	}
	var checks []Check
	for _, dir := range dirs {
		if !dir.IsDir() || !strings.HasPrefix(dir.Name(), "call-") {
			continue
		}
		place := filepath.Join(root, dir.Name())
		raw, err := os.ReadFile(filepath.Join(place, "command.json"))
		// Prepare has not admitted execution until its command metadata is published.
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return append(inherited, checks...), err
		}
		var command string
		if err := json.Unmarshal(raw, &command); err != nil {
			return append(inherited, checks...), err
		}
		owner, ownerErr := os.ReadFile(filepath.Join(place, "owner"))
		if ownerErr != nil && !os.IsNotExist(ownerErr) {
			return append(inherited, checks...), ownerErr
		}
		callID, callErr := os.ReadFile(filepath.Join(place, "call-id"))
		if callErr != nil && !os.IsNotExist(callErr) {
			return append(inherited, checks...), callErr
		}
		files, err := os.ReadDir(place)
		if err != nil {
			return append(inherited, checks...), err
		}
		for _, file := range files {
			if file.IsDir() || !strings.HasPrefix(file.Name(), "check-") || strings.Contains(file.Name(), ".") {
				continue
			}
			path := filepath.Join(place, file.Name())
			c := Check{Command: command, Source: transcript, ID: filepath.Join(dir.Name(), file.Name()), CallID: string(callID), OwnerRoot: string(owner)}
			info, err := os.Stat(path)
			if err != nil {
				return append(inherited, checks...), err
			}
			c.StartedAt, c.order = info.ModTime(), info.ModTime().UnixNano()
			status, statusErr := os.ReadFile(path + ".exit")
			terminal := statusErr == nil
			if statusErr != nil && !os.IsNotExist(statusErr) {
				c.Problem = statusErr.Error()
			}
			if terminal {
				exit, err := strconv.Atoi(strings.TrimSpace(string(status)))
				if err != nil || exit < 0 || exit > 255 {
					c.Problem = "invalid committed check exit status"
				} else {
					c.Exit = &exit
				}
				if st, err := os.Stat(path + ".exit"); err == nil {
					ended := st.ModTime()
					c.CompletedAt = &ended
					c.order = ended.UnixNano()
				}
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				c.Problem = err.Error()
			} else {
				fields := strings.Split(string(raw), "\x00")
				if len(fields) < 11 || fields[len(fields)-1] != "" {
					// Legacy writers can expose a partial argument file. It is a running
					// record until a terminal commit makes malformed metadata an error.
					if terminal {
						c.Problem = "incomplete committed check arguments"
					}
				} else {
					c.Cwd = fields[0]
					c.Context = fields[1:9]
					c.Args = fields[9 : len(fields)-1]
				}
			}
			if c.OwnerRoot != "" {
				scope, err := os.ReadFile(path + ".scope")
				if err == nil {
					switch strings.TrimSpace(string(scope)) {
					case "diagnostic":
						c.Diagnostic = true
					case "owned":
					default:
						if terminal {
							c.Problem = "invalid committed check scope"
						}
					}
				} else if terminal {
					c.Problem = "owned check scope unavailable"
				}
			}
			c.Before, err = readSnapshot(path + ".before")
			if err != nil && terminal {
				c.Problem = err.Error()
			}
			c.After, err = readSnapshot(path + ".after")
			if err != nil && terminal {
				c.Problem = err.Error()
			}
			checks = append(checks, c)
		}
	}
	sort.SliceStable(checks, func(i, j int) bool {
		if checks[i].order == checks[j].order {
			return checks[i].ID < checks[j].ID
		}
		return checks[i].order < checks[j].order
	})
	// Imported history always precedes this conversation's admitted executions.
	// Original timestamps and source IDs remain intact; no fresh receipt is forged.
	return append(inherited, checks...), importErr
}

// Inherit writes original checks as referenced history, never fabricated shell
// executions. The caller selects relevant stage history; unresolved entries
// travel alongside qualifying passes and remain authoritative blockers.
func Inherit(transcript string, checks []Check) error {
	if err := os.MkdirAll(transcript+".checks", 0700); err != nil {
		return err
	}
	raw, err := json.Marshal(checks)
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(transcript+".checks", "inherited.json"), raw)
}

// PresentationOnly recognizes one check's output reporting, including saved
// exits and reports read back from a log. Once that check was observed, its
// measured exit is authoritative; grep's exit and the spelling of the shell
// presentation must not become a second, permanent verification requirement.
// A different check or unknown executable stays a whole-command requirement:
// a zero-match grep can prevent a later && branch from running.
func PresentationOnly(command string) bool {
	if !simpleShellText(command) {
		return false
	}
	parts := approval.SplitBashCommand(command)
	found := false
	identity := ""
	savedExit := ""
	piped := false
	for _, part := range parts {
		words := strings.Fields(part.Command)
		if len(words) == 0 {
			return false
		}
		if !found && (words[0] == "cd" || words[0] == "export") {
			if part.Separator != "&&" && part.Separator != ";" {
				return false
			}
			continue
		}
		if found && len(words) == 1 && capturedExit.MatchString(words[0]) {
			savedExit, _, _ = strings.Cut(words[0], "=")
			if part.Separator != ";" && part.Separator != "\n" && part.Separator != "&&" {
				return false
			}
			continue
		}
		if found && savedExit != "" && len(words) == 2 && words[0] == "exit" && strings.Trim(words[1], "\"'") == "$"+savedExit && part.Separator == "" {
			continue
		}
		if !piped && (words[0] == "echo" || words[0] == "printf") {
			// Headers cannot run substitutions or hide another command.
			if strings.ContainsAny(part.Command, "<>") || (part.Separator != "" && part.Separator != "&&" && part.Separator != ";") {
				return false
			}
			continue
		}
		if found && reportingProgram(words[0]) {
			// A reporter may consume the check's redirected log in a separate
			// command, rather than only sitting immediately after a pipe.
		} else if !piped {
			for len(words) > 0 && assignment.MatchString(words[0]) {
				words = words[1:]
			}
			if len(words) == 0 {
				return false
			}
			if !checkWords(words) {
				return false
			}
			check := strings.Join(strings.Fields(strings.TrimSpace(strings.TrimSuffix(part.Command, "2>&1"))), " ")
			if identity != "" && check != identity {
				return false
			}
			identity = check
			found = true
		} else {
			if !reportingProgram(words[0]) {
				return false
			}
		}
		if part.Separator != "" && part.Separator != "|" && part.Separator != "&&" && part.Separator != ";" && part.Separator != "\n" {
			return false
		}
		piped = part.Separator == "|"
	}
	return found
}

func reportingProgram(name string) bool {
	switch name {
	case "grep", "rg", "tail", "head", "cat", "tee", "wc", "sed", "awk", "cut", "sort", "uniq":
		return true
	}
	return false
}

var capturedExit = regexp.MustCompile(`^[A-Za-z_][A-Za-z_0-9]*=(\$\?|"\$\?"|'\$\?')$`)

// Quoted regex punctuation is data. Executable substitutions, grouping and
// unclosed quotes are outside the small reporting grammar and stay guarded.
func simpleShellText(command string) bool {
	var quote byte
	for i := 0; i < len(command); i++ {
		ch := command[i]
		if ch == '\\' && quote != '\'' {
			i++
			continue
		}
		if quote == '\'' {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '`' || (ch == '$' && i+1 < len(command) && command[i+1] == '(') {
			return false
		}
		if quote == '"' {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			continue
		}
		if strings.ContainsRune("\n(){}", rune(ch)) {
			return false
		}
	}
	return quote == 0
}

var assignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z_0-9]*=`)
