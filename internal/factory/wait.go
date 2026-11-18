package factory

import (
	"fmt"
	"strings"
	"time"
)

// A JOB IS NEVER IDLE WITHOUT A NAMED OWNER AND AN ACTION THAT WILL WORK.
//
// Wait is that owner, stored on the job. Before it, "what does this job wait
// on?" was worked out from the job's other fields in three places that could
// disagree (the queue, the job page and the manager's status), and a display
// order among them could lock a job: job 31 (2026-11-17) read "PR has a
// conflict" for hours over a merge that had resolved it, because the conflict
// outranked the approval that would have pushed the fix.
//
// The store sets Wait and Offers on every write ([SettleWait]), in the same
// update as the change that caused them, and nothing else writes them. The
// watchdog reads their clocks; the manager's status and the wire read their
// words.

// Who a job waits on.
const (
	// WaitWorking is a step, a publish or a merge under way.
	WaitWorking = "working"
	// WaitGitHub is a pull request waiting on its checks, a review or the
	// forge's own answer about merging.
	WaitGitHub = "github"
	// WaitYou is the person: one action of theirs moves the job.
	WaitYou = "you"
	// WaitMainAgent is a stop the job's manager holds: a step's question it
	// has been given a turn on.
	WaitMainAgent = "main_agent"
	// WaitRunner is a job queued for a computer or a slot.
	WaitRunner = "runner"
	// WaitBacklog is a job nobody started, or one its person stopped.
	WaitBacklog = "backlog"
	// WaitDone is a finished job.
	WaitDone = "done"
)

// What the person is asked for when Who is [WaitYou].
const (
	ActApproveStart   = "approve_start"
	ActApprovePlan    = "approve_plan"
	ActApproveStep    = "approve_step"
	ActApprovePublish = "approve_publish"
	ActApproveReady   = "approve_ready"
	ActMerge          = "merge"
	ActAnswer         = "answer"
	ActBudget         = "budget"
	ActRetry          = "retry"
	ActRetryPublish   = "retry_publish"
	ActComputer       = "computer"
	ActResume         = "resume"
	// ActDecide is the fallback: the job needs the person and no narrower
	// action names why. Words says it plainly.
	ActDecide = "decide"
	// ActApproveActs is public acts on the job's pull request that wait for
	// the person's yes ([PublicAct]). Words says all of them in one line.
	ActApproveActs = "approve_acts"
)

// The actions a job can offer. Each is one door of the service.
const (
	DoApprove        = "approve"
	DoAnswer         = "answer"
	DoSpendAnswer    = "spend_answer"
	DoRejectPlan     = "reject_plan"
	DoAcceptStep     = "accept_step"
	DoMerge          = "merge"
	DoMarkReady      = "pr_ready"
	DoRetryPublish   = "retry_publish"
	DoPublishAnyway  = "publish_anyway"
	DoRerunStep      = "rerun_step"
	DoResume         = "resume"
	DoResumePipeline = "resume_pipeline"
	DoResumeAuto     = "resume_auto"
	DoRaiseBudget    = "raise_budget"
	DoStart          = "start"
	// The two answers to public acts that wait: one yes or one no for all.
	DoActsApprove = "acts_approve"
	DoActsDecline = "acts_decline"
	// DoTask sends a delegate into the job's checkout on an extra task
	// (task.go).
	DoTask = "task"
)

// Who may take an offer.
const (
	OfferByYou       = "you"
	OfferByMainAgent = "main_agent"
)

// Wait is what a job waits on, and since when.
type Wait struct {
	// Who is one of the Wait* words.
	Who string `json:"who"`
	// Action is what the person is asked for; set only when Who is you.
	Action string `json:"action,omitempty"`
	// What narrows the wait: the step that is running, "publishing",
	// "merging", "checks", "review".
	What string `json:"what,omitempty"`
	// Words says the wait in a line a person reads.
	Words string `json:"words,omitempty"`
	// PutAway is a wait the person has seen and set aside: a held job, a
	// decision put away. It still waits on them, and is not a stall.
	PutAway bool `json:"put_away,omitempty"`
	// Since is when this wait began. A write that leaves the wait the same
	// keeps it.
	Since time.Time `json:"since,omitzero"`
	// Deadline is when the wait is overdue and the watchdog looks at it; zero
	// for a wait with no clock of its own (the person's, a finished job's).
	Deadline time.Time `json:"deadline,omitzero"`
}

// Stall is a wait the watchdog found wrong: late, invalid, or with no way out.
// It is handed to the job's manager first; GaveUp says the manager's turn did
// not move the job, and the job then waits on its person to decide.
type Stall struct {
	// Kind names what tripped, one of the Stall* words.
	Kind string `json:"kind"`
	// Words says it in a line a person reads.
	Words string `json:"words"`
	// Wait is the wait that stalled, by its key. A job that reads another
	// wait has moved on, and the stall goes with it.
	Wait string `json:"wait"`
	// At is when the watchdog first saw it, Turns how many manager turns it
	// has had, and NextAt when the watchdog looks again.
	At     time.Time `json:"at,omitzero"`
	Turns  int       `json:"turns,omitempty"`
	NextAt time.Time `json:"next_at,omitzero"`
	GaveUp bool      `json:"gave_up,omitempty"`
}

// Offer is one action the service accepts on the job now.
type Offer struct {
	Action string `json:"action"`
	// By is who may take it: the person, or the job's manager.
	By string `json:"by"`
}

// The clocks a wait is given. A wait past its deadline is the watchdog's to
// look at; none of them moves a job by itself.
const (
	// WaitMainAgentFor is how long the manager holds a stop before it is late.
	WaitMainAgentFor = 5 * time.Minute
	// WaitRunnerFor is how long a queued job waits for a computer.
	WaitRunnerFor = 15 * time.Minute
	// WaitGitHubFor is how long a pull request goes unread before its wait is
	// late: two failed-read back-offs of the poller.
	WaitGitHubFor = 20 * time.Minute
)

// same says two waits are the same wait, so the second keeps the first's clock.
func (w Wait) same(o Wait) bool {
	return w.Who == o.Who && w.Action == o.Action && w.What == o.What && w.PutAway == o.PutAway
}

// SettleWait stores the job's wait and offers across one write, from before to
// it. Every store write calls it, so no road that moves a job has to remember
// to say what the job now waits on.
func SettleWait(before Item, it *Item, now time.Time) {
	w := WaitOf(*it)
	if prior := before.Wait; prior != nil && prior.same(w) && !prior.Since.IsZero() {
		w.Since = prior.Since
	} else {
		w.Since = now
	}
	switch w.Who {
	case WaitMainAgent:
		w.Deadline = w.Since.Add(WaitMainAgentFor)
	case WaitRunner:
		w.Deadline = w.Since.Add(WaitRunnerFor)
		if r := it.Recovery; r != nil && !r.Deadline.IsZero() {
			w.Deadline = r.Deadline
		}
	case WaitGitHub:
		// EVERY READ OF THE PULL REQUEST MOVES THIS CLOCK: the poller writes the
		// job when it reads, and a wait on GitHub is late only when nothing has
		// read it for a while.
		w.Deadline = now.Add(WaitGitHubFor)
	case WaitWorking:
		switch {
		case it.Approval != nil && it.Approval.State == "approving":
			w.Deadline = it.Approval.UndoUntil
		case it.Merging != nil && it.Merging.State == MergeAsked:
			w.Deadline = it.Merging.UndoUntil
		}
	}
	// A STALL BELONGS TO THE WAIT IT WAS SEEN ON. Once the job reads another
	// wait it has moved, and the record would only mislabel the new one. A
	// stall the watchdog gave up on reads "you: decide", so it is compared
	// with the wait underneath it.
	if st := it.Stall; st != nil && st.Wait != underStall(*it).key() {
		it.Stall = nil
		w = WaitOf(*it)
		w.Since = now
	}
	it.Wait = &w
	it.Offers = OffersOf(*it)
}

// WaitChanged says a write moved the job to another wait. Readers that follow
// jobs are told on exactly this.
func WaitChanged(before, after Item) bool {
	switch {
	case before.Wait == nil && after.Wait == nil:
		return false
	case before.Wait == nil || after.Wait == nil:
		return true
	}
	return !before.Wait.same(*after.Wait)
}

// underStall is the wait the job's fields give with the watchdog's record set
// aside: the wait a stall was seen on.
func underStall(it Item) Wait {
	it.Stall = nil
	return WaitOf(it)
}

// WaitOf says what the job waits on, with no clock. It is the one reading of
// the job's other fields into a wait; [SettleWait] stores it.
func WaitOf(it Item) Wait { return waitWithActs(it, waitUnderActs(it)) }

// waitUnderActs is the wait the job's fields give with its public acts set
// aside; [waitWithActs] reads those in (public_act.go).
func waitUnderActs(it Item) Wait {
	state := JobState(it)
	if state == "done" {
		return Wait{Who: WaitDone}
	}
	if state == "backlog" {
		w := Wait{Who: WaitBacklog}
		if StoppedByPerson(it) {
			w.What = "stopped"
		}
		return w
	}
	// A STALL THE MANAGER COULD NOT MOVE IS THE PERSON'S TO DECIDE, said in the
	// watchdog's own words.
	if st := it.Stall; st != nil && st.GaveUp {
		// NO COMPUTER IS A STALL ONLY A COMPUTER ENDS. The job is asked for
		// nothing it can be given through a door: its person turns a computer
		// on, and the job goes on by itself ([WaitFault] allows this one wait
		// with nothing to press).
		if st.Kind == StallRunner || st.Kind == StallUnheld {
			return Wait{Who: WaitYou, Action: ActComputer, Words: firstLine(st.Words)}
		}
		return Wait{Who: WaitYou, Action: ActDecide, Words: firstLine(st.Words)}
	}
	// A DECISION THE PERSON PUT AWAY IS STILL THEIRS, whatever the job reads
	// underneath: nothing moves it until they say Resume.
	if PausedDecision(it) {
		return Wait{Who: WaitYou, Action: ActResume, PutAway: true, Words: DecisionPauseDetail(it)}
	}
	if (state == "queued" || state == "running") && it.Stream != nil && it.Stream.Paused && !it.Publishing {
		return Wait{Who: WaitYou, Action: ActResume, PutAway: true, Words: "Held. Resume to carry on."}
	}
	// AN APPROVAL OR A MERGE INSIDE ITS UNDO WINDOW IS THE SERVICE'S ACT IN
	// FLIGHT, whatever state the job is in: the settle pass carries it out
	// when the window closes. A landed job in that window used to read "you:
	// approve" with nothing to press, because the approval was already given.
	if _, what := actInFlight(it); what != "" {
		return Wait{Who: WaitWorking, What: what}
	}
	switch state {
	case "queued":
		return Wait{Who: WaitRunner, What: waitStep(it)}
	case "running":
		return runningWait(it)
	}
	return youWait(it)
}

// waitStep is the step the job is on, by its one word.
func waitStep(it Item) string {
	if s := it.Stream; s != nil && s.Cur >= 0 && s.Cur < len(s.Phases) {
		return s.Phases[s.Cur].Name
	}
	return ""
}

func runningWait(it Item) Wait {
	switch {
	case it.Publishing:
		return Wait{Who: WaitWorking, What: "publishing"}
	case it.Merging != nil && (it.Merging.State == MergeAsked || it.Merging.State == MergeQueued || it.Merging.State == MergeMerging):
		return Wait{Who: WaitWorking, What: "merging"}
	case it.Approval != nil && it.Approval.State == "approving":
		return Wait{Who: WaitWorking, What: "approving"}
	}
	if it.State == StateShipped && it.PullRequest != nil {
		// A PULL REQUEST THE GATE PASSES WAITS ON THE PERSON'S MERGE, and on
		// nothing at GitHub: it used to read "running" beside a Merge card.
		if MergeOffered(it).OK {
			return Wait{Who: WaitYou, Action: ActMerge, Words: "Ready to merge."}
		}
		w := Wait{Who: WaitGitHub, What: githubWhat(it)}
		if f := it.FollowUp; f != nil {
			w.Words = firstLine(f.Reason)
		}
		if w.Words == "" {
			w.Words = PRWaitingWords(it.PullRequest)
		}
		return w
	}
	// A STEP'S QUESTION THE MANAGER HOLDS is the manager's, with a clock: the
	// step is parked on its answer.
	if a := it.Asking; a != nil && a.With == AskedManager {
		return Wait{Who: WaitMainAgent, What: a.Stage, Words: a.Question}
	}
	// A task that is queued for a runner or running is the step the job is
	// on, so the wait names it. One that waits behind the round running now
	// is said in the wait's words (task.go).
	w := Wait{Who: WaitWorking, What: waitStep(it)}
	if i, ok := WaitingTask(it); ok && it.Stream.Cur != i {
		w.Words = "A task runs when this round ends."
	}
	return w
}

// githubWhat names what a watched pull request waits on at the forge.
func githubWhat(it Item) string {
	pr := it.PullRequest
	if pr == nil {
		return ""
	}
	switch {
	case pr.Checks.Pending > 0 || !pr.ChecksRead:
		return "checks"
	case pr.Checks.Failed > 0:
		return "checks"
	case pr.Mergeable == "":
		return "mergeable"
	}
	return "review"
}

func youWait(it Item) Wait {
	you := func(action, words string) Wait {
		return Wait{Who: WaitYou, Action: action, Words: firstLine(words)}
	}
	switch JobReason(it) {
	case "paused":
		w := you(ActResume, DecisionPauseDetail(it))
		w.PutAway = true
		return w
	case "publish_failed":
		// A REPAIR THE PULL REQUEST HAS MOVED PAST CANNOT BE PUBLISHED AGAIN AS
		// IT STANDS: the publish reads the forge first and refuses a head that
		// is not the one the repair was made on. Follow PR has to take the new
		// commits in, so the wait names that, not a retry that fails the same way.
		if publishOutrun(it) {
			return you(ActRetry, firstNonEmpty(it.PublishError, "The pull request moved after this repair was made."))
		}
		return you(ActRetryPublish, firstNonEmpty(it.PublishError, "Publishing failed."))
	case "merge_failed":
		words := "The merge failed."
		if m := it.Merging; m != nil && strings.TrimSpace(m.Why) != "" {
			words = m.Why
		}
		return you(ActMerge, words)
	case "failed":
		// A STEP THE SERVICE IS ABOUT TO RUN AGAIN BY ITSELF waits on a runner,
		// as a lost runner does: a dropped connection is retried within
		// seconds, and a job that read "retry" for those seconds offered its
		// person a rerun the retry then refused as "not run yet".
		if retrying(it) {
			return Wait{Who: WaitRunner, What: waitStep(it), Words: firstLine(it.Recovery.Reason)}
		}
		// A FAILED STEP WITH THE BUDGET SPENT WAITS ON THE BUDGET FIRST: the
		// rerun is refused until it is raised, so "retry" named a door that
		// was shut. Once raised, the job reads "retry" and offers the rerun.
		if !budgetLeft(it) {
			return you(ActBudget, "The budget is reached. Raise it to run this step again.")
		}
		return you(ActRetry, failureWords(it))
	case "runner_offline":
		// A LOST RUNNER THE SERVICE IS STILL RETRYING waits on a computer, with
		// the retry's own deadline; one it gave up on is the person's.
		if retrying(it) {
			return Wait{Who: WaitRunner, What: waitStep(it), Words: firstLine(it.Recovery.Reason)}
		}
		return you(ActComputer, failureWords(it))
	case "approval_start":
		return you(ActApproveStart, it.Question)
	case "approval_plan":
		return you(ActApprovePlan, it.Question)
	case "approval_step":
		return you(ActApproveStep, it.Question)
	case "approval_proof":
		if f := it.FollowUp; f != nil && f.State == "ready_approval" {
			return you(ActApproveReady, f.Reason)
		}
		if f := it.FollowUp; f != nil && f.State == "waiting_approval" {
			return you(ActApprovePublish, f.Reason)
		}
		return you(ActApprovePublish, "Checked and ready to publish.")
	case "budget":
		return you(ActBudget, it.Question)
	case "out_of_rounds":
		return you(ActAnswer, it.Question)
	}
	// What is left: a pull request's own trouble, a take-over, a Follow PR
	// that stopped, an open question.
	switch {
	case it.Manual:
		return you(ActResume, firstNonEmpty(it.Question, "You took over. Resume the pipeline to hand it back."))
	case it.QKind == QKindSpendStopped:
		return you(ActAnswer, it.Question)
	}
	// RESULTS THE SERVICE REFUSED ARE NOT A QUESTION. The runner that holds
	// them keeps them: the step is run again, or Resume pipeline tells the
	// runner to send them again. An answer would take the notice down and
	// send nothing.
	if resultsNotSaved(it) {
		return you(ActRetry, it.Question)
	}
	if k := QuestionKind(it); (k == NeedsQuestion || k == NeedsOutOfAttempts) && (strings.TrimSpace(it.Question) != "" || it.Asking != nil) {
		words := it.Question
		if words == "" && it.Asking != nil {
			words = it.Asking.Question
		}
		return you(ActAnswer, words)
	}
	if f := it.FollowUp; f != nil && f.State == "needs_you" {
		switch f.BlockedBy {
		case FollowBlockedBudget:
			// A BUDGET STOP WITH THE ATTEMPTS SPENT TOO is not lifted by a
			// raise: the next read lets Follow PR go only while it has a
			// repair attempt left ([QueueCIFollowUp]). Once the budget is
			// there the stop that stands is the attempts', and its door is
			// Resume pipeline; "raise the budget" would be pressed for ever.
			if budgetLeft(it) && f.Limit > 0 && f.Attempt >= f.Limit {
				return you(ActRetry, "The budget is raised. Follow PR has used its repair attempts; resume the pipeline to give it more.")
			}
			return you(ActBudget, f.Reason)
		case FollowBlockedAttempts:
			return you(ActRetry, f.Reason)
		}
		return you(ActDecide, f.Reason)
	}
	if pr := it.PullRequest; pr != nil {
		switch {
		case pr.Conflict:
			return you(ActDecide, "The pull request has a conflict with its base.")
		case pr.ReviewDecision == "changes_requested":
			return you(ActDecide, "A reviewer requested changes.")
		}
	}
	return you(ActDecide, firstNonEmpty(it.Question, "This job needs you."))
}

// budgetLeft says the job may spend more: it has no budget, or has not
// reached it.
func budgetLeft(it Item) bool { return it.Cap <= 0 || JobLifetimeSpent(it) < it.Cap }

// retrying says the service has a retry of the failed step planned or queued
// and nothing blocks it ([PlanStepRecovery]).
func retrying(it Item) bool {
	r := it.Recovery
	return r != nil && (r.State == "waiting" || r.State == "queued") && RecoveryBlockReason(it) == ""
}

// publishOutrun says the job's failed publish is of a Follow PR repair made on
// a head the pull request has since left: someone else pushed, or part of the
// publish itself went through.
func publishOutrun(it Item) bool {
	f, pr := it.FollowUp, it.PullRequest
	return publishFailed(it) && f != nil && pr != nil && pr.State == PROpen &&
		f.HeadSHA != "" && pr.HeadSHA != "" && pr.HeadSHA != f.HeadSHA
}

// resultsNotSaved says the job stopped because the service refused a
// runner's report, and the runner still holds the results it could not save.
func resultsNotSaved(it Item) bool {
	return it.State == StateNeedsYou && it.QKind == "results_not_saved" && !it.Manual
}

func failureWords(it Item) string {
	if f := it.Failure; f != nil {
		return firstNonEmpty(f.Summary, f.Detail, "The step failed.")
	}
	return "The step failed."
}

func firstNonEmpty(words ...string) string {
	for _, w := range words {
		if w = strings.TrimSpace(w); w != "" {
			return w
		}
	}
	return ""
}

// OffersOf lists the actions the service accepts on the job now, for the
// job's own person and for its manager. It names the same rules the doors
// apply; a door that refuses an offer for the job's state is a bug the
// scenario tests look for.
func OffersOf(it Item) []Offer {
	var out []Offer
	add := func(by, action string) { out = append(out, Offer{Action: action, By: by}) }
	state := JobState(it)
	if state == "done" {
		return nil
	}
	if StoppedByPerson(it) {
		add(OfferByYou, DoStart)
		return out
	}
	budgetLeft := budgetLeft(it)
	if RecoveryPaused(it) {
		// A DECISION PUT AWAY TAKES ONLY ITS OWN RESUME: every other decision
		// door refuses until then.
		add(OfferByYou, DoResume)
		add(OfferByYou, DoRaiseBudget)
		return out
	}
	if state == "backlog" {
		add(OfferByYou, DoStart)
		return out
	}
	if (state == "queued" || state == "running") && it.Stream != nil && it.Stream.Paused && !it.Publishing {
		add(OfferByYou, DoResume)
	}
	if a := JobApproval(it); a != nil && a.State == "waiting" {
		add(OfferByYou, DoApprove)
		if a.Kind == "proof" && WaitingApprover(it) == "" {
			add(OfferByMainAgent, DoApprove)
		}
	}
	kind := NeedsKind(it)
	answer := false
	switch kind {
	case NeedsQuestion, NeedsApproveStart, NeedsApprovePlan, NeedsApproveStep, NeedsSpendingLimit, NeedsOutOfAttempts:
		answer = true
	}
	if q := QuestionKind(it); q == NeedsQuestion || q == NeedsOutOfAttempts {
		answer = true
	}
	// A LOST RUNNER'S NOTICE TAKES AN ANSWER ONLY OVER A PUBLISH: "try once
	// more" queues the approved publish again and anything else stops it
	// (lease.Doors.Answer). A step that lost its runner asks nothing; an
	// answer to it was refused as "not waiting on you", and its doors are the
	// rerun and Resume pipeline.
	if it.Publishing && it.QKind == NeedsRunnerOffline {
		answer = true
	}
	if it.QKind == QKindSpendStopped && it.State == StateNeedsYou {
		add(OfferByYou, DoSpendAnswer)
		answer = true
	}
	if answer && it.State == StateNeedsYou && !resultsNotSaved(it) {
		add(OfferByYou, DoAnswer)
		if _, ok := PlanSendsBack(it); ok {
			add(OfferByYou, DoRejectPlan)
		}
		if OffersAccept(it) {
			add(OfferByYou, DoAcceptStep)
		}
	}
	if a := it.Asking; a != nil && a.With == AskedManager {
		add(OfferByMainAgent, DoAnswer)
	}
	if publishFailed(it) && !publishOutrun(it) {
		add(OfferByYou, DoRetryPublish)
	}
	if OffersPublishAnyway(it) {
		add(OfferByYou, DoPublishAnyway)
	}
	if OffersMarkReady(it) && !it.Publishing {
		add(OfferByYou, DoMarkReady)
		add(OfferByMainAgent, DoMarkReady)
	}
	if MergeOffered(it).OK {
		add(OfferByYou, DoMerge)
	}
	if OffersAutoResume(it) {
		add(OfferByYou, DoResumeAuto)
	}
	if !it.Publishing {
		// RESUME PIPELINE IS THE DOOR OUT OF A PULL REQUEST'S OWN TROUBLE: it
		// queues a Follow PR round on an open pull request whatever stopped
		// the last one, and hands a taken-over job back.
		open := it.PullRequest != nil && it.PullRequest.State == PROpen
		if state == "needs_you" && (it.Manual || open || resultsNotSaved(it)) {
			add(OfferByYou, DoResumePipeline)
		}
		if it.Failure != nil && it.Failure.Step != "" && budgetLeft {
			add(OfferByYou, DoRerunStep)
		}
		// RESULTS THE SERVICE REFUSED CAN BE SENT AGAIN, AND THE STEP CAN BE
		// RUN AGAIN. Only a few kinds of result are taken on a second send
		// (serve's validateRetainedReport); a refused final review or repair
		// is refused again every time, so the rerun is the door that always
		// moves the job, and the wait names it first.
		if s := it.Stream; resultsNotSaved(it) && it.Failure == nil && budgetLeft && s != nil && s.Cur >= 0 && s.Cur < len(s.Phases) {
			add(OfferByYou, DoRerunStep)
		}
	}
	// A STALL HANDED TO THE PERSON OFFERS THE TWO DOORS THAT RESTART WORK from
	// any state: run the current step again, or queue a Follow PR round.
	if st := it.Stall; st != nil && st.GaveUp && !it.Publishing {
		open := it.PullRequest != nil && it.PullRequest.State == PROpen
		if open && !Offered(Item{Offers: out}, OfferByYou, DoResumePipeline) {
			add(OfferByYou, DoResumePipeline)
		}
		if s := it.Stream; !open && s != nil && s.Cur >= 0 && s.Cur < len(s.Phases) && budgetLeft && !Offered(Item{Offers: out}, OfferByYou, DoRerunStep) {
			if ph := s.Phases[s.Cur]; ph.State != PhasePending || ph.Round > 0 {
				add(OfferByYou, DoRerunStep)
			}
		}
	}
	if kind == NeedsSpendingLimit || !budgetLeft || (it.FollowUp != nil && it.FollowUp.BlockedBy == FollowBlockedBudget) {
		add(OfferByYou, DoRaiseBudget)
	}
	// PUBLIC ACTS THAT WAIT ARE THE PERSON'S TO APPROVE OR DECLINE whatever
	// else the job waits on, one answer for all of them. In Auto the manager
	// may give the yes too.
	if len(ProposedActs(it)) > 0 {
		add(OfferByYou, DoActsApprove)
		add(OfferByYou, DoActsDecline)
		if Automatic(it) {
			add(OfferByMainAgent, DoActsApprove)
		}
	}
	// A TASK IS OFFERED WHEREVER ITS OWN RULES TAKE ONE ([TaskRefusal]), to the
	// person and to the manager alike. No wait asks for one, so it is a move
	// of none ([moves]): a job that offers only a task still offers its
	// person nothing that does what it waits on them for.
	if TaskRefusal(it, DoTask) == nil {
		add(OfferByYou, DoTask)
		add(OfferByMainAgent, DoTask)
	}
	return out
}

// Offered says whether the job offers action to by.
func Offered(it Item, by, action string) bool {
	for _, o := range it.Offers {
		if o.By == by && o.Action == action {
			return true
		}
	}
	return false
}

// moves are the offers that move a job that waits on its person. Stop, drop
// and a reply are always there and move nothing.
var moves = map[string][]string{
	ActApproveStart:   {DoApprove, DoAnswer},
	ActApprovePlan:    {DoApprove, DoAnswer},
	ActApproveStep:    {DoApprove, DoAnswer},
	ActApprovePublish: {DoApprove},
	ActApproveReady:   {DoApprove, DoMarkReady},
	ActMerge:          {DoMerge, DoResumePipeline},
	ActAnswer:         {DoAnswer, DoSpendAnswer},
	ActBudget:         {DoRaiseBudget, DoAnswer},
	ActRetry:          {DoRerunStep, DoResumePipeline, DoAnswer},
	ActRetryPublish:   {DoRetryPublish},
	ActComputer:       {DoRerunStep, DoAnswer},
	ActResume:         {DoResume, DoResumePipeline},
	ActDecide:         {DoResumePipeline, DoRerunStep, DoAnswer, DoResumeAuto, DoRaiseBudget},
	ActApproveActs:    {DoActsApprove, DoActsDecline},
}

// WaitMoves lists the actions the job offers its person that move it off what
// it waits on them for, the one the wait names first. It is empty for a job
// that does not wait on its person.
func WaitMoves(it Item) []string {
	w := it.Wait
	if w == nil || w.Who != WaitYou {
		return nil
	}
	var out []string
	for _, action := range moves[w.Action] {
		if Offered(it, OfferByYou, action) {
			out = append(out, action)
		}
	}
	return out
}

// WaitFault says what is wrong with the job's stored wait, or nil. It is the
// rule the scenario tests hold after every event and the watchdog reads on a
// timer: the wait is the one the job's fields give, and a job that waits on
// its person offers them something that moves it.
func WaitFault(it Item) error {
	w := it.Wait
	if w == nil {
		return fmt.Errorf("%s has no stored wait", it.Ref())
	}
	want := WaitOf(it)
	if !w.same(want) {
		return fmt.Errorf("%s stores wait %s but its fields read %s", it.Ref(), w.key(), want.key())
	}
	if w.Who != WaitYou {
		return nil
	}
	if w.Action == "" {
		return fmt.Errorf("%s waits on its person with no action", it.Ref())
	}
	// A stall no computer picked up is ended by a computer, not by a door.
	if st := it.Stall; st != nil && st.GaveUp && w.Action == ActComputer {
		return nil
	}
	for _, action := range moves[w.Action] {
		if Offered(it, OfferByYou, action) {
			return nil
		}
	}
	return fmt.Errorf("%s waits on its person to %s and offers nothing that does it", it.Ref(), w.Action)
}

func (w Wait) key() string {
	parts := []string{w.Who}
	for _, p := range []string{w.Action, w.What} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if w.PutAway {
		parts = append(parts, "put away")
	}
	return strings.Join(parts, ":")
}

// actionWords says each action the person is asked for, as the end of
// "waits on the person to …".
var actionWords = map[string]string{
	ActApproveStart:   "approve the start",
	ActApprovePlan:    "approve the plan",
	ActApproveStep:    "approve the next step",
	ActApprovePublish: "approve publishing the checked work",
	ActApproveReady:   "approve marking the pull request ready",
	ActMerge:          "merge the pull request",
	ActAnswer:         "answer the question",
	ActBudget:         "raise the budget",
	ActRetry:          "retry the step",
	ActRetryPublish:   "retry publishing",
	ActComputer:       "bring a computer online",
	ActResume:         "resume it",
	ActDecide:         "decide what happens next",
	ActApproveActs:    "approve what is to be posted or changed on the pull request",
}

// WaitLine says the job's stored wait and offers in the words the manager's
// status carries, so the manager and the job page name the same wait. It is ""
// for a job that waits on nothing (backlog, done).
func WaitLine(it Item) string {
	w := it.Wait
	if w == nil {
		read := WaitOf(it)
		w = &read
	}
	var line string
	switch w.Who {
	case WaitWorking:
		line = "Waits on: nobody; " + firstNonEmpty(w.What, "work") + " is under way."
	case WaitGitHub:
		line = "Waits on: GitHub (" + firstNonEmpty(w.What, "the pull request") + ")."
	case WaitRunner:
		line = "Waits on: a computer to run it."
	case WaitMainAgent:
		line = "Waits on: you, the manager, to answer " + firstNonEmpty(w.What, "the step") + "'s question."
	case WaitYou:
		line = "Waits on: the person, to " + firstNonEmpty(actionWords[w.Action], w.Action) + "."
		if w.PutAway {
			line += " They put it away; it is not urgent."
		}
	default:
		return ""
	}
	offers := it.Offers
	if it.Wait == nil {
		offers = OffersOf(it)
	}
	var you, manager []string
	for _, o := range offers {
		if o.By == OfferByMainAgent {
			manager = append(manager, o.Action)
		} else {
			you = append(you, o.Action)
		}
	}
	if len(you) > 0 {
		line += " Accepted from the person now: " + strings.Join(you, ", ") + "."
	}
	if len(manager) > 0 {
		line += " Accepted from you now: " + strings.Join(manager, ", ") + "."
	}
	return line
}
