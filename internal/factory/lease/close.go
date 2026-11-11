package lease

import (
	"context"
	"fmt"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

type CloseRefusal string

func (e CloseRefusal) Error() string { return string(e) }

// Close revokes the lease and ends the job atomically, preserving its branch,
// chats, PR, steps and accounting. Old worker reports cannot resurrect it.
func (d *Doors) Close(id int, revision *int, who string) error {
	return d.updateAt(id, revision, false, func(doc *document, held *Lease, it *factory.Item, now time.Time) (bool, error) {
		if factory.JobState(*it) == "done" {
			return false, CloseRefusal(fmt.Sprintf("%s is already closed", it.Ref()))
		}
		if it.Merging != nil && it.Merging.State == factory.MergeMerging {
			return false, CloseRefusal("merge is already in progress; wait for GitHub's result before closing")
		}
		if it.PersonStopped == nil && !factory.StoppedByPerson(*it) {
			stopped := false
			it.PersonStopped = &stopped
		}
		if factory.UnderWay(*it) {
			if err := run.StopItem(it, now); err != nil {
				return false, err
			}
		}
		it.DoneHow, it.DoneAt = "closed_locally", now
		it.State, it.Marked, it.Manual = factory.StateNew, false, true
		it.Publishing, it.Approval, it.StopUndo, it.Recovery = false, nil, nil, nil
		it.Question, it.QKind, it.Asking = "", "", nil
		for i := range it.Thread {
			if it.Thread[i].State == "pending" {
				it.Thread[i].State = "cancelled"
			}
		}
		if it.Merging != nil && (it.Merging.State == factory.MergeAsked || it.Merging.State == factory.MergeQueued) {
			it.Merging = nil
		}
		if d.desk.record != nil {
			if _, err := d.desk.record.Append(context.Background(), record.Event{Kind: record.ClosedLocally, Job: id, Handle: who, Detail: "work and spending kept; GitHub unchanged"}); err != nil {
				return false, err
			}
		}
		if held != nil {
			release(doc, id)
		}
		it.Note(now, "Closed locally; work and spending kept. GitHub unchanged.")
		return held != nil, nil
	})
}

// RejectRefusal is the sentence Reject refuses a job with that waits on no
// approval: there is nothing for the person to reject.
const RejectRefusal = "is not waiting on an approval · Stop or Close end it instead"

// Reject is a person's rejection of what a job waits on them to approve: its
// run ends, and the job is closed as rejected with their words
// ([factory.Item.EndRejected]). It is Close's write with the decision on it,
// in the same lock, so an old worker report cannot bring the job back either.
//
// ANY APPROVAL, ANY STEP KIND. The job need only be waiting on a person's
// approval: an approve step, a start that asked first, or a step whose run
// paused for an approval somewhere else. Nothing here reads a pipeline's or a
// step's name.
func (d *Doors) Reject(id int, revision *int, who, words string) error {
	return d.updateAt(id, revision, func(doc *document, held *Lease, it *factory.Item, now time.Time) (bool, error) {
		if factory.JobState(*it) == "done" {
			return false, CloseRefusal(fmt.Sprintf("%s is already closed", it.Ref()))
		}
		if !factory.WaitsOnApproval(*it) {
			return false, CloseRefusal(it.Ref() + " " + RejectRefusal)
		}
		if factory.UnderWay(*it) {
			if err := run.StopItem(it, now); err != nil {
				return false, err
			}
		}
		it.EndRejected(who, words, now)
		if d.desk.record != nil {
			if _, err := d.desk.record.Append(context.Background(), record.Event{Kind: record.ClosedLocally, Job: id, Handle: who, Detail: factory.RejectedWords(words)}); err != nil {
				return false, err
			}
		}
		if held != nil {
			release(doc, id)
		}
		return held != nil, nil
	})
}
