package serve

// ── STEP-UP: ONE CONFIRMATION FOR ONE ACTION ────────────────────────────────
//
// A session is a bearer: whoever holds one is the person to this server. That
// is fine for reading and working, but not for changing how the person gets
// in. A stolen session that could add its own passkey, set a first password
// and remove the person's own passkey would lock the person out for good. So
// each of those needs a confirmation made for it: a passkey confirm flow
// started for that action (the page names it and the computer asking), or
// the current password given for it. A confirmation lets exactly that action
// happen once, within passkeyStepUpLife; nothing else, and not twice. A
// sign-in is never one. A refusal says step_up_required and names the action
// it wanted, so the client confirms that action and retries.
//
// Confirmations are kept in memory; a server restart forgets them, which only
// means confirming again.

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

// stepUpKey is the one action a confirmation is for.
func stepUpKey(a api.StepUpAction) (string, bool) {
	switch a.Kind {
	case api.StepUpAddPasskey, api.StepUpSetPassword:
		return a.Kind, a.Passkey == ""
	case api.StepUpRemovePasskey:
		return a.Kind + ":" + a.Passkey, a.Passkey != "" && len(a.Passkey) <= 512
	}
	return "", false
}

// grant records a confirmation by session id for action.
func (h *Handler) grant(sessionID string, a api.StepUpAction, now time.Time) {
	key, ok := stepUpKey(a)
	if !ok || sessionID == "" {
		return
	}
	st := h.pk()
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.grants == nil {
		st.grants = map[string]time.Time{}
	}
	for k, at := range st.grants {
		if now.Sub(at) >= passkeyStepUpLife {
			delete(st.grants, k)
		}
	}
	if len(st.grants) < passkeyGrantsMost {
		st.grants[sessionID+"\x00"+key] = now
	}
}

// granted is whether session holds a live confirmation for action; take
// spends it as well, so it lets the action happen once.
func (h *Handler) granted(session accessSession, a api.StepUpAction, take bool) bool {
	key, ok := stepUpKey(a)
	if !ok {
		return false
	}
	st := h.pk()
	st.mu.Lock()
	defer st.mu.Unlock()
	at, held := st.grants[session.id+"\x00"+key]
	if !held || h.now().Sub(at) >= passkeyStepUpLife {
		return false
	}
	if take {
		delete(st.grants, session.id+"\x00"+key)
	}
	return true
}

// stepUp refuses an action that needs a confirmation, naming it.
func (h *Handler) stepUp(w http.ResponseWriter, a api.StepUpAction) {
	h.writeRefusal(w, http.StatusForbidden, refusalBody{Error: passkeyStepUpWords, Reason: api.ReasonStepUp, Action: &a})
}

// requestedBy is the computer a confirm names on its page: what the engine
// says it is, cleaned, or the session's own device label.
func requestedBy(given string, session accessSession) string {
	if name, ok := cleanDisplayName(given); ok && name != "" {
		if len(name) > 60 {
			name = strings.TrimSpace(name[:60])
		}
		return name
	}
	if session.device != "" {
		return session.device
	}
	return "another computer"
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
	if _, ok := stepUpKey(body.Action); !ok {
		h.fail(w, http.StatusUnprocessableEntity, "action names one of add_passkey, remove_passkey (with passkey) or set_password")
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
	h.grant(session.id, body.Action, h.now())
	w.WriteHeader(http.StatusNoContent)
}
