// Package merger is a bug-for-bug port of
// src/session/merger.ts:1-231 (swe-pro 3b25a1a). It captures the conflicted
// worktree state, assembles the semantic Merger prompt, dispatches the
// structured agent through a narrow agent-json seam, and verifies that a
// reported resolution no longer contains conflict-marker lines.
//
// The TypeScript dispatch ultimately folds prompt failure, timeout, and panic
// into the same safe fallback. Dispatcher receives a context with the source's
// 15-minute deadline; every dispatcher error, panic, deadline, or invalid
// typed result returns the one fallback branch here as well.
package merger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/logshim"
)

var log = logshim.Create(map[string]any{"service": "session.merger"})

const mergerTimeoutMS = 15 * 60_000

// FileTouched is one successfully resolved file.
type FileTouched struct {
	File    string `json:"file"`
	Summary string `json:"summary"`
}

// UnresolvedFile is one conflict the agent could not resolve.
type UnresolvedFile struct {
	File   string `json:"file"`
	Detail string `json:"detail"`
}

// MergerDecision is the strict structured agent result. Nil slices encode as
// the required JSON null; nonnil empty slices encode as [].
type MergerDecision struct {
	Result       string           `json:"result"`
	Reason       string           `json:"reason"`
	FilesTouched []FileTouched    `json:"files_touched"`
	Unresolved   []UnresolvedFile `json:"unresolved"`
}

// MergerFallback is the safe default from merger.ts.
var MergerFallback = MergerDecision{
	Result: "unresolvable",
	Reason: "Merger produced no parseable JSON after retries. Defaulting to unresolvable — " +
		"scheduler will route through existing conflict-recovery path.",
	FilesTouched: nil,
	Unresolved: []UnresolvedFile{
		{
			File: "(unknown)",
			Detail: "merger-fallback: agent failed to emit valid JSON; " +
				"worktree may still contain conflict markers",
		},
	},
}

// DecisionSchema is the narrow strict-schema seam passed to Dispatcher.
type DecisionSchema struct{}

// Validate applies the MergerDecisionSchema value constraints.
func (DecisionSchema) Validate(decision MergerDecision) error {
	if decision.Result != "resolved" && decision.Result != "unresolvable" {
		return errors.New("result must be resolved or unresolvable")
	}
	reasonLength := utf16Length(decision.Reason)
	if reasonLength < 1 || reasonLength > 4000 {
		return errors.New("reason must contain 1 to 4000 characters")
	}
	for _, file := range decision.FilesTouched {
		if utf16Length(file.File) < 1 || utf16Length(file.Summary) < 1 {
			return errors.New("files_touched entries require nonempty file and summary")
		}
	}
	for _, file := range decision.Unresolved {
		if utf16Length(file.File) < 1 || utf16Length(file.Detail) < 1 {
			return errors.New("unresolved entries require nonempty file and detail")
		}
	}
	return nil
}

// ParseDecision reproduces the schema's strict-object JSON boundary.
func ParseDecision(raw []byte) (MergerDecision, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return MergerDecision{}, err
	}
	required := []string{"result", "reason", "files_touched", "unresolved"}
	if len(object) != len(required) {
		return MergerDecision{}, errors.New("merger decision must contain exactly four keys")
	}
	for _, key := range required {
		if _, ok := object[key]; !ok {
			return MergerDecision{}, errors.New("merger decision missing key " + key)
		}
	}
	if err := validateStrictObjectArray(
		object["files_touched"], "files_touched", []string{"file", "summary"},
	); err != nil {
		return MergerDecision{}, err
	}
	if err := validateStrictObjectArray(
		object["unresolved"], "unresolved", []string{"file", "detail"},
	); err != nil {
		return MergerDecision{}, err
	}
	var decision MergerDecision
	if err := json.Unmarshal(raw, &decision); err != nil {
		return MergerDecision{}, err
	}
	if err := (DecisionSchema{}).Validate(decision); err != nil {
		return MergerDecision{}, err
	}
	return decision, nil
}

func validateStrictObjectArray(
	raw json.RawMessage,
	field string,
	required []string,
) error {
	if string(raw) == "null" {
		return nil
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return err
	}
	for _, entry := range entries {
		if len(entry) != len(required) {
			return errors.New(field + " entry has unknown or missing keys")
		}
		for _, key := range required {
			if _, ok := entry[key]; !ok {
				return errors.New(field + " entry missing key " + key)
			}
		}
	}
	return nil
}

// Input is DispatchMergerInput.
type Input struct {
	Workspace       string
	ParentSessionID string
	PromptOps       any
	Worktree        string
	MergeAt         string
	TaskID          string
	TaskTitle       string
	SourceBranch    string
	TargetBranch    string
	UserGoal        string
	PriorFailure    string
}

// ProcessResult is the Process.run result consumed by capture and verify.
type ProcessResult struct {
	Code   int
	Stdout []byte
	Stderr []byte
}

// Runner is the process seam for git and grep.
type Runner interface {
	Run(argv []string, cwd string) (ProcessResult, error)
}

// RunnerFunc adapts a function to Runner.
type RunnerFunc func(argv []string, cwd string) (ProcessResult, error)

// Run implements Runner.
func (f RunnerFunc) Run(argv []string, cwd string) (ProcessResult, error) {
	return f(argv, cwd)
}

// ToolOverrides is the exact object supplied by dispatchMerger.
type ToolOverrides struct {
	Edit       bool `json:"edit"`
	ApplyPatch bool `json:"apply_patch"`
}

// DispatchRequest is the narrow agent-json input used by this module.
type DispatchRequest struct {
	Agent           string
	ParentSessionID string
	Workspace       string
	TaskPrompt      string
	OutputPath      string
	Schema          DecisionSchema
	Fallback        MergerDecision
	MaxRetries      int
	PromptOps       any
	Label           string
	Tools           ToolOverrides
	TimeoutMS       int
}

// DispatchResult is agent-json's structured return.
type DispatchResult struct {
	Data         MergerDecision
	FirstTry     bool
	UsedFallback bool
}

// Dispatcher is the single-method agent-json seam.
type Dispatcher interface {
	Dispatch(ctx context.Context, request DispatchRequest) (DispatchResult, error)
}

// DispatcherFunc adapts a function to Dispatcher.
type DispatcherFunc func(ctx context.Context, request DispatchRequest) (DispatchResult, error)

// Dispatch implements Dispatcher.
func (f DispatcherFunc) Dispatch(
	ctx context.Context,
	request DispatchRequest,
) (DispatchResult, error) {
	return f(ctx, request)
}

// Dependencies contains the two effect seams. CallTimeout is test-only; zero
// uses the source's 15-minute timeout and never changes request.TimeoutMS.
type Dependencies struct {
	Runner      Runner
	Dispatcher  Dispatcher
	CallTimeout time.Duration
}

// DispatchMerger captures state, builds the prompt, and invokes agent-json.
func DispatchMerger(input Input, deps Dependencies) DispatchResult {
	runner := deps.Runner
	if runner == nil {
		runner = processRunner{}
	}
	status := captureGitStatus(input.Worktree, runner)
	conflictFiles := captureConflictFiles(input.Worktree, runner)
	outputPath := filepath.Join(
		input.Worktree, ".codeaf", "agents", "merger", input.TaskID+".json",
	)
	request := DispatchRequest{
		Agent: "merger", ParentSessionID: input.ParentSessionID,
		Workspace:  input.Worktree,
		TaskPrompt: BuildMergerPrompt(input, status, conflictFiles, outputPath),
		OutputPath: outputPath, Schema: DecisionSchema{},
		Fallback: MergerFallback, MaxRetries: 1, PromptOps: input.PromptOps,
		Label:     "merger",
		Tools:     ToolOverrides{Edit: true, ApplyPatch: true},
		TimeoutMS: mergerTimeoutMS,
	}

	timeout := deps.CallTimeout
	if timeout == 0 {
		timeout = time.Duration(mergerTimeoutMS) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	result, err := invokeDispatcher(ctx, deps.Dispatcher, request)
	if err != nil || ctx.Err() != nil {
		return fallbackResult()
	}
	if err := request.Schema.Validate(result.Data); err != nil {
		return fallbackResult()
	}
	return result
}

// BuildMergerPrompt returns the exact semantic-merger task prompt.
func BuildMergerPrompt(
	input Input,
	status string,
	conflictFiles []string,
	outputPath string,
) string {
	conflicts := "(no conflict markers detected — may already be resolved or pipeline state is unusual)"
	if len(conflictFiles) > 0 {
		lines := make([]string, 0, len(conflictFiles))
		for _, file := range conflictFiles {
			lines = append(lines, "- "+file)
		}
		conflicts = strings.Join(lines, "\n")
	}
	lines := []string{
		"# Semantic merge conflict resolution",
		"",
		"Task: " + input.TaskTitle + " (" + input.TaskID + ")",
		"Source branch: " + input.SourceBranch,
		"Target branch: " + input.TargetBranch,
		"Worktree: " + input.Worktree,
		"",
		"## User goal",
		"",
		input.UserGoal,
		"",
		"## Prior conflict report",
		"",
		"```",
		sliceUTF16(input.PriorFailure, 3000),
		"```",
		"",
		"## Current git status",
		"",
		"```",
		status,
		"```",
		"",
		"## Conflicted files",
		"",
		conflicts,
		"",
		"## Your output",
		"",
		"Resolve the conflict by editing the files in this worktree. Remove ALL conflict markers " +
			"('<<<<<<<', '=======', '>>>>>>>'). When done, write a single JSON MergerDecision object to " +
			outputPath + ".",
		"",
		"You do NOT need to commit — the scheduler will `git add` and commit after you report.",
		"",
		"If the conflict genuinely cannot be resolved within this worktree (intents contradict, " +
			"requires plan changes), pick `unresolvable` and list each remaining conflict file.",
	}
	return strings.Join(lines, "\n")
}

// VerifyResult is verifyMergeResolved's result.
type VerifyResult struct {
	OK               bool     `json:"ok"`
	RemainingMarkers []string `json:"remainingMarkers"`
}

// VerifyMergeResolved scans the worktree for exact seven-character marker
// lines. Runner is variadic to retain a convenient real-process default.
func VerifyMergeResolved(worktree string, injected ...Runner) VerifyResult {
	var runner Runner = processRunner{}
	if len(injected) > 0 && injected[0] != nil {
		runner = injected[0]
	}
	result, err := safeRun(
		runner,
		[]string{"grep", "-rln", "-E", "^(<{7}|>{7}|={7})$", "--include=*", "."},
		worktree,
	)
	if err != nil {
		return VerifyResult{OK: true, RemainingMarkers: []string{}}
	}
	lines := parseMarkerPaths(validUTF8(result.Stdout))
	return VerifyResult{OK: len(lines) == 0, RemainingMarkers: lines}
}

func captureGitStatus(worktree string, runner Runner) string {
	result, err := safeRun(
		runner, []string{"git", "status", "--short"}, worktree,
	)
	if err != nil {
		return "(git status failed)"
	}
	return sliceUTF16(validUTF8(result.Stdout), 4000)
}

func captureConflictFiles(worktree string, runner Runner) []string {
	result, err := safeRun(
		runner,
		[]string{"git", "diff", "--name-only", "--diff-filter=U"},
		worktree,
	)
	if err != nil {
		return []string{}
	}
	lines := strings.Split(validUTF8(result.Stdout), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = jscompat.Trim(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func parseMarkerPaths(stdout string) []string {
	lines := strings.Split(stdout, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = jscompat.Trim(line)
		if line == "" ||
			strings.Contains(line, ".plandb/") ||
			strings.Contains(line, "/.git/") {
			continue
		}
		out = append(out, line)
	}
	return out
}

func fallbackResult() DispatchResult {
	return DispatchResult{
		Data: MergerFallback, FirstTry: false, UsedFallback: true,
	}
}

func invokeDispatcher(
	ctx context.Context,
	dispatcher Dispatcher,
	request DispatchRequest,
) (result DispatchResult, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if recoveredErr, ok := recovered.(error); ok {
				err = recoveredErr
			} else {
				err = fmt.Errorf("%v", recovered)
			}
		}
	}()
	if dispatcher == nil {
		return DispatchResult{}, errors.New("merger dispatcher is required")
	}
	return dispatcher.Dispatch(ctx, request)
}

func safeRun(runner Runner, argv []string, cwd string) (result ProcessResult, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if recoveredErr, ok := recovered.(error); ok {
				err = recoveredErr
			} else {
				err = fmt.Errorf("%v", recovered)
			}
		}
	}()
	return runner.Run(argv, cwd)
}

func validUTF8(value []byte) string {
	return strings.ToValidUTF8(string(value), "\uFFFD")
}

func sliceUTF16(value string, limit int) string {
	units := utf16.Encode([]rune(value))
	if len(units) <= limit {
		return value
	}
	return string(utf16.Decode(units[:limit]))
}

func utf16Length(value string) int {
	return len(utf16.Encode([]rune(value)))
}

type processRunner struct{}

func (processRunner) Run(argv []string, cwd string) (ProcessResult, error) {
	if len(argv) == 0 {
		return ProcessResult{Code: 1, Stderr: []byte("Command is required")}, nil
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = cwd
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return ProcessResult{Code: 0, Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return ProcessResult{
			Code: exitErr.ExitCode(), Stdout: stdout.Bytes(), Stderr: stderr.Bytes(),
		}, nil
	}
	return ProcessResult{
		Code: 1, Stdout: stdout.Bytes(), Stderr: []byte(err.Error()),
	}, nil
}
