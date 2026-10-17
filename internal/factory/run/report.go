package run

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/publish"
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
	if report.PR != nil && report.CommentURL != "" {
		return fmt.Errorf("publish reports either a pr or a comment")
	}
	if report.Change != nil {
		if err := validateChange(report.Change); err != nil {
			return err
		}
		it.Change = report.Change
	}
	if report.ChangeBase != "" {
		it.ChangeBase = report.ChangeBase
	}
	if report.HeadCommit != "" {
		if !publish.ValidCommit(report.HeadCommit) {
			return fmt.Errorf("head_commit requires a full commit SHA")
		}
		it.HeadCommit = report.HeadCommit
	}
	if p := report.Publishing; p != nil {
		switch p.State {
		case "queued", "pushing", "opened", "failed":
		default:
			return fmt.Errorf("unknown publishing state")
		}
		if p.Attempt < 1 || (p.Code != "" && !publish.Known(p.Code)) {
			return fmt.Errorf("invalid publishing attempt or code")
		}
		if p.State == "failed" && p.Code == "" {
			return fmt.Errorf("failed publishing requires a code")
		}
		it.PublishStatus = p
	}
	ensureStream(it, now)
	if err := reportRoom(it, report.Room); err != nil {
		return err
	}
	if report.Publish != "" {
		if report.Publish != "pr" && report.Publish != "comment" {
			return fmt.Errorf("publish must be pr or comment")
		}
		it.Publish = report.Publish
	}
	if report.CommentURL != "" {
		if strings.TrimSpace(report.CommentURL) == "" {
			return fmt.Errorf("comment requires a url")
		}
		it.CommentURL = report.CommentURL
		it.PullRequest = nil
	}
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
	if err := factory.MergeThread(it, report.Thread, report.Delivered); err != nil {
		return err
	}
	return reportPR(it, report.PR)
}

// reportRoom records the item team the worker's factory stage opened, which
// only the worker's machine can know: its Stream.Room lives on the mirror, and
// without this the server's copy of the job never learns its team. THE ROOM IS
// WRITE-ONCE: a job that already has one keeps it, a different one is refused
// whole, and one that is not shaped like a team id is never recorded, so a
// worker cannot move a job into another team.
// ValidRoom validates the opaque logical crew id carried between a worker and its factory.
// It is an id, never a filesystem path. This does not establish local crew ownership.
func ValidRoom(room string) bool {
	return room != "" && len(room) <= 64 && strings.IndexFunc(room, func(r rune) bool {
		return !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '-' || r == '_')
	}) < 0
}

func reportRoom(it *factory.Item, room string) error {
	room = strings.TrimSpace(room)
	if room == "" {
		return nil
	}
	if !ValidRoom(room) {
		return fmt.Errorf("room is not a team id")
	}
	if have := strings.TrimSpace(it.Stream.Room); have != "" {
		if have != room {
			return fmt.Errorf("job already has a team")
		}
		return nil
	}
	it.Stream.Room = room
	return nil
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

// dropSettledPlanApproval frees the item's decision slot when a new approve
// question arrives after a plan approval already settled: THE OLD CONSENT NEVER
// CARRIES TO A NEW QUESTION, so the new ask needs a new approval with its own
// five-second window. Only a settled plan approval goes; one still inside its
// window ("approving") belongs to undo and settle, and any other kind is not
// this question's. The factory record keeps the earlier consent.
func dropSettledPlanApproval(it *factory.Item) {
	if a := it.Approval; a != nil && a.Kind == "plan" && a.State == "approved" {
		it.Approval = nil
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
	if kind == factory.QKindApprove {
		dropSettledPlanApproval(it)
	}
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
	it.CommentURL = ""
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
	if len(step.Rounds) > 0 {
		ph.Rounds = append([]factory.RoundLine(nil), step.Rounds...)
		// A report cannot leave more lines than the step's own max.
		cap := factory.RoundCap(st.Max, ph.Round)
		if len(ph.Rounds) > cap {
			ph.Rounds = append([]factory.RoundLine(nil), ph.Rounds[len(ph.Rounds)-cap:]...)
		}
	}
	if step.Failure != nil {
		f := *step.Failure
		if f.Step == "" {
			f.Step = step.Name
		}
		factory.StampFailure(it, f)
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
	round := ph.Round
	if round < 1 {
		round = 1
	}
	factory.NoteRound(ph, factory.RoundCap(st.Max, round), factory.RoundLine{
		Round: round, Summary: factory.RoundSummary(kept), At: now,
	})
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
