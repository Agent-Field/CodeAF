package serve

import (
	"net/http"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// followJob is POST /v1/jobs/{id}/follow: the caller follows the job, or stops.
// It is the caller's own switch, so it is written through
// [store.Store.UpdatePersonal]: the job's version and Changed stay as they
// were, nothing is recorded in the job's activity, and nobody's open page goes
// stale over it.
func (h *Handler) followJob(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body api.FollowRequest
	if !h.decode(w, r, &body, false) {
		return
	}
	if body.On == nil {
		h.fail(w, http.StatusUnprocessableEntity, "on is required: true to follow the job, false to stop")
		return
	}
	who, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	if factory.NormalizeHandle(who) == "" {
		h.fail(w, http.StatusUnprocessableEntity, "Following a job requires a GitHub login")
		return
	}
	h.finish(w, h.config.Store.UpdatePersonal(it.ID, func(cur *factory.Item) bool {
		return factory.SetFollowing(cur, who, *body.On)
	}))
}
