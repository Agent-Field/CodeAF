// Package verification keeps the exit of a check apart from the shell pipeline
// that presents its output. A grep finding no failures is not a failing test.
package verification

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

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
	Command string   `json:"command"`
	Cwd     string   `json:"cwd"`
	Args    []string `json:"args"`
	Context []string `json:"context"`
	Exit    *int     `json:"exit"`
	order   int64
}

func (c Check) Key() string {
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
	raw, _ := json.Marshal(command)
	if err := os.WriteFile(filepath.Join(dir, "command.json"), raw, 0600); err != nil {
		return "", err
	}
	// The small context is execution scope, never the environment wholesale:
	// secrets must not be copied into command receipts. Flags and target/runtime
	// settings distinguish checks that happen to have identical argv.
	// The Unix shell resolver also permits sh without Bash. That shell keeps
	// its original execution path and journal evidence; export -f is Bash-only.
	prefix := observerShellGuard + `
_codeaf_check() {
 local _codeaf_file _codeaf_status
 _codeaf_file=$(command mktemp ` + quote(shell.ListPath(filepath.Join(dir, "check-XXXXXX"))) + `) || return 125
 printf '%s\0' "$PWD" "${GOOS-}" "${GOARCH-}" "${GOFLAGS-}" "${CGO_ENABLED-}" "${GOWORK-}" "${GORACE-}" "${NODE_ENV-}" "${PYTEST_ADDOPTS-}" "$@" > "$_codeaf_file" || return 125
 if command "$@"; then _codeaf_status=0; else _codeaf_status=$?; fi
 printf '%s\n' "$_codeaf_status" > "$_codeaf_file.exit"
 return "$_codeaf_status"
}
go() { if [ "${1-}" = test ] || [ "${1-}" = vet ]; then _codeaf_check go "$@"; else command go "$@"; fi; }
pytest() { _codeaf_check pytest "$@"; }
npm() { if [ "${1-}" = test ] || { [ "${1-}" = run ] && [ "${2-}" = test ]; }; then _codeaf_check npm "$@"; else command npm "$@"; fi; }
python() { if [ "${1-}" = -m ] && [ "${2-}" = pytest ]; then _codeaf_check python "$@"; else command python "$@"; fi; }
python3() { if [ "${1-}" = -m ] && [ "${2-}" = pytest ]; then _codeaf_check python3 "$@"; else command python3 "$@"; fi; }
export -f _codeaf_check go pytest npm python python3
fi
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
	dirs, err := os.ReadDir(transcript + ".checks")
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var checks []Check
	for _, dir := range dirs {
		if !dir.IsDir() || !strings.HasPrefix(dir.Name(), "call-") {
			continue
		}
		place := filepath.Join(transcript+".checks", dir.Name())
		raw, err := os.ReadFile(filepath.Join(place, "command.json"))
		if err != nil {
			return nil, err
		}
		var command string
		if err := json.Unmarshal(raw, &command); err != nil {
			return nil, err
		}
		files, err := os.ReadDir(place)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			if file.IsDir() || !strings.HasPrefix(file.Name(), "check-") || strings.HasSuffix(file.Name(), ".exit") {
				continue
			}
			path := filepath.Join(place, file.Name())
			raw, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			fields := strings.Split(string(raw), "\x00")
			if len(fields) < 11 || fields[len(fields)-1] != "" {
				return nil, fmt.Errorf("incomplete check arguments")
			}
			c := Check{Command: command, Cwd: fields[0], Context: fields[1:9], Args: fields[9 : len(fields)-1]}
			info, err := os.Stat(path)
			if err != nil {
				return nil, err
			}
			c.order = info.ModTime().UnixNano()
			status, err := os.ReadFile(path + ".exit")
			if err == nil {
				exit, parseErr := strconv.Atoi(strings.TrimSpace(string(status)))
				if parseErr != nil || exit < 0 || exit > 255 {
					return nil, fmt.Errorf("invalid check exit status")
				}
				c.Exit = &exit
				info, err = os.Stat(path + ".exit")
				if err != nil {
					return nil, err
				}
				c.order = info.ModTime().UnixNano()
			} else if !os.IsNotExist(err) {
				return nil, err
			}
			checks = append(checks, c)
		}
	}
	sort.SliceStable(checks, func(i, j int) bool { return checks[i].order < checks[j].order })
	return checks, nil
}

// PresentationOnly recognizes a check and its output reporting, including
// repeated counts of the SAME check joined by headers. A different check or
// executable remains a whole-command failure: a zero-match grep can prevent
// a later && branch from running, so one receipt cannot vouch for that branch.
func PresentationOnly(command string) bool {
	if !simpleShellText(command) {
		return false
	}
	parts := approval.SplitBashCommand(command)
	found := false
	identity := ""
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
		if !piped && (words[0] == "echo" || words[0] == "printf") {
			// Headers cannot run substitutions or hide another command.
			if strings.ContainsAny(part.Command, "<>") || (part.Separator != "" && part.Separator != "&&" && part.Separator != ";") {
				return false
			}
			continue
		}
		if !piped {
			for len(words) > 0 && assignment.MatchString(words[0]) {
				words = words[1:]
			}
			if len(words) == 0 {
				return false
			}
			switch words[0] {
			case "go":
				if len(words) < 2 || (words[1] != "test" && words[1] != "vet") {
					return false
				}
			case "npm":
				if len(words) < 2 || (words[1] != "test" && !(len(words) > 2 && words[1] == "run" && words[2] == "test")) {
					return false
				}
			case "pytest":
			case "python", "python3":
				if len(words) < 3 || words[1] != "-m" || words[2] != "pytest" {
					return false
				}
			default:
				return false
			}
			check := strings.Join(strings.Fields(strings.TrimSpace(strings.TrimSuffix(part.Command, "2>&1"))), " ")
			if identity != "" && check != identity {
				return false
			}
			identity = check
			found = true
		} else {
			switch words[0] {
			case "grep", "rg", "tail", "head", "cat", "tee", "wc", "sed", "awk", "cut", "sort", "uniq":
			default:
				return false
			}
		}
		if part.Separator != "" && part.Separator != "|" && part.Separator != "&&" && part.Separator != ";" {
			return false
		}
		piped = part.Separator == "|"
	}
	return found
}

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
