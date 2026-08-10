// Package productgate ports src/session/product-gate.ts:1-284 from swe-pro
// commit 3b25a1a. It runs the single-shot product-manager document phase.
package productgate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	pmTimeout     = 30 * time.Minute
	pmMaxAttempts = 3
)

// Input mirrors ProductGateInput.
type Input struct {
	Workspace         string
	ParentSessionID   string
	PromptOps         PromptOps
	UserPrompt        string
	AdditionalContext *string
}

// Result mirrors ProductGateResult.
type Result struct {
	Status  string  `json:"status"`
	PRDPath string  `json:"prdPath"`
	Reason  *string `json:"reason,omitempty"`
}

// PromptOps is the TaskPromptOps projection consumed by this phase.
type PromptOps interface {
	ResolvePromptParts(ctx context.Context, template string) ([]any, error)
	Prompt(ctx context.Context, input any) (any, error)
}

// ModelResolver is the HIGH-tier router projection.
type ModelResolver interface {
	CandidatesForTier(tier string) []string
}

// ModelResolverFunc adapts a function to ModelResolver.
type ModelResolverFunc func(tier string) []string

func (function ModelResolverFunc) CandidatesForTier(tier string) []string {
	return function(tier)
}

// SessionCreator is the child-session seam.
type SessionCreator interface {
	Create(ctx context.Context, parentID, agent string) (string, error)
}

// SessionCreatorFunc adapts a function to SessionCreator.
type SessionCreatorFunc func(ctx context.Context, parentID, agent string) (string, error)

func (function SessionCreatorFunc) Create(
	ctx context.Context, parentID, agent string,
) (string, error) {
	return function(ctx, parentID, agent)
}

// Dependencies are the Effect services read by runProductGate.
type Dependencies struct {
	Models       ModelResolver
	Sessions     SessionCreator
	NewMessageID func() string
}

// PromptPart is one synthetic reminder part.
type PromptPart struct {
	Type      string `json:"type"`
	Synthetic bool   `json:"synthetic"`
	Text      string `json:"text"`
}

// PromptModel is the resolved provider/model pair.
type PromptModel struct {
	ModelID    string `json:"modelID"`
	ProviderID string `json:"providerID"`
}

// ToolSettings is the exact disabled-tool object supplied to the PM.
type ToolSettings struct {
	Edit       bool `json:"edit"`
	ApplyPatch bool `json:"apply_patch"`
	Task       bool `json:"task"`
	PlanDB     bool `json:"plandb"`
	TodoWrite  bool `json:"todowrite"`
}

// PromptRequest is the portable shape passed to PromptOps.Prompt. Workspace is
// an adapter-only projection of InstanceState.directory and is not serialized.
type PromptRequest struct {
	MessageID string       `json:"messageID"`
	SessionID string       `json:"sessionID"`
	Model     PromptModel  `json:"model"`
	Agent     string       `json:"agent"`
	Tools     ToolSettings `json:"tools"`
	Parts     []any        `json:"parts"`
	Workspace string       `json:"-"`
}

// BuildTaskPrompt ports product-gate.ts:94-139 byte-for-byte.
func BuildTaskPrompt(input Input, prdPath string) string {
	additionalBlock := ""
	if input.AdditionalContext != nil && *input.AdditionalContext != "" {
		additionalBlock = "\n## Additional Context\n" + *input.AdditionalContext + "\n"
	}
	return strings.Join([]string{
		"## Goal",
		input.UserPrompt,
		"",
		"## Repository",
		input.Workspace,
		additionalBlock,
		"## How Your PRD Will Be Used",
		"",
		"1. An architect designs the technical solution from your PRD",
		"2. A planner decomposes into plandb tasks with a dependency graph",
		"3. Leaves at the same dependency level execute IN PARALLEL by isolated",
		"   agents in git worktrees",
		"4. A session-end auditor verifies each acceptance criterion LITERALLY",
		"   by running commands",
		"",
		"Write acceptance criteria as test assertions, not human briefings.",
		"",
		"## Your Mission",
		"",
		"Produce a PRD for this goal. Read the codebase first — understand the",
		"current state deeply before defining what needs to change.",
		"",
		"Write your PRD to: " + prdPath,
		"",
		"## Per-turn discipline (HARD RULE)",
		"",
		"Be concise AND detailed in every single turn. Long sustained generations",
		"stall on the provider side and your stream gets killed mid-write.",
		"",
		"- Each `write` call's content stays under ~3KB (roughly 60 lines of",
		"  markdown). If a section is bigger, split it across multiple writes.",
		"- Your assistant message text stays under ~10 short lines. Detail goes",
		"  INTO the file, not into your chat. Bullets, not prose paragraphs.",
		"- Detail comes from MANY small writes, not one huge response. The",
		"  cumulative file at the end has every detail — each individual write",
		"  is short.",
		"",
		"## The bar",
		"",
		"An engineering team of autonomous agents can execute this PRD without",
		"asking a single clarifying question. Every acceptance criterion is a",
		"test they can automate. Every scope boundary is a decision they don't",
		"have to make. Every assumption is a constraint they can rely on.",
	}, "\n")
}

// BuildReminder returns the first-attempt system reminder.
func BuildReminder(prdPath string) PromptPart {
	return PromptPart{
		Type: "text", Synthetic: true,
		Text: strings.Join([]string{
			"<system-reminder>",
			"You are running as the product-manager subagent. Single-shot",
			"dispatch — no review loop. Write the full PRD as free-form markdown",
			"to the exact path given in your prompt. End your turn after writing.",
			"PRD path: " + prdPath,
			"</system-reminder>",
		}, "\n"),
	}
}

// BuildRetryReminder returns the retry-only synthetic reminder.
func BuildRetryReminder(attempt int, prdPath, lastFailReason string) PromptPart {
	if lastFailReason == "" {
		lastFailReason = "unknown"
	}
	return PromptPart{
		Type: "text", Synthetic: true,
		Text: strings.Join([]string{
			"<system-reminder>",
			"RETRY attempt " + intString(attempt) + "/" + intString(pmMaxAttempts) +
				". Your previous attempt did not write the PRD file at " + prdPath + ".",
			"Reason: " + lastFailReason + ".",
			"Write the PRD file now — that is your only deliverable. Use the write tool with the absolute path. Do not narrate; do not explain; just write the file.",
			"</system-reminder>",
		}, "\n"),
	}
}

// RunProductGate executes the PM phase.
func RunProductGate(
	ctx context.Context, input Input, deps Dependencies,
) (Result, error) {
	prdPath := filepath.Join(input.Workspace, ".codeaf", "plan", "product.md")
	if deps.Models == nil {
		return Result{}, errors.New("product-gate: nil model resolver")
	}
	candidates := deps.Models.CandidatesForTier("high")
	if len(candidates) == 0 {
		reason := "no HIGH-tier model available"
		return Result{Status: "failed", PRDPath: prdPath, Reason: &reason}, nil
	}
	model := splitModel(candidates[0])
	if err := os.MkdirAll(filepath.Dir(prdPath), 0o777); err != nil {
		return Result{}, err
	}
	if input.PromptOps == nil {
		return Result{}, errors.New("product-gate: nil prompt ops")
	}
	if deps.Sessions == nil {
		return Result{}, errors.New("product-gate: nil session creator")
	}
	taskPrompt := BuildTaskPrompt(input, prdPath)
	sessionID, err := deps.Sessions.Create(ctx, input.ParentSessionID, "product-manager")
	if err != nil {
		return Result{}, err
	}
	// The source allocates one ascending ID here and never uses it.
	if deps.NewMessageID != nil {
		_ = deps.NewMessageID()
	}
	promptParts, err := input.PromptOps.ResolvePromptParts(ctx, taskPrompt)
	if err != nil {
		return Result{}, err
	}
	reminder := BuildReminder(prdPath)
	lastFailReason := ""
	for attempt := 1; attempt <= pmMaxAttempts; attempt++ {
		parts := make([]any, 0, len(promptParts)+2)
		parts = append(parts, reminder)
		if attempt > 1 {
			parts = append(parts, BuildRetryReminder(attempt, prdPath, lastFailReason))
		}
		parts = append(parts, promptParts...)
		messageID := ""
		if deps.NewMessageID != nil {
			messageID = deps.NewMessageID()
		}
		attemptCtx, cancel := context.WithTimeout(ctx, pmTimeout)
		_, _ = input.PromptOps.Prompt(attemptCtx, PromptRequest{
			MessageID: messageID, SessionID: sessionID, Model: model,
			Agent: "product-manager", Tools: ToolSettings{}, Parts: parts,
			Workspace: input.Workspace,
		})
		cancel()
		if _, accessErr := os.Stat(prdPath); accessErr == nil {
			return Result{Status: "wrote", PRDPath: prdPath}, nil
		}
		lastFailReason = "attempt " + intString(attempt) +
			" ended without writing " + prdPath
	}
	reason := "PM did not write the PRD file after " +
		intString(pmMaxAttempts) + " attempts (" + lastFailReason + ")"
	return Result{Status: "failed", PRDPath: prdPath, Reason: &reason}, nil
}

func splitModel(value string) PromptModel {
	parts := strings.Split(value, "/")
	return PromptModel{
		ProviderID: parts[0],
		ModelID:    strings.Join(parts[1:], "/"),
	}
}

func intString(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [24]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}
