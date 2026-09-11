package provider

import (
	"strings"
	"sync"
)

// ── A MODEL THE ROUTER HAS PUT DOWN IS REMEMBERED ───────────────────────────
//
// ── THE MEASURED FAILURE (2026-09-10 22:39) ─────────────────────────────────
//
// A conversation's model resolved to one the router carries with ZERO endpoints.
// Every turn: three start rows for that model thirty to forty milliseconds apart
// — the shape ladder climbing rungs on a request that could never be served by
// any shape — and then a fourth attempt on a different model, which answered. The
// next turn did the whole thing again, and picked a DIFFERENT second model,
// because nothing anywhere remembered what the first turn had just found out.
//
// ── WHAT IS REMEMBERED AND FOR HOW LONG ─────────────────────────────────────
//
// The fact is "the router does not carry this model", which is about the router
// and this account rather than about one request — the same shape as the account
// exclusion internal/lane's account.go keeps, and it is kept the same way: named
// once, read on every later request, and cleared by an answer.
//
// IT IS PER PROCESS AND NOT ON DISK, which is the one difference and it is
// deliberate. An account's privacy switch is a setting somebody changed and will
// still be true tomorrow; a model with no endpoints today is very often a model
// with endpoints next week, and a memo that outlived the process would take a
// model away from somebody for reasons they could not see or undo. A session's
// worth of not paying the same three refusals is the whole of what this is for.
//
// AND AN ANSWER CLEARS IT. If the model serves one request, it is carried; the
// memo was about a moment and the moment has passed.

var withdrawnModels struct {
	sync.RWMutex
	gone map[string]bool
}

// noteWithdrawn remembers that the router answered for itself about a model it
// does not carry. It is called from the refusal door, where the fact is decided
// ([Client.withdrawnModel]).
func noteWithdrawn(model string) {
	key := normalizeModel(model)
	if key == "" {
		return
	}
	withdrawnModels.Lock()
	defer withdrawnModels.Unlock()
	if withdrawnModels.gone == nil {
		withdrawnModels.gone = map[string]bool{}
	}
	withdrawnModels.gone[key] = true
}

// carriedAgain forgets the memo because the model has just answered. It is
// called on every clean send rather than only where a memo is known to exist:
// the map read is one RLock on a path that has just paid for a network round
// trip, and a memo that needed somebody to remember to clear it is a memo that
// takes a model away for a session.
func carriedAgain(model string) {
	key := normalizeModel(model)
	if key == "" {
		return
	}
	withdrawnModels.RLock()
	gone := withdrawnModels.gone[key]
	withdrawnModels.RUnlock()
	if !gone {
		return
	}
	withdrawnModels.Lock()
	defer withdrawnModels.Unlock()
	delete(withdrawnModels.gone, key)
}

// WithdrawnModel reports that this process has already been told the router does
// not carry this model.
//
// It is exported because the layer that owns the one model hop has to be able to
// skip such a model rather than hop onto it (internal/session's nextFallback
// reads it through [Client.FallbackModels], which does the skipping).
func WithdrawnModel(model string) bool {
	key := normalizeModel(model)
	if key == "" {
		return false
	}
	withdrawnModels.RLock()
	defer withdrawnModels.RUnlock()
	return withdrawnModels.gone[key]
}

// ForgetWithdrawnModels empties the memo. It is for tests, which must not
// inherit another test's refusals.
func ForgetWithdrawnModels() {
	withdrawnModels.Lock()
	defer withdrawnModels.Unlock()
	withdrawnModels.gone = nil
}

// withdrawnRefusal is the refusal a request for a remembered model is answered
// with instead of being sent.
//
// IT IS THE ROUTER'S OWN ANSWER, SAID BACK. The status, the sentence and the
// mark are what the router produced the first time, so everything downstream
// reads the identical evidence and reaches the identical verdict — one
// [taxonomy.Classify] call, [taxonomy.ActionHop], and no wire attempt at all.
// A second shape of the same fact would be a second classifier.
func withdrawnRefusal(model string) error {
	return &APIError{
		Status:    404,
		Message:   "No endpoints found for " + strings.TrimSpace(model) + ".",
		Withdrawn: true,
	}
}
