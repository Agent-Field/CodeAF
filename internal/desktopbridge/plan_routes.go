package desktopbridge

import (
	"errors"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/session"
)

// The three answers a person can give a plan card. Each claims its row on the
// shared seam table; the engine's PlanBook owns the rules (idempotent Go,
// zero side effects on Cancel), so these handlers only find the conversation
// and carry the answer across.
func init() {
	registerSeamRoute(http.MethodPost, "/sessions/{id}/plan/{plan}/go", planGo)
	registerSeamRoute(http.MethodPost, "/sessions/{id}/plan/{plan}/edit", planEdit)
	registerSeamRoute(http.MethodPost, "/sessions/{id}/plan/{plan}/cancel", planCancel)
}

// planBook finds the conversation's book, answering the refusal itself when
// there is none to give.
func (b *Bridge) planBook(w http.ResponseWriter, id string) (*session.PlanBook, *conversation) {
	b.mu.Lock()
	s := b.sessions[id]
	b.mu.Unlock()
	if s == nil {
		fail(w, 404, "reattach this conversation")
		return nil, nil
	}
	door, ok := s.conn.Agent.(interface{ PlanCards() *session.PlanBook })
	if !ok {
		fail(w, 409, "this engine cannot run plans")
		return nil, nil
	}
	return door.PlanCards(), s
}

// planRefusal maps the book's sentinel errors to statuses: a plan that is not
// waiting is 404, one the person already cancelled is 409, a malformed edit 400.
func planRefusal(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, session.ErrPlanUnknown):
		fail(w, 404, err.Error())
	case errors.Is(err, session.ErrPlanCancelled):
		fail(w, 409, err.Error())
	default:
		fail(w, 400, err.Error())
	}
}

func planGo(b *Bridge, w http.ResponseWriter, r *http.Request, ids map[string]string) {
	var none struct{}
	if !decode(w, r, &none) {
		return
	}
	book, s := b.planBook(w, ids["id"])
	if book == nil {
		return
	}
	receipt, err := book.Go(r.Context(), ids["plan"])
	if err != nil {
		planRefusal(w, err)
		return
	}
	s.publishSnapshot()
	write(w, receipt)
}

func planEdit(b *Bridge, w http.ResponseWriter, r *http.Request, ids map[string]string) {
	var ask struct {
		Steps []session.PlanCardStep `json:"steps"`
	}
	if !decode(w, r, &ask) {
		return
	}
	book, _ := b.planBook(w, ids["id"])
	if book == nil {
		return
	}
	if err := book.Edit(ids["plan"], ask.Steps); err != nil {
		planRefusal(w, err)
		return
	}
	write(w, map[string]bool{"accepted": true})
}

func planCancel(b *Bridge, w http.ResponseWriter, r *http.Request, ids map[string]string) {
	var none struct{}
	if !decode(w, r, &none) {
		return
	}
	book, _ := b.planBook(w, ids["id"])
	if book == nil {
		return
	}
	if err := book.Cancel(ids["plan"]); err != nil {
		planRefusal(w, err)
		return
	}
	write(w, map[string]bool{"cancelled": true})
}
