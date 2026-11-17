package serve

import (
	"errors"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// errSpendingMoved refuses a write whose reader's copy is no longer what the
// factory keeps.
var errSpendingMoved = errors.New("the factory's spending settings changed since they were read")

// spending answers and changes the factory's spending defaults
// ([factory.Spending]): GET for anyone who may read this factory, PATCH for
// its operator only, because a default binds every repository and every
// person's work on it. On a solo factory the one person is its operator.
//
// A PATCH changes only the fields it sends and answers what was saved. One
// that sends `expect` is refused with 409 when the values changed after the
// writer read them. Every refusal before the save carries the phase marker
// (phase.go), so a client knows it changed nothing.
func (h *Handler) spending(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.config.Store != nil) {
		return
	}
	if r.Method == http.MethodGet {
		saved, err := h.config.Store.Spending()
		if h.failed(w, err) {
			return
		}
		h.write(w, 200, saved)
		return
	}
	if !h.requireOperator(w, r) {
		return
	}
	var body api.SpendingPatch
	if !h.decode(w, r, &body, false) {
		return
	}
	if body.Empty() {
		h.fail(w, http.StatusUnprocessableEntity, "name at least one of default_budget_usd, auto_start_max_usd and default_model")
		return
	}
	// Every field the patch sends is checked before the lock: the values the
	// factory keeps already passed the same check.
	if err := checkSpendingPatch(body); err != nil {
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	clearPhase(w)
	saved, err := h.config.Store.ChangeSpending(func(cur *factory.Spending) error {
		if body.Expect != nil && !body.Expect.Same(*cur) {
			return errSpendingMoved
		}
		next, err := body.Apply(*cur)
		*cur = next
		return err
	})
	if errors.Is(err, errSpendingMoved) {
		// Refused before anything was written: again a proven no-write.
		w.Header().Set(api.PhaseHeader, api.PhasePreMutation)
		h.fail(w, http.StatusConflict, errSpendingMoved.Error()+"; read them again, then make the change")
		return
	}
	if h.failed(w, err) {
		return
	}
	h.write(w, 200, saved)
}

// checkSpendingPatch refuses a patch whose own fields the factory would not
// keep, whatever it keeps now.
func checkSpendingPatch(body api.SpendingPatch) error {
	next, err := body.Apply(factory.Spending{})
	if err != nil {
		return err
	}
	return next.Check()
}
