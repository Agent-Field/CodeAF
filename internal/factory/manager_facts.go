package factory

import (
	"fmt"
	"strings"
)

// ── THE FACTS A MANAGER MAY STATE ──────────────────────────────────────────
//
// The 2026-11-06 live audit caught a job's manager saying four things it could
// not know: that a gate held an Auto run, that a step was running after it had
// been stopped, "Stage 6 of 7" where the rail said 4 of 8, and that it could
// not read the pull request the factory had just opened. And with "Merge pull
// request #161?" on the job page it said nothing was waiting. Every one was a
// fact factory_status did not carry, so the model guessed. These lines carry
// them, from the job as saved, and the manager's brief says to state job facts
// only from here.
//
// THE PULL REQUEST IS READ BY THE FACTORY, never by the manager's shell: the
// GitHub token is kept out of agent shells on purpose (internal/exec's
// tools.go), and the facts here are the ones the factory's own follow read.

// managerFactsLines are the job's autonomy, its one step count and current
// step, and its pull request, one line each, nothing for what is unknown; and
// the guidance only the manager reads about stating them.
func managerFactsLines(it Item) (out, guide []string) {
	switch {
	case Automatic(it):
		out = append(out, "Autonomy: Auto. Routine approve steps and publication pass by themselves; merging stays the person's decision.")
		guide = append(guide, "This job is in Auto: never say a gate holds the run or that nothing posts before the person approves.")
	case it.Autonomy == AutonomyManual || it.Manual:
		out = append(out, "Autonomy: Manual. Approve steps wait for the person's word.")
	default:
		out = append(out, "Autonomy: the pipeline's own. Its approve steps wait for the person's word.")
	}
	// THE ONE COUNT ([StepCount]): the rail, the header, the queue and the
	// Brief all say it, so the manager says it too and never counts again.
	if done, total := StepCount(it); total > 0 {
		line := fmt.Sprintf("Steps: %d of %d done.", done, total)
		guide = append(guide, "Say the step count exactly as the Steps line gives it; it is the count every page of the job shows.")
		if it.Stream != nil && it.Stream.Cur >= 0 && it.Stream.Cur < len(it.Stream.Phases) {
			ph := it.Stream.Phases[it.Stream.Cur]
			line += " Current step: " + StepWords(it, ph.Name) + ", " + phaseWords(it, ph) + "."
		}
		out = append(out, line)
	}
	if pr := it.PullRequest; pr != nil && pr.Number > 0 {
		line := fmt.Sprintf("Pull request #%d (%s)", pr.Number, prStateWords(*pr))
		if u := strings.TrimSpace(pr.URL); u != "" {
			line += ": " + u
		}
		line += "."
		if it.Change != nil && it.Change.Stat != nil && it.Change.Stat.Files > 0 {
			s := it.Change.Stat
			line += fmt.Sprintf(" Change: %s, +%d −%d.", plural(s.Files, "file", "files"), s.Added, s.Removed)
		}
		line += " Checks: " + prChecksWords(*pr) + "."
		line += " Review: " + prReviewWords(*pr) + "."
		out = append(out, line)
		guide = append(guide, "The factory read the pull request facts above on its last follow; your shell has no GitHub access by design, so answer pull request questions from them and never say the pull request cannot be read.")
	}
	return out, guide
}

// StepWords is a step's name as a person reads it: its label when the
// pipeline gave one, "Follow PR" for the factory's own follow step, and its
// name otherwise. A step key (`followpr`) is the store's spelling, never a
// person's, so every manager fact names a step through here.
func StepWords(it Item, name string) string {
	if i := StageIndex(it.Stages, name); i >= 0 {
		if l := strings.TrimSpace(it.Stages[i].Label); l != "" {
			return l
		}
	}
	if name == FollowPRStep {
		return "Follow PR"
	}
	return name
}

// phaseWords is a phase's state as the job page says it, a step a person or a
// failure stopped said as stopped, never running.
func phaseWords(it Item, ph Phase) string {
	switch ph.State {
	case PhaseDone:
		return "done"
	case PhaseSkipped:
		return "skipped"
	case PhaseWaiting:
		return "waiting"
	case PhaseFailed:
		if StoppedByPerson(it) || it.Failure != nil && (it.Failure.Code == CodeStopped || it.Failure.Code == FailureStopped) {
			return "stopped"
		}
		return "failed"
	case PhaseRunning:
		if it.State == StateRunning {
			return "running"
		}
		if it.State == StateQueued {
			return "queued to carry on"
		}
		return "stopped"
	}
	return "not started"
}

func prStateWords(pr PullRequest) string {
	switch {
	case pr.State == PRMerged:
		return "merged"
	case pr.State == PRClosed:
		return "closed"
	case pr.State == PROpen && pr.Draft:
		return "draft"
	case pr.State == PROpen:
		return "open, ready for review"
	}
	return "state not read yet"
}

func prChecksWords(pr PullRequest) string {
	switch {
	case !pr.ChecksRead:
		return "not read yet"
	case pr.NoChecks || pr.NoWorkflows && pr.Checks == (PRChecks{}):
		return "no GitHub checks are configured on this head"
	}
	return fmt.Sprintf("%d passed, %d failed, %d still running", pr.Checks.Passed, pr.Checks.Failed, pr.Checks.Pending)
}

func prReviewWords(pr PullRequest) string {
	switch {
	case !pr.ReviewsRead:
		return "not read yet"
	case pr.ReviewDecision == "approved":
		return "approved"
	case pr.ReviewDecision == "changes_requested":
		return "a reviewer asked for changes"
	case len(pr.Reviews) == 0:
		return "no reviews yet"
	}
	return plural(len(pr.Reviews), "review", "reviews") + ", no decision"
}

// OffersMarkReady says whether the job page offers Mark ready: the job's own
// draft pull request, open. api's Can.PRReady is this and the caller's
// ownership; the manager's status reads the same rule.
func OffersMarkReady(it Item) bool {
	pr := it.PullRequest
	return it.Kind != KindPR && it.Publish == "pr" && it.DoneHow != "dropped" && pr != nil && pr.Number > 0 && pr.State == PROpen && pr.Draft
}

// managerPRDecision is the post-PR decision the job page shows, in a line the
// person reads and a line for the manager, or nothing when none waits: Mark
// ready on a draft, Merge on an open pull request the gate passes, and the
// merge already pressed and waiting out its Undo.
func managerPRDecision(it Item) (person, actions []string) {
	if OffersPublishApproval(it) {
		person = append(person, "Waiting on the person: Approve publishing this job's checked work?")
		actions = append(actions, "To approve publishing when the person asks in this conversation, call factory_answer with action=approve and answer set to their words; it waits out its Undo window first.")
	}
	pr := it.PullRequest
	if pr == nil || pr.Number <= 0 || pr.State != PROpen {
		return person, actions
	}
	if m := it.Merging; m != nil && (m.State == MergeAsked || m.State == MergeQueued || m.State == MergeMerging) {
		line := fmt.Sprintf("Merge of pull request #%d is on its way", pr.Number)
		if m.State == MergeAsked && !m.UndoUntil.IsZero() {
			line += "; Undo is open until " + m.UndoUntil.UTC().Format("15:04:05 UTC")
		}
		return append(person, line+"."), actions
	}
	if OffersMarkReady(it) {
		person = append(person, fmt.Sprintf("Waiting on the person: Mark pull request #%d ready for review? (%s)", pr.Number, "it is a draft: "+DraftWhy(it)))
		actions = append(actions, "To mark it ready when the person asks in this conversation, call factory_answer with action=mark_ready and answer set to their words.")
	}
	if v := MergeOffered(it); v.OK {
		person = append(person, fmt.Sprintf("Waiting on the person: Merge pull request #%d?", pr.Number))
		actions = append(actions, "To merge when the person asks in this conversation, call factory_answer with action=merge and answer set to their words; the merge waits out its Undo window first. Never merge on your own judgment, in Auto or Manual.")
	} else if it.Kind != KindPR && it.Publish == "pr" {
		person = append(person, "Merge is not offered yet: "+v.Why+".")
	}
	return person, actions
}

// PRDecisionWords is the post-PR decision lines the job page shows as they
// stand on it now, the same lines factory_status carries. A press that changed
// the job (Mark ready) reads them again, so the manager's answer says what the
// page now offers rather than what it offered before the press.
func PRDecisionWords(it Item) []string {
	person, _ := managerPRDecision(it)
	return person
}

// OffersPublishApproval says whether the job page asks the person to approve
// publishing the job's checked work: its proof approval, waiting.
func OffersPublishApproval(it Item) bool {
	a := JobApproval(it)
	return a != nil && a.Kind == "proof" && a.State == "waiting"
}
