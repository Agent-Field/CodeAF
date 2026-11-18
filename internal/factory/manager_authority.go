package factory

import (
	"errors"
	"fmt"
)

// ── WHAT A JOB'S MANAGER MAY PRESS BY ITSELF ───────────────────────────────
//
// The owner's authority table (2026-11-17) says who decides what on a job:
// merging is the person's in Manual and in Auto; in Manual so are approving a
// publish, marking the pull request ready, approving proposed public acts and
// passing an approve step; in Auto the manager may do those four itself,
// except where Auto itself stopped for the person.
// Everything else the manager's tools take (another round, a rerun, a task,
// proposing an act, handing a stop to the person) is the manager's in both.
//
// Until this rule the table was held only by the manager's instructions: with
// the job page asking "Approve publishing?" a manager turn the engine started
// could press it in Manual, and nothing in code refused. THE RULE IS CHECKED
// AT THE ONE DOOR EVERY MANAGER TOOL PRESSES THROUGH (cmd/codeaf's
// factory_manager_press.go), so choosing another tool does not get round it.
//
// "The person said so" is read from the turn, never from the model: it is
// true only when the running turn carries words the person typed
// (internal/session's turnPersonWords). A turn the engine started (a step's
// question, a stall, a stop) carries none.

// The presses [ManagerMay] rules on. Any other press is the manager's own.
const (
	ManagerMerge       = "merge"
	ManagerResumeAuto  = "resume"
	ManagerApprove     = "approve"
	ManagerMarkReady   = "mark_ready"
	ManagerApproveActs = "approve_acts"
	ManagerPassStep    = "pass_step"
)

// managerPressWords says each ruled press as the end of "… is the person's",
// and managerPressHow how the person does it. Proposed public acts have no
// card on the job page yet, so their yes is said in the conversation.
var managerPressWords = map[string]string{
	ManagerApprove:     "approving publication",
	ManagerMarkReady:   "marking the pull request ready",
	ManagerApproveActs: "approving public acts",
	ManagerPassStep:    "going past an approve step",
}

var managerPressHow = map[string]string{
	ManagerApprove:     "They approve on the job page, or by saying so here.",
	ManagerMarkReady:   "They mark it ready on the job page, or by saying so here.",
	ManagerApproveActs: "They approve by saying yes here.",
	ManagerPassStep:    "They approve it on the job page, or by saying so here.",
}

// The refusals, said once so the tests and the manual quote the same words.
const (
	ManagerMergeRefusal    = "Nothing changed: merging is the person's alone, in Manual and in Auto, and nothing in this turn came from them. They merge with the Merge card on the job page, or by telling you here to merge it."
	ManagerResumeRefusal   = "Nothing changed: only the person can tell Auto to carry on, and nothing in this turn came from them."
	managerModeRefusal     = "Nothing changed: this job is not in Auto, so %s is the person's, and nothing in this turn came from them. %s"
	managerAutoStopRefusal = "Nothing changed: Auto stopped here for the person, and nothing in this turn came from them. %s"
	// ManagerPersonStepRefusal is an approve step only a person passes, asked
	// of the manager on a turn the person did not speak in.
	ManagerPersonStepRefusal = "Nothing changed: only a person can pass this approve step, and nothing in this turn came from them. They approve it on the job page, or by saying so here."
)

// ManagerMay says whether the job's manager may press act on it now, nil for
// yes. said is whether the running turn carries the person's own words.
//
// ANYTHING SHORT OF A RUNNING AUTO IS THE PERSON'S: a job with no saved mode,
// one the person took over or stopped, and one whose decision is put away all
// read as not Auto ([Automatic]), so an unknown mode never widens what the
// manager may do.
func ManagerMay(it Item, act string, said bool) error {
	if said {
		return nil
	}
	switch act {
	case ManagerMerge:
		return errors.New(ManagerMergeRefusal)
	case ManagerResumeAuto:
		return errors.New(ManagerResumeRefusal)
	}
	words, ruled := managerPressWords[act]
	if !ruled {
		return nil
	}
	if !Automatic(it) {
		return fmt.Errorf(managerModeRefusal, words, managerPressHow[act])
	}
	if act == ManagerApprove || act == ManagerPassStep {
		// AN APPROVE STEP ONLY A PERSON PASSES IS NOT AUTO'S EITHER ([PersonOnly]).
		if st, _, ok := WaitingGateStage(it); ok && PersonOnly(st) {
			return errors.New(ManagerPersonStepRefusal)
		}
		// WHAT AUTO ITSELF STOPPED AT IS THE PERSON'S ([AutonomyBlockReason]):
		// a failing finding, a rule that asks first, a named approver, a spent
		// budget. The manager does not approve past what Auto would not.
		if why := AutonomyBlockReason(it); why != "" {
			return fmt.Errorf(managerAutoStopRefusal, why)
		}
	}
	return nil
}
