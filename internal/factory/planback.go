package factory

import "strings"

// WHERE A PLAN'S NO SENDS THE WORK. A person who answers no to an approve step
// sends the run back to the finished step before it, which runs again in a new
// round with their words. These are the ONE reading of that target: the
// door that answers a no when no worker holds the job (internal/factory/run),
// the typed plan revision that refuses to send a no with nowhere to go
// (internal/factory/lease, internal/factory/serve) and the wire that tells a
// reader whether the revision is possible and where it goes (internal/factory/
// api) all ask here, so the button, the refusal and the rewind can never
// disagree about which step a plan came from.

// ApproveBackTarget finds the approve step a no answers and the finished step
// before it. The step waiting is the gate itself; the shape older workers
// stored, the finished step waiting with the gate pending right after it, is
// read the same way, on the same terms the yes path's repair asks. A gate of
// -1 is a shape not recognised; a prev of -1 is no step to send back to.
func ApproveBackTarget(s *Stream) (gate, prev int) {
	if s == nil || s.Cur < 0 || s.Cur >= len(s.Phases) || s.Phases[s.Cur].State != PhaseWaiting {
		return -1, -1
	}
	ph := s.Phases[s.Cur]
	if ph.Kind == StageGate {
		return s.Cur, PreviousWork(s.Phases, s.Cur)
	}
	if ph.Result != nil && ph.Result.Done && s.Cur+1 < len(s.Phases) && s.Phases[s.Cur+1].Kind == StageGate && s.Phases[s.Cur+1].State == PhasePending {
		return s.Cur + 1, s.Cur
	}
	return -1, -1
}

// PreviousWork is the last finished step before i that is not an approve
// step, or -1 when there is nowhere to send the work back to. A skipped step
// is not finished work: the run never did it.
func PreviousWork(ph []Phase, i int) int {
	for k := i - 1; k >= 0; k-- {
		if ph[k].Kind != StageGate && ph[k].State == PhaseDone {
			return k
		}
	}
	return -1
}

// PlanSendsBack says whether a no to this item, right now, is a plan
// revision: the item is a plan waiting for its person (needs you, an approve
// question asked, the wire kind approve_plan) and its run has a finished step
// before the approve step to send the work back to. backTo is that step's
// name, which a document from an old build may not carry; ok with an empty
// name is still a real target.
//
// A NO THAT IS NOT A REVISION IS NOT ANSWERED HERE. With no finished step
// before it the same no stops the job, and with a run shape nobody recognises
// it only asks again; neither is "send the plan back", so both answer false.
func PlanSendsBack(it Item) (backTo string, ok bool) {
	if !PlanWaiting(it) {
		return "", false
	}
	gate, prev := ApproveBackTarget(it.Stream)
	if gate < 0 || prev < 0 {
		return "", false
	}
	return it.Stream.Phases[prev].Name, true
}

// PlanWaiting says the item is a plan waiting for its person: needs you, with
// an approve question asked, read on the wire as approve_plan. It says nothing
// about where a no would send the work; [PlanSendsBack] does.
func PlanWaiting(it Item) bool {
	return it.State == StateNeedsYou && it.QKind == QKindApprove && strings.TrimSpace(it.Question) != "" && NeedsKind(it) == NeedsApprovePlan
}
