package run

import (
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// StepControlRefusal identifies a state conflict without changing its person-facing words.
type StepControlRefusal string

func (e StepControlRefusal) Error() string { return string(e) }

// StepControl changes only a recorded run. A rerun keeps completed earlier
// steps and the money already spent; publishing and final jobs cannot rewind.
func StepControl(it *factory.Item, name string, rerun bool, now time.Time) error {
	if factory.JobState(*it) == "done" || it.Publishing {
		return StepControlRefusal("finished or publishing jobs cannot change steps")
	}
	s := it.Stream
	if s == nil {
		return StepControlRefusal("step not found")
	}
	index := -1
	for i := range s.Phases {
		if s.Phases[i].Name == name {
			index = i
			break
		}
	}
	if index < 0 {
		return StepControlRefusal("step not found")
	}
	if !rerun && (index != s.Cur || (it.State != factory.StateRunning && it.State != factory.StateNeedsYou && it.State != factory.StateQueued)) {
		return StepControlRefusal("only the current running step can stop")
	}
	if rerun && s.Phases[index].State == factory.PhasePending {
		return StepControlRefusal("step has not run yet")
	}
	if rerun {
		// THE SHEET IS SETTLED BEFORE ANYTHING MOVES, so a rerun the sheet
		// refuses leaves every field of the job as it was.
		proof, policy, err := keepSheet(it, index, name)
		if err != nil {
			return err
		}
		it.Proof, it.Policy = proof, policy
		for i := index; i < len(s.Phases); i++ {
			p := s.Phases[i]
			s.Phases[i] = factory.Phase{Name: p.Name, Kind: p.Kind, Note: p.Note, State: factory.PhasePending}
		}
		// The queued target is marked begun so a worker revives this stream,
		// including a rerun from the first step, instead of erasing its spend.
		s.Phases[index].State = factory.PhaseRunning
		it.State = factory.StateQueued
		it.Failure = nil
		it.PublishError = ""
		it.PublishStatus = nil
		s.Ended = time.Time{}
	} else {
		it.State = factory.StateNew
		failure := &factory.Failure{Step: name, Code: "stopped", Detail: "Stopped by you", Attempts: s.Phases[index].Round, Next: factory.FailureStopped, At: now}
		it.Failure = failure
		s.Phases[index].State, s.Phases[index].Failure, s.Phases[index].At = factory.PhaseFailed, failure, now
		s.Ended = now
	}
	s.Cur, s.Paused = index, false
	it.Question, it.QKind, it.Asking = "", "", nil
	it.Approval, it.StopUndo = nil, nil
	loopSay(it, now, "said", map[bool]string{true: "Rerun from " + name, false: "Stopped step " + name}[rerun])
	return nil
}

// keepSheet says what the proof and policy sheets hold after a rerun rewinds
// phases [index..]; it changes nothing itself.
//
// Before any proof has run, and when a proof that ran is in the rewound range,
// both sheets clear, as they always did: there is no sheet a finished proof
// stands behind, or that proof runs again and writes it anew.
//
// After a proof that stays done, THE SHEET PROOF MADE STAYS: that proof will
// not run again, so its rows remain what the approve card, the pull request and
// landing read. When the rewound steps that ran are all gates, nothing rewound
// could have written a row, and the whole sheet stays exactly as it was.
//
// Otherwise a rewound step may have written rows, and they must not outlive
// the rewind. A phase keeps only its last round's result, and a worker's
// report can add a row no step result carries, so a row is only known to be a
// step's when the sheets can be rebuilt from every phase's last result, in
// order, the way [foldResult] built them, and the rebuild is exactly the sheet
// the job holds. Then the sheets become the rebuild of the steps that stay
// done: a rewound step's row goes, and a row it overwrote goes back to what the
// done step recorded, pass or fail. WHEN THE REBUILD DOES NOT MATCH, THE RERUN
// IS REFUSED, because some row's provenance is unknown and keeping it could
// show a rewound step's pass over proof's failure. The refusal names the proof
// to rerun from instead; proof is never run again behind the person's back.
func keepSheet(it *factory.Item, index int, name string) (proof, policy []factory.Claim, err error) {
	phases := it.Stream.Phases
	held := ""
	for i, ph := range phases {
		if factory.ProofPhaseRan(ph, it.Stages) {
			if i >= index {
				return nil, nil, nil
			}
			held = ph.Name
		}
	}
	if held == "" {
		return nil, nil, nil
	}
	gatesOnly := true
	for _, ph := range phases[index:] {
		ran := ph.State != "" && ph.State != factory.PhasePending || ph.Round > 0 || ph.Result != nil
		if ran && phaseStage(ph, it.Stages).Kind != factory.StageGate {
			gatesOnly = false
		}
	}
	if gatesOnly {
		return it.Proof, it.Policy, nil
	}
	allProof, allPolicy := rebuildSheets(phases, it.Stages)
	if !slices.Equal(allProof, it.Proof) || !slices.Equal(allPolicy, it.Policy) {
		return nil, nil, StepControlRefusal("the proof sheet has rows the steps before " + name + " cannot account for; rerun from " + held + " to prove it again")
	}
	proof, policy = rebuildSheets(phases[:index], it.Stages)
	return proof, policy, nil
}

// rebuildSheets folds each phase's last result onto empty sheets the way
// [foldResult] folds it: a policy claim from any step is a Policy row, and any
// other claim is a Proof row only from a step that [makesSheet].
func rebuildSheets(phases []factory.Phase, stages []factory.Stage) (proof, policy []factory.Claim) {
	for _, ph := range phases {
		if ph.Result == nil {
			continue
		}
		sheet := makesSheet(phaseStage(ph, stages))
		for _, cl := range ph.Result.Claims {
			switch {
			case cl.Medium == "policy":
				policy = mergeClaim(policy, cl)
			case sheet:
				proof = mergeClaim(proof, cl)
			}
		}
	}
	return proof, policy
}

// phaseStage is the stage a recorded phase ran as: the configured stage of
// its name, or, for a phase whose stage has since gone, one rebuilt from the
// phase's own name and kind, which is all [makesSheet] reads.
func phaseStage(ph factory.Phase, stages []factory.Stage) factory.Stage {
	for _, st := range stages {
		if strings.EqualFold(strings.TrimSpace(st.Name), strings.TrimSpace(ph.Name)) {
			return st
		}
	}
	return factory.Stage{Name: ph.Name, Kind: ph.Kind}
}

// EditStepsItem recompiles the unstarted tail after a bounded person edit.
// Completed and current phases retain their results and conversation identity.
func EditStepsItem(it *factory.Item, edit factory.RunEdit, recipe factory.Recipe) ([]string, error) {
	mode, last := factory.EditBeforeRun, -1
	if it.Stream != nil {
		for i, p := range it.Stream.Phases {
			if p.State != factory.PhasePending {
				mode, last = factory.EditInRun, i
			}
		}
	}
	next, lines, err := factory.Edit(*it, edit, recipe, mode)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, nil
	}
	if next.Stream != nil {
		if last >= 0 {
			next.Stream.Phases = retail(next, next.Stream.Phases, last)
		} else {
			next.Stream.Phases = loopPhases(next)
			next.Stream.Cur = 0
		}
	}
	*it = next
	return lines, nil
}
