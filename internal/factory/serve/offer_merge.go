package serve

import (
	"net/http"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// OfferMergeVerb is the job door that offers the person the merge of its open
// pull request again (factory's merge_offer.go): the job's manager presses it
// when the person asks to merge, or whether the job is ready to, and the job
// page brings its Merge card up anew from merge_offered_at.
const OfferMergeVerb = "offer_merge"

// offerMerge stamps the job's merge offer when the page offers Merge now, and
// refuses with the merge gate's own words otherwise. It merges nothing: the
// merge stays the person's press on the card.
func (h *Handler) offerMerge(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		Expect *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, true) || !h.fresh(w, r, it, OfferMergeVerb, body.Expect) || !h.owns(w, r, it) {
		return
	}
	if err := h.config.Store.Update(it.ID, func(cur *factory.Item) error {
		return factory.OfferMerge(cur, h.clock())
	}); err != nil {
		h.refuse(w, http.StatusConflict, err.Error(), "merge_refused", "", nil, factory.JobState(it))
		return
	}
	h.finish(w, nil)
}
