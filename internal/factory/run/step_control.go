package run

import (
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// StepControlRefusal identifies a state conflict without changing its person-facing words.
type StepControlRefusal string

func (e StepControlRefusal) Error() string { return string(e) }

// StepControl changes only a recorded run. A rerun keeps completed earlier
// steps and the money already spent; publishing and final jobs cannot rewind.
// words, on a rerun, are the person's for it: kept on the item's notes, which
// every step's brief carries, so trying again or sending work back says what to
// do differently. A stop takes none.
func StepControl(it *factory.Item, name string, rerun bool, words string, now time.Time) error {
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
	failedClaim := it.Failure != nil && it.Failure.Step == name && it.Failure.Next == factory.FailureStopped
	if rerun && s.Phases[index].State == factory.PhasePending && !failedClaim {
		return StepControlRefusal("step has not run yet")
	}
	if rerun && it.Cap > 0 && factory.JobSpent(*it) >= it.Cap {
		return StepControlRefusal("job budget reached; raise Budget before trying again")
	}
	if rerun {
		it.Recovery = nil
		previous := s.Phases[index]
		for i := index; i < len(s.Phases); i++ {
			p := s.Phases[i]
			s.Phases[i] = factory.Phase{Name: p.Name, Kind: p.Kind, Note: p.Note, State: factory.PhasePending}
		}
		// A fresh round retains this step's history and identity. Its nonzero
		// round marks the stream begun, even when retrying the first step, so
		// worker admission revives it without erasing spend or earlier results.
		target := &s.Phases[index]
		target.Round = max(previous.Round+1, 1)
		target.Rounds = append([]factory.RoundLine(nil), previous.Rounds...)
		if planReportPending(name, previous.Result) && previous.Chat != "" {
			target.Chat = previous.Chat
			target.State = factory.PhaseRunning
			target.Carry = append([]string(nil), previous.Carry...)
		}
		if previous.Failure != nil && previous.Round > 0 {
			factory.NoteRound(target, factory.RoundLinesMost, factory.RoundLine{Round: previous.Round, Summary: previous.Failure.Detail, Rejected: true, At: previous.Failure.At, Started: previous.Started})
		}
		it.State = factory.StateQueued
		it.Failure = nil
		it.Proof, it.Policy = nil, nil
		it.PublishError = ""
		it.PublishStatus = nil
		if it.FollowUp != nil {
			f := *it.FollowUp
			f.State, f.Reason = "queued", "Waiting for a runner to retry "+name
			it.FollowUp = &f
		}
		s.Ended = time.Time{}
	} else {
		it.State = factory.StateNew
		failure := stoppedByYou(name, s.Phases[index].Round, now)
		it.Failure = failure
		s.Phases[index].State, s.Phases[index].Failure, s.Phases[index].At = factory.PhaseFailed, failure, now
		s.Ended = now
	}
	s.Cur, s.Paused = index, false
	it.Question, it.QKind, it.Asking = "", "", nil
	it.Approval, it.StopUndo = nil, nil
	said := map[bool]string{true: "Rerun from " + name, false: "Stopped step " + name}[rerun]
	if words = strings.TrimSpace(words); rerun && words != "" {
		it.Notes = append(it.Notes, words)
		said += ": " + oneLine(words)
	}
	loopSay(it, now, "said", said)
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
