package lease

import (
	"context"
	"errors"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

// ErrAgentFieldMoved refuses an answer to an AgentField pause the job no
// longer waits on: the run moved on (answered elsewhere, re-planned, ended)
// between the person reading the question and answering it.
var ErrAgentFieldMoved = errors.New("the run is no longer waiting for this answer")

// AnswerAgentField is a person's answer to the AgentField pause the job
// waits on, taken by the factory that holds the job.
//
// THE FACTORY KEEPS THE DECISION AND THE RUNNER CARRIES IT. The decision is
// written on the job's run record with who gave it, the question comes down at
// once, and the runner holding the job hears it as [SignalAgentField] on its
// next heartbeat and sends it to the control plane as the run's owner. A job
// no runner holds keeps the decision all the same: the runner that next takes
// the job reads the control plane's pause again and sends any decision it
// still owes (re-arm), so a restart anywhere loses no answer. request is the
// pause the person read; a different one now is [ErrAgentFieldMoved].
//
// A REJECT ENDS THE JOB AS THE PERSON'S STOP, AFTER THE RUNNER CARRIED IT: a
// stop lets go of the lease, and with it every signal not yet collected, so a
// held job stops only when its runner acknowledges the reject (drain). A job
// no runner holds stops at once; nothing could carry the reject until a
// runner came back, and a stopped job is not taken again, so the control
// plane's pause then runs out on its own.
func (d *Doors) AnswerAgentField(id int, request string, decision factory.AgentFieldDecision) error {
	history, err := record.Open(d.desk.store, record.Options{Now: d.desk.now})
	if err != nil {
		return err
	}
	return d.update(id, func(doc *document, held *Lease, it *factory.Item, now time.Time) (bool, error) {
		w := factory.AgentFieldWaiting(*it)
		if !factory.AgentFieldPause(*it) || w == nil || w.RequestID != request {
			return false, ErrAgentFieldMoved
		}
		if decision.At.IsZero() {
			decision.At = now
		}
		decision.RequestID, decision.Execution = w.RequestID, w.Execution
		for _, r := range agentFieldRuns(it) {
			if r.Waiting == nil || r.Waiting.Execution != w.Execution {
				continue
			}
			r.Decisions = append(r.Decisions, decision)
			r.Waiting = nil
		}
		run.SyncAgentFieldWait(it, now)
		if held == nil {
			if decision.Decision == factory.AgentFieldRejected {
				return endRejectedLocked(history, doc, nil, id, it, now, decision.By, decision.Words)
			}
			return true, nil
		}
		signal(doc, id, Signal{Verb: SignalAgentField, ID: decision.RequestID, Execution: decision.Execution,
			Decision: decision.Decision, Words: decision.Words, By: decision.By})
		return true, nil
	})
}

// agentFieldRuns are the job's run records: its own run, and each
// agentfield step's.
func agentFieldRuns(it *factory.Item) []*factory.AgentFieldRun {
	var out []*factory.AgentFieldRun
	if it.AgentField != nil {
		out = append(out, it.AgentField)
	}
	for _, r := range it.AgentFieldSteps {
		if r != nil {
			out = append(out, r)
		}
	}
	return out
}

// endRejectedAfterCarried ends a held job whose runner has just acknowledged
// carrying a person's Reject of its run's pause ([Doors.AnswerAgentField]):
// done, rejected with their words. Callers hold the desk's lock.
func (d *Desk) endRejectedAfterCarried(doc *document, held *Lease, by, words string) error {
	history, err := record.Open(d.store, record.Options{Now: d.now})
	if err != nil {
		return err
	}
	job := held.Job
	return d.store.Update(job, func(it *factory.Item) error {
		if it.Rejection != nil {
			return nil
		}
		_, err := endRejectedLocked(history, doc, held, job, it, d.now(), by, words)
		return err
	})
}

// endRejectedLocked is a rejection's write, under the desk's lock and the
// item's: the run stops, the job ends rejected with the person's words
// ([factory.Item.EndRejected]), and the runner holding it lets go. A REJECTED
// JOB IS DONE, never "Stopped by you" offering to carry on: the person's no
// is a decision ([Doors.Reject] is the same write for a job's own approval).
func endRejectedLocked(history *record.Record, doc *document, held *Lease, id int, it *factory.Item, now time.Time, by, words string) (bool, error) {
	if factory.UnderWay(*it) {
		if err := run.StopItem(it, now); err != nil {
			return false, err
		}
	}
	it.EndRejected(by, words, now)
	if _, err := history.Append(context.Background(), record.Event{Kind: record.ClosedLocally, Job: id, Handle: by, Detail: factory.RejectedWords(words)}); err != nil {
		return false, err
	}
	_, pendingSignals := doc.Signals[id]
	delete(doc.Signals, id)
	if held == nil {
		return pendingSignals, nil
	}
	release(doc, id)
	return true, nil
}
