package run

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/publish"
)

// ApplyReport runs inside the store's existing Update path. The same fold,
// claim merge and bounded log writes serve both local rounds and HTTP reports.
func ApplyReport(it *factory.Item, report factory.RunReport, now time.Time) error {
	if it.Manual {
		return fmt.Errorf("automatic worker no longer owns this job; manual control is active")
	}
	if math.IsNaN(report.Cost) || math.IsInf(report.Cost, 0) || report.Cost < 0 {
		return fmt.Errorf("cost must be a finite nonnegative dollar amount")
	}
	if it.State != factory.StateRunning && it.State != factory.StateNeedsYou {
		return fmt.Errorf("job is no longer running")
	}
	if report.PR != nil && report.CommentURL != "" {
		return fmt.Errorf("publish reports either a pr or a comment")
	}
	legacyReset := report.FollowUp != nil && it.FollowUp != nil && report.FollowUp.Attempt == 0 && it.FollowUp.Attempt > 0
	if legacyReset {
		f := *report.FollowUp
		f.Attempt = it.FollowUp.Attempt
		report.FollowUp = &f
	}
	previousPR := it.PullRequest
	if err := applyFollowObservation(it, report); err != nil {
		return err
	}
	completed := completedFollowReport(*it, report)
	if legacyReset && !completed {
		return fmt.Errorf("follow-up identity changed")
	}
	if completed && previousPR != nil && previousPR.HeadSHA == it.FollowUp.HeadSHA {
		// The service may have observed newer feedback while the worker was
		// checking this head. Completion cannot roll that observation backward.
		report.PR = nil
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
	if report.FollowUp != nil {
		if it.FollowUp == nil || report.FollowUp.HeadSHA != it.FollowUp.HeadSHA || (report.FollowUp.Attempt != it.FollowUp.Attempt && !completed) {
			return fmt.Errorf("follow-up identity changed")
		}
		f := *it.FollowUp
		f.State, f.Reason, f.BlockedBy = report.FollowUp.State, report.FollowUp.Reason, report.FollowUp.BlockedBy
		if f.Kind == "readiness" && report.FollowUp.Kind == "pr" && report.FollowUp.Readiness == nil {
			f.Kind = "pr"
			f.Readiness = nil
			f.Request = report.FollowUp.Request
		}

		if report.FollowUp.Readiness != nil {
			a := report.FollowUp.Readiness
			if a.State != "reviewing" && a.State != "reviewed" && a.State != "ready" && a.State != "checked" {
				return fmt.Errorf("unknown readiness state")
			}
			if a.State == "ready" && (f.Readiness == nil || f.Readiness.State != "reviewed" || !it.Publishing) {
				return fmt.Errorf("ready requires the recorded publication transition")
			}

			if f.Readiness == nil || a.Key != f.Readiness.Key || a.Head != f.Readiness.Head {
				return fmt.Errorf("readiness snapshot changed")
			}
			if (a.State == "reviewed" || a.State == "checked") && f.Readiness.State != a.State {
				if a.State == "checked" && (it.PullRequest == nil || it.PullRequest.Draft || it.PullRequest.HeadSHA != a.Head || !f.Readiness.External) {
					return fmt.Errorf("checked requires the captured external ready PR head")
				}
				found := false
				for _, step := range report.Stages {
					if step.Name == factory.FollowPRStep && step.Result != nil {
						if e := factory.ValidateReadinessReview(*it, *step.Result); e != nil {
							return e
						}
						found = true
					}
				}
				if !found && it.Stream != nil {
					for _, ph := range it.Stream.Phases {
						if ph.Name == factory.FollowPRStep && ph.Result != nil {
							if e := factory.ValidateReadinessReview(*it, *ph.Result); e != nil {
								return e
							}
							found = true
						}
					}
				}
				if !found {
					return fmt.Errorf("readiness requires the final review report")
				}
			}
			if f.Readiness.State != a.State && (a.State == "reviewed" || a.State == "ready") {
				word := "Final readiness review passed"
				if a.State == "ready" {
					word = "PR marked ready for review"
				}
				it.Note(now, word+" · head "+a.Head+" · "+a.Summary)
			}
			cp := *a
			cp.External = f.Readiness.External
			f.Readiness = &cp
		}
		it.FollowUp = &f
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
	if completed {
		// Acknowledge only the revisions captured for this attempt. Feedback
		// arriving in flight remains pending; no worker-supplied map overwrites it.
		factory.AcknowledgeReview(it)
		it.FollowUp.Request = ""
		if it.QKind == "results_not_saved" {
			it.Question, it.QKind = "", ""
			it.Asking = nil
			it.Failure = nil
			it.State = factory.StateRunning
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
	// THE HOLD IS THE SERVER'S OWN READING: a worker's runner records it on
	// its mirror, which a report does not carry, so the copy that decides Auto
	// works it out again from the plan and the rules it holds.
	it.RuleHold = PlanRuleHold(*it)
	if talk := strings.TrimSpace(report.Talk); talk != "" && strings.TrimSpace(it.Talk) == "" {
		it.Talk = talk
	}
	it.Notes = append(it.Notes, report.Notes...)
	if err := factory.MergeThread(it, report.Thread, report.Delivered); err != nil {
		return err
	}
	if err := reportPR(it, report.PR); err != nil {
		return err
	}
	return applyMerge(it, report.Merge, now)
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
	if q.At.IsZero() {
		q.At = now
	}
	q.With = factory.AskedYou
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
	newLoop := step.State == factory.PhasePending && step.Round > ph.Round
	ph.Verification = nil
	if step.Chat != "" {
		ph.Chat = step.Chat
	}
	if step.State != "" {
		setPhaseState(ph, step.State, now)
	}
	// WHEN THE ROUND BEGAN IS THE WORKER'S CLOCK, not this report's arrival:
	// the report comes after the round has run a while. It lands before the
	// result, so the round line folded below carries it.
	if !step.Started.IsZero() {
		ph.Started = step.Started
	}
	if step.Round > 0 {
		ph.Round = step.Round
	}
	if step.RoundLimit > 0 {
		if newLoop {
			ph.RoundLimit = step.RoundLimit
		} else {
			ph.RoundLimit = max(ph.RoundLimit, step.RoundLimit)
		}
	}
	it.Stream.Cur = i
	if step.Result != nil {
		if step.Result.Edit != nil {
			return fmt.Errorf("worker reports cannot change the pipeline")
		}
		foldResult(it, st, i, *step.Result, now)
	}
	// This snapshot describes the current report, not the result folded above.
	ph.Verification = step.Verification
	if ph.State == factory.PhaseDone || ph.State == factory.PhaseFailed || ph.State == factory.PhasePending {
		ph.Verification = nil
	}
	if len(step.Rounds) > 0 {
		// Merge by conversation identity. A restarted worker may have only a
		// suffix; it must not erase tries already acknowledged by the service.
		for _, line := range step.Rounds {
			factory.NoteRound(ph, factory.RoundLinesMost, line)
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
	if res.Checkpoint {
		return
	}
	ph := &it.Stream.Phases[i]
	ph.Verification = nil
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
	factory.NoteRound(ph, factory.RoundLinesMost, factory.RoundLine{
		Rejected: !factory.Met(st, res) && res.ExecutionBlocked == "", Round: round, Summary: factory.RoundSummary(kept), At: now, Started: ph.Started, Chat: res.Chat,
	})
	it.Stream.Findings = res.Findings
	sheet := makesSheet(st)
	for _, cl := range res.Claims {
		switch {
		case cl.Medium == "policy" && sheet:
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
		// A REPORT THE FACTORY MADE IS NEVER READ AS THE STEP'S OWN WORDS
		// (stagereport.go).
		who := st.Name
		if res.Inferred {
			who += " (" + factory.ReportedForTheStep + ")"
		}
		loopSay(it, now, tone, who+": "+line)
	}
	if !factory.Met(st, res) && res.Done {
		ph.Note = shortfall(res)
	}
	// THE PLAN'S DECLARED FILE LIST IS THE BRIEF'S, and only a finished plan
	// round speaks for it. A completed plan that declares none CLEARS the
	// cached list (unknown is not the previous plan's files); a partial report
	// leaves what is known. Readers derive from the saved result
	// ([factory.PlanFilesOf]); the cache remains for older builds reading the
	// same persisted item.
	if res.Done && strings.EqualFold(strings.TrimSpace(st.Name), "plan") {
		it.PlannedFiles = factory.ParsePlanFiles(res.Output)
	}
}

// setPhaseState moves a phase and stamps its finish. Done and failed are the
// ends the brief's clock reads (step.at). Pending and running clear a stamp,
// so a step sent back does not keep the time it ended before. Waiting leaves
// the stamp alone: a question is not an ending. Pending also clears when the
// step began (step.started_at): a step sent back has not started again until
// its next round does, and running leaves it to the round that sets it
// ([floorLoop.runPhase]), since an answer that puts a step running again is
// not a new round.
func setPhaseState(ph *factory.Phase, state factory.PhaseState, now time.Time) {
	ph.State = state
	if state == factory.PhaseDone || state == factory.PhaseFailed || state == factory.PhasePending {
		ph.Verification = nil
	}
	switch state {
	case factory.PhaseDone, factory.PhaseFailed:
		ph.At = now
	case factory.PhaseRunning:
		ph.At = time.Time{}
	case factory.PhasePending:
		ph.At, ph.Started = time.Time{}, time.Time{}
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
		if it.FollowUp != nil && it.FollowUp.State == "healthy" && it.FollowUp.Readiness != nil && it.FollowUp.Readiness.State == "checked" && !ExternalPRHeadChecked(*it) {
			return fmt.Errorf("checked PR snapshot changed before completion")
		}
		if ExternalPRHeadChecked(*it) {
			if it.Question != "" || it.Asking != nil {
				return fmt.Errorf("a pending decision must be resolved before completing the PR review")
			}
			if err := factory.ReadinessGate(*it); err != nil {
				return err
			}
			found := false
			for _, ph := range it.Stream.Phases {
				if ph.Name == factory.FollowPRStep && ph.Result != nil {
					if err := factory.ValidateReadinessReview(*it, *ph.Result); err != nil {
						return err
					}
					found = true
				}
			}
			if !found {
				return fmt.Errorf("current-head review report is missing")
			}
			it.State = factory.StateShipped
			it.Question, it.QKind = "", ""
			it.Asking = nil
			it.Stream.Ended = now
			it.Stream.Paused = false
			return nil
		}
		if factory.NeedsPublishApproval(*it) || len(it.Proof) == 0 || !clean(*it) {
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
	if it.FollowUp != nil && (it.FollowUp.State == "queued" || it.FollowUp.State == "running") {
		f := *it.FollowUp
		f.State, f.Reason = "waiting_approval", "Repair and proof passed locally; updating the existing PR."
		if factory.ReadinessPending(*it) {
			f.State, f.Reason = "ready_approval", factory.ReadinessApprovalWords(*it)
		}
		it.FollowUp = &f
	}
	it.Question, it.QKind = "", ""
	it.Asking = nil
	it.Stream.Ended = now
	it.Stream.Paused = false
	if n := len(it.Stream.Phases); n > 0 {
		it.Stream.Cur = n - 1
		if it.PullRequest == nil && it.Stream.Phases[n-1].Name == factory.FollowPRStep && n > 1 {
			it.Stream.Cur = n - 2
		}
	}
	if it.FollowUp == nil && !factory.NeedsPublishApproval(*it) && len(it.Proof) > 0 && clean(*it) {
		it.State = factory.StateShipped
		loopSay(it, now, "ok", logShippedHabit)
		return true
	}
	if factory.ReadinessPending(*it) {
		loopSay(it, now, "said", factory.ReadinessApprovalWords(*it))
		return false
	}
	if factory.Automatic(*it) {
		loopSay(it, now, "said", sayAutoLanded)
	} else {
		loopSay(it, now, "said", sayLanded)
	}
	return false
}

// ExternalPRHeadChecked is completion of an inspection of an already-published
// ready PR, not an outward action or a worker's substitute publication consent.
func ExternalPRHeadChecked(it factory.Item) bool {
	f, p := it.FollowUp, it.PullRequest
	return f != nil && f.Kind == "readiness" && f.State == "healthy" && f.Readiness != nil && f.Readiness.External && f.Readiness.State == "checked" && p != nil && !p.Draft && p.State == factory.PROpen && p.HeadSHA == it.HeadCommit && p.HeadSHA == f.Readiness.Head && p.ReadinessKey != "" && p.ReadinessKey == f.Readiness.Key
}

// A held attempt may refresh its captured remote head or review snapshot before
// work. Accept only the same PR and attempt, following the previous captured
// head; never overwrite a third, newer service observation or publication.
func applyFollowObservation(it *factory.Item, report factory.RunReport) error {
	f, next, pr := it.FollowUp, report.FollowUp, report.PR
	if f == nil || next == nil || f.Attempt != next.Attempt || pr == nil {
		return nil
	}
	moved := f.HeadSHA != next.HeadSHA
	snapshot := f.Kind == "readiness" && f.Readiness != nil && next.Readiness != nil && f.Readiness.Key != next.Readiness.Key
	if !moved && !snapshot {
		return nil
	}
	old := it.PullRequest
	if it.Publishing || old == nil || old.Number != pr.Number || old.URL != pr.URL || pr.HeadSHA != next.HeadSHA || (old.HeadSHA != f.HeadSHA && old.HeadSHA != pr.HeadSHA) || (moved && next.ObservedFrom != f.HeadSHA) {
		return fmt.Errorf("follow-up observation no longer matches the held PR attempt")
	}
	if err := reportPR(it, pr); err != nil {
		return err
	}
	f.HeadSHA, f.ObservedFrom = next.HeadSHA, next.ObservedFrom
	if snapshot || moved {
		it.Approval, it.Proof, it.Policy = nil, nil, nil
		if f.Kind == "readiness" && next.Readiness != nil {
			if pr.ReadinessKey == "" || next.Readiness.Key != pr.ReadinessKey || next.Readiness.Head != pr.HeadSHA {
				return fmt.Errorf("complete current-head review material is required")
			}
			f.Readiness = &factory.PRReadiness{Key: pr.ReadinessKey, Head: pr.HeadSHA, State: "reviewing", External: pr.HeadSHA != it.HeadCommit}
		}
	}
	return nil
}

// PlanRuleHold is why the approval it waits on is a person's by the rules the
// job started under ([pipeline.PlanHold]), or "": only an approve question
// right after the plan step can be held, and only by that plan's files.
func PlanRuleHold(it factory.Item) string {
	if it.QKind != factory.QKindApprove {
		return ""
	}
	gate, prev := factory.ApproveBackTarget(it.Stream)
	if gate < 0 || prev < 0 || !strings.EqualFold(strings.TrimSpace(it.Stream.Phases[prev].Name), "plan") {
		return ""
	}
	return pipeline.PlanHold(it.StartRules, factory.PlanFilesOf(it))
}
