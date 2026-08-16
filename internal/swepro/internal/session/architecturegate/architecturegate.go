// Package architecturegate ports src/session/architecture-gate.ts:1-607 from
// swe-pro commit 3b25a1a. It owns the architecture document writer and the
// full-mode architect/tech-lead review loop.
package architecturegate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/agentjson"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/observer"
)

const (
	architectTimeout     = 30 * time.Minute
	architectMaxAttempts = 3
)

// ArchitectureReview is the strict tech-lead verdict.
type ArchitectureReview struct {
	Approved bool   `json:"approved"`
	Feedback string `json:"feedback"`
	Summary  string `json:"summary"`
}

// ReviewFallback is the source's default-fail review value.
var ReviewFallback = ArchitectureReview{
	Approved: false,
	Feedback: "Tech Lead produced no parseable JSON after retries. Treating as not-approved per the default-fail discipline. " +
		"The architect should re-read the PRD and architecture, look for completeness gaps, and revise.",
	Summary: "tech-lead-fallback: parse failure → not approved",
}

// ReviewSchema implements the strict ArchitectureReviewSchema.
type ReviewSchema struct{}

func (ReviewSchema) SafeParse(raw json.RawMessage) agentjson.Validation[ArchitectureReview] {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return invalidReview("(root)", "Expected object")
	}
	if len(object) != 3 {
		return invalidReview("(root)", "Unrecognized key(s) in object")
	}
	var review ArchitectureReview
	if value, ok := object["approved"]; !ok || json.Unmarshal(value, &review.Approved) != nil {
		return invalidReview("approved", "Expected boolean")
	}
	for _, field := range []struct {
		key     string
		minimum int
		maximum int
	}{
		{key: "feedback", minimum: 1, maximum: 8000},
		{key: "summary", minimum: 1, maximum: 2000},
	} {
		key := field.key
		var target *string
		if key == "feedback" {
			target = &review.Feedback
		} else {
			target = &review.Summary
		}
		value, ok := object[key]
		if !ok || json.Unmarshal(value, target) != nil {
			return invalidReview(key, "Expected string")
		}
		length := len(utf16.Encode([]rune(*target)))
		if length < field.minimum || length > field.maximum {
			return invalidReview(key, "String constraint failed")
		}
	}
	return agentjson.Validation[ArchitectureReview]{Data: review}
}

func invalidReview(path, message string) agentjson.Validation[ArchitectureReview] {
	return agentjson.Validation[ArchitectureReview]{
		Issues: []agentjson.Issue{{Path: []string{path}, Message: message}},
	}
}

// Mode is ArchitectureGateMode.
type Mode string

const (
	ModeSinglePass Mode = "single-pass"
	ModeFull       Mode = "full"
)

// Input mirrors ArchitectureGateInput.
type Input struct {
	Workspace       string
	ParentSessionID string
	PromptOps       PromptOps
	Mode            Mode
	UserPrompt      string
	PRDPath         *string
}

// Result mirrors ArchitectureGateResult.
type Result struct {
	Status              string              `json:"status"`
	ArchitecturePath    string              `json:"architecturePath"`
	ArchitectIterations int                 `json:"architectIterations"`
	TechLeadIterations  int                 `json:"techLeadIterations"`
	LastReview          *ArchitectureReview `json:"lastReview,omitempty"`
	Reason              *string             `json:"reason,omitempty"`
}

// PromptOps is the TaskPromptOps projection consumed by the architect.
type PromptOps interface {
	ResolvePromptParts(ctx context.Context, template string) ([]any, error)
	Prompt(ctx context.Context, input any) (any, error)
}

// ModelResolver is the router projection used by free-form architect runs.
type ModelResolver interface {
	CandidatesForTier(tier string) []string
}

type ModelResolverFunc func(tier string) []string

func (function ModelResolverFunc) CandidatesForTier(tier string) []string {
	return function(tier)
}

// SessionCreator creates one fresh architect session per attempt.
type SessionCreator interface {
	Create(ctx context.Context, parentID, agent string) (string, error)
}

type SessionCreatorFunc func(ctx context.Context, parentID, agent string) (string, error)

func (function SessionCreatorFunc) Create(
	ctx context.Context, parentID, agent string,
) (string, error) {
	return function(ctx, parentID, agent)
}

// Dependencies are the source Effect services and ambient hooks.
type Dependencies struct {
	Models       ModelResolver
	Sessions     SessionCreator
	AgentJSON    agentjson.Dependencies
	NewMessageID func() string
	Observer     observer.Tracker
	NowMillis    func() int64
	LookupEnv    func(string) (string, bool)
}

type PromptPart struct {
	Type      string `json:"type"`
	Synthetic bool   `json:"synthetic"`
	Text      string `json:"text"`
}

type PromptModel struct {
	ModelID    string `json:"modelID"`
	ProviderID string `json:"providerID"`
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

type architectInput struct {
	workspace        string
	parentSessionID  string
	promptOps        PromptOps
	userPrompt       string
	prdPath          *string
	architecturePath string
	feedback         *string
	revisionNumber   int
}

type architectResult struct {
	ok     bool
	reason *string
}

// BuildArchitectPrompt ports architecture-gate.ts:166-218.
func BuildArchitectPrompt(input Input, architecturePath string, feedback *string) string {
	feedbackBlock := ""
	if feedback != nil && *feedback != "" {
		feedbackBlock = strings.Join([]string{
			"## Revision Feedback from Tech Lead",
			"",
			"The previous architecture was reviewed and needs revision:",
			"",
			*feedback,
			"",
			"Address these concerns directly.",
			"",
		}, "\n")
	}
	prdBlock := ""
	if input.PRDPath != nil && *input.PRDPath != "" {
		prdBlock = "## Product Requirements\n\nFull PRD at: " + *input.PRDPath +
			"\nRead it first, then design against its acceptance criteria.\n\n"
	} else {
		prdBlock = "## User Goal\n\n" + input.UserPrompt +
			"\n\nThere is no PRD for this task — the user's goal above is your direct input." +
			"\nDerive scope from the goal and the codebase.\n\n"
	}
	return strings.Join([]string{
		prdBlock,
		"## Repository",
		"",
		input.Workspace,
		"",
		feedbackBlock,
		"## Your Mission",
		"",
		"Design the technical architecture. Read the codebase deeply first — your",
		"design should feel like a natural extension of what already exists.",
		"",
		"Write your architecture document to: " + architecturePath,
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
		"This document is the single source of truth. Every interface you define",
		"will be copied verbatim into code. Every type signature becomes a real",
		"type. Every component boundary becomes a real module. Two engineers",
		"working independently from this document should produce code that",
		"integrates on the first try.",
	}, "\n")
}

// BuildArchitectReminder ports baseReminder.
func BuildArchitectReminder(
	architecturePath string, feedback *string, revisionNumber, attempt int,
) PromptPart {
	mode := "running the initial pass"
	if feedback != nil && *feedback != "" {
		mode = "running revision #" + intString(revisionNumber)
	}
	lines := []string{
		"<system-reminder>",
		"You are the architect, " + mode + ".",
		"Architecture path: " + architecturePath,
		"Write a complete, self-contained architecture markdown.",
		"Read the codebase obsessively before designing.",
	}
	if attempt > 1 {
		lines = append(lines,
			"",
			"RETRY ATTEMPT "+intString(attempt)+"/"+intString(architectMaxAttempts)+
				". Your previous attempt did not produce an architecture file at "+architecturePath+".",
			"Whatever you did last time, do something different — most importantly,",
			"you MUST call the `write` tool with the absolute architecture path",
			"before ending your turn. Do not just narrate; write the file. If you",
			"are unsure of the design, write an explicit \"TBD — see open questions\"",
			"section rather than producing nothing. An empty file is a failure;",
			"any structurally-complete architecture is acceptable.",
		)
	}
	lines = append(lines, "</system-reminder>")
	return PromptPart{Type: "text", Synthetic: true, Text: strings.Join(lines, "\n")}
}

// BuildTechLeadPrompt ports architecture-gate.ts:380-438.
func BuildTechLeadPrompt(
	input Input, architecturePath, reviewPath string, revisionNumber int,
) string {
	revisionBlock := ""
	if revisionNumber > 0 {
		revisionBlock = strings.Join([]string{
			"",
			"This is revision #" + intString(revisionNumber) + ". The architect has revised",
			"based on your previous feedback. Check whether the concerns were",
			"addressed.",
			"",
		}, "\n")
	}
	prdLine := "There is no separate PRD for this task. The architecture must be evaluated against the user's goal (described inside architecture.md's overview)."
	if input.PRDPath != nil && *input.PRDPath != "" {
		prdLine = "The PRD is at: " + *input.PRDPath
	}
	return strings.Join([]string{
		"## Your Mission",
		"",
		"Review the proposed architecture against the product requirements.",
		"",
		prdLine,
		"The architecture is at: " + architecturePath,
		revisionBlock,
		"Read both documents thoroughly, then assess:",
		"",
		"1. **Requirements coverage**: For each acceptance criterion in the PRD",
		"   (or implied by the user's goal if no PRD), identify the specific",
		"   architecture component and interface that satisfies it.",
		"",
		"2. **Interface precision**: Are types, signatures, error cases, and edge",
		"   behaviors defined precisely enough that an autonomous agent could",
		"   implement them without guessing?",
		"",
		"3. **Internal consistency**: Do all sections of the architecture agree",
		"   with each other?",
		"",
		"4. **Complexity calibration**: Is the design appropriately complex —",
		"   neither more nor less than the problem demands?",
		"",
		"5. **Scope alignment**: Does the architecture solve exactly what the",
		"   PRD/goal specified? Flag additions or omissions.",
		"",
		"Be decisive. Your approval means autonomous agents can implement this",
		"safely. Your rejection means proceeding would cause rework or",
		"integration failures.",
		"",
		"Write your verdict JSON to " + reviewPath + ".",
	}, "\n")
}

// RunArchitectureGate executes single-pass or full review mode.
func RunArchitectureGate(
	ctx context.Context, input Input, deps Dependencies,
) (Result, error) {
	architecturePath := filepath.Join(input.Workspace, ".codeaf", "plan", "architecture.md")
	reviewPath := filepath.Join(input.Workspace, ".codeaf", "plan", "architecture-review.json")
	if err := os.MkdirAll(filepath.Dir(architecturePath), 0o777); err != nil {
		return Result{}, err
	}
	if input.Mode == ModeSinglePass {
		architect, err := dispatchArchitect(ctx, architectInput{
			workspace: input.Workspace, parentSessionID: input.ParentSessionID,
			promptOps: input.PromptOps, userPrompt: input.UserPrompt,
			prdPath: input.PRDPath, architecturePath: architecturePath,
		}, deps)
		if err != nil {
			return Result{}, err
		}
		if !architect.ok {
			reason := "architect did not write architecture.md"
			if architect.reason != nil {
				reason = *architect.reason
			}
			return Result{
				Status: "failed", ArchitecturePath: architecturePath,
				ArchitectIterations: 1, TechLeadIterations: 0, Reason: &reason,
			}, nil
		}
		return Result{
			Status: "wrote", ArchitecturePath: architecturePath,
			ArchitectIterations: 1, TechLeadIterations: 0,
		}, nil
	}

	maxIterations := maxArchReviewIterations(deps.LookupEnv)
	initial, err := dispatchArchitect(ctx, architectInput{
		workspace: input.Workspace, parentSessionID: input.ParentSessionID,
		promptOps: input.PromptOps, userPrompt: input.UserPrompt,
		prdPath: input.PRDPath, architecturePath: architecturePath,
	}, deps)
	if err != nil {
		return Result{}, err
	}
	if !initial.ok {
		reason := "architect did not write architecture.md (initial pass)"
		if initial.reason != nil {
			reason = *initial.reason
		}
		return Result{
			Status: "failed", ArchitecturePath: architecturePath,
			ArchitectIterations: 1, TechLeadIterations: 0, Reason: &reason,
		}, nil
	}

	architectIterations := 1
	techLeadIterations := 0
	var lastReview *ArchitectureReview
	for iteration := 0; iteration <= maxIterations; iteration++ {
		review, reviewErr := dispatchTechLead(
			ctx, input, architecturePath, reviewPath, iteration, deps.AgentJSON,
		)
		if reviewErr != nil {
			return Result{}, reviewErr
		}
		techLeadIterations++
		lastReview = &review
		if review.Approved {
			return Result{
				Status: "wrote", ArchitecturePath: architecturePath,
				ArchitectIterations: architectIterations,
				TechLeadIterations:  techLeadIterations, LastReview: lastReview,
			}, nil
		}
		if iteration < maxIterations {
			feedback := review.Feedback
			revision, revisionErr := dispatchArchitect(ctx, architectInput{
				workspace: input.Workspace, parentSessionID: input.ParentSessionID,
				promptOps: input.PromptOps, userPrompt: input.UserPrompt,
				prdPath: input.PRDPath, architecturePath: architecturePath,
				feedback: &feedback, revisionNumber: iteration + 1,
			}, deps)
			if revisionErr != nil {
				return Result{}, revisionErr
			}
			architectIterations++
			if !revision.ok {
				break
			}
		}
	}
	reason := "auto-approved after " + intString(maxIterations) + " tech-lead iterations"
	return Result{
		Status: "force_approved", ArchitecturePath: architecturePath,
		ArchitectIterations: architectIterations,
		TechLeadIterations:  techLeadIterations, LastReview: lastReview,
		Reason: &reason,
	}, nil
}

func dispatchArchitect(
	ctx context.Context, input architectInput, deps Dependencies,
) (architectResult, error) {
	if deps.Models == nil {
		return architectResult{}, errors.New("architecture-gate: nil model resolver")
	}
	candidates := deps.Models.CandidatesForTier("high")
	if len(candidates) == 0 {
		reason := "no HIGH-tier model available"
		return architectResult{reason: &reason}, nil
	}
	if input.promptOps == nil || deps.Sessions == nil {
		return architectResult{}, errors.New("architecture-gate: missing prompt/session service")
	}
	model := splitModel(candidates[0])
	publicInput := Input{
		Workspace: input.workspace, ParentSessionID: input.parentSessionID,
		PromptOps: input.promptOps, UserPrompt: input.userPrompt, PRDPath: input.prdPath,
	}
	taskPrompt := BuildArchitectPrompt(publicInput, input.architecturePath, input.feedback)
	promptParts, err := input.promptOps.ResolvePromptParts(ctx, taskPrompt)
	if err != nil {
		return architectResult{}, err
	}
	lastReason := ""
	for attempt := 1; attempt <= architectMaxAttempts; attempt++ {
		sessionID, createErr := deps.Sessions.Create(ctx, input.parentSessionID, "architect")
		if createErr != nil {
			return architectResult{}, createErr
		}
		if deps.Observer != nil {
			now := time.Now().UnixMilli()
			if deps.NowMillis != nil {
				now = deps.NowMillis()
			}
			// TS architecture-gate.ts:264-280 tracks immediately after the
			// fresh architect session is created; this is its Go semantic peer.
			deps.Observer.Track(observer.ObservedSession{
				SessionID: sessionID, AgentRole: "architect",
				TaskSummary: "architecture-gate attempt " + intString(attempt) +
					": " + sliceUTF16(input.userPrompt, 200),
				StartedAt: float64(now), Workspace: input.workspace,
				ParentSessionID: input.parentSessionID,
			})
		}
		messageID := ""
		if deps.NewMessageID != nil {
			messageID = deps.NewMessageID()
		}
		parts := []any{BuildArchitectReminder(
			input.architecturePath, input.feedback, input.revisionNumber, attempt,
		)}
		parts = append(parts, promptParts...)
		attemptCtx, cancel := context.WithTimeout(ctx, architectTimeout)
		_, _ = input.promptOps.Prompt(attemptCtx, PromptRequest{
			MessageID: messageID, SessionID: sessionID, Model: model,
			Agent: "architect", Tools: ToolSettings{}, Parts: parts,
			Workspace: input.workspace,
		})
		cancel()
		if deps.Observer != nil {
			deps.Observer.Untrack(sessionID)
		}
		if _, accessErr := os.Stat(input.architecturePath); accessErr == nil {
			return architectResult{ok: true}, nil
		}
		lastReason = "attempt " + intString(attempt) +
			" ended without writing " + input.architecturePath
	}
	reason := "architecture.md not written after " + intString(architectMaxAttempts) +
		" attempts (" + lastReason + ")"
	return architectResult{reason: &reason}, nil
}

func dispatchTechLead(
	ctx context.Context,
	input Input,
	architecturePath, reviewPath string,
	revisionNumber int,
	deps agentjson.Dependencies,
) (ArchitectureReview, error) {
	maxRetries := 2
	timeout := int64(30 * 60_000)
	label := "tech-lead"
	fallback := ReviewFallback
	result, err := agentjson.DispatchJSON(ctx, agentjson.Input[ArchitectureReview]{
		Agent: "tech-lead", ParentSessionID: input.ParentSessionID,
		Workspace:  input.Workspace,
		TaskPrompt: BuildTechLeadPrompt(input, architecturePath, reviewPath, revisionNumber),
		OutputPath: reviewPath, Schema: ReviewSchema{}, Fallback: &fallback,
		MaxRetries: &maxRetries, TimeoutMS: &timeout, Label: &label,
	}, deps)
	return result.Data, err
}

func maxArchReviewIterations(lookup func(string) (string, bool)) int {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	value, exists := lookup("MAX_ARCH_REVIEW_ITERATIONS")
	if !exists {
		return 2
	}
	number, ok := parseInt10(value)
	if !ok || number < 0 {
		return 2
	}
	return number
}

func parseInt10(value string) (int, bool) {
	value = strings.TrimLeft(value, " \t\n\v\f\r\u00a0\ufeff")
	sign := 1
	if strings.HasPrefix(value, "+") {
		value = value[1:]
	} else if strings.HasPrefix(value, "-") {
		sign = -1
		value = value[1:]
	}
	end := 0
	for end < len(value) && value[end] >= '0' && value[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}
	number, err := strconv.ParseInt(value[:end], 10, 64)
	if err != nil {
		return 0, false
	}
	return sign * int(number), true
}

func splitModel(value string) PromptModel {
	parts := strings.Split(value, "/")
	return PromptModel{ProviderID: parts[0], ModelID: strings.Join(parts[1:], "/")}
}

func intString(value int) string { return strconv.Itoa(value) }

func sliceUTF16(value string, maximum int) string {
	units := utf16.Encode([]rune(value))
	if len(units) > maximum {
		units = units[:maximum]
	}
	return string(utf16.Decode(units))
}
