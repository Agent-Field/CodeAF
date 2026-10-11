package run

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// THE PERSON'S HALF OF THE DOORS, AS STORE CHANGES. A factory server owns the
// job's state but runs nothing: a worker that pulls the job runs it with its
// own Runner. When a person presses a verb on the server, these are the writes
// the server makes to the job itself, each the same write the Runner's door
// makes when it holds no run (internal/factory/lease's Doors calls them). The
// state machine stays in loop.go; these only stand a job where a worker's
// runner will pick it up, or where a person sees what they asked.

// QueueItem is a launch with no runner: the job's run is reset and it waits
// in the queue for a worker to pull it.
func QueueItem(it *factory.Item, now time.Time) error {
	if err := launchable(*it); err != nil {
		return err
	}
	if err := strangerWrites(*it); err != nil {
		return err
	}
	it.State = factory.StateQueued
	it.PublishStatus, it.Publishing = nil, false
	it.Question, it.QKind = "", ""
	it.Proof, it.Policy = nil, nil
	it.Stream = &factory.Stream{Started: now, Phases: loopPhases(*it)}
	loopSay(it, now, "said", "queued")
	return nil
}

// StopItem is a stop with no run to cancel here: the job is new again. A job
// that is not under way refuses, in the Runner's words.
func StopItem(it *factory.Item, now time.Time) error {
	switch it.State {
	case factory.StateQueued, factory.StateRunning, factory.StateNeedsYou:
		stopStored(it, now)
		return nil
	}
	return fmt.Errorf("%s is not running", it.Ref())
}

// StoredPaused says the job's document holds it paused.
func StoredPaused(it factory.Item) bool { return storedPaused(it) }

// ResumeItem takes a paused job that no worker holds off pause and puts it
// back in the queue; the worker that pulls it goes on at the step it stopped in.
func ResumeItem(it *factory.Item, now time.Time) error {
	if !storedPaused(*it) {
		return fmt.Errorf("%s is not paused", it.Ref())
	}
	it.Stream.Paused = false
	it.State = factory.StateQueued
	loopSay(it, now, "said", sayResumed)
	return nil
}

// AnswerHeld takes the question down from a job a worker holds: the worker's
// runner hears the answer through the lease and logs it. An approve step is
// passed (yes) or sent back to wait (no) here, because only a person's door
// moves it. Passing it stamps the step's finish, which the brief's clock reads.
func AnswerHeld(it *factory.Item, yes bool, now time.Time) error {
	if it.State != factory.StateNeedsYou || strings.TrimSpace(it.Question) == "" {
		return fmt.Errorf("%s is not waiting on you", it.Ref())
	}
	it.State = factory.StateRunning
	it.Question, it.QKind = "", ""
	it.Asking = nil
	s := it.Stream
	if s == nil || s.Cur < 0 || s.Cur >= len(s.Phases) || s.Phases[s.Cur].State != factory.PhaseWaiting {
		return nil
	}
	ph := &s.Phases[s.Cur]
	switch {
	case ph.Kind != factory.StageGate:
		setPhaseState(ph, factory.PhaseRunning, now)
	case yes:
		setPhaseState(ph, factory.PhaseDone, now)
	default:
		setPhaseState(ph, factory.PhasePending, now)
	}
	return nil
}

// KeepAnswer is an answer to a job no worker holds any more: it is kept for
// the step's next round, as the Runner keeps it, and the job is queued again.
func KeepAnswer(it *factory.Item, yes bool, words string, now time.Time) error {
	if it.Asking == nil || it.Asking.With != factory.AskedYou {
		return fmt.Errorf("%s is not waiting on you", it.Ref())
	}
	if err := keepStored(it, answeredWord(loopAnswer{yes: yes, words: strings.TrimSpace(words)}), now); err != nil {
		return err
	}
	if it.State == factory.StateRunning {
		it.State = factory.StateQueued
	}
	return nil
}

// DeclineAskFirst is a no to a job that asked before it starts.
func DeclineAskFirst(it *factory.Item, words string) error { return declineAskFirst(it, words) }

// SendBackItem queues a landed job again with one more step, prove.
func SendBackItem(it *factory.Item, words string, now time.Time) error {
	if words = strings.TrimSpace(words); words == "" {
		return errors.New("say what to prove")
	}
	_, err := sendBackStored(it, words, now)
	return err
}

// RecheckItem queues a landed job for a worker to run its checks again.
func RecheckItem(it *factory.Item) error {
	if it.State != factory.StateLanded || it.Stream == nil || len(it.Stream.Phases) == 0 {
		return fmt.Errorf("%s has not landed", it.Ref())
	}
	it.State = factory.StateQueued
	it.PublishStatus, it.Publishing = nil, false
	return nil
}
