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
// IT IS NOT ON DISK, which is the one difference from the account exclusion and
// it is deliberate. An account's privacy switch is a setting somebody changed
// and will still be true tomorrow; a model with no endpoints today is very often
// a model with endpoints next week, and a memo that outlived the process would
// take a model away from somebody for reasons they could not see or undo. A
// conversation's worth of not paying the same three refusals is the whole of
// what this is for.
//
// AND IT IS CARRIED PER CLIENT, not in a package `var`, which is the second
// difference and the one worth stating. An adapter is built against one router
// and one account, and "the router does not carry this model" is a fact about
// exactly that pair — the same reason `encodes` and the other per-client memos
// on [Client] are where they are. The span it needs to cover is a conversation,
// and internal/session builds one client per agent and asks it every turn, so a
// fact learned on turn one is known on turn two, which is the measured failure
// above.
//
// A PACKAGE `var` WOULD ALSO BE A LEARNER THAT CHANGES WHETHER A REQUEST IS SENT
// AT ALL, and that is the thing no learner in this package has ever done.
// `resetSharedLearners` (learners_test.go) says in as many words that the
// twenty-nine test files not on its rig are safe because they "assert on request
// counts, bodies and logged rows — which no learner reshapes". A process-wide
// withdrawal memo reshapes request counts, so it would have quietly made those
// assertions depend on which test ran first.
//
// AND AN ANSWER CLEARS IT. If the model serves one request, it is carried; the
// memo was about a moment and the moment has passed.
//
// The zero value is a usable empty memo, so a [Client] built as a literal — as a
// few tests in this package do for the encoder — needs no construction step.
type withdrawnMemo struct {
	mu   sync.RWMutex
	gone map[string]bool
}

// noteWithdrawn remembers that the router answered for itself about a model it
// does not carry. It is called from the refusal door, where the fact is decided
// ([Client.withdrawnModel]).
func (m *withdrawnMemo) noteWithdrawn(model string) {
	key := normalizeModel(model)
	if key == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.gone == nil {
		m.gone = map[string]bool{}
	}
	m.gone[key] = true
}

// carriedAgain forgets the memo because the model has just answered. It is
// called on every clean send rather than only where a memo is known to exist:
// the map read is one RLock on a path that has just paid for a network round
// trip, and a memo that needed somebody to remember to clear it is a memo that
// takes a model away for a whole conversation.
func (m *withdrawnMemo) carriedAgain(model string) {
	key := normalizeModel(model)
	if key == "" {
		return
	}
	if !m.isGone(key) {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.gone, key)
}

// isGone is the cheap read that keeps the write lock off the common path: a
// memo with nothing to clear is the ordinary case, and its cost is one RLock
// on a path that has just paid for a network round trip.
func (m *withdrawnMemo) isGone(key string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.gone[key]
}

// WithdrawnModel reports that this client has already been told the router does
// not carry this model.
//
// It is exported because the layer that owns the one model hop has to be able to
// skip such a model rather than hop onto it (internal/session's nextFallback
// reads it through [Client.FallbackModels], which does the skipping).
func (c *Client) WithdrawnModel(model string) bool {
	key := normalizeModel(model)
	if key == "" {
		return false
	}
	c.withdrawn.mu.RLock()
	defer c.withdrawn.mu.RUnlock()
	return c.withdrawn.gone[key]
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
