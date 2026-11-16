package serve

// ── STEP-UP: A RECENT PROOF BEFORE A WAY IN CHANGES ─────────────────────────
//
// A session is a bearer: whoever holds one is the person to this server. That
// is fine for reading and working, but not for changing how the person gets
// in. A stolen session that could add its own passkey, set a first password
// and remove the person's own passkey would lock the person out for good. So
// adding a passkey, setting a first password (no current one) and removing a
// passkey need a proof made in the last passkeyStepUpLife: the session was
// just made by a passkey ceremony or a password, or it has since confirmed
// with one of the person's passkeys (purpose confirm) or with the current
// password (POST /v1/me/password/confirm). A refusal says step_up_required.
//
// The proof is kept in memory with the session; a server restart forgets it,
// which only means confirming again.

import (
	"errors"
	"net/http"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

// recentlyVerified is whether session made a passkey or password proof in
// the last passkeyStepUpLife.
func (h *Handler) recentlyVerified(session accessSession) bool {
	return !session.verified.IsZero() && h.now().Sub(session.verified) < passkeyStepUpLife
}

// markVerified records a proof just made by the session whose public id is id.
func (h *Handler) markVerified(id string, now time.Time) {
	if id == "" {
		return
	}
	h.sessions.mu.Lock()
	defer h.sessions.mu.Unlock()
	for key, s := range h.sessions.ids {
		if s.id == id {
			s.verified = now
			h.sessions.ids[key] = s
		}
	}
}

// passwordConfirm is POST /v1/me/password/confirm: the signed-in person shows
// their current password, and their session may change a way in for the next
// few minutes. A wrong password is counted by E9's guard against this session
// (F1), as a wrong current password on a change is.
func (h *Handler) passwordConfirm(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	session, ok := h.credential(r)
	if !ok || session.profile == nil || session.profile.Kind != tokens.KindPassword || !h.passwordsOn() {
		h.fail(w, http.StatusNotFound, "this sign-in has no password")
		return
	}
	if !h.passwordTransport(w, r) {
		return
	}
	var body api.PasswordConfirm
	if !h.readPasswordBody(w, r, &body) {
		return
	}
	handle := session.profile.Handle
	try, ok := h.admitTry(w, r, handle, "session:"+session.id, false)
	if !ok {
		return
	}
	defer try.abandon(h.now())
	acct, found, err := h.passwordAccount(handle)
	if h.failed(w, err) {
		return
	}
	if !found || acct.Hash == "" {
		h.fail(w, http.StatusNotFound, "this sign-in has no password")
		return
	}
	given := normalPassword(body.Password)
	if len(given) > passwordMaxBytes {
		given = ""
	}
	match, _, err := verifyPassword(given, acct.Hash)
	if errors.Is(err, errHashBusy) {
		h.tooManyTries(w)
		return
	}
	if !match || err != nil {
		try.failed(h.now())
		h.refuse(w, http.StatusForbidden, "Your current password is not right.", "current_wrong", "", nil, "")
		return
	}
	try.succeeded(h.now())
	h.markVerified(session.id, h.now())
	w.WriteHeader(http.StatusNoContent)
}
