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
)

const accessSessionTTL = 30 * 24 * time.Hour
const accessCookieName = "codeaf_factory_session"
const accessFormLimit = 4096

// accessSessions keeps opaque browser credentials only in the running service.
// Restarting the service or revoking sessions makes every old cookie unusable.
type accessSessions struct {
	mu  sync.Mutex
	ids map[string]time.Time
	now func() time.Time
}

func (h *Handler) allowed(r *http.Request) bool {
	// Only the socket peer can grant loopback access. Forwarded headers and
	// hostnames come from callers and must never open the floor.
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return true
		}
	}
	if h.validAccessToken(bearer(r)) {
		return true
	}
	cookie, err := r.Cookie(accessCookieName)
	if err != nil {
		return false
	}
	h.sessions.mu.Lock()
	defer h.sessions.mu.Unlock()
	expires, ok := h.sessions.ids[cookie.Value]
	if !ok || !h.sessions.now().Before(expires) {
		delete(h.sessions.ids, cookie.Value)
		return false
	}
	return true
}

func (h *Handler) validAccessToken(token string) bool {
	if token == "" || h.config.AccessToken == "" {
		return false
	}
	// Fixed-size hashes keep token length out of the comparison's timing.
	want, got := sha256.Sum256([]byte(h.config.AccessToken)), sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(want[:], got[:]) == 1
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
	if !h.validAccessToken(token) {
		h.fail(w, http.StatusUnauthorized, "invalid access token")
		return
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		h.fail(w, http.StatusInternalServerError, "could not create an access session")
		return
	}
	id := hex.EncodeToString(secret)
	h.sessions.mu.Lock()
	now := h.sessions.now()
	expires := now.Add(accessSessionTTL)
	// Expired sessions are discarded on issuance so unused cookies do not
	// retain entries forever when people return to enter their token again.
	for old, until := range h.sessions.ids {
		if !now.Before(until) {
			delete(h.sessions.ids, old)
		}
	}
	h.sessions.ids[id] = expires
	h.sessions.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: accessCookieName, Value: id, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil,
		MaxAge: int(accessSessionTTL.Seconds()), Expires: expires})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) revokeSessions(w http.ResponseWriter, r *http.Request) {
	// A browser cookie grants dashboard access, not permission to sign out
	// every browser. The local command sends the service's bearer token.
	if !h.validAccessToken(bearer(r)) {
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
