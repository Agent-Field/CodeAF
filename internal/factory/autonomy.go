package factory

import "fmt"

// Auto approves routine pipeline progress, never merge or critical questions.
const AutonomyAuto = "auto"
const AutonomyManual = "manual"

// Automatic is deliberately false for old records without a saved mode and
// while Take over has suspended the pipeline. A mode never resumes Take over.
func Automatic(it Item) bool { return it.Autonomy == AutonomyAuto && !it.Manual }

// CheckAutomaticApproval runs under the item's write lock before a queued
// automatic decision can start work. Budget and mode may have changed since
// the service selected the candidate; an earlier read is not authority.
// A settled ("approved") automatic approval is history, not a queued decision:
// it must not refuse a person's later answer after a switch to Manual.
func CheckAutomaticApproval(it Item) error {
	if it.Approval == nil || !it.Approval.Automatic || it.Approval.State == "approved" {
		return nil
	}
	if !Automatic(it) {
		return fmt.Errorf("Auto approval stopped: the job is no longer in Auto")
	}
	if it.Cap <= 0 || JobSpent(it) >= it.Cap {
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
	if it.Cap <= 0 {
		return "A job budget is required. Set Budget before continuing."
	}
	if JobSpent(it) >= it.Cap {
		return "Job budget reached. Choose a higher Budget to continue."
	}
	if it.PublishError != "" {
		return "Publication failed: " + it.PublishError + ". Resolve the problem, then retry publishing."
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
		return "A rule asks first: " + it.RuleHold + ". Review the plan, then press Approve."
	}
	if it.State == StateLanded && it.RuleHold != "" {
		return "A rule asks first: " + it.RuleHold + ". Review the changes, then press Approve."
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
