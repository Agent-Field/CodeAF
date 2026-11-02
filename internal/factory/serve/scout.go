package serve

import (
	"errors"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/reporead"
	"github.com/Agent-Field/codeaf/internal/factory/scout"
)

func (h *Handler) scoutRoutes() {
	h.postJob("scout", h.config.Scout != nil, h.scoutJob)
}

// scoutJob answers POST /v1/jobs/{id}/scout {refresh?}: Look closer on one
// job anyone who can see it may ask for. The kept look of the same text at
// the same head is answered free; refresh asks for a new one. Limits refuse
// in the page's own words.
func (h *Handler) scoutJob(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		Refresh bool `json:"refresh,omitempty"`
	}
	if !h.decode(w, r, &body, true) {
		return
	}
	s, cached, err := h.config.Scout(r.Context(), it.ID, body.Refresh)
	switch {
	case errors.Is(err, scout.ErrMonthLimit):
		h.refuse(w, http.StatusTooManyRequests, "Monthly limit for AI reached", api.ReasonLimit, "", nil, "")
	case errors.Is(err, scout.ErrDayLimit):
		h.refuse(w, http.StatusTooManyRequests, "Daily limit for this repo reached", api.ReasonLimit, "", nil, "")
	case errors.Is(err, scout.ErrNoAnswer):
		h.refuse(w, http.StatusBadGateway, "The look found no answer within its budget", api.ReasonNoAnswer, "", nil, "")
	case errors.Is(err, reporead.ErrNotCloned):
		h.refuse(w, http.StatusConflict, it.Repo+" has no git checkout on the factory's machine", api.ReasonNotCloned, "", nil, "")
	case err != nil:
		h.fail(w, http.StatusBadGateway, "the look failed: "+err.Error())
	default:
		it.Scout = &s
		h.write(w, http.StatusOK, api.ScoutLook{Factory: h.factory, Job: it.ID, Cached: cached, Scout: *api.FromItem(it).Scout})
	}
}
