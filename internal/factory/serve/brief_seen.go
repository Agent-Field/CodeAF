package serve

import (
	"net/http"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// briefSeen is POST /v1/jobs/{id}/brief_seen: the caller saw the job's Brief
// at a revision. It is the caller's own fact, so it is written through
// [store.Store.UpdatePersonal]: the job's version and Changed stay as they
// were, nothing is recorded in the job's activity, and nobody's open page goes
// stale over it. A revision no higher than the one kept changes nothing
// and writes nothing ([factory.SeeBrief]).
func (h *Handler) briefSeen(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body api.BriefSeenRequest
	if !h.decode(w, r, &body, false) {
		return
	}
	if body.Rev < 1 {
		h.fail(w, http.StatusUnprocessableEntity, "rev must be the Brief revision you saw, a whole number from 1")
		return
	}
	who, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	if factory.NormalizeHandle(who) == "" {
		h.fail(w, http.StatusUnprocessableEntity, "Marking the brief seen requires a GitHub login")
		return
	}
	h.finish(w, h.config.Store.UpdatePersonal(it.ID, func(cur *factory.Item) bool {
		return factory.SeeBrief(cur, who, body.Rev)
	}))
}
