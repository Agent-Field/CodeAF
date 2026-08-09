// Package isolationfurrow ports src/session/isolation-furrow.ts:1-253 from
// swe-pro (commit 3b25a1a).
//
// It is the process boundary around vendor/bin/furrow. The default executor
// captures stdout and stderr separately, exactly as Process.run does in the TS
// source. Callers match the manufactured failure text: stderr is preferred over
// stdout, and the prefixes are respectively
//
//	furrow <operation> failed (exit <code>):
//	furrow <operation> returned unparseable output (exit <code>):
//
// Keep those strings and their 4,000-JS-code-unit truncation stable.
package isolationfurrow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// ExecOptions mirrors the cwd-only options object passed to FurrowExec.
type ExecOptions struct {
	Cwd string
}

// ExecResult is the captured process result. ExitCode is a JavaScript number
// so injected edge cases such as NaN retain the source's !== 0 semantics.
type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode float64
}

// FurrowExec is the injectable process boundary. Args excludes the binary,
// matching the TS seam.
type FurrowExec func(args []string, opts ExecOptions) (ExecResult, error)

// CreateOptions controls adapter construction.
type CreateOptions struct {
	Exec   FurrowExec
	Binary string
}

// ForkResult is the fork discriminated union. Exactly one field is populated.
type ForkResult struct {
	Path  string `json:"path,omitempty"`
	Error string `json:"error,omitempty"`
}

// MergeResult mirrors the merge method's object.
type MergeResult struct {
	Merged bool   `json:"merged"`
	Detail string `json:"detail"`
}

// Fork describes one universe returned by `furrow forks --json`.
type Fork struct {
	Name      string   `json:"name"`
	Path      string   `json:"path"`
	Conflicts []string `json:"conflicts"`
}

// RadarEntry is the file-to-universes aggregation expected by callers.
type RadarEntry struct {
	File      string   `json:"file"`
	Universes []string `json:"universes"`
}

// FurrowIsolation is a configured Furrow process adapter.
type FurrowIsolation struct {
	exec FurrowExec
}

func compiledRepoRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

// ResolveFurrowBinary resolves the repository copy first, then the cwd's
// vendor copy, then an executable on PATH.
func ResolveFurrowBinary() string {
	cwd, _ := os.Getwd()
	return resolveFurrowBinaryAt(compiledRepoRoot(), cwd, os.Getenv("PATH"), runtime.GOOS)
}

func resolveFurrowBinaryAt(repoRoot, cwd, pathEnv, goos string) string {
	for _, candidate := range []string{
		filepath.Join(repoRoot, "vendor", "bin", "furrow"),
		filepath.Join(cwd, "vendor", "bin", "furrow"),
	} {
		if executableFile(candidate, goos) {
			return candidate
		}
	}

	executable := "furrow"
	if goos == "windows" {
		executable = "furrow.exe"
	}
	for _, directory := range filepath.SplitList(pathEnv) {
		if directory == "" {
			continue
		}
		candidate := filepath.Join(directory, executable)
		if executableFile(candidate, goos) {
			return candidate
		}
	}
	return ""
}

func executableFile(candidate, goos string) bool {
	info, err := os.Stat(candidate)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	if goos != "windows" && !fileExecutable(candidate, info) {
		return false
	}
	return true
}

func defaultExec(binary string) FurrowExec {
	return func(args []string, opts ExecOptions) (ExecResult, error) {
		cmdArgs := make([]string, 0, len(args)+1)
		cmdArgs = append(cmdArgs, binary)
		cmdArgs = append(cmdArgs, args...)
		cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
		cmd.Dir = opts.Cwd
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		if err == nil {
			return ExecResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: 0}, nil
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return ExecResult{
				Stdout:   stdout.String(),
				Stderr:   stderr.String(),
				ExitCode: float64(exitErr.ExitCode()),
			}, nil
		}
		// Process.run throws on a spawn/setup failure; defaultExec catches it
		// and deliberately discards any partial captured output.
		return ExecResult{Stdout: "", Stderr: err.Error(), ExitCode: 127}, nil
	}
}

var ansiEscape = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

func parseOutput(stdout string) any {
	text := jscompat.Trim(ansiEscape.ReplaceAllString(stdout, ""))
	if text == "" {
		return nil
	}

	candidates := []string{text}
	for start := 0; start < len(text); start++ {
		if text[start] != '{' && text[start] != '[' {
			continue
		}
		for end := len(text); end > start; end-- {
			candidate := jscompat.Trim(text[start:end])
			if (strings.HasPrefix(candidate, "{") && strings.HasSuffix(candidate, "}")) ||
				(strings.HasPrefix(candidate, "[") && strings.HasSuffix(candidate, "]")) {
				candidates = append(candidates, candidate)
				break
			}
		}
	}

	for _, candidate := range candidates {
		if value, ok := parseJSON(candidate); ok {
			return value
		}
	}
	return nil
}

func parseJSON(text string) (any, bool) {
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, false
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, false
	}
	return value, true
}

func record(value any) map[string]any {
	root, _ := value.(map[string]any)
	return root
}

func nonEmptyString(value any) (string, bool) {
	text, ok := value.(string)
	if !ok {
		return "", false
	}
	text = jscompat.Trim(text)
	return text, text != ""
}

func stringArray(value any) ([]string, bool) {
	items, ok := value.([]any)
	if !ok {
		return nil, false
	}
	for _, item := range items {
		if _, ok := item.(string); !ok {
			return nil, false
		}
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		text := jscompat.Trim(item.(string))
		if text != "" {
			result = append(result, text)
		}
	}
	return result, true
}

func responseDetail(operation string, result ExecResult) string {
	stderr := jscompat.Trim(result.Stderr)
	stdout := jscompat.Trim(result.Stdout)
	output := stderr
	if output == "" {
		output = stdout
	}
	if output == "" {
		output = "no output"
	}
	return "furrow " + operation + " failed (exit " +
		jscompat.FormatNumber(result.ExitCode) + "): " + jsSlicePrefix(output, 4000)
}

func unparseableDetail(operation string, result ExecResult) string {
	output := jscompat.Trim(result.Stderr)
	if output == "" {
		output = jscompat.Trim(result.Stdout)
	}
	if output == "" {
		output = "no output"
	}
	return "furrow " + operation + " returned unparseable output (exit " +
		jscompat.FormatNumber(result.ExitCode) + "): " + jsSlicePrefix(output, 4000)
}

func jsSlicePrefix(value string, units int) string {
	encoded := utf16.Encode([]rune(value))
	if len(encoded) <= units {
		return value
	}
	return string(utf16.Decode(encoded[:units]))
}

func jsSliceSuffix(value string, units int) string {
	encoded := utf16.Encode([]rune(value))
	if len(encoded) <= units {
		return value
	}
	return string(utf16.Decode(encoded[len(encoded)-units:]))
}

func parseFork(value any) (string, bool) {
	root := record(value)
	if root == nil {
		return "", false
	}
	if result := record(root["result"]); result != nil {
		if destination, ok := nonEmptyString(result["destination"]); ok {
			return destination, true
		}
	}
	if destination, ok := nonEmptyString(root["destination"]); ok {
		return destination, true
	}
	if path, ok := nonEmptyString(root["path"]); ok {
		return path, true
	}
	if nested := record(root["fork"]); nested != nil {
		if path, ok := nonEmptyString(nested["path"]); ok {
			return path, true
		}
	}
	return "", false
}

func parseMerge(value any) (MergeResult, bool) {
	root := record(value)
	if root == nil {
		return MergeResult{}, false
	}
	merged, ok := root["merged"].(bool)
	if !ok {
		return MergeResult{}, false
	}
	detail, validDetail := nonEmptyString(root["detail"])
	if !validDetail {
		if merged {
			detail = "merged"
		} else {
			detail = "not merged"
		}
	}
	return MergeResult{Merged: merged, Detail: detail}, true
}

func nullishProperty(root map[string]any, first, second string) any {
	if value, ok := root[first]; ok && value != nil {
		return value
	}
	if value, ok := root[second]; ok && value != nil {
		return value
	}
	return nil
}

func parseForks(value any) ([]Fork, bool) {
	items, isArray := value.([]any)
	if !isArray {
		root := record(value)
		if root == nil {
			return nil, false
		}
		items, isArray = nullishProperty(root, "forks", "universes").([]any)
		if !isArray {
			return nil, false
		}
	}

	parsed := make([]Fork, 0, len(items))
	for _, item := range items {
		entry := record(item)
		if entry == nil {
			return nil, false
		}
		name, ok := nonEmptyString(entry["name"])
		if !ok {
			return nil, false
		}
		path, ok := nonEmptyString(entry["destination"])
		if !ok {
			path, ok = nonEmptyString(entry["path"])
		}
		if !ok {
			return nil, false
		}
		conflicts, ok := stringArray(entry["conflict_paths"])
		if !ok {
			conflicts, ok = stringArray(entry["conflicts"])
		}
		if !ok {
			conflicts = []string{}
		}
		parsed = append(parsed, Fork{Name: name, Path: path, Conflicts: conflicts})
	}
	return parsed, true
}

// parseRadar is retained even though Furrow 0.1.0's adapter currently derives
// Radar from Forks. It ports the source's parser verbatim for the CLI shape.
func parseRadar(value any) ([]RadarEntry, bool) {
	items, isArray := value.([]any)
	if !isArray {
		root := record(value)
		if root == nil {
			return nil, false
		}
		items, isArray = nullishProperty(root, "radar", "conflicts").([]any)
		if !isArray {
			return nil, false
		}
	}

	parsed := make([]RadarEntry, 0, len(items))
	for _, item := range items {
		entry := record(item)
		if entry == nil {
			return nil, false
		}
		file, ok := nonEmptyString(entry["file"])
		if !ok {
			return nil, false
		}
		universes, ok := stringArray(entry["universes"])
		if !ok {
			return nil, false
		}
		parsed = append(parsed, RadarEntry{File: file, Universes: universes})
	}
	return parsed, true
}

// CreateFurrowIsolation resolves/configures the process adapter. A whitespace
// binary is treated as absent. Nil means no executable is available.
func CreateFurrowIsolation(options ...CreateOptions) *FurrowIsolation {
	opts := CreateOptions{}
	if len(options) > 0 {
		opts = options[0]
	}
	binary := jscompat.Trim(opts.Binary)
	if binary == "" {
		binary = ResolveFurrowBinary()
	}
	if binary == "" {
		return nil
	}
	runner := opts.Exec
	if runner == nil {
		runner = defaultExec(binary)
	}
	return &FurrowIsolation{exec: runner}
}

func (f *FurrowIsolation) run(args []string, workspace string) (result ExecResult) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = ExecResult{Stdout: "", Stderr: fmt.Sprint(recovered), ExitCode: 127}
		}
	}()
	result, err := f.exec(args, ExecOptions{Cwd: workspace})
	if err != nil {
		return ExecResult{Stdout: "", Stderr: err.Error(), ExitCode: 127}
	}
	return result
}

// Fork runs `furrow fork <name> --json`.
func (f *FurrowIsolation) Fork(workspace, name string) ForkResult {
	result := f.run([]string{"fork", name, "--json"}, workspace)
	if result.ExitCode != 0 {
		return ForkResult{Error: responseDetail("fork", result)}
	}
	output := parseOutput(result.Stdout)
	path, ok := parseFork(output)
	if !ok {
		return ForkResult{Error: unparseableDetail("fork", result)}
	}
	return ForkResult{Path: path}
}

// Merge runs `furrow merge <universe> --json`, optionally followed by the
// first `--check <command>`. A zero exit is success even when stdout is not
// parseable, matching Furrow 0.1.0 and the source.
func (f *FurrowIsolation) Merge(workspace, universe string, checkCommands ...string) MergeResult {
	args := []string{"merge", universe, "--json"}
	if len(checkCommands) > 0 && checkCommands[0] != "" {
		args = append(args, "--check", checkCommands[0])
	}
	result := f.run(args, workspace)
	if result.ExitCode != 0 {
		return MergeResult{Merged: false, Detail: responseDetail("merge", result)}
	}
	if parsed, ok := parseMerge(parseOutput(result.Stdout)); ok {
		return parsed
	}
	detail := jsSliceSuffix(jscompat.Trim(result.Stdout), 400)
	if detail == "" {
		detail = "merged"
	}
	return MergeResult{Merged: true, Detail: detail}
}

// Forks runs `furrow forks --json`. Failures and invalid output become [].
func (f *FurrowIsolation) Forks(workspace string) []Fork {
	result := f.run([]string{"forks", "--json"}, workspace)
	if result.ExitCode != 0 {
		return []Fork{}
	}
	parsed, ok := parseForks(parseOutput(result.Stdout))
	if !ok {
		return []Fork{}
	}
	return parsed
}

// Radar aggregates the conflict paths reported by `forks --json`, since
// Furrow 0.1.0 has no radar subcommand.
func (f *FurrowIsolation) Radar(workspace string) []RadarEntry {
	result := f.run([]string{"forks", "--json"}, workspace)
	if result.ExitCode != 0 {
		return []RadarEntry{}
	}
	forks, ok := parseForks(parseOutput(result.Stdout))
	if !ok {
		return []RadarEntry{}
	}

	files := make([]string, 0)
	byFile := make(map[string][]string)
	seen := make(map[string]map[string]bool)
	for _, fork := range forks {
		for _, file := range fork.Conflicts {
			if _, exists := byFile[file]; !exists {
				files = append(files, file)
				byFile[file] = []string{}
				seen[file] = map[string]bool{}
			}
			if !seen[file][fork.Name] {
				seen[file][fork.Name] = true
				byFile[file] = append(byFile[file], fork.Name)
			}
		}
	}

	resultEntries := make([]RadarEntry, 0, len(files))
	for _, file := range files {
		universes := byFile[file]
		sort.SliceStable(universes, func(i, j int) bool {
			return jsUTF16Less(universes[i], universes[j])
		})
		resultEntries = append(resultEntries, RadarEntry{File: file, Universes: universes})
	}
	sort.SliceStable(resultEntries, func(i, j int) bool {
		return jscompat.LocaleCompare(resultEntries[i].File, resultEntries[j].File) < 0
	})
	return resultEntries
}

func jsUTF16Less(a, b string) bool {
	left := utf16.Encode([]rune(a))
	right := utf16.Encode([]rune(b))
	for i := 0; i < len(left) && i < len(right); i++ {
		if left[i] != right[i] {
			return left[i] < right[i]
		}
	}
	return len(left) < len(right)
}
