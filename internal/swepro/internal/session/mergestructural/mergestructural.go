// Package mergestructural is a bug-for-bug port of
// src/session/merge-structural.ts:1-431 (swe-pro 3b25a1a). It wraps the
// weave-driver entity-level three-way merger and applies it conservatively to
// files in git's unmerged index.
//
// The TypeScript module gets process execution, the filesystem, the
// environment, and logging from the runtime. Those effects are narrow seams
// here: MergeExec, GitRunner, StructuralMerge, and LogFunc. The extension
// support gate remains pure and is pinned against the real TS module by
// testdata/fixtures.json.
//
// Fidelity notes:
//   - An omitted Merge3Options.OutPath allocates a weave-merge-* directory and
//     deliberately never removes it, exactly like the TS adapter.
//   - ResolveOptions.StructuralSet distinguishes TS `structural: null` from an
//     omitted property. StructuralSet=true with Structural=nil force-disables
//     the adapter; false asks CreateStructuralMerge to resolve a binary.
//   - ConflictedFiles filters only empty strings. Whitespace-only paths are
//     truthy in JS and therefore stay in the work list here too.
//   - Index stages are extracted in 1,2,3 order even when an earlier stage is
//     missing, because Promise sequencing in the source does exactly that.
package mergestructural

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

const (
	StatusClean     = "clean"
	StatusConflicts = "conflicts"
	StatusFallback  = "fallback"
)

// MergeExecResult mirrors the object resolved by MergeExec.
type MergeExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// MergeExec is the Process.run seam used by the weave adapter. Returning an
// error mirrors a rejected custom exec promise.
type MergeExec func(args []string, cwd string) (MergeExecResult, error)

// Merge3Result is the Go representation of the TS discriminated union. Only
// fields belonging to Status are emitted by MarshalJSON.
type Merge3Result struct {
	Status        string
	Merged        string
	ConflictCount int
	Detail        string
}

// MarshalJSON preserves the three exact object shapes in merge-structural.ts.
func (r Merge3Result) MarshalJSON() ([]byte, error) {
	switch r.Status {
	case StatusClean:
		return jscompat.Stringify(struct {
			Status string `json:"status"`
			Merged string `json:"merged"`
		}{r.Status, r.Merged})
	case StatusConflicts:
		return jscompat.Stringify(struct {
			Status        string `json:"status"`
			Merged        string `json:"merged"`
			ConflictCount int    `json:"conflictCount"`
		}{r.Status, r.Merged, r.ConflictCount})
	default:
		return jscompat.Stringify(struct {
			Status string `json:"status"`
			Detail string `json:"detail"`
		}{r.Status, r.Detail})
	}
}

// Merge3Options mirrors StructuralMerge.merge3's options bag. Pointer strings
// preserve nullish-coalescing semantics: an explicitly empty OutPath or
// LogicalPath is different from an omitted one.
type Merge3Options struct {
	BasePath    string
	LeftPath    string
	RightPath   string
	OutPath     *string
	LogicalPath *string
	CWD         string
}

// StructuralMerge is the narrow interface consumed by the conflict-set pass.
type StructuralMerge interface {
	Merge3(opts Merge3Options) (Merge3Result, error)
}

// CreateStructuralMergeOptions mirrors createStructuralMerge's optional bag.
type CreateStructuralMergeOptions struct {
	Exec   MergeExec
	Binary string
}

type structuralAdapter struct {
	exec MergeExec
}

// ResolveWeaveBinary resolves the repository copy first, a cwd-relative copy
// second, and finally an executable on PATH.
func ResolveWeaveBinary() string {
	candidates := []string{vendoredBinary()}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Clean(filepath.Join(cwd, "vendor", "bin", "weave-driver")))
	}
	for _, candidate := range candidates {
		if executableFile(candidate) {
			return candidate
		}
	}

	binaryName := "weave-driver"
	if runtime.GOOS == "windows" {
		binaryName = "weave-driver.exe"
	}
	for _, directory := range filepath.SplitList(os.Getenv("PATH")) {
		if directory == "" {
			continue
		}
		candidate := filepath.Join(directory, binaryName)
		if executableFile(candidate) {
			return candidate
		}
	}
	return ""
}

func vendoredBinary() string {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		return filepath.Join("vendor", "bin", "weave-driver")
	}
	// internal/session/mergestructural -> repository root.
	return filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "..", "vendor", "bin", "weave-driver"))
}

func executableFile(candidate string) bool {
	info, err := os.Stat(candidate)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return false
	}
	return true
}

// CreateStructuralMerge constructs the weave adapter or returns nil when no
// binary can be resolved. It is variadic solely to mirror TS's default `{}`.
func CreateStructuralMerge(options ...CreateStructuralMergeOptions) StructuralMerge {
	var opts CreateStructuralMergeOptions
	if len(options) > 0 {
		opts = options[0]
	}
	binary := jscompat.Trim(opts.Binary)
	if binary == "" {
		binary = ResolveWeaveBinary()
	}
	if binary == "" {
		return nil
	}
	run := opts.Exec
	if run == nil {
		run = defaultMergeExec(binary)
	}
	return &structuralAdapter{exec: run}
}

func defaultMergeExec(binary string) MergeExec {
	return func(args []string, cwd string) (MergeExecResult, error) {
		cmd := exec.Command(binary, args...)
		cmd.Dir = cwd
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		if err == nil {
			return MergeExecResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: 0}, nil
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return MergeExecResult{
				Stdout:   stdout.String(),
				Stderr:   stderr.String(),
				ExitCode: exitErr.ExitCode(),
			}, nil
		}
		// Process.run(..., {nothrow:true}) converts a spawn error into a
		// nonzero result rather than rejecting.
		return MergeExecResult{Stdout: stdout.String(), Stderr: err.Error(), ExitCode: 1}, nil
	}
}

func (a *structuralAdapter) Merge3(opts Merge3Options) (Merge3Result, error) {
	var out string
	if opts.OutPath != nil {
		out = *opts.OutPath
	} else {
		dir, err := os.MkdirTemp("", "weave-merge-")
		if err != nil {
			return Merge3Result{}, err
		}
		out = filepath.Join(dir, "merged")
	}
	logical := jsBasename(opts.LeftPath)
	if opts.LogicalPath != nil {
		logical = *opts.LogicalPath
	}
	args := []string{opts.BasePath, opts.LeftPath, opts.RightPath, "-o", out, "-p", logical}

	result, err := invokeMergeExec(a.exec, args, opts.CWD)
	if err != nil {
		return Merge3Result{Status: StatusFallback, Detail: err.Error()}, nil
	}
	if result.ExitCode != 0 && result.ExitCode != 1 {
		return Merge3Result{Status: StatusFallback, Detail: failureDetail(result)}, nil
	}

	raw, err := os.ReadFile(out)
	if err != nil {
		return Merge3Result{
			Status: StatusFallback,
			Detail: fmt.Sprintf(
				"weave-driver exit %d but output unreadable: %s",
				result.ExitCode,
				nodeReadError(out, err),
			),
		}, nil
	}
	merged := strings.ToValidUTF8(string(raw), "\uFFFD")
	if result.ExitCode == 0 {
		return Merge3Result{Status: StatusClean, Merged: merged}, nil
	}
	return Merge3Result{
		Status:        StatusConflicts,
		Merged:        merged,
		ConflictCount: max(1, countConflictMarkers(merged)),
	}, nil
}

func invokeMergeExec(run MergeExec, args []string, cwd string) (result MergeExecResult, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if recoveredErr, ok := recovered.(error); ok {
				err = recoveredErr
			} else {
				err = fmt.Errorf("%v", recovered)
			}
		}
	}()
	return run(args, cwd)
}

func nodeReadError(path string, err error) string {
	quoted := "'" + path + "'"
	switch {
	case os.IsNotExist(err):
		return "ENOENT: no such file or directory, open " + quoted
	case os.IsPermission(err):
		return "EACCES: permission denied, open " + quoted
	default:
		return err.Error()
	}
}

func countConflictMarkers(text string) int {
	count := 0
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "<<<<<<<") {
			count++
		}
	}
	return count
}

func failureDetail(result MergeExecResult) string {
	output := jscompat.Trim(result.Stderr)
	if output == "" {
		output = jscompat.Trim(result.Stdout)
	}
	if output == "" {
		output = "no output"
	}
	return "weave-driver failed (exit " + strconv.Itoa(result.ExitCode) + "): " + sliceUTF16(output, 4000)
}

func sliceUTF16(s string, n int) string {
	units := utf16.Encode([]rune(s))
	if len(units) <= n {
		return s
	}
	// A JS slice may leave an unpaired surrogate. Go strings cannot represent
	// that scalar directly, so utf16.Decode uses U+FFFD at the split boundary.
	return string(utf16.Decode(units[:n]))
}

func jsBasename(value string) string {
	if runtime.GOOS == "windows" {
		// filepath.Base follows the host separator rules, as node:path does.
		// Correct its two sentinel spellings to Node's empty basename.
		if value == "" || strings.Trim(value, `/\`) == "" {
			return ""
		}
		return filepath.Base(value)
	}
	end := len(value)
	for end > 0 && value[end-1] == '/' {
		end--
	}
	if end == 0 {
		return ""
	}
	start := strings.LastIndex(value[:end], "/")
	return value[start+1 : end]
}

// WEAVE_SUPPORTED_LANGUAGES is the exact grammar set compiled into the
// vendored weave-driver 0.3.6.
var WEAVE_SUPPORTED_LANGUAGES = []string{
	"bash",
	"c",
	"c_sharp",
	"cpp",
	"dart",
	"elixir",
	"embedded_template",
	"fortran",
	"go",
	"hcl",
	"java",
	"javascript",
	"kotlin",
	"nix",
	"ocaml",
	"ocaml_interface",
	"perl",
	"php",
	"python",
	"ruby",
	"rust",
	"scala",
	"svelte",
	"swift",
	"tsx",
	"typescript",
	"xml",
	"zig",
}

var extensionToLanguage = map[string]string{
	"sh":     "bash",
	"bash":   "bash",
	"c":      "c",
	"h":      "c",
	"cs":     "c_sharp",
	"cc":     "cpp",
	"cpp":    "cpp",
	"cxx":    "cpp",
	"hpp":    "cpp",
	"hh":     "cpp",
	"hxx":    "cpp",
	"dart":   "dart",
	"ex":     "elixir",
	"exs":    "elixir",
	"erb":    "embedded_template",
	"f":      "fortran",
	"for":    "fortran",
	"f90":    "fortran",
	"f95":    "fortran",
	"f03":    "fortran",
	"go":     "go",
	"hcl":    "hcl",
	"tf":     "hcl",
	"tfvars": "hcl",
	"java":   "java",
	"js":     "javascript",
	"mjs":    "javascript",
	"cjs":    "javascript",
	"jsx":    "javascript",
	"kt":     "kotlin",
	"kts":    "kotlin",
	"nix":    "nix",
	"ml":     "ocaml",
	"mli":    "ocaml_interface",
	"pl":     "perl",
	"pm":     "perl",
	"php":    "php",
	"py":     "python",
	"pyi":    "python",
	"rb":     "ruby",
	"rs":     "rust",
	"scala":  "scala",
	"sc":     "scala",
	"svelte": "svelte",
	"swift":  "swift",
	"tsx":    "tsx",
	"ts":     "typescript",
	"mts":    "typescript",
	"cts":    "typescript",
	"xml":    "xml",
	"zig":    "zig",
}

// WeaveLanguageForPath returns the weave grammar for logicalPath, or nil when
// its final extension is unsupported.
func WeaveLanguageForPath(logicalPath string) *string {
	base := jsBasename(logicalPath)
	dot := strings.LastIndex(base, ".")
	if dot <= 0 {
		return nil
	}
	ext := strings.ToLower(base[dot+1:])
	language, ok := extensionToLanguage[ext]
	if !ok {
		return nil
	}
	out := language
	return &out
}

// WeaveSupportsPath reports whether weave has a tree-sitter grammar for the
// path's extension.
func WeaveSupportsPath(logicalPath string) bool {
	return WeaveLanguageForPath(logicalPath) != nil
}

// StructuralMergeEnabled is the CODEAF_STRUCTURAL_MERGE kill switch. Only the
// exact string "0" disables the fast path.
func StructuralMergeEnabled() bool {
	return os.Getenv("CODEAF_STRUCTURAL_MERGE") != "0"
}

// GitResult mirrors Process.Result for the conflict-set git seam.
type GitResult struct {
	Code   int
	Stdout []byte
	Stderr []byte
}

// GitRunner runs argv in cwd. Returning an error mirrors a rejected promise.
type GitRunner func(argv []string, cwd string) (GitResult, error)

const (
	OutcomeResolved  = "resolved"
	OutcomeEscalated = "escalated"
)

// StructuralConflictDetail is one per-file trace entry.
type StructuralConflictDetail struct {
	File    string `json:"file"`
	Outcome string `json:"outcome"`
	Reason  string `json:"reason"`
}

// StructuralConflictOutcome is the conservative subset-remover result.
type StructuralConflictOutcome struct {
	Resolved  []string                   `json:"resolved"`
	Remaining []string                   `json:"remaining"`
	Details   []StructuralConflictDetail `json:"details"`
}

// StructuralLogMeta is the fixed metadata shape supplied to LogFunc.
type StructuralLogMeta struct {
	Dir                  string
	Considered           int
	ResolvedStructurally int
	Escalated            int
}

// LogFunc is the optional structural-pass logging seam.
type LogFunc func(event string, meta StructuralLogMeta)

// ResolveOptions mirrors resolveConflictedFilesStructurally's options.
type ResolveOptions struct {
	Dir             string
	ConflictedFiles []string
	Structural      StructuralMerge
	StructuralSet   bool
	Git             GitRunner
	Log             LogFunc
}

// ResolveConflictedFilesStructurally attempts clean entity-disjoint merges and
// leaves every failure in Remaining.
func ResolveConflictedFilesStructurally(opts ResolveOptions) (StructuralConflictOutcome, error) {
	files := make([]string, 0, len(opts.ConflictedFiles))
	for _, file := range opts.ConflictedFiles {
		if file != "" {
			files = append(files, file)
		}
	}
	empty := func() StructuralConflictOutcome {
		return StructuralConflictOutcome{
			Resolved:  []string{},
			Remaining: []string{},
			Details:   []StructuralConflictDetail{},
		}
	}
	escalateAll := func(reason string) StructuralConflictOutcome {
		details := make([]StructuralConflictDetail, 0, len(files))
		for _, file := range files {
			details = append(details, StructuralConflictDetail{
				File: file, Outcome: OutcomeEscalated, Reason: reason,
			})
		}
		remaining := append([]string{}, files...)
		return StructuralConflictOutcome{
			Resolved: []string{}, Remaining: remaining, Details: details,
		}
	}

	if len(files) == 0 {
		return empty(), nil
	}
	if !StructuralMergeEnabled() {
		return escalateAll("disabled via CODEAF_STRUCTURAL_MERGE=0"), nil
	}

	structural := opts.Structural
	if !opts.StructuralSet {
		structural = CreateStructuralMerge()
	}
	if structural == nil {
		return escalateAll("weave-driver binary unavailable"), nil
	}

	git := opts.Git
	if git == nil {
		git = defaultGitRunner
	}
	tmpRoot, err := os.MkdirTemp("", "weave-conflicts-")
	if err != nil {
		return StructuralConflictOutcome{}, err
	}
	defer func() { _ = os.RemoveAll(tmpRoot) }()

	outcome := empty()
	for i, file := range files {
		escalate := func(reason string) {
			outcome.Remaining = append(outcome.Remaining, file)
			outcome.Details = append(outcome.Details, StructuralConflictDetail{
				File: file, Outcome: OutcomeEscalated, Reason: reason,
			})
		}

		if !WeaveSupportsPath(file) {
			escalate("unsupported language")
			continue
		}

		if err := func() (err error) {
			defer func() {
				if recovered := recover(); recovered != nil {
					if recoveredErr, ok := recovered.(error); ok {
						err = recoveredErr
					} else {
						err = fmt.Errorf("%v", recovered)
					}
				}
			}()
			tag := strconv.Itoa(i) + "-" + jsBasename(file)
			base, err := extractStage(git, opts.Dir, 1, file, tmpRoot, tag)
			if err != nil {
				return err
			}
			ours, err := extractStage(git, opts.Dir, 2, file, tmpRoot, tag)
			if err != nil {
				return err
			}
			theirs, err := extractStage(git, opts.Dir, 3, file, tmpRoot, tag)
			if err != nil {
				return err
			}
			if base == "" || ours == "" || theirs == "" {
				escalate("missing merge stage (add/add or delete/modify)")
				return nil
			}

			logical := file
			result, err := structural.Merge3(Merge3Options{
				BasePath: base, LeftPath: ours, RightPath: theirs,
				LogicalPath: &logical, CWD: opts.Dir,
			})
			if err != nil {
				return err
			}
			if result.Status != StatusClean {
				if result.Status == StatusConflicts {
					escalate("structural conflict")
				} else {
					escalate("fallback: " + result.Detail)
				}
				return nil
			}
			if strings.Contains(result.Merged, "<<<<<<<") || strings.Contains(result.Merged, ">>>>>>>") {
				escalate("clean status but markers present")
				return nil
			}

			abs := filepath.Join(opts.Dir, file)
			if err := os.WriteFile(abs, []byte(result.Merged), 0o666); err != nil {
				return err
			}
			add, err := git([]string{"git", "add", "--", file}, opts.Dir)
			if err != nil {
				return err
			}
			if add.Code != 0 {
				if _, err := git([]string{"git", "checkout", "--merge", "--", file}, opts.Dir); err != nil {
					return err
				}
				escalate("git add failed: " + sliceUTF16(strings.ToValidUTF8(string(add.Stderr), "\uFFFD"), 200))
				return nil
			}

			outcome.Resolved = append(outcome.Resolved, file)
			outcome.Details = append(outcome.Details, StructuralConflictDetail{
				File: file, Outcome: OutcomeResolved, Reason: "entity-disjoint auto-merge",
			})
			return nil
		}(); err != nil {
			escalate("error: " + err.Error())
		}
	}

	if opts.Log != nil {
		opts.Log("structural merge pass", StructuralLogMeta{
			Dir:                  opts.Dir,
			Considered:           len(files),
			ResolvedStructurally: len(outcome.Resolved),
			Escalated:            len(outcome.Remaining),
		})
	}
	return outcome, nil
}

func extractStage(
	git GitRunner,
	cwd string,
	stage int,
	relPath string,
	tmpDir string,
	tag string,
) (string, error) {
	result, err := git([]string{"git", "show", ":" + strconv.Itoa(stage) + ":" + relPath}, cwd)
	if err != nil {
		return "", err
	}
	if result.Code != 0 {
		return "", nil
	}
	out := filepath.Join(tmpDir, strconv.Itoa(stage)+"-"+tag)
	if err := os.WriteFile(out, result.Stdout, 0o666); err != nil {
		return "", err
	}
	return out, nil
}

func defaultGitRunner(argv []string, cwd string) (GitResult, error) {
	if len(argv) == 0 {
		return GitResult{Code: 1, Stderr: []byte("Command is required")}, nil
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = cwd
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return GitResult{Code: 0, Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return GitResult{
			Code: exitErr.ExitCode(), Stdout: stdout.Bytes(), Stderr: stderr.Bytes(),
		}, nil
	}
	return GitResult{Code: 1, Stdout: stdout.Bytes(), Stderr: []byte(err.Error())}, nil
}
