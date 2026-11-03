package serve

import (
	"fmt"
	"io/fs"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

func (h *Handler) stopAfterStep(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		On     *bool       `json:"on"`
		Expect *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, false) || !h.fresh(w, r, it, "stop-after-step", body.Expect) {
		return
	}
	if body.On == nil {
		h.fail(w, 422, "on must be a boolean")
		return
	}
	handle, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	change := func(cur *factory.Item) error {
		if h.mode() != api.FactorySolo && cur.Person() != "" && cur.Person() != factory.NormalizeHandle(handle) {
			return fs.ErrPermission
		}
		if !factory.MayStopAfterStep(*cur) {
			return fmt.Errorf("job is not running or queued")
		}
		cur.StopAfterStep = *body.On
		return nil
	}
	err = h.recordAction(r, it, record.StopAfterStepChanged, fmt.Sprintf("stop_after_step=%t", *body.On), func() error {
		if body.Expect != nil && body.Expect.Version != nil {
			return h.config.Store.UpdateExpected(it.ID, *body.Expect.Version, change)
		}
		return h.config.Store.Update(it.ID, change)
	})
	h.finish(w, err)
}

// workerStopAfterStep reads only the choice for the caller's own live lease.
func (h *Handler) workerStopAfterStep(w http.ResponseWriter, r *http.Request) {
	if !h.workerReady(w) {
		return
	}
	held, err := h.workers.desk.ReadLease(r.PathValue("lease"), requestWorker(r).ID)
	if h.workerError(w, err) {
		return
	}
	it, err := h.workers.st.Get(held.Job)
	if h.workerError(w, err) {
		return
	}
	h.write(w, http.StatusOK, struct {
		On bool `json:"on"`
	}{it.StopAfterStep})
}
