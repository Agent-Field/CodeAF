package serve

import (
	"net/http"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// houseRulesBody is the whole PUT /v1/rules body. EVERY LIST IS REQUIRED, so a
// client that meant to change one list and sent only that one cannot wipe the
// other two by leaving them out; an empty list is said with `[]`.
type houseRulesBody struct {
	Rules    *[]string `json:"rules"`
	AskFirst *[]string `json:"ask_first"`
	Never    *[]string `json:"never"`
}

// houseRules answers and replaces the factory's own rules
// ([factory.HouseRules]): GET for anyone who may read this factory, PUT for
// its operator only, because a rule binds every repository and every person's
// work on it. On a solo factory the one person is its operator.
//
// A PUT replaces all three lists at once and answers what was saved, cleaned
// (trimmed, repeats dropped). Every refusal before the save carries the phase
// marker (phase.go), so a client knows it changed nothing.
func (h *Handler) houseRules(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.config.Store != nil) {
		return
	}
	if r.Method == http.MethodGet {
		rules, err := h.config.Store.HouseRules()
		if h.failed(w, err) {
			return
		}
		h.write(w, 200, rules)
		return
	}
	if !h.requireOperator(w, r) {
		return
	}
	var body houseRulesBody
	if !h.decode(w, r, &body, false) {
		return
	}
	if body.Rules == nil || body.AskFirst == nil || body.Never == nil {
		h.fail(w, http.StatusUnprocessableEntity, "rules, ask_first and never are all required; send [] for an empty list")
		return
	}
	next, err := factory.HouseRules{Rules: *body.Rules, AskFirst: *body.AskFirst, Never: *body.Never}.Clean()
	if err != nil {
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	clearPhase(w)
	saved, err := h.config.Store.ChangeHouseRules(func(cur *factory.HouseRules) error {
		*cur = next
		return nil
	})
	if h.failed(w, err) {
		return
	}
	h.write(w, 200, saved)
}
