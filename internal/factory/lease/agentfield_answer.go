package lease

import (
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
				by := decision.By
				return stopLocked(history, doc, nil, id, it, now, &by, true)
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

// stopAfterReject ends a held job whose runner has just acknowledged carrying
// a person's Reject of its run's pause ([Doors.AnswerAgentField]), as that
// person's Stop. Callers hold the desk's lock.
func (d *Desk) stopAfterReject(doc *document, held *Lease, by string) error {
	history, err := record.Open(d.store, record.Options{Now: d.now})
	if err != nil {
		return err
	}
	job := held.Job
	return d.store.Update(job, func(it *factory.Item) error {
		if factory.StoppedByPerson(*it) {
			return nil
		}
		_, err := stopLocked(history, doc, held, job, it, d.now(), &by, true)
		return err
	})
}
