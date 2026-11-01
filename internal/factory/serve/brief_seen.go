package serve

import (
	"errors"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// briefSeen is POST /v1/jobs/{id}/brief_seen: the caller saw the job's Brief
// at a revision. It is the caller's own fact, so it is written through
// [store.Store.UpdatePersonal]: the job's version and Changed stay as they
// were, nothing is recorded in the job's activity, and nobody's open page goes
// stale over it. A lower revision than the one kept changes nothing
// ([factory.SeeBrief]).
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
	err = h.config.Store.UpdatePersonal(it.ID, func(cur *factory.Item) error {
		if !factory.SeeBrief(cur, who, body.Rev) {
			return errPersonalUnchanged
		}
		return nil
	})
	if errors.Is(err, errPersonalUnchanged) {
		err = nil
	}
	h.finish(w, err)
}

// errPersonalUnchanged ends a personal write that would change nothing, so a
// page that reports the same revision on every read does not rewrite the job's
// document (and wake every watcher of the factory's folder) each time.
var errPersonalUnchanged = errors.New("nothing to write")
