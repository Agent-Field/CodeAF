package lease

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/publish"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

// Doors is the runner's doors on a factory that runs nothing itself
// ([factory.RunnerDoors] and [factory.HoldDoor]). A person's verb becomes a
// change of the job's state in the store and, while a worker holds the job, a
// [Signal] on its lease; the worker's own Runner carries the verb out and its
// writes come back as reports. Every process with the store can hang these
// doors, because they only write files under the desk's lock: no process owns
// the floor.
type Doors struct {
	desk *Desk
	read StartRead
}

// StartRead is one job's pipeline read with the factory's own rules in it, as
// a start decides under it: the same read intake and a person's Start make,
// of THE JOB'S OWN FILE (pipeline.JobFile: the one its first start decided,
// else, for a job never started, its repository's as this factory reads it
// now, its own copy first). An error wrapping [pipeline.ErrHouseRulesUnread]
// means the factory's rules could not be read; any other error, the
// repository's file.
type StartRead func(ctx context.Context, it factory.Item) (pipeline.File, error)

// NewDoors hangs the doors over desk's store.
func NewDoors(desk *Desk) *Doors { return &Doors{desk: desk} }

// WithStartRead makes every Launch read the job's repository first, so a job
// started by any door (a person's, the manager's, a new job's, intake's) runs
// the steps and carries the rules a person's Start would give it.
func (d *Doors) WithStartRead(read StartRead) *Doors {
	d.read = read
	return d
}

// update changes job id under the desk's lock: held is its live lease, nil
// when no worker holds it. change answers whether the desk itself changed.
func (d *Doors) update(id int, change func(doc *document, held *Lease, it *factory.Item, now time.Time) (bool, error)) error {
	return d.updateAt(id, nil, change)
}

// updateAt is update that writes only on the document revision named, when
// one is: the store compares it inside the item's flock, which is taken
// inside the desk's lock. The comparison and item mutation use the same
// document; a different revision is the store's StaleError and change never
// runs. The lease file is persisted separately after the item write.
func (d *Doors) updateAt(id int, revision *int, change func(doc *document, held *Lease, it *factory.Item, now time.Time) (bool, error)) error {
	return d.desk.change(func(doc *document, now time.Time) (bool, error) {
		held := find(doc, id)
		dirty := false
		write := func(it *factory.Item) error {
			var err error
			dirty, err = change(doc, held, it, now)
			return err
		}
		var err error
		if revision == nil {
			err = d.desk.store.Update(id, write)
		} else {
			err = d.desk.store.UpdateExpected(id, *revision, write)
		}
		return dirty && err == nil, err
	})
}

// Launch queues the job for a worker; a paused job goes on instead.
func (d *Doors) Launch(id int) error {
	it, err := d.desk.store.Get(id)
	if err != nil {
		return err
	}
	if run.StoredPaused(it) {
		return d.Hold(id, false)
	}
	// THE FACTORY'S OWN RULES GO WITH THE JOB ([factory.Item.House]), read
	// here, on the factory that queues it, because a runner on another machine
	// cannot read this store. A rules file that cannot be read refuses the
	// start: rules that silently vanished would let work begin that a person
	// said must not.
	house, err := d.desk.store.HouseRules()
	if err != nil {
		return err
	}
	// THE REPOSITORY IS READ AS A PERSON'S START READS IT: with the factory's
	// rules unread nothing starts; with only the repository's file unread the
	// job keeps the steps it has and is marked so its plan waits for a person.
	var file *pipeline.File
	var copied *factory.PipelineCopy
	unread, decide := false, false
	if d.read != nil {
		f, err := d.read(context.Background(), it)
		if errors.Is(err, pipeline.ErrHouseRulesUnread) {
			return err
		}
		if err == nil {
			file = &f
		}
		unread = err != nil
		copied, decide = f.Copy, true
	}
	return d.update(id, func(doc *document, held *Lease, it *factory.Item, now time.Time) (bool, error) {
		if held != nil {
			return false, run.AlreadyRunning(it.Ref())
		}
		if it.Publishing && it.State == factory.StateNeedsYou && it.QKind == factory.NeedsRunnerOffline {
			// Resume the approved publish without resetting its proof or
			// replacing the trailer recovered from the expired lease.
			it.State = factory.StateQueued
			it.Question, it.QKind, it.Failure = "", "", nil
			it.MarkPublishing("queued", "", "", now)
			return true, nil
		}
		// A JOB THAT NEVER RAN RUNS THE PIPELINE ITS FILE SAYS NOW, not the
		// one it was stamped with when it arrived; the run is built from
		// these steps just below.
		if file != nil {
			pipeline.StampSteps(it, *file)
		}
		// THE JOB'S PIPELINE FILE IS DECIDED AT ITS FIRST START, ONCE
		// ([factory.Item.DecidePipeline]): the factory's copy this read used
		// goes with the job ([factory.Item.PipelineCopy]), as the factory's
		// rules do, so a runner on another machine reads it from the job and
		// never from its own read of main; no copy decides main's file alone.
		// A job already decided read its own file above and keeps it, so a
		// copy saved or stopped since never reaches its steps, rules or
		// merge. Even a read of main that failed decides: the job runs
		// main's file, or the copy the read still used.
		if decide {
			it.DecidePipeline(copied, now)
		}
		// EVERY START CARRIES ITS START RULES ([factory.Item.StartRules]), so
		// the approval after the plan is checked whichever door started it.
		// One a door already stamped keeps them.
		if it.StartRules == nil {
			switch {
			case file != nil:
				it.StartRules = pipeline.StartRulesOf(*file)
			case unread:
				it.StartRules = &factory.StartRules{AskFirst: append([]string{}, house.AskFirst...), Never: append([]string{}, house.Never...), Unread: true}
				it.Note(now, "Started with the repository's rules unread; its plan will wait for you")
			default:
				it.StartRules = &factory.StartRules{AskFirst: append([]string{}, house.AskFirst...), Never: append([]string{}, house.Never...)}
			}
			it.RuleHold = ""
		}
		if err := run.QueueItem(it, now); err != nil {
			return false, err
		}
		it.House = &house
		_, stale := doc.Signals[id]
		delete(doc.Signals, id)
		return stale, nil
	})
}

// Stop puts the job back to new and takes its lease away: the worker learns
// it on its next heartbeat or report, which the server no longer accepts, and
// stops its runner. A dead worker needs to learn nothing.
func (d *Doors) Stop(id int) error { return d.stop(id, nil, true) }

// StopAs records the caller when the HTTP door has authenticated one.
// An unknown caller stays blank; only the local Stop door uses the job's person.
func (d *Doors) StopAs(id int, handle string) error { return d.stop(id, &handle, true) }

// Reset stops before reassignment or Drop without recording a second human action.
func (d *Doors) Reset(id int) error { return d.stop(id, nil, false) }

func (d *Doors) stop(id int, caller *string, receipt bool) error {
	history, err := record.Open(d.desk.store, record.Options{Now: d.desk.now})
	if err != nil {
		return err
	}
	return d.update(id, func(doc *document, held *Lease, it *factory.Item, now time.Time) (bool, error) {
		// A RESET OF A RUN THAT ALREADY ENDED CHANGES NOTHING: a failed or
		// stopped job is reassigned or dropped as it stands, and why it ended
		// stays. Only a person's Stop of it is refused, as not running.
		if receipt || !factory.RunEnded(*it) {
			if err := run.StopItem(it, now); err != nil {
				return false, err
			}
		}
		// THE PERSON'S STOP WINS: keep it durable while the job is locked,
		// before intake can see the job back in the backlog.
		if receipt {
			if _, err := history.Append(context.Background(), record.Event{Kind: record.Stopped, Job: id, Handle: stopHandle(caller, it.Person())}); err != nil {
				return false, err
			}
		}
		it.Publishing = false
		if held == nil {
			return false, nil
		}
		release(doc, id)
		return true, nil
	})
}

// Pause holds a running job, or lets a held one go on.
func (d *Doors) Pause(id int) error {
	it, err := d.desk.store.Get(id)
	if err != nil {
		return err
	}
	return d.Hold(id, !run.StoredPaused(it))
}

// Hold says on (pause) or off (go on). An ask already true changes nothing.
// A paused job nobody holds is queued again, and its worker resumes it. A
// queued job nobody holds yet is held where it is: no worker takes it until a
// person lets it go on.
func (d *Doors) Hold(id int, on bool) error {
	return d.update(id, func(doc *document, held *Lease, it *factory.Item, now time.Time) (bool, error) {
		paused := run.StoredPaused(*it)
		switch {
		case paused == on:
			return false, nil
		case held == nil && on && it.State == factory.StateQueued:
			return false, run.HoldQueued(it, now)
		case held == nil && on:
			return false, fmt.Errorf("%s is not running", it.Ref())
		case held == nil:
			return false, run.ResumeItem(it, now)
		}
		it.Stream.Paused = on
		signal(doc, id, Signal{Verb: SignalHold, Yes: on})
		return true, nil
	})
}

// Answer answers the question the job waits on. A job that asked before it
// starts is launched by a yes; a held job hears the answer through its lease;
// an answer to a job no worker holds is kept for the step's next round.
func (d *Doors) Answer(id int, yes bool, words string) error {
	it, err := d.desk.store.Get(id)
	if err != nil {
		return err
	}
	if it.Publishing && it.State == factory.StateNeedsYou && it.QKind == factory.NeedsRunnerOffline {
		if factory.OutOfAttemptsChoice(yes, words) == factory.ChoiceTryOnceMore {
			return d.Launch(id)
		}
		return d.Stop(id)
	}
	if it.State == factory.StateNeedsYou && it.QKind == factory.QKindAskFirst {
		if yes {
			return d.Launch(id)
		}
		return d.desk.store.Update(id, func(it *factory.Item) error { return run.DeclineAskFirst(it, words) })
	}
	return d.update(id, func(doc *document, held *Lease, it *factory.Item, now time.Time) (bool, error) {
		return answerLocked(doc, held, id, it, yes, words, now)
	})
}

// answerLocked is the answer's write, under the desk's lock and the item's:
// a job no worker holds is answered on the document, and a held job has its
// question taken down and hears the answer through its lease.
func answerLocked(doc *document, held *Lease, id int, it *factory.Item, yes bool, words string, now time.Time) (bool, error) {
	if yes {
		if err := factory.CheckAutomaticApproval(*it); err != nil {
			return false, err
		}
	}
	if held == nil {
		return false, run.AnswerUnheld(it, yes, words, now)
	}
	if err := run.AnswerHeld(it, yes, now, words); err != nil {
		return false, err
	}
	signal(doc, id, Signal{Verb: SignalAnswer, Yes: yes, Words: words})
	return true, nil
}

// RejectPlan sends a plan back with the person's words: the approve
// question's no, answered exactly as [Doors.Answer] answers it, but ONLY when
// the job's document is still the revision the person reviewed and is, at
// that revision, a plan with a finished step before it to go back to
// (run.PlanRevision). may is the caller's own permission, asked of the same
// locked document. Every check runs inside the desk's lock and the item's
// flock, before anything changes, so a worker or another person who moves the
// job first makes this a refusal, never a no to a question nobody reviewed.
// A validation refusal adds no item, revision or signal change of its own;
// normal lease expiry can still update the lease file.
//
// It answers the job as it stood before the no, a copy no later write can
// reach, so the caller's receipt names the question that was answered.
func (d *Doors) RejectPlan(id, revision int, words string, may func(factory.Item) error) (factory.Item, error) {
	var before factory.Item
	err := d.updateAt(id, &revision, func(doc *document, held *Lease, it *factory.Item, now time.Time) (bool, error) {
		if err := run.PlanRevision(*it); err != nil {
			return false, err
		}
		var err error
		if before, err = copyItem(*it); err != nil {
			return false, err
		}
		if may != nil {
			if err := may(before); err != nil {
				return false, err
			}
		}
		return answerLocked(doc, held, id, it, false, words, now)
	})
	if err != nil {
		return factory.Item{}, err
	}
	return before, nil
}

// copyItem is a copy of the item by its own document bytes: the change the
// store hands a door edits the run's steps in place, and a shallow copy would
// share them.
func copyItem(it factory.Item) (factory.Item, error) {
	data, err := json.Marshal(it)
	if err != nil {
		return factory.Item{}, err
	}
	var out factory.Item
	err = json.Unmarshal(data, &out)
	return out, err
}

// Steer hands words to the round running now, through the lease.
func (d *Doors) Steer(id int, words string) error {
	return d.update(id, func(doc *document, held *Lease, it *factory.Item, _ time.Time) (bool, error) {
		if held == nil {
			return false, fmt.Errorf("%s is not running", it.Ref())
		}
		signal(doc, id, Signal{Verb: SignalSteer, Words: words})
		return true, nil
	})
}

// SignOff is the person's approval at the proof gate, and APPROVING
// PUBLISHES. The approval must already be in the factory's record (the
// service writes it before it opens this door); its trailer rides to the
// worker as a publish signal: to the job's holder when a worker still holds
// it, else on the job queued again for the next pull. That worker commits,
// pushes and opens the pull request, and the job is done when it reports the
// pr. A job with no recorded approval publishes nothing.
func (d *Doors) SignOff(id int, edited bool) (bool, error) {
	trailer, err := d.trailer(id)
	if err != nil {
		return false, err
	}
	var repo string
	err = d.update(id, func(doc *document, held *Lease, it *factory.Item, now time.Time) (bool, error) {
		if it.Cap > 0 && factory.JobSpent(*it) >= it.Cap {
			return false, fmt.Errorf("job budget reached; raise Budget before publishing")
		}
		if err := factory.CheckAutomaticApproval(*it); err != nil {
			return false, err
		}
		if run.TrailerCommit(trailer) != it.HeadCommit {
			return false, publish.ErrCommitChanged
		}
		if it.PublishStatus != nil && it.PublishStatus.State == "failed" {
			it.State = factory.StateQueued
			it.PublishError = ""
			it.MarkPublishing("queued", "", "", now)
		} else if err := run.PublishItem(it, edited, now); err != nil {
			return false, err
		}
		if held != nil {
			// The holder is finishing its run; its done releases the lease
			// and keeps this signal, which queues the job again.
			it.State = factory.StateLanded
		}
		if it.FollowUp == nil {
			it.FollowUp = &factory.PRFollowUp{Kind: "pr", Limit: factory.DefaultPRRepairAttempts}
		}
		it.FollowUp.AutoPush = true
		it.FollowUp.PublishConsent = trailer
		it.Publishing = true
		repo = it.Repo
		signal(doc, id, Signal{Verb: SignalPublish, Words: trailer})
		return true, nil
	})
	if err != nil || edited {
		return false, err
	}
	return run.HabitDue(d.desk.store, repo), nil
}

// Merge sends a merge the factory's gate passed to the runner that carries
// it out: m, queued, goes on the job, and its exact commit rides beside the
// lease as a merge signal, to the job's holder when a worker still holds it
// (its done keeps the signal and queues the job again), else on the job
// queued for the next pull. That worker reads the pull request again, runs
// the same gate, and merges only that commit.
func (d *Doors) Merge(id int, m factory.MergeRun) error {
	if m.Commit == "" {
		return errors.New("a merge names the exact commit to merge")
	}
	return d.update(id, func(doc *document, held *Lease, it *factory.Item, now time.Time) (bool, error) {
		if it.State != factory.StateShipped {
			return false, fmt.Errorf("%s has not opened its pull request", it.Ref())
		}
		m.State = factory.MergeQueued
		m.UndoUntil = time.Time{}
		if m.At.IsZero() {
			m.At = now
		}
		cp := m
		it.Merging = &cp
		if held == nil {
			it.State = factory.StateQueued
		}
		signal(doc, id, Signal{Verb: SignalMerge, Words: m.Commit})
		return true, nil
	})
}

// trailer is the job's recorded approval as its trailer line, read from the
// record beside the desk's store: the one consent a publish may cite.
func (d *Doors) trailer(id int) (string, error) {
	history, err := record.Open(d.desk.store, record.Options{})
	if err != nil {
		return "", err
	}
	trailer, err := history.Trailer(context.Background(), id)
	if err != nil {
		return "", err
	}
	if !run.ValidTrailer(trailer) {
		return "", run.ErrNoApproval
	}
	return trailer, nil
}

// SendBack queues a landed job again with one more step, prove.
func (d *Doors) SendBack(id int, words string) error {
	return d.update(id, func(_ *document, _ *Lease, it *factory.Item, now time.Time) (bool, error) {
		return false, run.SendBackItem(it, words, now)
	})
}

// Reverify queues a landed job with a recheck signal: the worker that pulls
// it runs its checks again rather than its steps.
func (d *Doors) Reverify(id int) error {
	return d.update(id, func(doc *document, _ *Lease, it *factory.Item, _ time.Time) (bool, error) {
		if err := run.RecheckItem(it); err != nil {
			return false, err
		}
		signal(doc, id, Signal{Verb: SignalRecheck})
		return true, nil
	})
}

func stopHandle(caller *string, person string) string {
	if caller != nil {
		return *caller
	}
	return person
}

// AnswerPending commits an exact manager decision before signaling a held worker.
// A nonempty receipt with an error means the answer WAS saved, but delivery needs
// another attempt. Retrying the same answer confirms it without answering a newer ask.
func (d *Doors) AnswerPending(id int, manager, questionID, words string) (factory.ManagerAnswer, error) {
	var saved factory.ManagerAnswer
	words = strings.TrimSpace(words)
	err := d.desk.change(func(doc *document, now time.Time) (bool, error) {
		held := find(doc, id)
		dirty := false
		var receipt factory.ManagerAnswer
		err := d.desk.store.Update(id, func(it *factory.Item) error {
			if manager == "" || it.Talk != manager {
				return errors.New("the job's manager changed; read factory_status from its current conversation")
			}
			if questionID == "" || words == "" {
				return errors.New("an exact question identity and answer are required")
			}
			if old := it.ManagerAnswer; old != nil && old.QuestionID == questionID {
				if old.Manager != manager || old.Words != words {
					return errors.New("this decision already has a different saved answer")
				}
				receipt = *old
				// Confirmation is safe even if a newer question exists; only the same
				// still-running attempt with no pending decision may receive a redelivery.
			} else {
				if factory.PendingQuestionID(*it) != questionID {
					return errors.New("the pending step question changed; read factory_status before answering again")
				}
				if it.Manual || it.Publishing {
					return errors.New("manual control or publication owns this job")
				}
				if it.Stream == nil {
					return errors.New("the pending step has no saved run to resume")
				}
				receipt = factory.ManagerAnswer{QuestionID: questionID, Manager: manager, Words: words, Stage: it.Asking.Stage, Phase: it.Stream.Cur, At: now}
				if receipt.Phase >= 0 && receipt.Phase < len(it.Stream.Phases) {
					p := it.Stream.Phases[receipt.Phase]
					receipt.Round, receipt.Chat = p.Round, p.Chat
				}
				note := fmt.Sprintf("%s asked: %s · the person answered: %s", it.Asking.Stage, it.Asking.Question, words)
				if err := run.KeepAnswer(it, false, words, now); err != nil {
					return err
				}
				it.Note(now, note)
				it.ManagerAnswer = &receipt
			}
			if held != nil && !it.Manual && !it.Publishing && it.Question == "" && it.Asking == nil && it.Stream != nil && it.Stream.Cur == receipt.Phase && receipt.Phase >= 0 && receipt.Phase < len(it.Stream.Phases) {
				p := it.Stream.Phases[receipt.Phase]
				if p.Round == receipt.Round && p.Chat == receipt.Chat && (it.State == factory.StateRunning || it.State == factory.StateQueued) {
					it.State = factory.StateRunning
					exists := false
					for _, s := range doc.Signals[id] {
						if s.Verb == SignalAnswer && s.ID == questionID {
							exists = true
						}
					}
					if !exists {
						signal(doc, id, Signal{Verb: SignalAnswer, ID: questionID, Words: words})
						dirty = true
					}
				}
			}
			return nil
		})
		if err != nil {
			return false, err
		}
		saved = receipt // Only after the item file has successfully persisted.
		return dirty, nil
	})
	return saved, err
}
