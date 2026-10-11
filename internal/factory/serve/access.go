package serve

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

const accessSessionTTL = 30 * 24 * time.Hour
const accessCookieName = "codeaf_factory_session"
const accessFormLimit = 4096

// accessSession is one browser sign-in. tokenID is empty when the host
// config token created it. An invite row's id is kept so the session dies
// when that row is revoked or expires, even inside the cookie's 30 days.
type accessSession struct {
	expires   time.Time
	invitedBy string
	tokenID   string
	id        string
	profile   *api.Profile
}

// accessSessions keeps opaque browser credentials only in the running service.
// Restarting the service or revoking sessions makes every old cookie unusable.
type accessSessions struct {
	mu  sync.Mutex
	ids map[string]accessSession
	now func() time.Time
}

func loopbackRequest(r *http.Request) bool {
	// Only the socket peer can grant loopback access. Forwarded headers and
	// hostnames come from callers and must never open the floor.
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return true
		}
	}
	return false
}

func (h *Handler) now() time.Time {
	if h != nil && h.sessions != nil && h.sessions.now != nil {
		return h.sessions.now()
	}
	return time.Now()
}

func (h *Handler) allowed(r *http.Request) bool {
	if _, ok := h.credential(r); ok {
		return true
	}
	return loopbackRequest(r)
}

// credential gives explicit credentials precedence over an open loopback peer.
func (h *Handler) credential(r *http.Request) (accessSession, bool) {
	if token := bearer(r); token != "" {
		if by, id, ok := h.identifyToken(token); ok {
			session := accessSession{invitedBy: by, tokenID: id, id: id}
			for _, row := range h.tokenRows() {
				if row.ID == id {
					session.profile = row.Profile
					break
				}
			}
			return session, true
		}
		return h.liveSession(token)
	}
	if cookie, err := r.Cookie(accessCookieName); err == nil {
		return h.liveSession(cookie.Value)
	}
	return accessSession{}, false
}

func (h *Handler) liveSession(key string) (accessSession, bool) {
	h.sessions.mu.Lock()
	defer h.sessions.mu.Unlock()
	session, ok := h.sessions.ids[key]
	if !ok || !h.sessionUsable(session) {
		delete(h.sessions.ids, key)
		return accessSession{}, false
	}
	if session.profile == nil && session.tokenID != "" {
		for _, row := range h.tokenRows() {
			if row.ID == session.tokenID {
				session.profile = row.Profile
				break
			}
		}
	}
	return session, true
}

// identifyToken accepts the host config token or an unexpired row. The row's
// invited_by and id come back so a session can remember them. The host token
// matches with an empty id.
func (h *Handler) identifyToken(token string) (invitedBy, id string, ok bool) {
	if token == "" {
		return "", "", false
	}
	got := sha256.Sum256([]byte(token))
	floor := h.sameSecret(h.config.AccessToken, got)
	now := h.now()
	var match tokens.Token
	matched := false
	for _, row := range h.tokenRows() {
		if !h.sameSecret(row.Token, got) {
			continue
		}
		if !row.Expires.IsZero() && !now.Before(row.Expires) {
			continue
		}
		match = row
		matched = true
	}
	if matched {
		return match.InvitedBy, match.ID, true
	}
	if floor {
		return "", "", true
	}
	return "", "", false
}

func (h *Handler) sameSecret(secret string, got [sha256.Size]byte) bool {
	if secret == "" {
		return false
	}
	want := sha256.Sum256([]byte(secret))
	return subtle.ConstantTimeCompare(want[:], got[:]) == 1
}

// floorToken is the host config secret. Invite rows are access tokens too,
// and they do not get this door: revoking every session stays with the host.
func (h *Handler) floorToken(token string) bool {
	if token == "" {
		return false
	}
	got := sha256.Sum256([]byte(token))
	return h.sameSecret(h.config.AccessToken, got)
}

// tokenRows is the file named by TokensPath. A missing file is no rows. A
// read or parse failure fails closed for those rows; the host token still
// works. The file is small and read every time so two writes in one second
// cannot hide a revoke behind a stale mtime.
func (h *Handler) tokenRows() []tokens.Token {
	if h.config.TokensPath == "" {
		return nil
	}
	rows, err := tokens.Load(h.config.TokensPath)
	if err != nil {
		return nil
	}
	return rows
}

// sessionUsable is a cookie that has not reached its own expiry and, when it
// was minted from a row, that row is still present and unexpired.
func (h *Handler) sessionUsable(session accessSession) bool {
	if !h.now().Before(session.expires) {
		return false
	}
	if session.tokenID == "" {
		return true
	}
	now := h.now()
	for _, row := range h.tokenRows() {
		if row.ID == session.tokenID && (row.Expires.IsZero() || now.Before(row.Expires)) {
			return true
		}
	}
	return false
}

func (h *Handler) access(w http.ResponseWriter, r *http.Request) {
	// The one-time query must not travel to dashboard resources as a referrer.
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	var token string
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, accessFormLimit)
		if err := r.ParseForm(); err != nil {
			h.fail(w, http.StatusBadRequest, "invalid access form")
			return
		}
		token = r.PostForm.Get("token")
	} else {
		values := r.URL.Query()
		if !values.Has("token") {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprintf(w, accessForm, int(accessSessionTTL.Hours()/24))
			return
		}
		token = values.Get("token")
	}
	invitedBy, tokenID, ok := h.identifyToken(token)
	if !ok {
		h.fail(w, http.StatusUnauthorized, "invalid access token")
		return
	}
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		h.fail(w, http.StatusInternalServerError, "could not create an access session")
		return
	}
	sessionID := hex.EncodeToString(bytes)
	publicID, err := secret()
	if h.failed(w, err) {
		return
	}
	h.sessions.mu.Lock()
	now := h.now()
	expires := now.Add(accessSessionTTL)
	// Expired sessions are discarded on issuance so unused cookies do not
	// retain entries forever when people return to enter their token again.
	for old, session := range h.sessions.ids {
		if !now.Before(session.expires) {
			delete(h.sessions.ids, old)
		}
	}
	h.sessions.ids[sessionID] = accessSession{expires: expires, invitedBy: invitedBy, tokenID: tokenID, id: "session_" + publicID}
	h.sessions.mu.Unlock()
	h.setSessionCookie(w, r, sessionID, accessSession{expires: expires})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) revokeSessions(w http.ResponseWriter, r *http.Request) {
	// A browser cookie grants dashboard access, not permission to sign out
	// every browser. The local command sends the service's bearer token.
	// An invite row is an access token and still cannot do this.
	if !h.floorToken(bearer(r)) {
		h.fail(w, http.StatusUnauthorized, "a bearer access token is required to revoke sessions")
		return
	}
	h.sessions.mu.Lock()
	clear(h.sessions.ids)
	h.sessions.mu.Unlock()
	h.write(w, http.StatusOK, struct {
		Factory api.Factory `json:"factory"`
		Revoked bool        `json:"revoked"`
	}{h.factory, true})
}

const accessForm = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>codeaf factory access</title></head>
<body><main><h1>codeaf factory access</h1>
<form method="post" action="/access"><label for="token">Access token</label>
<input id="token" name="token" type="password" autocomplete="off" required autofocus>
<button type="submit">Open dashboard</button></form>
<p>This browser stays signed in for %d days, until sessions are revoked or the service restarts.</p>
</main></body></html>`
