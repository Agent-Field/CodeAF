package run

import (
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
		for i := index; i < len(s.Phases); i++ {
			p := s.Phases[i]
			s.Phases[i] = factory.Phase{Name: p.Name, Kind: p.Kind, Note: p.Note, State: factory.PhasePending}
		}
		// The queued target is marked begun so a worker revives this stream,
		// including a rerun from the first step, instead of erasing its spend.
		s.Phases[index].State = factory.PhaseRunning
		it.State = factory.StateQueued
		it.Failure = nil
		it.Proof, it.Policy = nil, nil
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
