package serve

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

const accessSessionTTL = 30 * 24 * time.Hour
const accessCookieName = "codeaf_factory_session"
const accessFormLimit = 4096

// accessSession is one browser sign-in. tokenID is empty when the host
// config token created it. A row's id is kept so the session dies when that
// row is revoked or expires, even inside the cookie's 30 days.
// GitHub credentials are encrypted in the session file and never returned.
type accessSession struct {
	expires        time.Time
	created        time.Time
	lastSeen       time.Time
	tokenID        string
	id             string
	device         string
	githubToken    string
	githubRefresh  string
	githubExpires  time.Time
	refreshExpires time.Time
	githubExpired  bool
	profile        *api.Profile
}

// accessSessions indexes sign-ins by credential hash, never the credential.
type accessSessions struct {
	mu   sync.Mutex
	ids  map[string]accessSession
	now  func() time.Time
	path string
	key  []byte
	err  error
}

func loopbackRequest(r *http.Request) bool {
	// Only the socket peer can grant loopback access, and only when nobody
	// forwarded the request: a proxy on this machine is a loopback peer for
	// every caller on the internet. Forwarded headers from a trusted proxy may
	// name a loopback caller; from anyone else they only take access away.
	return clientOf(r).onThisMachine()
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
	// A refused explicit credential cannot regain access through loopback.
	if bearer(r) != "" {
		return false
	}
	if _, err := r.Cookie(accessCookieName); err == nil {
		return false
	}
	return h.loopbackOwner(r)
}

// The app host token is the only person credential on its private listener.
func (h *Handler) loopbackOwner(r *http.Request) bool {
	return h.config.ServedBy != "app" && loopbackRequest(r)
}

// credential gives explicit credentials precedence over an open loopback peer.
func (h *Handler) credential(r *http.Request) (accessSession, bool) {
	if token := bearer(r); token != "" {
		if id, ok := h.identifyToken(token); ok {
			session := accessSession{tokenID: id, id: id}
			for _, row := range h.tokenRows() {
				if row.ID == id {
					// A shared link secret names no one: it may start a GitHub
					// sign-in, and what that sign-in makes is the person's session.
					if !row.Counted() {
						session.profile = row.Profile
					}
					break
				}
			}
			if session.profile != nil && h.viewer != nil && r.URL.Path != "/v1/auth/device" && r.URL.Path != "/v1/auth/device/poll" {
				linked, found := h.linkedGitHubSession(r, session.profile.Handle)
				if found {
					session.githubToken = linked.githubToken
				}
				h.viewer.see(*session.profile, time.Time{}, h.now(), "")
			}
			return session, true
		}
		return h.liveSession(r, token)
	}
	if cookie, err := r.Cookie(accessCookieName); err == nil {
		return h.liveSession(r, cookie.Value)
	}
	return accessSession{}, false
}

func (h *Handler) liveSession(r *http.Request, credential string) (accessSession, bool) {
	return h.liveSessionHash(r, sessionHash(credential))
}

func (h *Handler) liveSessionHash(r *http.Request, key string) (accessSession, bool) {
	h.sessions.mu.Lock()
	defer h.sessions.mu.Unlock()
	if h.sessions.err != nil {
		h.setAuthError(r, h.sessions.err)
		return accessSession{}, false
	}
	session, ok := h.sessions.ids[key]
	if ok && session.githubExpired && h.now().Before(session.expires) {
		h.setAuthError(r, errGitHubExpired)
		return accessSession{}, false
	}
	if !ok || !h.sessionUsable(session) {
		if ok {
			delete(h.sessions.ids, key)
			h.releaseToken(profileHandle(session))
			h.setAuthError(r, h.saveSessions())
		}
		if state, hasState := r.Context().Value(authErrorKey{}).(*authErrorState); hasState && (ok || state.hadSession) && state.err == nil {
			state.err = errSessionEnded
		}
		return accessSession{}, false
	}
	if err := h.refreshSession(r, key, &session); err != nil {
		h.setAuthError(r, err)
		return accessSession{}, false
	}
	if session.profile == nil && session.tokenID != "" {
		for _, row := range h.tokenRows() {
			if row.ID == session.tokenID {
				if !row.Counted() {
					session.profile = row.Profile
				}
				break
			}
		}
	}
	// Every use of the session is the person being here.
	previousSeen := session.lastSeen
	session.lastSeen = h.now()
	h.sessions.ids[key] = session
	// Persist activity at most once a minute; refreshing and revoking always
	// save immediately, so painting a queue does not rewrite the file repeatedly.
	if session.lastSeen.Sub(previousSeen) >= time.Minute {
		if err := h.saveSessions(); err != nil {
			h.setAuthError(r, err)
			return accessSession{}, false
		}
	}
	if session.profile != nil && h.viewer != nil {
		h.viewer.see(*session.profile, session.created, session.lastSeen, session.githubToken)
	}
	if state, ok := r.Context().Value(authErrorKey{}).(*authErrorState); ok {
		state.hadSession = true
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
		case row.Revoked || (row.Operator && !row.Joined.IsZero()):
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
	if session.githubExpired || !h.now().Before(session.expires) {
		return false
	}
	if session.tokenID == "" {
		return true
	}
	now := h.now()
	for _, row := range h.tokenRows() {
		if row.ID != session.tokenID {
			continue
		}
		// A link with its own use limit invites people; it does not hold their
		// sessions. Revoke or expiry stops new joins, and a person leaves
		// through the session's own expiry or the operator removing them.
		// Only while the row still counts the person: a use given back removes
		// them, and a session of theirs left behind must not outlive it.
		if row.Counted() {
			return session.profile == nil || row.Has(session.profile.Handle)
		}
		if !row.Revoked && ((row.Operator && !row.Joined.IsZero()) || row.Expires.IsZero() || now.Before(row.Expires)) {
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
	h.sessions.ids[sessionHash(sessionID)] = accessSession{expires: expires, created: now, lastSeen: now,
		tokenID: tokenID, id: "session_" + publicID, device: deviceLabel(r)}
	err = h.saveSessions()
	if err != nil {
		delete(h.sessions.ids, sessionHash(sessionID))
	}
	h.sessions.mu.Unlock()
	if h.failed(w, err) {
		return
	}
	h.setSessionCookie(w, r, sessionID, accessSession{expires: expires})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) revokeSessions(w http.ResponseWriter, r *http.Request) {
	// A browser cookie grants dashboard access, not permission to sign out
	// every browser. The local command sends the service's bearer token.
	// A minted access-token row still cannot do this.
	if !h.isOperator(r) {
		h.fail(w, http.StatusUnauthorized, "a bearer access token is required to revoke sessions")
		return
	}
	handles := map[string]bool{}
	h.sessions.mu.Lock()
	for _, session := range h.sessions.ids {
		if handle := profileHandle(session); handle != "" {
			handles[handle] = true
		}
	}
	clearPhase(w)
	clear(h.sessions.ids)
	err := h.saveSessions()
	h.sessions.mu.Unlock()
	if h.failed(w, err) {
		return
	}
	for handle := range handles {
		if h.failed(w, h.revokePerson(r, handle)) {
			return
		}
	}
	if h.failed(w, h.operatorEvent(r, record.SessionsRevoked, "factory", "Revoke all sessions")) {
		return
	}
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
<p>This browser stays signed in for %d days, until sessions are revoked or expire.</p>
</main></body></html>`
