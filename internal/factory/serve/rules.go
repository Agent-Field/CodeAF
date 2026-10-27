package serve

import (
	"errors"
	"net/http"
	"reflect"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// errRulesMoved refuses a write whose reader's copy is no longer what the
// factory keeps.
var errRulesMoved = errors.New("the factory's rules changed since they were read")

// houseRulesBody is the whole PUT /v1/rules body. EVERY LIST IS REQUIRED, so a
// client that meant to change one list and sent only that one cannot wipe the
// other two by leaving them out; an empty list is said with `[]`.
type houseRulesBody struct {
	Rules    *[]string `json:"rules"`
	AskFirst *[]string `json:"ask_first"`
	Never    *[]string `json:"never"`
	// Adapt is optional: omitted keeps the factory's word as it is, and ""
	// says the factory has none.
	Adapt *factory.AdaptMode `json:"adapt"`
	// StartOnItsOwn and StartLabels are optional too: omitted keeps what the
	// factory has.
	StartOnItsOwn *bool     `json:"start_on_its_own"`
	StartLabels   *[]string `json:"start_labels"`
	// Expect is the rules as the writer last read them. When it is sent and
	// the factory keeps anything else, NOTHING IS WRITTEN: two views that
	// each read the rules cannot quietly undo each other's change.
	Expect *factory.HouseRules `json:"expect"`
}

// houseRules answers and replaces the factory's own rules
// ([factory.HouseRules]): GET for anyone who may read this factory, PUT for
// its operator only, because a rule binds every repository and every person's
// work on it. On a solo factory the one person is its operator.
//
// A PUT replaces all three lists at once and answers what was saved, cleaned
// (trimmed, repeats dropped). One that sends `expect` is refused with 409 when
// the rules changed after the writer read them. Every refusal before the save carries the phase
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
	next := factory.HouseRules{Rules: *body.Rules, AskFirst: *body.AskFirst, Never: *body.Never}
	if body.Adapt != nil {
		next.Adapt = *body.Adapt
	}
	if body.StartOnItsOwn != nil {
		next.StartOnItsOwn = *body.StartOnItsOwn
	}
	if body.StartLabels != nil {
		next.StartLabels = *body.StartLabels
	}
	if _, err := next.Clean(); err != nil {
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	var expect *factory.HouseRules
	if body.Expect != nil {
		clean, err := body.Expect.Clean()
		if err != nil {
			h.fail(w, http.StatusUnprocessableEntity, "expect: "+err.Error())
			return
		}
		expect = &clean
	}
	clearPhase(w)
	saved, err := h.config.Store.ChangeHouseRules(func(cur *factory.HouseRules) error {
		if expect != nil && !reflect.DeepEqual(*expect, *cur) {
			return errRulesMoved
		}
		was := *cur
		*cur = next
		if body.Adapt == nil {
			cur.Adapt = was.Adapt
		}
		if body.StartOnItsOwn == nil {
			cur.StartOnItsOwn = was.StartOnItsOwn
		}
		if body.StartLabels == nil {
			cur.StartLabels = was.StartLabels
		}
		return nil
	})
	if errors.Is(err, errRulesMoved) {
		// Refused before anything was written: again a proven no-write.
		w.Header().Set(api.PhaseHeader, api.PhasePreMutation)
		h.fail(w, http.StatusConflict, errRulesMoved.Error()+"; read them again, then make the change")
		return
	}
	if h.failed(w, err) {
		return
	}
	h.write(w, 200, saved)
}
