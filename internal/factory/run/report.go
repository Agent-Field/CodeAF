package run

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ApplyReport runs inside the store's existing Update path. The same fold,
// claim merge and bounded log writes serve both local rounds and HTTP reports.
func ApplyReport(it *factory.Item, report factory.RunReport, now time.Time) error {
	if math.IsNaN(report.Cost) || math.IsInf(report.Cost, 0) || report.Cost < 0 {
		return fmt.Errorf("cost must be a finite nonnegative dollar amount")
	}
	if it.State != factory.StateRunning && it.State != factory.StateNeedsYou {
		return fmt.Errorf("job is no longer running")
	}
	ensureStream(it, now)
	if err := applyPipeline(it, report.Pipeline); err != nil {
		return err
	}
	for _, step := range report.Stages {
		if err := reportStep(it, step, now); err != nil {
			return err
		}
	}
	addCost(it, report.Cost)
	for _, line := range report.Log {
		loopSay(it, now, line.Tone, line.Text)
	}
	mergeClaims(it, report.Claims)
	if err := reportQuestion(it, report.Question, report.QuestionKind, now); err != nil {
		return err
	}
	if talk := strings.TrimSpace(report.Talk); talk != "" && strings.TrimSpace(it.Talk) == "" {
		it.Talk = talk
	}
	it.Notes = append(it.Notes, report.Notes...)
	return reportPR(it, report.PR)
}

// applyPipeline takes the shape of the worker's reshaped steps. The recipe
// bounded the reshaping on the worker; the one law kept here is the person's:
// an approve step on the job stays on, so no worker can skip a person's gate.
// A phase the job already has keeps the state the server holds for it.
func applyPipeline(it *factory.Item, change *factory.PipelineChange) error {
	if change == nil {
		return nil
	}
	for _, st := range it.Stages {
		if !st.On || !factory.IsApprove(st) {
			continue
		}
		if i := factory.StageIndex(change.Stages, st.Name); i < 0 || !change.Stages[i].On {
			return fmt.Errorf("a worker cannot take an approve step off the pipeline")
		}
	}
	held := map[string]factory.Phase{}
	for _, ph := range it.Stream.Phases {
		held[ph.Name] = ph
	}
	phases := make([]factory.Phase, 0, len(change.Phases))
	for _, ph := range change.Phases {
		if kept, ok := held[ph.Name]; ok {
			phases = append(phases, kept)
			continue
		}
		phases = append(phases, factory.Phase{Name: ph.Name, Kind: ph.Kind, State: factory.PhasePending, Note: ph.Note})
	}
	it.Stages = append([]factory.Stage(nil), change.Stages...)
	it.Stream.Phases = phases
	if it.Stream.Cur >= len(phases) {
		it.Stream.Cur = max(len(phases)-1, 0)
	}
	return nil
}

func mergeClaims(it *factory.Item, claims []factory.Claim) {
	for _, claim := range claims {
		if claim.Medium == "policy" {
			it.Policy = mergeClaim(it.Policy, claim)
		} else {
			it.Proof = mergeClaim(it.Proof, claim)
		}
	}
}

func reportQuestion(it *factory.Item, question *factory.Asked, kind string, now time.Time) error {
	if question == nil {
		return nil
	}
	q := *question
	if strings.TrimSpace(q.Question) == "" {
		return fmt.Errorf("question requires text")
	}
	q.At, q.With = now, factory.AskedYou
	if q.Stage != "" {
		i := phaseIndex(it, q.Stage)
		if i < 0 {
			return fmt.Errorf("question names an unknown step")
		}
		it.Stream.Cur = i
		setPhaseState(&it.Stream.Phases[i], factory.PhaseWaiting, now)
	}
	it.Asking = &q
	it.State = factory.StateNeedsYou
	it.Question, it.QKind = q.Question, kind
	if it.QKind == "" {
		it.QKind = qkindStep
	}
	loopSay(it, now, "ask", q.Question)
	return nil
}

func reportPR(it *factory.Item, receipt *factory.PullRequest) error {
	if receipt == nil {
		return nil
	}
	if receipt.Number <= 0 || strings.TrimSpace(receipt.URL) == "" {
		return fmt.Errorf("pr requires a positive number and url")
	}
	pr := *receipt
	it.PullRequest = &pr
	return nil
}

func ensureStream(it *factory.Item, now time.Time) {
	if it.Stream == nil {
		it.Stream = &factory.Stream{Started: now}
	}
	if len(it.Stream.Phases) != 0 {
		return
	}
	for _, st := range it.Stages {
		if st.On && factory.Fits(st, *it) {
			it.Stream.Phases = append(it.Stream.Phases, factory.Phase{Name: st.Name, Kind: kindOf(st), State: factory.PhasePending, Note: st.Ask})
		}
	}
}

func phaseIndex(it *factory.Item, name string) int {
	for i, ph := range it.Stream.Phases {
		if ph.Name == name {
			return i
		}
	}
	return -1
}

func reportStep(it *factory.Item, step factory.StageReport, now time.Time) error {
	i, index := phaseIndex(it, step.Name), factory.StageIndex(it.Stages, step.Name)
	if i < 0 || index < 0 {
		return fmt.Errorf("report names an unknown step")
	}
	st := it.Stages[index]
	// Approval remains a person's door; an executing worker cannot answer it.
	if st.Kind == factory.StageGate && (step.State == factory.PhaseDone || step.Result != nil) {
		return fmt.Errorf("an approve step requires a person's answer")
	}
	switch step.State {
	case "", factory.PhasePending, factory.PhaseRunning, factory.PhaseDone, factory.PhaseFailed, factory.PhaseWaiting:
	default:
		return fmt.Errorf("unknown step state")
	}
	if step.Round < 0 {
		return fmt.Errorf("step round must not be negative")
	}
	ph := &it.Stream.Phases[i]
	if step.State != "" {
		setPhaseState(ph, step.State, now)
	}
	if step.Round > 0 {
		ph.Round = step.Round
	}
	it.Stream.Cur = i
	if step.Result != nil {
		if step.Result.Edit != nil {
			return fmt.Errorf("worker reports cannot change the pipeline")
		}
		foldResult(it, st, i, *step.Result, now)
	}
	return nil
}

func addCost(it *factory.Item, usd float64) { it.Stream.Spent += usd }

// foldResult is the persisted half of fold; the local loop also keeps its
// in-memory prior results, while a worker carries those in its own runner.
func foldResult(it *factory.Item, st factory.Stage, i int, res factory.StageResult, now time.Time) {
	ph := &it.Stream.Phases[i]
	if res.Chat != "" {
		ph.Chat = res.Chat
	}
	// THE RESULT OUTLIVES THE PROCESS: a run revived after a restart
	// reads it back ([Runner.revive]), so later steps keep their
	// `before this stage` lines.
	kept := res
	kept.Edit = nil
	if len(kept.Output) > factory.ResultOutputMost {
		kept.Output = strings.ToValidUTF8(kept.Output[:factory.ResultOutputMost], "")
	}
	ph.Result = &kept
	it.Stream.Findings = res.Findings
	sheet := makesSheet(st)
	for _, cl := range res.Claims {
		switch {
		case cl.Medium == "policy":
			it.Policy = mergeClaim(it.Policy, cl)
		case sheet:
			it.Proof = mergeClaim(it.Proof, cl)
		default:
			if text := oneLine(cl.Text); text != "" {
				loopSay(it, now, "thought", "claimed: "+text)
			}
		}
	}
	if line := loopFirstLine(res.Output); line != "" {
		tone := "said"
		if kindOf(st) == factory.StageCheck {
			tone = "test"
		}
		loopSay(it, now, tone, st.Name+": "+line)
	}
	if !factory.Met(st, res) && res.Done {
		ph.Note = shortfall(res)
	}
	// THE PLAN'S FILE LIST IS THE BRIEF'S. A later plan that names files
	// replaces the list; one that names none leaves the previous list.
	if strings.EqualFold(strings.TrimSpace(st.Name), "plan") {
		if files := planFiles(res); len(files) > 0 {
			it.PlannedFiles = files
		}
	}
}

// setPhaseState moves a phase and stamps its finish. Done and failed are the
// ends the brief's clock reads (step.at). Pending and running clear a stamp,
// so a step sent back does not keep the time it ended before. Waiting leaves
// the stamp alone: a question is not an ending.
func setPhaseState(ph *factory.Phase, state factory.PhaseState, now time.Time) {
	ph.State = state
	switch state {
	case factory.PhaseDone, factory.PhaseFailed:
		ph.At = now
	case factory.PhasePending, factory.PhaseRunning:
		ph.At = time.Time{}
	}
}

// FinishReport uses the local runner's landing gate before releasing a hold.
// A worker may report interruption or a question, but cannot approve proof.
func FinishReport(it *factory.Item, state factory.State, now time.Time) error {
	ensureStream(it, now)
	switch state {
	case "", factory.StateLanded:
		landItem(it, now)
	case factory.StateShipped:
		if factory.HasApprove(*it) || len(it.Proof) == 0 || !clean(*it) {
			return fmt.Errorf("job requires proof approval")
		}
		landItem(it, now)
	case factory.StateNeedsYou:
		if it.Question == "" {
			return fmt.Errorf("job requires a question")
		}
		it.State = state
		it.Stream.Ended = now
	case factory.StateNew, factory.StateDismissed:
		it.State = state
		it.Stream.Ended = now
	default:
		return fmt.Errorf("unknown final job state")
	}
	return nil
}

// logShippedHabit is the log's line for a job that shipped itself.
const logShippedHabit = "shipped · habit · proof all green"

func landItem(it *factory.Item, now time.Time) bool {
	it.State = factory.StateLanded
	it.Question, it.QKind = "", ""
	it.Asking = nil
	it.Stream.Ended = now
	it.Stream.Paused = false
	if n := len(it.Stream.Phases); n > 0 {
		it.Stream.Cur = n - 1
	}
	if !factory.HasApprove(*it) && len(it.Proof) > 0 && clean(*it) {
		it.State = factory.StateShipped
		loopSay(it, now, "ok", logShippedHabit)
		return true
	}
	loopSay(it, now, "said", sayLanded)
	return false
}
