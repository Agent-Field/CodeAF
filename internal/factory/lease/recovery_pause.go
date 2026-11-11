package lease

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

// PauseRecoveryExpected dismisses the decision shown at this document revision.
// The old lease is fenced before the pause becomes visible to another reader.
func (d *Doors) PauseRecoveryExpected(id, revision int, person string) error {
	return d.updateAt(id, &revision, func(doc *document, held *Lease, it *factory.Item, _ time.Time) (bool, error) {
		if it.Person() != person {
			return false, run.StepControlRefusal("The job's owner changed. Read the job before trying again.")
		}
		if !factory.CanPauseDecision(*it) || factory.RecoveryPaused(*it) {
			return false, run.StepControlRefusal("This decision is no longer waiting to be paused.")
		}
		if held != nil || len(doc.Signals[id]) != 0 {
			if held != nil {
				withdraw(doc, *held)
			} else {
				delete(doc.Signals, id)
			}
			if err := d.desk.write(*doc); err != nil {
				return false, err
			}
		}
		factory.PauseDecision(it, revision)
		return false, nil
	})
}

// RetryPausedRecovery resumes a dismissed decision from the job's manager
// chat. An identical client retry observes its saved receipt.
func (d *Doors) RetryPausedRecovery(id int, manager, questionID, words, clientID string) error {
	if strings.TrimSpace(clientID) == "" {
		return errors.New("Retry requires a message ID.")
	}
	var step string
	alreadySaved := errors.New("the retry was already saved")
	err := d.update(id, func(_ *document, held *Lease, it *factory.Item, now time.Time) (bool, error) {
		if manager == "" || it.Talk != manager {
			return false, errors.New("The job's manager changed. Open its current chat.")
		}
		if it.LastRecoveryResume == clientID {
			return false, alreadySaved
		}
		if questionID == "" || it.PausedRecoveryQuestion != questionID || !factory.RecoveryPaused(*it) {
			return false, run.StepControlRefusal("The paused decision changed. Read the job before trying again.")
		}
		if held != nil && !held.ThreadOnly {
			return false, ErrLeased
		}
		switches, err := d.desk.store.RepoSwitch()
		if err != nil {
			return false, err
		}
		if repo := factory.RepoOf(*it); switches.Off(repo) {
			return false, errors.New(factory.RepoOffWords(repo))
		}
		if factory.PausedDecision(*it) {
			it.PausedRecoveryQuestion = ""
			resumeDecisionWork(it, now)
			if strings.TrimSpace(words) != "" {
				it.Notes = append(it.Notes, words)
			}
			it.LastRecoveryResume = clientID
			return false, nil
		}
		if !factory.CanPauseRecovery(*it) {
			return false, run.StepControlRefusal("The paused decision changed. Read the job before trying again.")
		}
		step = it.Stream.Phases[it.Stream.Cur].Name
		if err := run.CheckWriteAccess(*it, step); err != nil {
			return false, err
		}
		it.PausedRecoveryQuestion = ""
		if err := run.StepControl(it, step, true, words, now); err != nil {
			return false, err
		}
		it.LastRecoveryResume = clientID
		return false, nil
	})
	if err == alreadySaved {
		return nil
	}
	if err == nil && d.desk.record != nil {
		kind := record.Resumed
		if step != "" {
			kind = record.StepRerun
		}
		_, _ = d.desk.record.Append(context.Background(), record.Event{Kind: kind, Job: id, Detail: step})
	}
	return err
}

// A waiting gate keeps its question and consent. Interrupted work can take a
// new lease for its saved round if the checkout is still the one it inspected.
func resumeDecisionWork(it *factory.Item, now time.Time) {
	if it.QKind == "cap" && it.Cap > factory.JobLifetimeSpent(*it) {
		run.ReleaseCap(it, now)
		return
	}
	if it.Question != "" || it.Asking != nil {
		return
	}
	f, pr := it.FollowUp, it.PullRequest
	if f != nil && pr != nil && (f.State == "running" || f.State == "queued") && f.HeadSHA != "" && pr.HeadSHA != "" && f.HeadSHA != pr.HeadSHA {
		// The normal watcher will choose work from fresh PR observations.
		f.Kind, f.State, f.EventKey, f.Readiness = "pr", "watching", "", nil
		f.Reason = "The PR head changed while paused. Watching the current head."
		it.State = factory.StateShipped
		if s := it.Stream; s != nil && s.Cur >= 0 && s.Cur < len(s.Phases) && s.Phases[s.Cur].Name == factory.FollowPRStep {
			s.Phases[s.Cur].State, s.Phases[s.Cur].Note = factory.PhaseWaiting, f.Reason
			s.Ended = now
		}
		return
	}
	if it.State == factory.StateRunning || (it.State == factory.StateShipped && f != nil && f.State == "running") {
		it.State = factory.StateQueued
	}
}
