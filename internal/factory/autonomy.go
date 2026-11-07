package factory

import (
	"fmt"
	"strings"
)

// Auto approves routine pipeline progress, never merge or critical questions.
const AutonomyAuto = "auto"
const AutonomyManual = "manual"

// Automatic is deliberately false for old records without a saved mode and
// while Take over has suspended the pipeline. A mode never resumes Take over.
func Automatic(it Item) bool {
	return !StoppedByPerson(it) && it.Autonomy == AutonomyAuto && !it.Manual
}

// AsksBeforeStep says Manual holds phase i for the person's word before its
// first round. Only an explicit Manual asks: a record with no saved mode keeps
// its pipeline's own approve steps. Start is the word for the first step, an
// approve step just passed is the word for the step after it, and a step that
// has run once (a retry, a send-back) is the same step, not a new one. Follow PR
// has its own door: Manual asks before each repair push.
func AsksBeforeStep(it Item, i int) bool {
	if it.Autonomy != AutonomyManual || it.Stream == nil || i < 0 || i >= len(it.Stream.Phases) {
		return false
	}
	ph := it.Stream.Phases
	p := ph[i]
	if p.Kind == StageGate || p.State != PhasePending || p.Round > 0 || it.FollowUp != nil || p.Name == FollowPRStep {
		return false
	}
	return PreviousWork(ph, i) >= 0 && !answeredByApprove(it, i)
}

// answeredByApprove says the step before phase i is an approve step that asked.
// One turned off passes quietly, so it said nothing about the next step.
func answeredByApprove(it Item, i int) bool {
	for k := i - 1; k >= 0; k-- {
		p := it.Stream.Phases[k]
		if p.Kind != StageGate {
			return false
		}
		if j := StageIndex(it.Stages, p.Name); j < 0 || it.Stages[j].On {
			return p.State == PhaseDone
		}
	}
	return false
}

// CheckAutomaticApproval runs under the item's write lock before a queued
// automatic decision can start work. Budget and mode may have changed since
// the service selected the candidate; an earlier read is not authority.
// A settled ("approved") automatic approval is history, not a queued decision:
// it must not refuse a person's later answer after a switch to Manual.
func CheckAutomaticApproval(it Item) error {
	if err := CheckPersonStop(it); err != nil {
		return err
	}
	if it.Approval == nil || !it.Approval.Automatic || it.Approval.State == "approved" {
		return nil
	}
	if !Automatic(it) {
		return fmt.Errorf("Auto approval stopped: the job is no longer in Auto")
	}
	if it.Cap <= 0 || JobLifetimeSpent(it) >= it.Cap {
		return fmt.Errorf("Auto approval stopped: job budget reached")
	}
	return nil
}

// AutonomyBlockReason explains a saved exception without asking a model to
// infer why Auto has stopped. A routine approval in flight has no exception.
func AutonomyBlockReason(it Item) string {
	if !Automatic(it) || (it.State != StateNeedsYou && it.State != StateLanded) {
		return ""
	}
	// A START HOLD IS NOT AUTO STOPPING: nothing ran, so there is nothing
	// Auto stopped. The job's own question ("Approval needed: start · …") is
	// the whole of what it waits on, and a surface that prefixes this reason
	// with "Auto stopped" would say something that did not happen.
	if it.QKind == QKindAskFirst {
		return ""
	}
	if it.Cap <= 0 {
		return "A job budget is required. Set Budget before continuing."
	}
	if JobLifetimeSpent(it) >= it.Cap {
		return "Job budget reached. Choose a higher Budget to continue."
	}
	if it.PublishError != "" {
		return "Publication failed: " + strings.TrimSuffix(it.PublishError, ".") + ". Resolve the problem, then retry publishing."
	}
	if it.QKind != "" && it.QKind != QKindApprove && it.QKind != "cap" {
		return it.Question
	}
	// AN APPROVAL THAT NAMES SOMEONE ELSE IS THEIRS: Auto never answers for
	// the person the pipeline asked by name.
	if NamesOtherApprover(it) {
		return "This approval waits on " + WaitingApprover(it) + ", whom the pipeline names."
	}
	// A PLAN THAT TOUCHES WHAT A RULE GUARDS IS NEVER ROUTINE: the person said
	// to be asked, so Auto asks.
	if it.QKind == QKindApprove && it.RuleHold != "" {
		if words, ok := planNoFilesWords(it.RuleHold); ok {
			return words + ". Read the plan, then approve it or send it back."
		}
		return ruleHoldWords(it.RuleHold) + ". Review the plan, then press Approve."
	}
	if it.State == StateLanded && it.RuleHold != "" {
		return ruleHoldWords(it.RuleHold) + ". Review the changes, then press Approve."
	}
	if it.Approval != nil && it.Approval.Error != "" {
		return "Automatic approval failed: " + it.Approval.Error + ". Review the problem before trying again."
	}
	if it.State == StateLanded {
		if len(it.Proof) == 0 {
			return "Proof is incomplete. Request changes or take over to continue."
		}
		for _, cl := range it.Proof {
			if !cl.OK {
				return "Proof needs attention: " + cl.Text + ". Request changes or take over to continue."
			}
		}
		for _, cl := range it.Policy {
			if !cl.OK {
				return "Repository policy needs attention: " + cl.Text + ". Request changes or take over to continue."
			}
		}
		for i := len(it.Stages) - 1; i >= 0; i-- {
			if !it.Stages[i].On {
				continue
			}
			if IsApprove(it.Stages[i]) && it.Stages[i].Fixed {
				return "Repository policy requires your approval before publication. Review the changes, then press Approve."
			}
			break
		}
	} else if it.QKind == QKindApprove && it.Stream != nil && it.Stream.Cur >= 0 && it.Stream.Cur < len(it.Stream.Phases) {
		name := it.Stream.Phases[it.Stream.Cur].Name
		for _, st := range it.Stages {
			if st.Name == name && st.Fixed {
				return "Repository policy requires this step's approval even in Auto. Review the result, then press Approve."
			}
		}
	}
	if it.Person() == "" {
		return "A job owner is required before Auto can approve progress."
	}
	return ""
}

// ruleHoldWords is a rule hold in a person's words: a protected path match
// (`protected · auth/ · auth/login.go`) is "A protected path: auth/ ·
// auth/login.go"; any other hold is said as the rule that asks first.
func ruleHoldWords(hold string) string {
	if words, ok := planNoFilesWords(hold); ok {
		return words
	}
	if rest, ok := strings.CutPrefix(hold, "protected · "); ok {
		return "A protected path: " + rest
	}
	return "A rule asks first: " + hold
}

// planNoFilesWords describes the missing checkable list without claiming the
// plan's prose names no files. The stored hold suffix stays compatible.
func planNoFilesWords(hold string) (string, bool) {
	rest, ok := strings.CutPrefix(hold, "protected · ")
	if !ok {
		return "", false
	}
	paths, ok := strings.CutSuffix(rest, PlanNoFiles)
	if !ok {
		return "", false
	}
	against := "its protected paths"
	if paths = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(paths), "·")); paths != "" {
		against = paths
	}
	return "This plan didn't give the factory a file list it can check against " + against, true
}
