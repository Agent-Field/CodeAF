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
	return d.updateAt(id, revision, func(doc *document, held *Lease, it *factory.Item, now time.Time) (bool, error) {
		if factory.JobState(*it) == "done" {
			return false, CloseRefusal(fmt.Sprintf("%s is already closed", it.Ref()))
		}
		if it.Merging != nil && it.Merging.State == factory.MergeMerging {
			return false, CloseRefusal("merge is already in progress; wait for GitHub's result before closing")
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
