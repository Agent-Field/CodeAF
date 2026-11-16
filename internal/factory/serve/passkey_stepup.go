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

// passkeyGrant is one confirmation: the action key it is for, the account
// (handle) it was made by, and when.
type passkeyGrant struct {
	key    string
	handle string
	at     time.Time
}

// grant records a confirmation by session id for action. A session keeps
// at most passkeyGrantsPerSession, and an account (handle) at most
// passkeyGrantsPerAccount across all its sessions, the newest; the table
// keeps at most passkeyGrantsMost. Each makes room by dropping the oldest it
// holds, never the new one.
func (h *Handler) grant(sessionID, handle string, a api.StepUpAction, now time.Time) {
	key, ok := stepUpKey(a)
	if !ok || sessionID == "" {
		return
	}
	st := h.pk()
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.grants == nil {
		st.grants = map[string][]passkeyGrant{}
	}
	for id, held := range st.grants {
		live := held[:0]
		for _, g := range held {
			if now.Sub(g.at) < passkeyStepUpLife {
				live = append(live, g)
			}
		}
		if len(live) == 0 {
			delete(st.grants, id)
			continue
		}
		st.grants[id] = live
	}
	// The same action again replaces its own.
	var held []passkeyGrant
	for _, g := range st.grants[sessionID] {
		if g.key != key {
			held = append(held, g)
		}
	}
	fresh := passkeyGrant{key: key, handle: handle, at: now}
	st.grants[sessionID] = append(held, fresh)
	count := func(match func(id string, g passkeyGrant) bool) int {
		n := 0
		for id, held := range st.grants {
			for _, g := range held {
				if match(id, g) {
					n++
				}
			}
		}
		return n
	}
	for _, limit := range []struct {
		most  int
		match func(id string, g passkeyGrant) bool
	}{
		{passkeyGrantsPerSession, func(id string, _ passkeyGrant) bool { return id == sessionID }},
		{passkeyGrantsPerAccount, func(_ string, g passkeyGrant) bool { return handle != "" && g.handle == handle }},
		{passkeyGrantsMost, func(string, passkeyGrant) bool { return true }},
	} {
		for count(limit.match) > limit.most && st.dropOldestGrantLocked(sessionID, fresh, limit.match) {
		}
	}
}

// dropOldestGrantLocked drops the oldest confirmation match picks, other
// than fresh (just made by session), or answers false.
func (st *passkeyState) dropOldestGrantLocked(session string, fresh passkeyGrant, match func(id string, g passkeyGrant) bool) bool {
	oldestID, oldestAt := "", 0
	for id, held := range st.grants {
		for i, g := range held {
			if (id == session && g == fresh) || !match(id, g) {
				continue
			}
			if oldestID == "" || g.at.Before(st.grants[oldestID][oldestAt].at) {
				oldestID, oldestAt = id, i
			}
		}
	}
	if oldestID == "" {
		return false
	}
	held := st.grants[oldestID]
	if rest := append(held[:oldestAt:oldestAt], held[oldestAt+1:]...); len(rest) > 0 {
		st.grants[oldestID] = rest
	} else {
		delete(st.grants, oldestID)
	}
	return true
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
	held := st.grants[session.id]
	for i, g := range held {
		if g.key != key {
			continue
		}
		if h.now().Sub(g.at) >= passkeyStepUpLife {
			return false
		}
		if take {
			st.grants[session.id] = append(held[:i:i], held[i+1:]...)
		}
		return true
	}
	return false
}

// passkeyNamed is the name of acct's passkey id, and whether it has one.
func passkeyNamed(acct tokens.Account, id string) (string, bool) {
	for _, pk := range acct.Passkeys {
		if pk.ID == id {
			return pk.Name, true
		}
	}
	return "", false
}

func passkeyKnown(acct tokens.Account, id string) bool {
	_, ok := passkeyNamed(acct, id)
	return ok
}

// stepUp refuses an action that needs a confirmation, naming it.
func (h *Handler) stepUp(w http.ResponseWriter, a api.StepUpAction) {
	h.writeRefusal(w, http.StatusForbidden, refusalBody{Error: passkeyStepUpWords, Reason: api.ReasonStepUp, Action: &a})
}

// requestedBy says which computer asked for a confirm, as its page names
// it: "a computer named “santosh-mac”, signed in 16 Nov 2026 from
// 203.0.113.7". The name is whatever the engine claims (or the session's own
// device label), so it is quoted and framed as a name, never shown bare
// where it could read as an instruction; beside it are what the server knows
// itself, the session's sign-in date and the address the start came from.
//
// It answers the name and the rest apart, so the page can isolate the name
// (<bdi>); the name has no control or format character either way, so it
// can neither reorder the server's words nor hide its own.
func requestedBy(given string, session accessSession, from string) (name, rest string) {
	name, ok := cleanDisplayName(given)
	if !ok || name == "" {
		name = strings.Join(strings.Fields(plainText(session.device)), " ")
	}
	name = strings.TrimSpace(strings.NewReplacer("“", "", "”", "", `"`, "").Replace(name))
	if len(name) > 60 {
		name = strings.TrimSpace(strings.ToValidUTF8(name[:60], ""))
	}
	if !session.created.IsZero() {
		rest += ", signed in " + session.created.UTC().Format("2 Jan 2006")
	}
	if from != "" && from != "invalid IP" {
		rest += " from " + from
	}
	return name, rest
}

// requestedByWords is requestedBy as one plain sentence part.
func requestedByWords(name, rest string) string {
	if name == "" {
		return "a computer" + rest
	}
	return "a computer named “" + name + "”" + rest
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
	if body.Action.Kind == api.StepUpRemovePasskey && !passkeyKnown(acct, body.Action.Passkey) {
		// The same answer a passkey confirm for it gets: nothing is held
		// for a passkey that is not this person's.
		h.fail(w, http.StatusNotFound, "no such passkey")
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
	h.grant(session.id, handle, body.Action, h.now())
	w.WriteHeader(http.StatusNoContent)
}
