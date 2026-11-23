package serve

import (
	"net/http"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// spendAnswer never reaches the general answer door: its question and caller
// are checked against the revision held for the runner's actual write.
func (h *Handler) spendAnswer(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body api.SpendAnswer
	if !h.decode(w, r, &body, false) {
		return
	}
	if body.CarryOn == nil || body.Expect == nil || body.Expect.Version == nil || *body.Expect.Version < 1 {
		h.fail(w, http.StatusUnprocessableEntity, "carry_on and expect.version are required")
		return
	}
	if !h.fresh(w, r, it, "spend_answer", body.Expect) {
		return
	}
	caller, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	may := h.mayRevise(caller, body.Expect.Step)
	var refused error
	err = h.recordAnswerBy(r, *body.CarryOn, "", func() (factory.Item, error) {
		before, err := h.seam.SpendAnswer(it.ID, *body.Expect.Version, *body.CarryOn, may)
		refused = err
		return before, err
	})
	if refused != nil {
		h.planRefused(w, r, it, refused)
		return
	}
	h.finish(w, err)
}
