package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/swe-pro-go/internal/baked"
	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/session/adaptiveflag"
	"github.com/Agent-Field/swe-pro-go/internal/session/auditorgate"
	"github.com/Agent-Field/swe-pro-go/internal/session/contract"
	"github.com/Agent-Field/swe-pro-go/internal/session/frontierplanning"
	"github.com/Agent-Field/swe-pro-go/internal/session/hardmode"
)

func (runner *pipeline) frontierPlanningLedger() *frontierplanning.FrontierPlanningLedger {
	if runner.frontierPlanning == nil {
		runner.frontierPlanning = frontierplanning.CreateFrontierPlanningLedger()
	}
	return runner.frontierPlanning
}

// dispatchPlanningOneShot is the Go counterpart of run.ts:637-680. An empty
// modelOverride resolves through the baked role tier; any child-session,
// timeout, prompt, or response-shape failure is the advisory null fallback.
func (runner *pipeline) dispatchPlanningOneShot(
	ctx context.Context,
	agent string,
	promptText string,
	modelOverride string,
	timeout time.Duration,
	tools *oneShotToolSettings,
) (string, bool) {
	modelID := modelOverride
	if modelID == "" {
		candidates := runner.pool.values(string(baked.TierFor(agent)))
		modelID = firstModel(candidates)
	}
	if modelID == "" {
		modelID = firstModel(runner.pool.high)
	}
	model := agentjsonModel(modelID)
	sessionID, err := runner.runtime.Create(ctx, runner.sessionID, agent)
	if err != nil {
		return "", false
	}
	if timeout <= 0 {
		timeout = 4 * time.Minute
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	response, err := runner.runtime.Prompt(timeoutCtx, oneShotPromptRequest{
		MessageID: runner.runtime.nextID("message"), SessionID: sessionID,
		Model: oneShotPromptModel{ModelID: model.ModelID, ProviderID: model.ProviderID},
		Agent: agent, Tools: tools,
		Parts:     []any{oneShotTextPart{Type: "text", Text: promptText}},
		Workspace: runner.workspace,
	})
	if err != nil {
		return "", false
	}
	result, ok := response.(turnResult)
	if !ok {
		return "", false
	}
	reply := turnText(result)
	return reply, reply != ""
}

// runPlanArbitration ports run.ts:1179-1235. The plan-sketch calls happen only
// for a hard-mode coder entry; only two parseable, structurally disagreeing
// sketches spend the frontier arbitration budget.
func (runner *pipeline) runPlanArbitration(ctx context.Context, goal string) string {
	if os.Getenv("CODEAF_PLAN_ARB") == "0" ||
		!adaptiveflag.AdaptiveCutsEnabled() ||
		!hardmode.IsHardMode() ||
		runner.entryAgent != "coder" {
		return ""
	}

	runner.note("[codeaf] W12c plan sketches: two independent sketchers (high + low), ≤6 tool calls each\n")
	sketchPrompt := frontierplanning.BuildSketchPrompt(goal)
	highModel := firstModel(runner.pool.high)
	lowModel := firstModel(runner.pool.low)
	if lowModel == "" {
		lowModel = highModel
	}
	type sketchReply struct {
		text string
		ok   bool
	}
	replies := make([]sketchReply, 2)
	done := make(chan int, 2)
	models := []string{highModel, lowModel}
	for index := range models {
		go func(index int) {
			replies[index].text, replies[index].ok = runner.dispatchPlanningOneShot(
				ctx, "plan-sketch", sketchPrompt, models[index], 5*time.Minute, nil,
			)
			done <- index
		}(index)
	}
	<-done
	<-done

	var sketchA, sketchB *frontierplanning.PlanSketch
	if replies[0].ok {
		sketchA = frontierplanning.ParseSketch(replies[0].text)
	}
	if replies[1].ok {
		sketchB = frontierplanning.ParseSketch(replies[1].text)
	}
	renderSketch := func(sketch frontierplanning.PlanSketch) string {
		files := strings.Join(sketch.Files, ", ")
		if files == "" {
			files = "(none)"
		}
		return "files: " + files + "\napproach: " + sketch.Approach
	}
	if sketchA != nil && sketchB != nil {
		jaccard := frontierplanning.SketchJaccard(*sketchA, *sketchB)
		if frontierplanning.SketchesDisagree(*sketchA, *sketchB) &&
			runner.frontierPlanningLedger().TryTake(frontierplanning.RoleSketchArbitration) {
			runner.note(fmt.Sprintf(
				"[codeaf] W12c: structural disagreement (jaccard=%.2f) — frontier arbitration\n",
				jaccard,
			))
			prompt := frontierplanning.BuildSketchArbitrationPrompt(struct {
				TaskText string                      `json:"taskText"`
				SketchA  frontierplanning.PlanSketch `json:"sketchA"`
				SketchB  frontierplanning.PlanSketch `json:"sketchB"`
			}{TaskText: goal, SketchA: *sketchA, SketchB: *sketchB})
			arbitrated, ok := runner.dispatchPlanningOneShot(
				ctx, "plan-arbiter", prompt, "", 4*time.Minute, &oneShotToolSettings{},
			)
			if ok {
				return frontierplanning.PlanContextBlock(arbitrated)
			}
			return frontierplanning.PlanContextBlock(renderSketch(*sketchA))
		}
		runner.note(fmt.Sprintf(
			"[codeaf] W12c: sketches agree (jaccard=%.2f) — seeding the agreed plan, no arbitration\n",
			jaccard,
		))
		return frontierplanning.PlanContextBlock(renderSketch(*sketchA))
	}
	if sketchA != nil || sketchB != nil {
		runner.note("[codeaf] W12c: one sketch parseable — seeding it without arbitration\n")
		if sketchA != nil {
			return frontierplanning.PlanContextBlock(renderSketch(*sketchA))
		}
		return frontierplanning.PlanContextBlock(renderSketch(*sketchB))
	}
	runner.note("[codeaf] W12c: no parseable sketches — proceeding without a plan seed\n")
	return ""
}

// runContractReview ports run.ts:1665-1708. The returned bool distinguishes
// "not triggered" from a dispatched-but-unparseable review, which TS records
// as the empty string so the one-shot cannot be retried later in the run.
func (runner *pipeline) runContractReview(
	ctx context.Context,
	goal string,
	registered *contract.Contract,
	result *contract.ContractResult,
) (string, bool) {
	if os.Getenv("CODEAF_CONTRACT_REVIEW") == "0" || registered == nil || result == nil ||
		!runner.frontierPlanningLedger().TryTake(frontierplanning.RoleContractReview) {
		return "", false
	}
	parts := make([]string, 0, 3)
	for _, relative := range registered.Paths[:min(len(registered.Paths), 3)] {
		body, err := os.ReadFile(filepath.Join(runner.workspace, relative))
		if err != nil {
			continue
		}
		parts = append(parts, "### "+relative+"\n"+prefixUTF16(strings.ToValidUTF8(string(body), "\uFFFD"), 6000))
	}
	contractJSON, _ := jscompat.Stringify(registered)
	resultSummary := "pass=" + strconv.FormatBool(result.Pass) +
		" exit=" + jscompat.FormatNumber(result.ExitCode)
	if result.TimedOut {
		resultSummary += " TIMED-OUT"
	}
	resultSummary += "\n" + suffixUTF16(result.TailOutput, 600)
	runner.note("[codeaf] W12a contract-reviewer: dispatching (frontier one-shot)\n")
	reply, ok := runner.dispatchPlanningOneShot(
		ctx,
		"contract-reviewer",
		frontierplanning.BuildContractReviewPrompt(struct {
			TaskText              string `json:"taskText"`
			ContractJSON          string `json:"contractJson"`
			ContractFileText      string `json:"contractFileText"`
			ContractResultSummary string `json:"contractResultSummary"`
		}{
			TaskText: goal, ContractJSON: string(contractJSON),
			ContractFileText: strings.Join(parts, "\n\n"), ContractResultSummary: resultSummary,
		}),
		"",
		4*time.Minute,
		&oneShotToolSettings{},
	)
	var review *frontierplanning.ContractReview
	if ok {
		review = frontierplanning.ParseContractReview(reply)
	}
	if review == nil {
		runner.note("[codeaf] W12a contract review: no parseable verdict — skipped\n")
		return "", true
	}
	line := "[codeaf] W12a contract review: " + review.Verdict
	if len(review.Reasons) > 0 {
		line += " — " + review.Reasons[0]
	}
	runner.note(line + "\n")
	return frontierplanning.ContractReviewEvidenceBlock(*review), true
}

// withRootCauseDiagnosis ports run.ts:2480-2515. Go's audit loop counter is
// one greater than run.ts's `cycle`: Go cycle=2 is the failed audit immediately
// before entering the second fix cycle, where TS cycle=1 trips the trigger.
func (runner *pipeline) withRootCauseDiagnosis(
	ctx context.Context,
	goal string,
	baseSHA string,
	cycle int,
	verdict auditorgate.AuditorVerdict,
	contractResult *contract.ContractResult,
) auditorgate.AuditorVerdict {
	if os.Getenv("CODEAF_ROOT_CAUSE") == "0" ||
		!adaptiveflag.AdaptiveCutsEnabled() ||
		cycle < 2 ||
		!runner.frontierPlanningLedger().TryTake(frontierplanning.RoleRootCause) {
		return verdict
	}
	args := []string{"diff", "--stat"}
	if baseSHA != "" {
		args = append(args, baseSHA)
	}
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = runner.workspace
	diffOutput, err := command.Output()
	if err != nil {
		diffOutput = nil
	}
	diffStat := suffixUTF16(string(diffOutput), 1200)
	blockers := make([]frontierplanning.RootCauseBlocker, 0, len(verdict.Blockers))
	for _, blocker := range verdict.Blockers {
		converted := frontierplanning.RootCauseBlocker{Detail: blocker.Detail}
		if blocker.File != nil {
			converted.File = *blocker.File
		}
		if blocker.Line != nil {
			line := jscompat.JSNumber(*blocker.Line)
			converted.Line = &line
		}
		blockers = append(blockers, converted)
	}
	contractTail := ""
	if contractResult != nil {
		contractTail = suffixUTF16(contractResult.TailOutput, 800)
	}
	runner.note(fmt.Sprintf(
		"[codeaf] W12d root-cause: dispatching (frontier one-shot, fix cycle %d)\n",
		cycle,
	))
	diagnosis, ok := runner.dispatchPlanningOneShot(
		ctx,
		"root-cause",
		frontierplanning.BuildRootCausePrompt(struct {
			TaskText     string                              `json:"taskText"`
			Cycle        jscompat.JSNumber                   `json:"cycle"`
			Blockers     []frontierplanning.RootCauseBlocker `json:"blockers"`
			DiffStat     string                              `json:"diffStat"`
			ContractTail string                              `json:"contractTail"`
		}{
			TaskText: goal, Cycle: jscompat.JSNumber(cycle - 1), Blockers: blockers,
			DiffStat: diffStat, ContractTail: contractTail,
		}),
		"",
		4*time.Minute,
		&oneShotToolSettings{},
	)
	if !ok {
		return verdict
	}
	hint := frontierplanning.RootCauseRepairHint(diagnosis)
	verdict.RepairHints = append([]string{hint}, verdict.RepairHints...)
	runner.note("[codeaf] W12d root-cause: diagnosis attached as lead repair hint\n")
	return verdict
}
