package serve

import (
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"net/http"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

// Close ends local work without deleting anything or changing GitHub.
func (h *Handler) closeJob(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		Expect *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, true) || !h.fresh(w, r, it, "close", body.Expect) || !h.owns(w, r, it) || !h.workerReady(w) {
		return
	}
	var revision *int
	if body.Expect != nil {
		revision = body.Expect.Version
	}
	who, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	h.finish(w, lease.NewDoors(h.workers.desk).Close(it.ID, revision, who))
}

// Reopen preserves the prior run and never launches or clears accounting.
func (h *Handler) reopenJob(w http.ResponseWriter, r *http.Request, it factory.Item) {
	if it.DoneHow != "closed_locally" {
		h.refuse(w, 409, "only a locally closed job can be reopened", "conflict", "", nil, factory.JobState(it))
		return
	}
	h.finish(w, h.recordAction(r, it, record.Restored, "reopened; work and spending kept", func() error {
		return h.config.Store.Update(it.ID, func(cur *factory.Item) error {
			if cur.DoneHow != "closed_locally" {
				return lease.CloseRefusal("job is no longer closed locally")
			}
			cur.DoneHow, cur.DoneAt, cur.Rejection = "", time.Time{}, nil
			cur.Manual = cur.Stream != nil
			cur.Note(h.clock(), "Reopened; work and spending kept. Resume the pipeline to continue.")
			return nil
		})
	}))
}

// rejectJob is POST /v1/jobs/{id}/reject: the person's rejection of what the
// job waits on them to approve, with their words. The job ends, closed here as
// rejected ([lease.Doors.Reject]); it is the answer that a job's approval
// offers beside Approve and Ask for changes, for an approval of any kind.
func (h *Handler) rejectJob(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body api.Reject
	if !h.decode(w, r, &body, true) || !h.fresh(w, r, it, "reject", body.Expect) || !h.approves(w, r, it) || !h.workerReady(w) {
		return
	}
	var revision *int
	if body.Expect != nil {
		revision = body.Expect.Version
	}
	who, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	h.finish(w, lease.NewDoors(h.workers.desk).Reject(it.ID, revision, who, body.Words))
}
