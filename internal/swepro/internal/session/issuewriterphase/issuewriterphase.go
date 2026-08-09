// Package issuewriterphase ports src/session/issue-writer-phase.ts:1-415
// from swe-pro commit 3b25a1a. It fans out one free-form issue writer per
// translated DAG task and records the non-empty issue artifacts.
package issuewriterphase

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// DAGDepInput is the issue-writer projection of one translated dependency.
type DAGDepInput struct {
	FromTask string  `json:"from_task"`
	Kind     *string `json:"kind,omitempty"`
}

// DAGTaskInput mirrors issue-writer-phase.ts:39-52.
type DAGTaskInput struct {
	TaskKey string        `json:"taskKey"`
	Title   string        `json:"title"`
	Kind    *string       `json:"kind,omitempty"`
	Tags    []string      `json:"tags,omitempty"`
	Deps    []DAGDepInput `json:"deps,omitempty"`
	Brief   *string       `json:"brief,omitempty"`
}

// Input mirrors IssueWriterPhaseInput.
type Input struct {
	Workspace       string
	ParentSessionID string
	PromptOps       PromptOps
	Tasks           []DAGTaskInput
	IssuesDir       string
	ArchPath        string
	ProductPath     string
	TimeoutMS       *float64
}

type FailedTask struct {
	TaskKey string `json:"taskKey"`
	Reason  string `json:"reason"`
}

// Result mirrors IssueWriterPhaseResult. WrittenByTaskKey is an ordered map
// because the source returns a JavaScript Map populated in task-result order.
type Result struct {
	Total            int
	Written          int
	WrittenByTaskKey *jscompat.OrderedMap[string, string]
	Failed           []FailedTask
}

type PromptOps interface {
	ResolvePromptParts(ctx context.Context, template string) ([]any, error)
	Prompt(ctx context.Context, input any) (any, error)
}

type ModelResolver interface {
	CandidatesForTier(tier string) []string
}

type ModelResolverFunc func(tier string) []string

func (function ModelResolverFunc) CandidatesForTier(tier string) []string {
	return function(tier)
}

type SessionCreator interface {
	Create(ctx context.Context, parentID, agent string) (string, error)
}

type SessionCreatorFunc func(ctx context.Context, parentID, agent string) (string, error)

func (function SessionCreatorFunc) Create(
	ctx context.Context, parentID, agent string,
) (string, error) {
	return function(ctx, parentID, agent)
}

type Dependencies struct {
	Models       ModelResolver
	Sessions     SessionCreator
	NewMessageID func() string
	LookupEnv    func(string) (string, bool)
}

type ResolvedDep struct {
	TaskKey string `json:"taskKey"`
	Title   string `json:"title"`
}

type SingleTaskInput struct {
	Task            DAGTaskInput  `json:"task"`
	ResolvedDeps    []ResolvedDep `json:"resolvedDeps"`
	OutputPath      string        `json:"outputPath"`
	ArchPath        string        `json:"archPath"`
	ProductPath     string        `json:"productPath"`
	Workspace       string        `json:"workspace"`
	ParentSessionID string        `json:"parentSessionID"`
	Model           PromptModel   `json:"model"`
	TimeoutMS       float64       `json:"timeoutMs"`
}

type PromptModel struct {
	ProviderID string `json:"providerID"`
	ModelID    string `json:"modelID"`
}

type PromptPart struct {
	Type      string `json:"type"`
	Synthetic bool   `json:"synthetic"`
	Text      string `json:"text"`
}

type ToolSettings struct {
	Edit       bool `json:"edit"`
	ApplyPatch bool `json:"apply_patch"`
	Task       bool `json:"task"`
	PlanDB     bool `json:"plandb"`
	TodoWrite  bool `json:"todowrite"`
}

type PromptRequest struct {
	MessageID string       `json:"messageID"`
	SessionID string       `json:"sessionID"`
	Model     PromptModel  `json:"model"`
	Agent     string       `json:"agent"`
	Tools     ToolSettings `json:"tools"`
	Parts     []any        `json:"parts"`
	Workspace string       `json:"-"`
}

// BuildTaskPrompt is the exact per-task issue-writer prompt.
func BuildTaskPrompt(input SingleTaskInput) string {
	scopeTag := "scope:medium"
	for _, tag := range input.Task.Tags {
		if strings.HasPrefix(tag, "scope:") {
			scopeTag = tag
			break
		}
	}
	depsLines := "(no upstream dependencies — this is a root task)"
	if len(input.ResolvedDeps) > 0 {
		lines := make([]string, 0, len(input.ResolvedDeps))
		for _, dependency := range input.ResolvedDeps {
			lines = append(lines, "  - "+dependency.TaskKey+": "+dependency.Title)
		}
		depsLines = strings.Join(lines, "\n")
	}
	kind := "code"
	if input.Task.Kind != nil {
		kind = *input.Task.Kind
	}
	return strings.Join([]string{
		"## Your task",
		"",
		"taskKey:      " + input.Task.TaskKey,
		"title:        " + input.Task.Title,
		"kind:         " + kind,
		"scope:        " + strings.TrimPrefix(scopeTag, "scope:"),
		"",
		"deps (upstream siblings whose interfaces you must respect):",
		depsLines,
		"",
		"## Authoritative sources (read these — do not infer)",
		"",
		"Architecture: " + input.ArchPath,
		"Product PRD:  " + input.ProductPath,
		"Workspace:    " + input.Workspace + " (the reference binary lives here if you need to probe behavior)",
		"",
		"## Your output",
		"",
		"Write the issue file to EXACTLY this path: " + input.OutputPath,
		"",
		"Follow the <Issue_file_template> from your agent definition. Every",
		"interface signature must be VERBATIM from arch.md. Every error string",
		"must be VERBATIM from product.md. If a fact is not in arch.md or",
		"product.md (and not observable by probing the binary), mark it as",
		"\"TBD — not specified in arch.md/product.md\". Do NOT invent.",
		"",
		"Per-turn brevity: each `write` or `bash cat >>` call ≤ 3KB. The full",
		"issue file is built across many small writes. Your assistant text per",
		"turn ≤ 6 short lines.",
	}, "\n")
}

// BuildReminder returns the issue-writer synthetic instruction.
func BuildReminder(outputPath string) PromptPart {
	return PromptPart{
		Type: "text", Synthetic: true,
		Text: strings.Join([]string{
			"<system-reminder>",
			"You are running as the issue-writer subagent. ONE TASK only:",
			"write the issue file at " + outputPath + ".",
			"",
			"Anti-hallucination is your hard rule. Every interface signature",
			"is VERBATIM from arch.md; every error string is VERBATIM from",
			"product.md. Unknown facts get \"TBD — not specified in arch.md\".",
			"",
			"Per-turn discipline: each write/cat ≤ 3KB. Build the file across",
			"many short turns, not one long generation.",
			"",
			"Output path: " + outputPath,
			"</system-reminder>",
		}, "\n"),
	}
}

// DispatchIssueWriterPhase runs the unbounded fan-out.
func DispatchIssueWriterPhase(
	ctx context.Context, input Input, deps Dependencies,
) (Result, error) {
	total := len(input.Tasks)
	if total == 0 {
		return Result{
			WrittenByTaskKey: jscompat.NewOrderedMap[string, string](),
			Failed:           []FailedTask{},
		}, nil
	}
	if deps.Models == nil {
		return Result{}, errors.New("issue-writer-phase: nil model resolver")
	}
	candidates := deps.Models.CandidatesForTier("high")
	if len(candidates) == 0 {
		failed := make([]FailedTask, 0, total)
		for _, task := range input.Tasks {
			failed = append(failed, FailedTask{
				TaskKey: task.TaskKey, Reason: "no HIGH-tier model available",
			})
		}
		return Result{
			Total: total, WrittenByTaskKey: jscompat.NewOrderedMap[string, string](),
			Failed: failed,
		}, nil
	}
	if err := os.MkdirAll(input.IssuesDir, 0o777); err != nil {
		return Result{}, err
	}
	model := splitModel(candidates[0])
	titleByKey := map[string]string{}
	for _, task := range input.Tasks {
		titleByKey[task.TaskKey] = task.Title
	}
	timeoutMS := float64(30 * 60_000)
	if input.TimeoutMS != nil {
		timeoutMS = *input.TimeoutMS
	} else {
		lookup := deps.LookupEnv
		if lookup == nil {
			lookup = os.LookupEnv
		}
		if raw, exists := lookup("CODEAF_ISSUE_WRITER_TIMEOUT_MS"); exists {
			timeoutMS = jscompat.ToNumber(raw)
		}
	}

	results := make([]singleResult, total)
	var group sync.WaitGroup
	for index, task := range input.Tasks {
		index, task := index, task
		group.Add(1)
		go func() {
			defer group.Done()
			resolved := make([]ResolvedDep, 0, len(task.Deps))
			for _, dependency := range task.Deps {
				title := titleByKey[dependency.FromTask]
				if title == "" {
					title = "(taskKey not in this DAG)"
				}
				resolved = append(resolved, ResolvedDep{
					TaskKey: dependency.FromTask, Title: title,
				})
			}
			singleInput := SingleTaskInput{
				Task: task, ResolvedDeps: resolved,
				OutputPath: filepath.Join(input.IssuesDir, task.TaskKey+".md"),
				ArchPath:   input.ArchPath, ProductPath: input.ProductPath,
				Workspace: input.Workspace, ParentSessionID: input.ParentSessionID,
				Model: model, TimeoutMS: timeoutMS,
			}
			result, err := dispatchOne(ctx, input.PromptOps, singleInput, deps)
			if err != nil {
				results[index] = singleResult{
					taskKey: task.TaskKey,
					reason:  "dispatch crashed: " + sliceUTF16(err.Error(), 200),
				}
				return
			}
			results[index] = result
		}()
	}
	group.Wait()

	written := jscompat.NewOrderedMap[string, string]()
	failed := []FailedTask{}
	for _, result := range results {
		if result.ok {
			written.Set(result.taskKey, result.outputPath)
		} else {
			failed = append(failed, FailedTask{
				TaskKey: result.taskKey, Reason: result.reason,
			})
		}
	}
	return Result{
		Total: total, Written: written.Len(),
		WrittenByTaskKey: written, Failed: failed,
	}, nil
}

type singleResult struct {
	ok         bool
	taskKey    string
	outputPath string
	reason     string
	bytes      int64
}

func dispatchOne(
	ctx context.Context,
	promptOps PromptOps,
	input SingleTaskInput,
	deps Dependencies,
) (singleResult, error) {
	if deps.Sessions == nil || promptOps == nil {
		return singleResult{}, errors.New("missing prompt/session service")
	}
	sessionID, err := deps.Sessions.Create(ctx, input.ParentSessionID, "issue-writer")
	if err != nil {
		return singleResult{}, err
	}
	// The source allocates this ID but uses a second one in Prompt.
	if deps.NewMessageID != nil {
		_ = deps.NewMessageID()
	}
	promptParts, err := promptOps.ResolvePromptParts(ctx, BuildTaskPrompt(input))
	if err != nil {
		return singleResult{}, err
	}
	messageID := ""
	if deps.NewMessageID != nil {
		messageID = deps.NewMessageID()
	}
	parts := []any{BuildReminder(input.OutputPath)}
	parts = append(parts, promptParts...)
	attemptCtx, cancel := context.WithTimeout(ctx, timeoutDuration(input.TimeoutMS))
	_, _ = promptOps.Prompt(attemptCtx, PromptRequest{
		MessageID: messageID, SessionID: sessionID, Model: input.Model,
		Agent: "issue-writer", Tools: ToolSettings{}, Parts: parts,
		Workspace: input.Workspace,
	})
	cancel()
	stat, statErr := os.Stat(input.OutputPath)
	size := int64(-1)
	if statErr == nil {
		size = stat.Size()
	}
	if size <= 0 {
		return singleResult{
			taskKey: input.Task.TaskKey,
			reason:  "issue file missing or empty at " + input.OutputPath,
		}, nil
	}
	return singleResult{
		ok: true, taskKey: input.Task.TaskKey,
		outputPath: input.OutputPath, bytes: size,
	}, nil
}

func timeoutDuration(milliseconds float64) time.Duration {
	if math.IsNaN(milliseconds) {
		return 0
	}
	if math.IsInf(milliseconds, 1) {
		return time.Duration(math.MaxInt64)
	}
	if math.IsInf(milliseconds, -1) {
		return time.Duration(math.MinInt64)
	}
	return time.Duration(milliseconds * float64(time.Millisecond))
}

func splitModel(value string) PromptModel {
	parts := strings.Split(value, "/")
	return PromptModel{ProviderID: parts[0], ModelID: strings.Join(parts[1:], "/")}
}

func sliceUTF16(value string, maximum int) string {
	units := utf16.Encode([]rune(value))
	if len(units) > maximum {
		units = units[:maximum]
	}
	return string(utf16.Decode(units))
}
