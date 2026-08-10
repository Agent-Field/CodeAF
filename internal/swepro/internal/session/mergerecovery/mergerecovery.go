// Package mergerecovery is a bug-for-bug port of
// src/session/merge-recovery.ts:1-347 (swe-pro 3b25a1a). It gives an injected
// language-model client an exact recovery playbook and five sandboxed tools
// for inspecting and repairing a failed leaf merge.
//
// The AI SDK stream is represented by the single-method Client seam. Tool
// definitions retain source order and model-visible descriptions. Git process
// execution uses Runner; filesystem reads and writes deliberately keep the
// source's lexical (not symlink-resolving) path guard.
package mergerecovery

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/attribution"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/logshim"
)

var log = logshim.Create(map[string]any{"service": "session.merge-recovery"})

const (
	gitLeafDescription = "Run a git command in the LEAF worktree. argv MUST start with 'git'. " +
		"Use this for inspection (git status, git diff, git log), staging " +
		"(git add), commits, rebase ops, skip-worktree, and conflict navigation."
	gitTargetDescription = "Run a git command in the TARGET (mergeAt) directory. argv MUST start " +
		"with 'git'. Use this for the final 'git merge --no-ff <branch>' that " +
		"lands the leaf branch on the target branch."
	readDescription  = "Read a file from the LEAF worktree (path may be absolute or relative to the worktree)."
	writeDescription = "Write a file in the LEAF worktree. Use this to resolve conflict files " +
		"(remove <<<<<<< markers, write the merged result)."
	reportDescription = "REQUIRED terminal call. Report the final outcome of the recovery. " +
		"Call this exactly once at the end, then stop calling tools."
)

// ProcessResult is the Process.run shape consumed by the git tools.
type ProcessResult struct {
	Code   int
	Stdout []byte
	Stderr []byte
}

// Runner is the git process seam. Nonzero exits are Results, not errors.
type Runner interface {
	Run(argv []string, cwd string) (ProcessResult, error)
}

// RunnerFunc adapts a function to Runner.
type RunnerFunc func(argv []string, cwd string) (ProcessResult, error)

// Run implements Runner.
func (f RunnerFunc) Run(argv []string, cwd string) (ProcessResult, error) {
	return f(argv, cwd)
}

// PriorFailure is the failed procedural pipeline result.
type PriorFailure struct {
	Summary      string  `json:"summary"`
	ConflictPath *string `json:"conflictPath,omitempty"`
}

// Input configures one recovery agent call.
type Input struct {
	Language any    `json:"-"`
	Client   Client `json:"-"`
	Runner   Runner `json:"-"`

	Workspace    string       `json:"workspace"`
	Worktree     string       `json:"worktree"`
	MergeAt      string       `json:"mergeAt"`
	TaskID       string       `json:"taskID"`
	TaskTitle    string       `json:"taskTitle"`
	TargetBranch string       `json:"targetBranch"`
	SourceBranch string       `json:"sourceBranch"`
	PriorFailure PriorFailure `json:"priorFailure"`
	MaxSteps     *float64     `json:"maxSteps,omitempty"`
}

// Result is the drop-in LeafMerger recovery outcome.
type Result struct {
	OK           bool
	Summary      string
	ConflictPath string
	ToolCalls    int
}

// ToolDefinition is one AI SDK tool, kept in source declaration order.
type ToolDefinition struct {
	Name        string
	Description string
	InputSchema map[string]any
	Execute     func(arg any) (string, error)
}

// Request is the complete single-stream client request.
type Request struct {
	Model       any
	Prompt      string
	Temperature float64
	MaxRetries  int
	MaxSteps    float64
	Tools       []ToolDefinition
}

// Tool returns a named tool definition, if present.
func (r Request) Tool(name string) (ToolDefinition, bool) {
	for _, tool := range r.Tools {
		if tool.Name == name {
			return tool, true
		}
	}
	return ToolDefinition{}, false
}

// Client drains one recovery stream. It may invoke tools synchronously through
// Request.Tools and should stop after report or MaxSteps model steps.
type Client interface {
	Run(request Request) error
}

// ClientFunc adapts a function to Client.
type ClientFunc func(request Request) error

// Run implements Client.
func (f ClientFunc) Run(request Request) error { return f(request) }

// ToolDescriptions is the model-visible request text fixture shape.
type ToolDescriptions struct {
	GitLeaf   string `json:"git_leaf"`
	GitTarget string `json:"git_target"`
	Read      string `json:"read"`
	Write     string `json:"write"`
	Report    string `json:"report"`
}

// RequestText is the model-visible constant part of Request.
type RequestText struct {
	Temperature  float64          `json:"temperature"`
	MaxRetries   int              `json:"maxRetries"`
	Descriptions ToolDescriptions `json:"descriptions"`
}

// ModelRequestText returns the constants captured from streamText.
func ModelRequestText() RequestText {
	return RequestText{
		Temperature: 0.1,
		MaxRetries:  0,
		Descriptions: ToolDescriptions{
			GitLeaf: gitLeafDescription, GitTarget: gitTargetDescription,
			Read: readDescription, Write: writeDescription, Report: reportDescription,
		},
	}
}

// BuildPlaybook assembles the exact model-visible recovery prompt.
func BuildPlaybook(input Input) string {
	leafRoot := resolvePath(input.Worktree)
	targetRoot := resolvePath(input.MergeAt)
	maxSteps := effectiveMaxSteps(input.MaxSteps)
	return buildPlaybook(input, leafRoot, targetRoot, maxSteps)
}

// LLMMergeRecovery invokes the injected client and returns the captured report
// verdict, or the source degradation result when the stream fails or never
// reports.
func LLMMergeRecovery(input Input) Result {
	maxSteps := effectiveMaxSteps(input.MaxSteps)
	leafRoot := resolvePath(input.Worktree)
	targetRoot := resolvePath(input.MergeAt)
	runner := input.Runner
	if runner == nil {
		runner = processRunner{}
	}
	state := &recoveryState{input: input, runner: runner}
	request := Request{
		Model: input.Language, Prompt: buildPlaybook(input, leafRoot, targetRoot, maxSteps),
		Temperature: 0.1, MaxRetries: 0, MaxSteps: maxSteps,
		Tools: state.tools(leafRoot, targetRoot),
	}

	err := invokeClient(input.Client, request)
	if err != nil {
		message := err.Error()
		log.Warn("recovery stream failed", map[string]any{
			"taskID": input.TaskID, "error": sliceUTF16(message, 300),
		})
		return Result{
			OK:        false,
			Summary:   "recovery stream errored: " + sliceUTF16(message, 200),
			ToolCalls: state.toolCalls,
		}
	}
	if !state.verdict.reported {
		return Result{
			OK: false,
			Summary: "recovery did not call report() within " +
				jscompat.FormatNumber(maxSteps) +
				" steps (last toolCalls=" +
				jscompat.FormatNumber(float64(state.toolCalls)) + ")",
			ToolCalls: state.toolCalls,
		}
	}

	log.Info("recovery completed", map[string]any{
		"taskID": input.TaskID, "ok": state.verdict.ok,
		"toolCalls":   state.toolCalls,
		"summaryHead": sliceUTF16(state.verdict.summary, 120),
	})
	return Result{
		OK: state.verdict.ok, Summary: state.verdict.summary,
		ConflictPath: state.verdict.conflictPath, ToolCalls: state.toolCalls,
	}
}

type verdict struct {
	ok           bool
	summary      string
	conflictPath string
	reported     bool
}

type recoveryState struct {
	input     Input
	runner    Runner
	toolCalls int
	verdict   verdict
}

func (s *recoveryState) tools(leafRoot, targetRoot string) []ToolDefinition {
	return []ToolDefinition{
		{
			Name: "git_leaf", Description: gitLeafDescription,
			InputSchema: argvSchema("Full argv. First element must be 'git'."),
			Execute: func(arg any) (string, error) {
				s.toolCalls++
				argv := argvFromArg(arg)
				if len(argv) == 0 || argv[0] != "git" {
					return "error: argv[0] must be 'git'", nil
				}
				withCommitter := attribution.AppendCommitTrailerToArgv(argv)
				if len(argv) > 1 &&
					(argv[1] == "commit" || argv[1] == "rebase" || argv[1] == "merge") {
					withCommitter = injectCommitter(withCommitter)
				}
				result, err := s.runner.Run(withCommitter, leafRoot)
				if err != nil {
					return "", err
				}
				return formatGitResult(result), nil
			},
		},
		{
			Name: "git_target", Description: gitTargetDescription,
			InputSchema: argvSchema(""),
			Execute: func(arg any) (string, error) {
				s.toolCalls++
				argv := argvFromArg(arg)
				if len(argv) == 0 || argv[0] != "git" {
					return "error: argv[0] must be 'git'", nil
				}
				withCommitter := attribution.AppendCommitTrailerToArgv(argv)
				if len(argv) > 1 && (argv[1] == "commit" || argv[1] == "merge") {
					withCommitter = injectCommitter(withCommitter)
				}
				result, err := s.runner.Run(withCommitter, targetRoot)
				if err != nil {
					return "", err
				}
				return formatGitResult(result), nil
			},
		},
		{
			Name: "read", Description: readDescription,
			InputSchema: pathSchema(false),
			Execute: func(arg any) (string, error) {
				s.toolCalls++
				path := jsString(nullishProperty(arg, "path"))
				abs, ok := pathInsideLeaf(leafRoot, path)
				if !ok {
					return "error: path " + path + " is outside the leaf worktree", nil
				}
				content, err := os.ReadFile(abs)
				if err != nil {
					return "error: " + nodeReadMessage(abs, err), nil
				}
				return truncate(strings.ToValidUTF8(string(content), "\uFFFD"), 8000), nil
			},
		},
		{
			Name: "write", Description: writeDescription,
			InputSchema: pathSchema(true),
			Execute: func(arg any) (string, error) {
				s.toolCalls++
				path := jsString(nullishProperty(arg, "path"))
				content := jsString(nullishProperty(arg, "content"))
				abs, ok := pathInsideLeaf(leafRoot, path)
				if !ok {
					return "error: path " + path + " is outside the leaf worktree", nil
				}
				if strings.Contains(content, "<<<<<<<") {
					return "error: content still contains conflict markers", nil
				}
				if err := os.MkdirAll(filepath.Dir(abs), 0o777); err != nil {
					return "error: " + nodeWriteMessage(filepath.Dir(abs), err), nil
				}
				if err := os.WriteFile(abs, []byte(content), 0o666); err != nil {
					return "error: " + nodeWriteMessage(abs, err), nil
				}
				return "ok: wrote " +
					jscompat.FormatNumber(float64(utf16Length(content))) + " bytes", nil
			},
		},
		{
			Name: "report", Description: reportDescription,
			InputSchema: reportSchema(),
			Execute: func(arg any) (string, error) {
				s.verdict.ok = jsBoolean(property(arg, "ok"))
				s.verdict.summary = jsString(nullishProperty(arg, "summary"))
				conflict := property(arg, "conflict_path")
				if jsBoolean(conflict) {
					s.verdict.conflictPath = jsString(conflict)
				} else {
					s.verdict.conflictPath = ""
				}
				s.verdict.reported = true
				return "verdict recorded; stop now", nil
			},
		},
	}
}

func buildPlaybook(input Input, leafRoot, targetRoot string, maxSteps float64) string {
	lines := []string{
		"You are the LeafMerger Recovery agent.",
		"",
		"A procedural git pipeline tried to merge a completed leaf branch into a",
		"target branch and FAILED. Your job is to inspect the state and drive the",
		"merge to completion (or write a conflict report if truly stuck).",
		"",
		"=== CONTEXT ===",
		"Task ID:           " + input.TaskID,
		"Task title:        " + input.TaskTitle,
		"Leaf worktree:     " + leafRoot,
		"Source branch:     " + input.SourceBranch,
		"Target directory:  " + targetRoot,
		"Target branch:     " + input.TargetBranch,
		"Prior failure:     " + input.PriorFailure.Summary,
		"",
		"=== TOOLS ===",
		` git_leaf({argv})   — run "git ..." in the leaf worktree`,
		` git_target({argv}) — run "git ..." in the target directory`,
		" read({path})       — read a file in the leaf worktree",
		" write({path, content}) — write a file in the leaf worktree (for conflict resolution)",
		" report({ok, summary, conflict_path?}) — REQUIRED terminal call",
		"",
		"=== PLAYBOOK ===",
		`1. Inspect: 'git_leaf {"argv":["git","status","--short"]}' and 'git_leaf {"argv":["git","log","--oneline","-3"]}'`,
		"",
		"2. Phantom-dirty paths (e.g. case-collision on macOS APFS like",
		"   .github/PULL_REQUEST_TEMPLATE.md showing as modified you didn't write):",
		`   suppress with 'git_leaf {"argv":["git","update-index","--skip-worktree","--","<path>"]}'`,
		"",
		`3. Real uncommitted leaf work: 'git_leaf {"argv":["git","add","-A"]}' then`,
		`   'git_leaf {"argv":["git","commit","-m","leaf <id>: <title>"]}'`,
		"",
		`4. Rebase onto target: 'git_leaf {"argv":["git","fetch","--all"]}' (optional),`,
		`   then 'git_leaf {"argv":["git","rebase","` + input.TargetBranch + `"]}'`,
		"",
		`5. On rebase conflicts: 'git_leaf {"argv":["git","diff","--name-only","--diff-filter=U"]}'`,
		"   read each file, resolve manually (write the merged content without",
		`   <<<<<<< markers), 'git_leaf {"argv":["git","add","<file>"]}',`,
		`   'git_leaf {"argv":["git","rebase","--continue"]}'.`,
		"",
		`6. After clean rebase: 'git_target {"argv":["git","merge","--no-ff",`,
		`   "` + input.SourceBranch + `","-m","leaf ` + input.TaskID + ` → ` +
			input.TargetBranch + `: ` + input.TaskTitle + `"]}'`,
		"",
		`7. On success: report({ok: true, summary: "<what you did>"})`,
		"",
		"8. On unrecoverable failure: write a markdown conflict report describing the",
		"   issue + manual-fix steps to <worktree>/.codeaf/RECOVERY_REPORT.md,",
		`   then report({ok: false, summary: "...", conflict_path: "<that path>"})`,
		"",
		"RULES:",
		"- Never call 'git reset --hard' against a remote ref (only HEAD is OK).",
		"- Never delete .git, .plandb, .codeaf directories.",
		"- Always end by calling report() exactly once.",
		"- Be terse. Don't narrate; just call tools.",
		"- Hard cap: " + jscompat.FormatNumber(maxSteps) +
			" tool calls. After that you will be cut off.",
	}
	return strings.Join(lines, "\n")
}

func effectiveMaxSteps(value *float64) float64 {
	if value == nil {
		return 24
	}
	return *value
}

func resolvePath(value string) string {
	abs, err := filepath.Abs(value)
	if err != nil {
		return filepath.Clean(value)
	}
	return filepath.Clean(abs)
}

func pathInsideLeaf(leafRoot, path string) (string, bool) {
	var abs string
	if filepath.IsAbs(path) {
		abs = filepath.Clean(path)
	} else {
		abs = filepath.Clean(filepath.Join(leafRoot, path))
	}
	if abs != leafRoot && !strings.HasPrefix(abs, leafRoot+string(filepath.Separator)) {
		return "", false
	}
	return abs, true
}

func argvFromArg(arg any) []string {
	value := property(arg, "argv")
	switch argv := value.(type) {
	case []string:
		return append([]string{}, argv...)
	case []any:
		out := make([]string, 0, len(argv))
		for _, item := range argv {
			text, ok := item.(string)
			if !ok {
				return []string{}
			}
			out = append(out, text)
		}
		return out
	default:
		return []string{}
	}
}

func injectCommitter(argv []string) []string {
	return attribution.GitArgv(argv[1:]...)
}

func formatGitResult(result ProcessResult) string {
	stdout := truncate(strings.ToValidUTF8(string(result.Stdout), "\uFFFD"), 4000)
	stderr := truncate(strings.ToValidUTF8(string(result.Stderr), "\uFFFD"), 4000)
	return "exit=" + jscompat.FormatNumber(float64(result.Code)) +
		"\nstdout:\n" + stdout + "\nstderr:\n" + stderr
}

func truncate(value string, limit int) string {
	if utf16Length(value) <= limit {
		return value
	}
	return sliceUTF16(value, limit-3) + "..."
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

func property(arg any, key string) any {
	if object, ok := arg.(map[string]any); ok {
		return object[key]
	}
	return nil
}

func nullishProperty(arg any, key string) any {
	value := property(arg, key)
	if value == nil {
		return ""
	}
	return value
}

func jsString(value any) string {
	switch value := value.(type) {
	case nil:
		return "undefined"
	case string:
		return value
	case bool:
		if value {
			return "true"
		}
		return "false"
	case float64:
		return jscompat.FormatNumber(value)
	case float32:
		return jscompat.FormatNumber(float64(value))
	case int:
		return strconv.Itoa(value)
	case []any:
		parts := make([]string, len(value))
		for i, item := range value {
			if item != nil {
				parts[i] = jsString(item)
			}
		}
		return strings.Join(parts, ",")
	default:
		return "[object Object]"
	}
}

func jsBoolean(value any) bool {
	switch value := value.(type) {
	case nil:
		return false
	case bool:
		return value
	case string:
		return value != ""
	case float64:
		return value != 0 && !math.IsNaN(value)
	case float32:
		return value != 0 && !math.IsNaN(float64(value))
	case int:
		return value != 0
	default:
		return true
	}
}

func invokeClient(client Client, request Request) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if recoveredErr, ok := recovered.(error); ok {
				err = recoveredErr
			} else {
				err = fmt.Errorf("%v", recovered)
			}
		}
	}()
	if client == nil {
		return errors.New("recovery client is required")
	}
	return client.Run(request)
}

func nodeReadMessage(path string, err error) string {
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

func nodeWriteMessage(path string, err error) string {
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

func argvSchema(description string) map[string]any {
	argv := map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	if description != "" {
		argv["description"] = description
	}
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{"argv": argv},
		"required":   []string{"argv"},
	}
}

func pathSchema(withContent bool) map[string]any {
	properties := map[string]any{"path": map[string]any{"type": "string"}}
	required := []string{"path"}
	if withContent {
		properties["content"] = map[string]any{"type": "string"}
		required = append(required, "content")
	}
	return map[string]any{
		"type": "object", "properties": properties, "required": required,
	}
}

func reportSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ok": map[string]any{
				"type": "boolean", "description": "true if the merge landed on the target branch.",
			},
			"summary": map[string]any{
				"type": "string", "description": "One-line description of what was done.",
			},
			"conflict_path": map[string]any{
				"type": "string",
				"description": "If ok=false, optional absolute path to a markdown conflict report you wrote " +
					"for the operator to inspect.",
			},
		},
		"required": []string{"ok", "summary"},
	}
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
