package serve

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

const accessSessionTTL = 30 * 24 * time.Hour
const accessCookieName = "codeaf_factory_session"
const accessFormLimit = 4096

// accessSession is one browser sign-in. tokenID is empty when the host
// config token created it. A row's id is kept so the session dies when that
// row is revoked or expires, even inside the cookie's 30 days.
// githubToken is the person's GitHub credential for repository checks. It
// stays in this process and is never written to disk or returned on the wire.
type accessSession struct {
	expires     time.Time
	created     time.Time
	lastSeen    time.Time
	tokenID     string
	id          string
	device      string
	githubToken string
	profile     *api.Profile
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
		if id, ok := h.identifyToken(token); ok {
			session := accessSession{tokenID: id, id: id}
			for _, row := range h.tokenRows() {
				if row.ID == id {
					session.profile = row.Profile
					break
				}
			}
			if session.profile != nil && h.viewer != nil {
				h.viewer.see(*session.profile, time.Time{}, h.now(), "")
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
	// Every use of the session is the person being here.
	session.lastSeen = h.now()
	h.sessions.ids[key] = session
	if session.profile != nil && h.viewer != nil {
		h.viewer.see(*session.profile, session.created, session.lastSeen, session.githubToken)
	}
	return session, true
}

// tokenHit is what a presented secret is. reason is empty when it may be
// used. expired, revoked and unknown are refusals. invitedBy and created
// are set for a row we still have, so the app can name who to ask.
type tokenHit struct {
	id        string
	reason    string
	invitedBy string
	created   time.Time
}

// identifyToken accepts the host config token or an unexpired, unrevoked
// row. The row's id comes back so a session can remember it. The host token
// matches with an empty id.
func (h *Handler) identifyToken(token string) (id string, ok bool) {
	hit := h.classifyToken(token)
	if hit.reason != "" {
		return "", false
	}
	return hit.id, token != "" && (hit.id != "" || h.floorToken(token))
}

// classifyToken says why a secret cannot be used. A live row or the host
// token has an empty reason. An expired or revoked row keeps who invited
// and when the row was made. Anything else is unknown, with neither.
func (h *Handler) classifyToken(token string) tokenHit {
	if token == "" {
		return tokenHit{reason: factory.LinkUnknown}
	}
	now := h.now()
	for _, row := range h.tokenRows() {
		if !row.Matches(token) {
			continue
		}
		hit := tokenHit{id: row.ID, invitedBy: row.InvitedBy, created: row.Created}
		switch {
		case row.Revoked:
			hit.reason = factory.LinkRevoked
		case !row.Expires.IsZero() && !now.Before(row.Expires):
			hit.reason = factory.LinkExpired
		default:
			return hit
		}
		return hit
	}
	if h.floorToken(token) {
		return tokenHit{}
	}
	return tokenHit{reason: factory.LinkUnknown}
}

func (h *Handler) sameSecret(secret string, got [sha256.Size]byte) bool {
	if secret == "" {
		return false
	}
	want := sha256.Sum256([]byte(secret))
	return subtle.ConstantTimeCompare(want[:], got[:]) == 1
}

// floorToken is the host config secret. Other access-token rows do not get
// this door: revoking every session stays with the host.
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
		if row.ID == session.tokenID && !row.Revoked && (row.Expires.IsZero() || now.Before(row.Expires)) {
			return true
		}
	}
	return false
}

// refuseToken answers 401 for a secret that was presented and refused.
// reason is expired, revoked or unknown. invited_by and created_at are set
// when the row is still on disk, so the app can say who to ask for a new link.
func (h *Handler) refuseToken(w http.ResponseWriter, token string) {
	hit := h.classifyToken(token)
	reason := hit.reason
	if reason == "" {
		reason = factory.LinkUnknown
	}
	words := "this link is not recognized"
	switch reason {
	case factory.LinkExpired:
		words = "this link has expired"
	case factory.LinkRevoked:
		words = "this link was revoked"
	default:
		reason = factory.LinkUnknown
	}
	var created *time.Time
	invited := ""
	if reason != factory.LinkUnknown {
		invited = strings.TrimSpace(hit.invitedBy)
		if invited != "" && !strings.HasPrefix(invited, "@") {
			invited = "@" + invited
		}
		if !hit.created.IsZero() {
			stamp := hit.created.UTC()
			created = &stamp
		}
	}
	h.write(w, http.StatusUnauthorized, struct {
		Factory   api.Factory `json:"factory"`
		Error     string      `json:"error"`
		Reason    string      `json:"reason"`
		InvitedBy string      `json:"invited_by,omitempty"`
		CreatedAt *time.Time  `json:"created_at,omitempty"`
		RequestID string      `json:"request_id"`
	}{h.factory, words, reason, invited, created, requestID()})
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
	tokenID, ok := h.identifyToken(token)
	if !ok {
		h.refuseToken(w, token)
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
	h.sessions.ids[sessionID] = accessSession{expires: expires, created: now, lastSeen: now,
		tokenID: tokenID, id: "session_" + publicID, device: deviceLabel(r)}
	h.sessions.mu.Unlock()
	h.setSessionCookie(w, r, sessionID, accessSession{expires: expires})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) revokeSessions(w http.ResponseWriter, r *http.Request) {
	// A browser cookie grants dashboard access, not permission to sign out
	// every browser. The local command sends the service's bearer token.
	// A minted access-token row still cannot do this.
	if !h.floorToken(bearer(r)) {
		h.fail(w, http.StatusUnauthorized, "a bearer access token is required to revoke sessions")
		return
	}
	h.sessions.mu.Lock()
	clear(h.sessions.ids)
	h.sessions.mu.Unlock()
	if h.viewer != nil {
		h.viewer.mu.Lock()
		clear(h.viewer.github)
		h.viewer.mu.Unlock()
	}
	h.write(w, http.StatusOK, struct {
		Factory api.Factory `json:"factory"`
		Revoked bool        `json:"revoked"`
	}{h.factory, true})
}

// deviceLabel is the sign-in's device line. The client does not send a
// separate name, so the User-Agent is the label, trimmed to one short line.
func deviceLabel(r *http.Request) string {
	label := strings.Join(strings.Fields(r.UserAgent()), " ")
	if label == "" {
		return "this device"
	}
	const max = 80
	if len(label) > max {
		label = label[:max]
	}
	return label
}

const accessForm = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>codeaf factory access</title></head>
<body><main><h1>codeaf factory access</h1>
<form method="post" action="/access"><label for="token">Access token</label>
<input id="token" name="token" type="password" autocomplete="off" required autofocus>
<button type="submit">Open dashboard</button></form>
<p>This browser stays signed in for %d days, until sessions are revoked or the service restarts.</p>
</main></body></html>`
