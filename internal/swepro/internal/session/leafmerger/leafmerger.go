// Package leafmerger is a bug-for-bug port of
// src/session/leaf-merger.ts:1-528 (swe-pro 3b25a1a). It stages completed leaf
// work, rebases the leaf branch onto the live target tip, merges it with
// --no-ff from the target checkout, and removes the worktree and branch.
//
// All git invocations use the Runner seam. Conflict reports use the local
// filesystem just as the TypeScript source does. StructuralMergeSet preserves
// the source's three-way option: false resolves the vendored weave driver,
// true plus nil disables structural merging, and true plus a value injects an
// adapter.
package leafmerger

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/Agent-Field/swe-pro-go/internal/attribution"
	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/logshim"
	"github.com/Agent-Field/swe-pro-go/internal/session/mergestructural"
)

var log = logshim.Create(map[string]any{"service": "session.leaf-merger"})

// Result is the Process.run result consumed by LeafMerger.
type Result struct {
	Code   int
	Stdout []byte
	Stderr []byte
}

// RunOptions are the Process.run options observable in this module.
type RunOptions struct {
	CWD string
	Env map[string]string
}

// Runner is the narrow git process seam. Nothrow behavior belongs in the
// runner: command exit failures should be returned as nonzero Results.
type Runner interface {
	Run(argv []string, opts RunOptions) (Result, error)
}

// RunnerFunc adapts a function to Runner.
type RunnerFunc func(argv []string, opts RunOptions) (Result, error)

// Run implements Runner.
func (f RunnerFunc) Run(argv []string, opts RunOptions) (Result, error) {
	return f(argv, opts)
}

// ConflictArgs are passed to the optional semantic conflict resolver.
type ConflictArgs struct {
	Worktree        string
	ConflictedFiles []string
	TaskID          string
	TaskTitle       string
	TargetBranch    string
}

// ConflictResult is the resolver's flat result shape.
type ConflictResult struct {
	OK      bool
	Summary string
}

// ConflictResolver is the optional LLM conflict-resolution seam.
type ConflictResolver func(args ConflictArgs) (ConflictResult, error)

// Input configures one leaf merge.
type Input struct {
	Workspace    string
	Worktree     string
	MergeAt      string
	TaskID       string
	TaskTitle    string
	TargetBranch string
	SourceBranch *string

	ResolveConflicts ConflictResolver

	StructuralMerge    mergestructural.StructuralMerge
	StructuralMergeSet bool
}

// Outcome is the result returned to the scheduler.
type Outcome struct {
	OK           bool   `json:"ok"`
	Summary      string `json:"summary"`
	ConflictPath string `json:"conflictPath,omitempty"`
}

type stageOutcome struct {
	ok     bool
	kind   string
	reason string
}

type rebaseOutcome struct {
	ok           bool
	resolverNote string
	summary      string
	conflictPath string
}

type mergeStepOutcome = rebaseOutcome

// LeafMerger runs the five source phases.
type LeafMerger struct {
	input  Input
	branch string
	runner Runner
}

// New constructs a merger. A nil runner uses real processes.
func New(input Input, runner Runner) *LeafMerger {
	branch := "plandb/" + input.TaskID
	if input.SourceBranch != nil {
		branch = *input.SourceBranch
	}
	if runner == nil {
		runner = processRunner{}
	}
	return &LeafMerger{input: input, branch: branch, runner: runner}
}

// Run executes the full leaf-merge pipeline.
func (m *LeafMerger) Run() (Outcome, error) {
	staged, err := m.stageAndCommit()
	if err != nil {
		return Outcome{}, err
	}
	if !staged.ok {
		summary := staged.reason
		if summary == "" {
			summary = "stage failed"
		}
		return Outcome{OK: false, Summary: summary}, nil
	}

	empty, err := m.isEmptyAgainstTarget()
	if err != nil {
		return Outcome{}, err
	}
	if empty {
		if err := m.cleanup(); err != nil {
			return Outcome{}, err
		}
		return Outcome{OK: true, Summary: "no changes vs target; worktree cleaned up"}, nil
	}

	rebase, err := m.rebaseOnto()
	if err != nil {
		return Outcome{}, err
	}
	if !rebase.ok {
		summary := rebase.summary
		if summary == "" {
			summary = "rebase failed"
		}
		return Outcome{
			OK: false, Summary: summary, ConflictPath: rebase.conflictPath,
		}, nil
	}

	merge, err := m.mergeNoFF()
	if err != nil {
		return Outcome{}, err
	}
	if !merge.ok {
		summary := merge.summary
		if summary == "" {
			summary = "merge failed"
		}
		return Outcome{
			OK: false, Summary: summary, ConflictPath: merge.conflictPath,
		}, nil
	}

	if err := m.cleanup(); err != nil {
		return Outcome{}, err
	}
	baseSummary := "merged " + m.branch + " into " + m.input.TargetBranch + " via --no-ff"
	notes := make([]string, 0, 2)
	if rebase.resolverNote != "" {
		notes = append(notes, rebase.resolverNote)
	}
	if merge.resolverNote != "" {
		notes = append(notes, merge.resolverNote)
	}
	if len(notes) > 0 {
		baseSummary += " (" + strings.Join(notes, " ") + ")"
	}
	return Outcome{OK: true, Summary: baseSummary}, nil
}

func (m *LeafMerger) stageAndCommit() (stageOutcome, error) {
	input := m.input
	if _, err := m.run(
		[]string{"git", "update-index", "--refresh", "--again"},
		input.Worktree,
		nil,
	); err != nil {
		return stageOutcome{}, err
	}
	if _, err := m.run([]string{"git", "add", "-A"}, input.Worktree, nil); err != nil {
		return stageOutcome{}, err
	}
	status, err := m.run([]string{"git", "status", "--porcelain"}, input.Worktree, nil)
	if err != nil {
		return stageOutcome{}, err
	}
	if jscompat.Trim(resultStdout(status)) == "" {
		return stageOutcome{ok: true, kind: "noop"}, nil
	}

	message := "leaf " + input.TaskID + ": " + compact(input.TaskTitle, 80)
	commit, err := m.run(
		gitWithCommitter("commit", "-m", attribution.AppendCommitTrailer(message)),
		input.Worktree,
		nil,
	)
	if err != nil {
		return stageOutcome{}, err
	}
	if commit.Code == 0 {
		return stageOutcome{ok: true, kind: "committed"}, nil
	}

	combined := jscompat.Trim(resultStderr(commit) + "\n" + resultStdout(commit))
	if phantomDirty(combined) {
		log.Info("merger commit skipped (phantom dirty)", map[string]any{
			"taskID": input.TaskID, "worktree": input.Worktree,
			"statusOutput": sliceUTF16(resultStdout(status), 200),
		})
		if _, err := m.run([]string{"git", "reset", "--hard", "HEAD"}, input.Worktree, nil); err != nil {
			return stageOutcome{}, err
		}
		return stageOutcome{ok: true, kind: "phantom"}, nil
	}

	if combined == "" {
		combined = "(no output from git)"
	}
	return stageOutcome{
		ok: false, reason: "failed to commit leaf work: " + sliceUTF16(combined, 300),
	}, nil
}

func (m *LeafMerger) isEmptyAgainstTarget() (bool, error) {
	result, err := m.run(
		[]string{"git", "diff", "--quiet", m.input.TargetBranch + "...HEAD"},
		m.input.Worktree,
		nil,
	)
	if err != nil {
		return false, err
	}
	return result.Code == 0, nil
}

func (m *LeafMerger) rebaseOnto() (rebaseOutcome, error) {
	input := m.input
	rebase, err := m.run(
		[]string{"git", "rebase", input.TargetBranch},
		input.Worktree,
		nil,
	)
	if err != nil {
		return rebaseOutcome{}, err
	}
	if rebase.Code == 0 {
		return rebaseOutcome{ok: true}, nil
	}

	conflictedFiles, err := m.listConflictedFiles(input.Worktree)
	if err != nil {
		return rebaseOutcome{}, err
	}
	structural := structuralResult{remaining: conflictedFiles}
	if len(conflictedFiles) > 0 {
		structural = m.structuralFastPath(input.Worktree, conflictedFiles)
	}
	remaining := structural.remaining
	structuralNote := ""
	if len(structural.resolved) > 0 {
		structuralNote = "structural resolver fixed " +
			strconv.Itoa(len(structural.resolved)) + " rebase conflict(s)"
	}

	if len(conflictedFiles) > 0 && len(remaining) == 0 {
		if _, err := m.run([]string{"git", "add", "-A"}, input.Worktree, nil); err != nil {
			return rebaseOutcome{}, err
		}
		cont, err := m.run(
			[]string{"git", "rebase", "--continue"},
			input.Worktree,
			map[string]string{"GIT_EDITOR": "true"},
		)
		if err != nil {
			return rebaseOutcome{}, err
		}
		if cont.Code == 0 {
			return rebaseOutcome{ok: true, resolverNote: structuralNote}, nil
		}
		if _, err := m.run([]string{"git", "rebase", "--abort"}, input.Worktree, nil); err != nil {
			return rebaseOutcome{}, err
		}
		report, err := m.writeReport(
			"rebase",
			"Structural resolver staged all conflicts but git rebase --continue failed:\n"+
				resultStdout(cont)+"\n"+resultStderr(cont),
		)
		if err != nil {
			return rebaseOutcome{}, err
		}
		return rebaseOutcome{
			ok: false,
			summary: "rebase --continue failed after structural resolution; worktree left at " +
				input.Worktree,
			conflictPath: report,
		}, nil
	}

	if input.ResolveConflicts != nil && len(remaining) > 0 {
		resolved, err := input.ResolveConflicts(ConflictArgs{
			Worktree: input.Worktree, ConflictedFiles: remaining,
			TaskID: input.TaskID, TaskTitle: input.TaskTitle,
			TargetBranch: input.TargetBranch,
		})
		if err != nil {
			return rebaseOutcome{}, err
		}
		if resolved.OK {
			if _, err := m.run([]string{"git", "add", "-A"}, input.Worktree, nil); err != nil {
				return rebaseOutcome{}, err
			}
			cont, err := m.run(
				[]string{"git", "rebase", "--continue"},
				input.Worktree,
				map[string]string{"GIT_EDITOR": "true"},
			)
			if err != nil {
				return rebaseOutcome{}, err
			}
			if cont.Code == 0 {
				llmNote := "LLM resolver fixed " + strconv.Itoa(len(remaining)) +
					" rebase conflict(s): " + resolved.Summary
				notes := []string{}
				if structuralNote != "" {
					notes = append(notes, structuralNote)
				}
				notes = append(notes, llmNote)
				return rebaseOutcome{ok: true, resolverNote: strings.Join(notes, "; ")}, nil
			}
			if _, err := m.run([]string{"git", "rebase", "--abort"}, input.Worktree, nil); err != nil {
				return rebaseOutcome{}, err
			}
			report, err := m.writeReport(
				"rebase",
				"LLM resolver wrote resolutions but git rebase --continue failed:\n"+
					resultStdout(cont)+"\n"+resultStderr(cont),
			)
			if err != nil {
				return rebaseOutcome{}, err
			}
			return rebaseOutcome{
				ok: false,
				summary: "rebase --continue failed after LLM resolution; worktree left at " +
					input.Worktree,
				conflictPath: report,
			}, nil
		}

		if _, err := m.run([]string{"git", "rebase", "--abort"}, input.Worktree, nil); err != nil {
			return rebaseOutcome{}, err
		}
		report, err := m.writeReport(
			"rebase",
			"LLM resolver failed: "+resolved.Summary+"\n\nGit output:\n"+
				resultStdout(rebase)+"\n"+resultStderr(rebase),
		)
		if err != nil {
			return rebaseOutcome{}, err
		}
		return rebaseOutcome{
			ok: false,
			summary: "LLM resolver could not handle conflicts; " + resolved.Summary +
				"; worktree left at " + input.Worktree,
			conflictPath: report,
		}, nil
	}

	if _, err := m.run([]string{"git", "rebase", "--abort"}, input.Worktree, nil); err != nil {
		return rebaseOutcome{}, err
	}
	report, err := m.writeReport(
		"rebase",
		resultStdout(rebase)+"\n"+resultStderr(rebase),
	)
	if err != nil {
		return rebaseOutcome{}, err
	}
	return rebaseOutcome{
		ok: false,
		summary: "rebase onto " + input.TargetBranch +
			" produced conflicts; worktree left at " + input.Worktree,
		conflictPath: report,
	}, nil
}

func (m *LeafMerger) mergeNoFF() (mergeStepOutcome, error) {
	input := m.input
	title := compact(input.TaskTitle, 80)
	message := "leaf " + input.TaskID + " → " + input.TargetBranch + ": " + title
	merge, err := m.run(
		gitWithCommitter(
			"merge", "--no-ff", m.branch,
			"-m", attribution.AppendCommitTrailer(message),
		),
		input.MergeAt,
		nil,
	)
	if err != nil {
		return mergeStepOutcome{}, err
	}
	if merge.Code == 0 {
		return mergeStepOutcome{ok: true}, nil
	}

	conflictedFiles, err := m.listConflictedFiles(input.MergeAt)
	if err != nil {
		return mergeStepOutcome{}, err
	}
	structural := structuralResult{remaining: conflictedFiles}
	if len(conflictedFiles) > 0 {
		structural = m.structuralFastPath(input.MergeAt, conflictedFiles)
	}
	remaining := structural.remaining
	structuralNote := ""
	if len(structural.resolved) > 0 {
		structuralNote = "structural resolver fixed " +
			strconv.Itoa(len(structural.resolved)) + " merge-step file(s)"
	}

	if len(conflictedFiles) > 0 && len(remaining) == 0 {
		if _, err := m.run([]string{"git", "add", "-A"}, input.MergeAt, nil); err != nil {
			return mergeStepOutcome{}, err
		}
		commitMessage := message + " (structural-resolved " +
			strconv.Itoa(len(structural.resolved)) + " file(s))"
		commit, err := m.run(
			gitWithCommitter(
				"commit", "--no-edit",
				"-m", attribution.AppendCommitTrailer(commitMessage),
			),
			input.MergeAt,
			map[string]string{"GIT_EDITOR": "true"},
		)
		if err != nil {
			return mergeStepOutcome{}, err
		}
		if commit.Code == 0 {
			return mergeStepOutcome{ok: true, resolverNote: structuralNote}, nil
		}
		if _, err := m.run([]string{"git", "merge", "--abort"}, input.MergeAt, nil); err != nil {
			return mergeStepOutcome{}, err
		}
		report, err := m.writeReport(
			"merge",
			"Structural resolver staged all conflicts but git commit failed:\n"+
				resultStdout(commit)+"\n"+resultStderr(commit),
		)
		if err != nil {
			return mergeStepOutcome{}, err
		}
		return mergeStepOutcome{
			ok: false,
			summary: "merge commit failed after structural resolution; worktree at " +
				input.Worktree,
			conflictPath: report,
		}, nil
	}

	if input.ResolveConflicts != nil && len(remaining) > 0 {
		resolved, err := input.ResolveConflicts(ConflictArgs{
			Worktree: input.MergeAt, ConflictedFiles: remaining,
			TaskID: input.TaskID, TaskTitle: input.TaskTitle,
			TargetBranch: input.TargetBranch,
		})
		if err != nil {
			return mergeStepOutcome{}, err
		}
		if resolved.OK {
			if _, err := m.run([]string{"git", "add", "-A"}, input.MergeAt, nil); err != nil {
				return mergeStepOutcome{}, err
			}
			commitMessage := message + " (LLM-resolved " +
				strconv.Itoa(len(remaining)) + " file(s))"
			commit, err := m.run(
				gitWithCommitter(
					"commit", "--no-edit",
					"-m", attribution.AppendCommitTrailer(commitMessage),
				),
				input.MergeAt,
				map[string]string{"GIT_EDITOR": "true"},
			)
			if err != nil {
				return mergeStepOutcome{}, err
			}
			if commit.Code == 0 {
				llmNote := "LLM resolver fixed " + strconv.Itoa(len(remaining)) +
					" merge-step file(s): " + resolved.Summary
				notes := []string{}
				if structuralNote != "" {
					notes = append(notes, structuralNote)
				}
				notes = append(notes, llmNote)
				return mergeStepOutcome{ok: true, resolverNote: strings.Join(notes, "; ")}, nil
			}
			if _, err := m.run([]string{"git", "merge", "--abort"}, input.MergeAt, nil); err != nil {
				return mergeStepOutcome{}, err
			}
			report, err := m.writeReport(
				"merge",
				"LLM resolver wrote resolutions but git commit failed:\n"+
					resultStdout(commit)+"\n"+resultStderr(commit),
			)
			if err != nil {
				return mergeStepOutcome{}, err
			}
			return mergeStepOutcome{
				ok: false,
				summary: "merge commit failed after LLM resolution; worktree at " +
					input.Worktree,
				conflictPath: report,
			}, nil
		}

		if _, err := m.run([]string{"git", "merge", "--abort"}, input.MergeAt, nil); err != nil {
			return mergeStepOutcome{}, err
		}
		report, err := m.writeReport(
			"merge",
			"LLM resolver failed at merge step: "+resolved.Summary+"\n\nGit output:\n"+
				resultStdout(merge)+"\n"+resultStderr(merge),
		)
		if err != nil {
			return mergeStepOutcome{}, err
		}
		return mergeStepOutcome{
			ok: false,
			summary: "LLM resolver could not handle merge conflicts; " +
				resolved.Summary + "; worktree at " + input.Worktree,
			conflictPath: report,
		}, nil
	}

	if _, err := m.run([]string{"git", "merge", "--abort"}, input.MergeAt, nil); err != nil {
		return mergeStepOutcome{}, err
	}
	report, err := m.writeReport(
		"merge",
		resultStdout(merge)+"\n"+resultStderr(merge),
	)
	if err != nil {
		return mergeStepOutcome{}, err
	}
	return mergeStepOutcome{
		ok: false,
		summary: "git merge --no-ff produced conflicts after clean rebase; worktree at " +
			input.Worktree,
		conflictPath: report,
	}, nil
}

func (m *LeafMerger) cleanup() error {
	if _, err := m.run(
		[]string{"git", "worktree", "remove", "--force", m.input.Worktree},
		m.input.Workspace,
		nil,
	); err != nil {
		return err
	}
	_, err := m.run(
		[]string{"git", "branch", "-D", m.branch},
		m.input.Workspace,
		nil,
	)
	return err
}

type structuralResult struct {
	resolved  []string
	remaining []string
}

func (m *LeafMerger) structuralFastPath(dir string, files []string) (result structuralResult) {
	result = structuralResult{
		resolved: []string{}, remaining: append([]string{}, files...),
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Warn("structural fast path errored; escalating all conflicts", map[string]any{
				"taskID": m.input.TaskID, "error": sliceUTF16(fmt.Sprint(recovered), 200),
			})
			result = structuralResult{
				resolved: []string{}, remaining: append([]string{}, files...),
			}
		}
	}()
	outcome, err := mergestructural.ResolveConflictedFilesStructurally(
		mergestructural.ResolveOptions{
			Dir: dir, ConflictedFiles: files,
			Structural: m.input.StructuralMerge, StructuralSet: m.input.StructuralMergeSet,
		},
	)
	if err != nil {
		log.Warn("structural fast path errored; escalating all conflicts", map[string]any{
			"taskID": m.input.TaskID, "error": sliceUTF16(err.Error(), 200),
		})
		return structuralResult{
			resolved: []string{}, remaining: append([]string{}, files...),
		}
	}
	if len(outcome.Resolved) > 0 {
		log.Info("structural resolver cleared conflicts", map[string]any{
			"taskID": m.input.TaskID, "dir": dir,
			"resolvedStructurally": len(outcome.Resolved),
			"escalated":            len(outcome.Remaining),
			"resolvedFiles":        firstStrings(outcome.Resolved, 20),
		})
	}
	return structuralResult{
		resolved: outcome.Resolved, remaining: outcome.Remaining,
	}
}

func (m *LeafMerger) listConflictedFiles(dir string) ([]string, error) {
	status, err := m.run(
		[]string{"git", "diff", "--name-only", "--diff-filter=U"},
		dir,
		nil,
	)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(resultStdout(status), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = jscompat.Trim(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out, nil
}

func (m *LeafMerger) writeReport(stage, output string) (string, error) {
	dir := filepath.Join(m.input.Workspace, ".plandb", "conflicts")
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return "", err
	}
	file := filepath.Join(dir, m.input.TaskID+".md")
	lines := []string{
		"# Merge conflict: " + m.input.TaskID,
		"",
		"Stage: `" + stage + "`",
		"Target branch: `" + m.input.TargetBranch + "`",
		"Task title: " + m.input.TaskTitle,
		"Worktree: `.plandb/wt-" + m.input.TaskID + "`",
		"Branch: `" + m.branch + "`",
		"",
		"## Output",
		"",
		"```",
		sliceUTF16(output, 4000),
		"```",
		"",
		"## Resolution",
		"",
		"The leaf's worktree was left intact for inspection. To resolve manually:",
		"",
		"```",
		"cd .plandb/wt-" + m.input.TaskID,
		"# inspect conflicts, fix files, then",
		"git add -A && git rebase --continue   # or: git rebase --abort to discard",
		"```",
		"",
		"Once resolved and the leaf branch is rebased on " + m.input.TargetBranch + ", retry the merge by hand:",
		"",
		"```",
		"git -C " + m.input.Workspace + " merge --no-ff " + m.branch,
		"git -C " + m.input.Workspace + " worktree remove --force " + m.input.Worktree,
		"git -C " + m.input.Workspace + " branch -D " + m.branch,
		"```",
		"",
	}
	body := strings.Join(lines, "\n")
	if err := os.WriteFile(file, []byte(body), 0o666); err != nil {
		return "", err
	}
	return file, nil
}

func (m *LeafMerger) run(argv []string, cwd string, env map[string]string) (Result, error) {
	return m.runner.Run(argv, RunOptions{CWD: cwd, Env: env})
}

func gitWithCommitter(args ...string) []string {
	return attribution.GitArgv(args...)
}

func compact(text string, limit int) string {
	if utf16Len(text) <= limit {
		return text
	}
	return sliceUTF16(text, limit-3) + "..."
}

func phantomDirty(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "nothing to commit") ||
		strings.Contains(lower, "nothing added to commit") ||
		strings.Contains(lower, "no changes added to commit")
}

func resultStdout(result Result) string {
	return strings.ToValidUTF8(string(result.Stdout), "\uFFFD")
}

func resultStderr(result Result) string {
	return strings.ToValidUTF8(string(result.Stderr), "\uFFFD")
}

func utf16Len(value string) int {
	return len(utf16.Encode([]rune(value)))
}

func sliceUTF16(value string, limit int) string {
	units := utf16.Encode([]rune(value))
	if len(units) <= limit {
		return value
	}
	return string(utf16.Decode(units[:limit]))
}

func firstStrings(values []string, limit int) []string {
	if len(values) <= limit {
		return values
	}
	return values[:limit]
}

type processRunner struct{}

func (processRunner) Run(argv []string, opts RunOptions) (Result, error) {
	if len(argv) == 0 {
		return Result{Code: 1, Stderr: []byte("Command is required")}, nil
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = opts.CWD
	if opts.Env != nil {
		overridden := make(map[string]struct{}, len(opts.Env))
		for key := range opts.Env {
			overridden[key] = struct{}{}
		}
		for _, pair := range os.Environ() {
			key := pair
			if equals := strings.IndexByte(pair, '='); equals >= 0 {
				key = pair[:equals]
			}
			if _, found := overridden[key]; !found {
				cmd.Env = append(cmd.Env, pair)
			}
		}
		for key, value := range opts.Env {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return Result{Code: 0, Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return Result{
			Code: exitErr.ExitCode(), Stdout: stdout.Bytes(), Stderr: stderr.Bytes(),
		}, nil
	}
	return Result{
		Code: 1, Stdout: stdout.Bytes(), Stderr: []byte(fmt.Sprint(err)),
	}, nil
}
