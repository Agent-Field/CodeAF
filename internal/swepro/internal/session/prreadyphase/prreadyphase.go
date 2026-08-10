// Package prreadyphase ports src/session/pr-ready-phase.ts:1-414 from swe-pro
// commit 3b25a1a. It runs the read-only PR-shape planner followed by the
// branch-formatting executor and validates their handoff artifacts.
package prreadyphase

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"
)

const (
	PlanRelativePath    = ".codeaf/pr-ready-plan.md"
	SummaryRelativePath = ".codeaf/pr-ready-summary.md"
)

type Input struct {
	Workspace       string
	ParentSessionID string
	PromptOps       PromptOps
	UserPrompt      string
	BaseSHA         string
	TimeoutMS       *float64
}

type Result struct {
	Status      string  `json:"status"`
	Reason      *string `json:"reason,omitempty"`
	PlanPath    *string `json:"planPath,omitempty"`
	SummaryPath *string `json:"summaryPath,omitempty"`
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

type PlannerTools struct {
	Edit       bool `json:"edit"`
	ApplyPatch bool `json:"apply_patch"`
	Task       bool `json:"task"`
	PlanDB     bool `json:"plandb"`
	TodoWrite  bool `json:"todowrite"`
}

type FormatterTools struct {
	Task      bool `json:"task"`
	PlanDB    bool `json:"plandb"`
	TodoWrite bool `json:"todowrite"`
}

type PromptRequest struct {
	MessageID string      `json:"messageID"`
	SessionID string      `json:"sessionID"`
	Model     PromptModel `json:"model"`
	Agent     string      `json:"agent"`
	Tools     any         `json:"tools"`
	Parts     []any       `json:"parts"`
	Workspace string      `json:"-"`
}

type StageInput struct {
	Workspace       string      `json:"workspace"`
	ParentSessionID string      `json:"parentSessionID"`
	UserPrompt      string      `json:"userPrompt"`
	BaseSHA         string      `json:"baseSha"`
	Model           PromptModel `json:"model"`
	TimeoutMS       float64     `json:"timeoutMs"`
	PlanPath        *string     `json:"planPath,omitempty"`
}

// BuildPlannerPrompt returns stage one's model-visible prompt.
func BuildPlannerPrompt(input StageInput) string {
	return strings.Join([]string{
		"You are the pr-ready-planner. Investigate the project at /workspace",
		"and produce a project-specific plan for getting the completed,",
		"audit-passed work into the shape a maintainer would accept. Write the",
		"plan to " + PlanRelativePath + " (relative to the workspace).",
		"",
		"## Original user request (so you know what the work was supposed to do)",
		"",
		truncateUserPrompt(input.UserPrompt),
		"",
		"## Base commit",
		"",
		"Base SHA: " + input.BaseSHA,
		"Diff to inspect: `git diff " + input.BaseSHA + "..HEAD --stat` and",
		"  `git log --oneline " + input.BaseSHA + "..HEAD`",
		"",
		"## What you must do",
		"",
		"Read your agent definition for HOW to investigate (CONTRIBUTING.md,",
		"recent merged PRs via `gh`, conventions in the repo). Produce a",
		"project-specific prose plan — not a generic checklist. End the plan",
		"with a single line: `COMPLEXITY: linear` or `COMPLEXITY: multi-step`.",
	}, "\n")
}

func BuildPlannerReminder() PromptPart {
	return PromptPart{
		Type: "text", Synthetic: true,
		Text: strings.Join([]string{
			"<system-reminder>",
			"You are running as the pr-ready-planner subagent.",
			"Read-only mode: investigate, plan, write the plan file.",
			"DO NOT edit any source files in the worktree.",
			"Tools enabled: read, grep, glob, bash, write (for the plan file only).",
			"",
			"Output: write your plan to " + PlanRelativePath + ".",
			"",
			"Your plan is the spec the pr-formatter will execute. Make it",
			"specific to THIS project — what you observed in CONTRIBUTING.md,",
			"recent merged PRs, the existing git log, the directory structure.",
			"End with COMPLEXITY: linear or COMPLEXITY: multi-step.",
			"</system-reminder>",
		}, "\n"),
	}
}

// BuildFormatterPrompt returns stage two's model-visible prompt.
func BuildFormatterPrompt(input StageInput) string {
	planPath := ""
	if input.PlanPath != nil {
		planPath = *input.PlanPath
	}
	return strings.Join([]string{
		"You are the pr-formatter. Read the plan and execute it.",
		"",
		"Plan path: " + planPath,
		"Workspace: " + input.Workspace,
		"Base commit: " + input.BaseSHA,
		"",
		"## The original user request (for context — what the work was for)",
		"",
		truncateUserPrompt(input.UserPrompt),
		"",
		"## What you must do",
		"",
		"Read " + PlanRelativePath + ". Execute the plan. The plan is the spec — it tells",
		"you WHAT; you choose HOW.",
		"",
		"When you're done, write a short summary to",
		SummaryRelativePath + " describing what changed in the branch state, the",
		"final commit list, files cleaned up, and where any PR body content",
		"you wrote lives. This is the final handoff.",
		"",
		"Before any history rewrite, capture HEAD as a safety tag",
		"(`git tag wip-backup-$(date +%s)`). Verify net diff against base is",
		"unchanged after rewriting. Run the project's build at the end and",
		"confirm it still succeeds.",
	}, "\n")
}

func BuildFormatterReminder(planPath string) PromptPart {
	return PromptPart{
		Type: "text", Synthetic: true,
		Text: strings.Join([]string{
			"<system-reminder>",
			"You are running as the pr-formatter subagent.",
			"Full write/edit/bash access. Read the plan, then execute it.",
			"",
			"Plan path: " + planPath,
			"Output summary path: " + SummaryRelativePath,
			"",
			"Safety: tag HEAD before history rewrites. Verify net diff after.",
			"Run the project's build at the end.",
			"Do NOT push to remote. Do NOT open a PR. Leave that to the user.",
			"</system-reminder>",
		}, "\n"),
	}
}

// RunPRReadyPhase executes both stages.
func RunPRReadyPhase(
	ctx context.Context, input Input, deps Dependencies,
) (Result, error) {
	if deps.Models == nil {
		return Result{}, errors.New("pr-ready-phase: nil model resolver")
	}
	candidates := deps.Models.CandidatesForTier("high")
	if len(candidates) == 0 {
		reason := "no HIGH-tier model available"
		return Result{Status: "skipped", Reason: &reason}, nil
	}
	timeoutMS := float64(30 * 60_000)
	if input.TimeoutMS != nil {
		timeoutMS = *input.TimeoutMS
	}
	stage := StageInput{
		Workspace: input.Workspace, ParentSessionID: input.ParentSessionID,
		UserPrompt: input.UserPrompt, BaseSHA: input.BaseSHA,
		Model: splitModel(candidates[0]), TimeoutMS: timeoutMS,
	}
	planPath, err := dispatchPlanner(ctx, input.PromptOps, stage, deps)
	if err != nil {
		return Result{}, err
	}
	if planPath == nil {
		reason := "planner produced no plan file"
		return Result{Status: "planner-failed", Reason: &reason}, nil
	}
	stage.PlanPath = planPath
	summaryPath, err := dispatchFormatter(ctx, input.PromptOps, stage, deps)
	if err != nil {
		return Result{}, err
	}
	if summaryPath == nil {
		reason := "formatter produced no summary file"
		return Result{
			Status: "formatter-failed", Reason: &reason, PlanPath: planPath,
		}, nil
	}
	return Result{
		Status: "completed", PlanPath: planPath, SummaryPath: summaryPath,
	}, nil
}

func dispatchPlanner(
	ctx context.Context, promptOps PromptOps, input StageInput, deps Dependencies,
) (*string, error) {
	if deps.Sessions == nil || promptOps == nil {
		return nil, errors.New("pr-ready-phase: missing prompt/session service")
	}
	sessionID, err := deps.Sessions.Create(ctx, input.ParentSessionID, "pr-ready-planner")
	if err != nil {
		return nil, err
	}
	messageID := ""
	if deps.NewMessageID != nil {
		messageID = deps.NewMessageID()
	}
	promptParts, err := promptOps.ResolvePromptParts(ctx, BuildPlannerPrompt(input))
	if err != nil {
		return nil, err
	}
	parts := []any{BuildPlannerReminder()}
	parts = append(parts, promptParts...)
	attemptCtx, cancel := context.WithTimeout(ctx, timeoutDuration(input.TimeoutMS))
	_, _ = promptOps.Prompt(attemptCtx, PromptRequest{
		MessageID: messageID, SessionID: sessionID, Model: input.Model,
		Agent: "pr-ready-planner", Tools: PlannerTools{}, Parts: parts,
		Workspace: input.Workspace,
	})
	cancel()
	path := filepath.Join(input.Workspace, PlanRelativePath)
	if fileSize(path) <= 0 {
		return nil, nil
	}
	return &path, nil
}

func dispatchFormatter(
	ctx context.Context, promptOps PromptOps, input StageInput, deps Dependencies,
) (*string, error) {
	sessionID, err := deps.Sessions.Create(ctx, input.ParentSessionID, "pr-formatter")
	if err != nil {
		return nil, err
	}
	messageID := ""
	if deps.NewMessageID != nil {
		messageID = deps.NewMessageID()
	}
	promptParts, err := promptOps.ResolvePromptParts(ctx, BuildFormatterPrompt(input))
	if err != nil {
		return nil, err
	}
	planPath := ""
	if input.PlanPath != nil {
		planPath = *input.PlanPath
	}
	parts := []any{BuildFormatterReminder(planPath)}
	parts = append(parts, promptParts...)
	attemptCtx, cancel := context.WithTimeout(ctx, timeoutDuration(input.TimeoutMS))
	_, _ = promptOps.Prompt(attemptCtx, PromptRequest{
		MessageID: messageID, SessionID: sessionID, Model: input.Model,
		Agent: "pr-formatter", Tools: FormatterTools{}, Parts: parts,
		Workspace: input.Workspace,
	})
	cancel()
	path := filepath.Join(input.Workspace, SummaryRelativePath)
	if fileSize(path) <= 0 {
		return nil, nil
	}
	return &path, nil
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return -1
	}
	return info.Size()
}

func truncateUserPrompt(value string) string {
	units := utf16.Encode([]rune(value))
	if len(units) <= 3000 {
		return value
	}
	return string(utf16.Decode(units[:3000])) + "\n\n[…truncated…]"
}

func splitModel(value string) PromptModel {
	parts := strings.Split(value, "/")
	return PromptModel{ProviderID: parts[0], ModelID: strings.Join(parts[1:], "/")}
}

func timeoutDuration(milliseconds float64) time.Duration {
	return time.Duration(milliseconds * float64(time.Millisecond))
}
