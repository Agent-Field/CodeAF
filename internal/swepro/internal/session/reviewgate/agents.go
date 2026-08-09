// This file ports reviewer dispatch, repair dispatch, and high-risk
// auditor/synthesizer dispatch from src/session/review-gate.ts:616-1172 and
// 1385-1481. Structured agents use internal/session/agentjson.
package reviewgate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/Agent-Field/swe-pro-go/internal/baked"
	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/plandb"
	"github.com/Agent-Field/swe-pro-go/internal/session/agentjson"
	"github.com/Agent-Field/swe-pro-go/internal/session/scheduler"
)

func BuildReviewTaskDescription(implTaskID, reviewerAgent string) string {
	return strings.Join([]string{
		"Independent review of impl leaf " + implTaskID + ".",
		"Read-only worktree on the impl's branch.",
		"Tools available: read, grep, glob, shell. NO edit/write/apply_patch/plandb.",
		"Output: structured verdict via generateObject.",
		"",
		"task_role: review",
		"access: read",
		"parallel: serial",
		"agent: " + reviewerAgent,
		"outputs: review_verdict",
		"context_inputs: parent,impl_summary,diff",
	}, "\n")
}

func BuildReviewReminder(
	reviewTaskID string,
	implTaskID string,
	attempt int,
	worktreePath string,
	baseSHA string,
) string {
	return strings.Join([]string{
		"<system-reminder>",
		"You are the merge-gate reviewer. Read-only mode.",
		"Review task ID: " + reviewTaskID,
		"Reviewing impl leaf: " + implTaskID,
		"Repair attempt #: " + strconvInt(attempt+1),
		"Worktree: " + worktreePath,
		"Base commit: " + baseSHA,
		"Branch under review: plandb/" + implTaskID,
		"Tools enabled: read, grep, glob, bash (for verification commands).",
		"Tools disabled: write, edit, apply_patch, plandb-add, task.",
		"</system-reminder>",
	}, "\n")
}

func BuildRepairReminder(
	repairTaskID string,
	implTaskID string,
	attempt int,
	repairCap int,
	prevBranch string,
	worktreePath string,
) string {
	return strings.Join([]string{
		"<system-reminder>",
		"You are repairing a failed implementation.",
		"Repair task ID: " + repairTaskID,
		"Original impl: " + implTaskID,
		"Repair attempt: #" + strconvInt(attempt+1) + " of " + strconvInt(repairCap),
		"Worktree (branched from " + prevBranch + "): " + worktreePath,
		"",
		"Read the bugs and repair_hints in your task description. Fix the blocker-severity issues first.",
		"Run the same verification the reviewer flagged (e.g. `go build`, `bun run typecheck`) to confirm before finishing.",
		"</system-reminder>",
	}, "\n")
}

func BuildVerdictNote(verdict ReviewVerdict, attempt int) string {
	bugs := "bugs: none"
	if len(verdict.Bugs) > 0 {
		count := min(4, len(verdict.Bugs))
		rows := make([]string, 0, count)
		for _, bug := range verdict.Bugs[:count] {
			file := bug.File
			if file == "" {
				file = "?"
			}
			rows = append(rows,
				"["+bug.Severity+"] "+file+": "+compact(bug.Detail, 80),
			)
		}
		bugs = "bugs: " + strings.Join(rows, " | ")
	}
	return strings.Join([]string{
		"verdict=" + verdict.Verdict + " confidence=" + verdict.Confidence +
			" (attempt #" + strconvInt(attempt+1) + ")",
		"spec_coverage: " + compact(verdict.SpecCoverage, 300),
		bugs,
		"evidence: " + compact(verdict.Evidence, 240),
	}, "\n")
}

func taskTitle(task *plandb.Task) string {
	if task == nil {
		return ""
	}
	return task.Title
}

func taskDescription(task *plandb.Task) string {
	if task == nil || task.Description == nil {
		return ""
	}
	return *task.Description
}

func parseAddedTask(result plandb.RunResult) *plandb.Task {
	var task plandb.Task
	if json.Unmarshal(result.Stdout, &task) != nil || task.ID == "" {
		return nil
	}
	return &task
}

type filteredResolver struct {
	resolver agentjson.Resolver
}

func (r filteredResolver) CandidatesForTier(tier baked.Tier) []string {
	if r.resolver == nil {
		return nil
	}
	out := []string{}
	for _, candidate := range r.resolver.CandidatesForTier(tier) {
		if strings.IndexByte(candidate, '/') > 0 {
			out = append(out, candidate)
		}
	}
	return out
}

func (s *Service) dispatchReviewAttempt(
	ctx context.Context,
	input scheduler.GateInput,
	worktreePath string,
	branch string,
	attempt int,
) (ReviewVerdict, bool) {
	task := input.Task
	reviewerAgent := s.config.ReviewerAgent
	title := "Review #" + strconvInt(attempt+1) + " of " + compact(taskTitle(task), 60)
	add := s.runPlanDB(
		"add", title,
		"--json",
		"--project", input.ProjectID,
		"--parent", input.RootTaskID,
		"--kind", "review",
		"--description", BuildReviewTaskDescription(task.ID, reviewerAgent),
		"--dep", task.ID+":feeds_into",
		"--tag", "review:of:"+task.ID,
	)
	reviewTask := parseAddedTask(add)
	if reviewTask == nil {
		return SynthesizeFailVerdict(
			"failed to create review plandb task: "+utf16Slice(string(add.Stderr), 0, 200),
			"(no review attempted)",
		), false
	}
	s.runPlanDB(
		"task", "claim", reviewTask.ID,
		"--agent", "scheduler:review", "--json",
	)
	s.runPlanDB("task", "start", reviewTask.ID, "--json")

	wt, allocErr := s.allocateGateWorktree(ctx, input.Workspace, reviewTask.ID, branch)
	if allocErr != nil {
		s.runPlanDB(
			"task", "fail", reviewTask.ID,
			"--error", "worktree alloc failed (base="+branch+"): "+allocErr.Error(),
		)
		return SynthesizeFailVerdict(
			"reviewer worktree alloc failed on "+branch,
			"(no review attempted)",
		), false
	}

	contract := ReadContract(taskDescription(task))
	diff := s.captureDiff(ctx, wt.Path, input.BaseSHA, 12_000)
	prompt := BuildReviewPrompt(ReviewPromptArgs{
		RootExcerpt: input.RootExcerpt, ImplTitle: taskTitle(task),
		ImplContract: contract, ImplSummary: input.Summary, Diff: diff,
		Config: s.config,
	})

	implTier := baked.TierFor(input.SubagentType, baked.TierLow)
	tier := baked.TierFor(reviewerAgent, implTier)
	resolver := filteredResolver{resolver: s.agent.Resolver}
	if len(resolver.CandidatesForTier(tier)) == 0 {
		s.runPlanDB(
			"task", "fail", reviewTask.ID,
			"--error", "no resolvable model in tier "+string(tier),
		)
		s.cleanupWorktree(ctx, input.Workspace, wt.Path, wt.Branch)
		return SynthesizeFailVerdict(
			"reviewer: no resolvable model in tier "+string(tier),
			"(no review attempted)",
		), false
	}

	outputPath := filepath.Join(wt.Path, ".codeaf", "review-verdict.json")
	maxRetries := 0
	timeoutMS := s.config.TimeoutMS
	label := "scheduler:review"
	agentDeps := s.agent
	agentDeps.Resolver = resolver
	result, err := agentjson.DispatchJSON(ctx, agentjson.Input[ReviewVerdict]{
		Agent: reviewerAgent, ParentSessionID: input.ParentSessionID,
		Workspace: wt.Path, TaskPrompt: prompt, OutputPath: outputPath,
		Schema: ReviewSchema{}, MaxRetries: &maxRetries,
		Tools: []agentjson.ToolSetting{
			{Name: "read", Enabled: true},
			{Name: "grep", Enabled: true},
			{Name: "glob", Enabled: true},
			{Name: "bash", Enabled: true},
			{Name: "write", Enabled: true},
		},
		TimeoutMS: &timeoutMS, Tier: &tier, Label: &label,
	}, agentDeps)

	var verdict ReviewVerdict
	authored := err == nil
	if err != nil {
		reason := "verdict extraction: reviewer did not write .codeaf/review-verdict.json AND no parseable verdict JSON in the reviewer's prose. The reviewer is responsible for writing the verdict file — see superpowers-code-reviewer.md."
		verdict = SynthesizeFailVerdict(reason, "(no transcript)")
	} else {
		verdict = result.Data
	}

	note := BuildVerdictNote(verdict, attempt)
	s.runPlanDB(
		"context", note, "--kind", "review", "--task", task.ID,
	)
	doneResult, _ := jscompat.Stringify(struct {
		Verdict      string `json:"verdict"`
		Confidence   string `json:"confidence"`
		SpecCoverage string `json:"spec_coverage"`
		BugCount     int    `json:"bug_count"`
	}{
		Verdict: verdict.Verdict, Confidence: verdict.Confidence,
		SpecCoverage: verdict.SpecCoverage, BugCount: len(verdict.Bugs),
	})
	s.runPlanDB(
		"done", reviewTask.ID,
		"--agent", "scheduler:review",
		"--result", string(doneResult),
	)
	s.cleanupWorktree(ctx, input.Workspace, wt.Path, wt.Branch)
	return verdict, authored
}

type repairOutput struct {
	OK           bool
	WorktreePath string
	Branch       string
	BaseSHA      string
	Summary      string
	Error        string
}

func (s *Service) dispatchRepair(
	ctx context.Context,
	input scheduler.GateInput,
	verdict ReviewVerdict,
	prevBranch string,
	attempt int,
) repairOutput {
	task := input.Task
	repairAgent := s.config.RepairAgent
	repairTitle := "Repair #" + strconvInt(attempt+1) + " for " +
		compact(taskTitle(task), 60)
	contract := ReadContract(taskDescription(task))
	description := BuildRepairDescription(RepairDescriptionArgs{
		ImplTaskID: task.ID, ImplTitle: taskTitle(task), Verdict: verdict,
		ImplContract: &contract,
	})
	add := s.runPlanDB(
		"add", repairTitle,
		"--json",
		"--project", input.ProjectID,
		"--parent", task.ID,
		"--kind", "code",
		"--description", description,
		"--dep", task.ID+":feeds_into",
		"--tag", "repair:of:"+task.ID,
	)
	repairTask := parseAddedTask(add)
	if repairTask == nil {
		return repairOutput{
			Error: "repair plandb add failed: " +
				utf16Slice(string(add.Stderr), 0, 200),
		}
	}
	s.runPlanDB(
		"task", "claim", repairTask.ID,
		"--agent", "scheduler:repair", "--json",
	)
	s.runPlanDB("task", "start", repairTask.ID, "--json")

	wt, allocErr := s.allocateGateWorktree(ctx, input.Workspace, repairTask.ID, prevBranch)
	if allocErr != nil {
		s.runPlanDB(
			"task", "fail", repairTask.ID,
			"--error", "worktree alloc failed (base="+prevBranch+"): "+allocErr.Error(),
		)
		return repairOutput{Error: "repair worktree alloc failed on " + prevBranch}
	}

	tier := baked.Tier(s.config.RepairTier)
	resolver := filteredResolver{resolver: s.agent.Resolver}
	candidates := resolver.CandidatesForTier(tier)
	if len(candidates) == 0 {
		s.runPlanDB(
			"task", "fail", repairTask.ID,
			"--error", "no resolvable model in tier "+string(tier),
		)
		s.cleanupWorktree(ctx, input.Workspace, wt.Path, wt.Branch)
		return repairOutput{Error: "repair: no resolvable model in tier " + string(tier)}
	}
	markdown, ok := baked.GetBakedAgent(repairAgent)
	if !ok {
		s.cleanupWorktree(ctx, input.Workspace, wt.Path, wt.Branch)
		return repairOutput{Error: "repair: baked agent not found: " + repairAgent}
	}
	prompt := BuildRepairPrompt(repairTitle, description)
	request := agentjson.Request{
		Agent: repairAgent, AgentMarkdown: markdown,
		ParentSessionID: input.ParentSessionID,
		SessionID:       s.newID("session"),
		MessageID:       s.newID("message"),
		Model:           agentjson.SplitModelID(candidates[0]),
		Workspace:       wt.Path,
		TaskPrompt:      prompt,
		Reminder: BuildRepairReminder(
			repairTask.ID, task.ID, attempt, s.config.RepairCap,
			prevBranch, wt.Path,
		),
		Tools: agentjson.ToolSettings{
			{Name: "task", Enabled: false},
			{Name: "todowrite", Enabled: false},
		},
		Phase: agentjson.PhaseMain, Attempt: attempt + 1,
	}
	err := callAgentClient(ctx, s.agent.Client, request, s.config.TimeoutMS)
	if err != nil {
		message := utf16Slice(err.Error(), 0, 300)
		s.runPlanDB(
			"task", "fail", repairTask.ID,
			"--error", "repair errored / timed out: "+message,
		)
		s.cleanupWorktree(ctx, input.Workspace, wt.Path, wt.Branch)
		return repairOutput{Error: "repair errored or timed out: " + message}
	}

	s.commitRepair(ctx, wt.Path, repairTask.ID, repairTitle)
	doneResult, _ := jscompat.Stringify(struct {
		SummaryChars int `json:"summary_chars"`
		Attempt      int `json:"attempt"`
	}{SummaryChars: 0, Attempt: attempt + 1})
	s.runPlanDB(
		"done", repairTask.ID,
		"--agent", "scheduler:repair",
		"--result", string(doneResult),
	)
	return repairOutput{
		OK: true, WorktreePath: wt.Path, Branch: wt.Branch,
		BaseSHA: wt.BaseSHA, Summary: "",
	}
}

func callAgentClient(
	ctx context.Context,
	client agentjson.Client,
	request agentjson.Request,
	timeoutMS int64,
) error {
	if client == nil {
		return errors.New("agent-json: nil client")
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		var err error
		defer func() {
			if recovered := recover(); recovered != nil {
				err = fmt.Errorf("%v\n%s", recovered, debug.Stack())
			}
			done <- err
		}()
		err = client.Run(callCtx, request)
	}()
	select {
	case err := <-done:
		return err
	case <-callCtx.Done():
		return callCtx.Err()
	}
}

type rawAuditorSchema struct{}

func (rawAuditorSchema) SafeParse(raw json.RawMessage) agentjson.Validation[json.RawMessage] {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return agentjson.Validation[json.RawMessage]{
			Issues: []agentjson.Issue{{Message: "Expected object"}},
		}
	}
	var verdict string
	if value, ok := object["verdict"]; !ok || json.Unmarshal(value, &verdict) != nil ||
		(verdict != "pass" && verdict != "fail") {
		return agentjson.Validation[json.RawMessage]{
			Issues: []agentjson.Issue{{
				Path: []string{"verdict"}, Message: "Invalid enum value",
			}},
		}
	}
	return agentjson.Validation[json.RawMessage]{
		Data: append(json.RawMessage(nil), raw...),
	}
}

func BuildAuditorPrompt(
	taskID string,
	branch string,
	worktree string,
	spec string,
	leafDiff string,
	outputPath string,
) string {
	if leafDiff == "" {
		leafDiff = "(no diff captured)"
	}
	return strings.Join([]string{
		"# Leaf-scoped audit (high-risk task)",
		"",
		"Task ID: " + taskID,
		"Branch: " + branch,
		"Workspace: " + worktree,
		"",
		"## Leaf spec",
		"",
		spec,
		"",
		"## Diff for this leaf only",
		"```diff",
		leafDiff,
		"```",
		"",
		"Run your adversarial four-step procedure against this leaf only. Write your verdict JSON to " + outputPath + ".",
		"Default verdict = fail. Burden of proof is on the work.",
	}, "\n")
}

func (s *Service) runFlaggedSynthesis(
	ctx context.Context,
	input scheduler.GateInput,
	worktree string,
	branch string,
	reviewerVerdict ReviewVerdict,
) ReviewVerdict {
	task := input.Task
	outputPath := filepath.Join(
		worktree, ".codeaf", "agents", "auditor-leaf", task.ID+".json",
	)
	diffResult := s.run(ctx,
		[]string{"git", "diff", "--no-color", input.BaseSHA, "HEAD"},
		worktree,
	)
	leafDiff := utf16Slice(string(diffResult.Stdout), 0, 16_000)
	spec := taskOriginalSpec(task)
	auditorPrompt := BuildAuditorPrompt(
		task.ID, branch, worktree, spec, leafDiff, outputPath,
	)
	fallback, _ := jscompat.Stringify(struct {
		Verdict  string `json:"verdict"`
		Blockers []struct {
			Step   int    `json:"step"`
			Detail string `json:"detail"`
		} `json:"blockers"`
		RepairHints []string `json:"repair_hints"`
	}{
		Verdict: "fail",
		Blockers: []struct {
			Step   int    `json:"step"`
			Detail string `json:"detail"`
		}{{
			Step:   0,
			Detail: "leaf-scoped auditor produced no parseable verdict; treating as fail per high-risk gate",
		}},
		RepairHints: []string{"Re-run with explicit acceptance criteria"},
	})
	fallbackRaw := json.RawMessage(fallback)
	maxRetries := 1
	timeoutMS := int64(8 * 60_000)
	label := "auditor-leaf"
	auditor, err := agentjson.DispatchJSON(ctx, agentjson.Input[json.RawMessage]{
		Agent: "auditor", ParentSessionID: input.ParentSessionID,
		Workspace: worktree, TaskPrompt: auditorPrompt, OutputPath: outputPath,
		Schema: rawAuditorSchema{}, Fallback: &fallbackRaw,
		MaxRetries: &maxRetries, TimeoutMS: &timeoutMS, Label: &label,
	}, s.agent)
	auditorRaw := fallbackRaw
	if err == nil {
		auditorRaw = auditor.Data
	}

	reviewerJSON, _ := jscompat.Stringify(reviewerVerdict)
	synthInput := SynthesizerPromptInput{
		Workspace: worktree, ParentSessionID: input.ParentSessionID,
		ReviewerVerdict: string(reviewerJSON),
		AuditorVerdict:  string(auditorRaw),
		TaskID:          task.ID,
	}
	synthPath := filepath.Join(
		worktree, ".codeaf", "agents", "review-synthesizer", task.ID+".json",
	)
	synthPrompt := BuildSynthesizerPrompt(synthInput, synthPath)
	synthFallback := SynthFallback
	synthRetries := 1
	synthLabel := "gate-synthesizer"
	synthResult, synthErr := agentjson.DispatchJSON(ctx, agentjson.Input[SynthesizerDecision]{
		Agent: "gate-synthesizer", ParentSessionID: input.ParentSessionID,
		Workspace: worktree, TaskPrompt: synthPrompt, OutputPath: synthPath,
		Schema: SynthesizerSchema{}, Fallback: &synthFallback,
		MaxRetries: &synthRetries, Label: &synthLabel,
	}, s.agent)
	if synthErr != nil {
		return SynthesizedReviewVerdict(SynthFallback)
	}
	return SynthesizedReviewVerdict(synthResult.Data)
}
