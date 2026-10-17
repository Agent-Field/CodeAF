package serve

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

const githubRefreshBefore = 5 * time.Minute

var errGitHubExpired = errors.New("GitHub sign-in has expired; sign in again")
var errSessionEnded = errors.New("factory session has ended; sign in again")

type authErrorKey struct{}
type authErrorState struct {
	err        error
	hadSession bool
}

func (h *Handler) setAuthError(r *http.Request, err error) {
	if err == nil {
		return
	}
	if state, ok := r.Context().Value(authErrorKey{}).(*authErrorState); ok {
		state.err = err
	}
}

// authFailed preserves a GitHub authentication failure instead of disguising
// it as an empty queue or a missing job. Transient refresh failures retain the
// session and answer 502, so the caller can retry without signing in again.
func (h *Handler) authFailed(w http.ResponseWriter, r *http.Request) bool {
	state, ok := r.Context().Value(authErrorKey{}).(*authErrorState)
	if !ok || state.err == nil {
		return false
	}
	if errors.Is(state.err, errGitHubExpired) || errors.Is(state.err, errSessionEnded) {
		reason := api.ReasonGitHubExpired
		if errors.Is(state.err, errSessionEnded) {
			reason = factory.LinkExpired
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="factory"`)
		h.write(w, http.StatusUnauthorized, api.AuthFailure{Factory: h.factory,
			Error: state.err.Error(), Reason: reason, RequestID: requestID()})
	} else {
		h.fail(w, http.StatusBadGateway, "factory sign-in is unavailable; try again")
	}
	return true
}

func authRequest(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), authErrorKey{}, &authErrorState{}))
}

// linkedGitHubSession keeps an original invite bearer on the same refreshed
// GitHub credential as its signed-in handle, including after a restart.
func (h *Handler) linkedGitHubSession(r *http.Request, handle string) (accessSession, bool) {
	h.sessions.mu.Lock()
	key := ""
	var latest accessSession
	for hash, s := range h.sessions.ids {
		if profileHandle(s) != handle || !h.now().Before(s.expires) {
			continue
		}
		if key == "" || (!s.githubExpired && latest.githubExpired) || (s.githubExpired == latest.githubExpired && s.created.After(latest.created)) {
			key, latest = hash, s
		}
	}
	h.sessions.mu.Unlock()
	if key == "" {
		return accessSession{}, false
	}
	return h.liveSessionHash(r, key)
}

// githubExchange accepts both expiring tokens and tokens without deadlines.
type githubExchange struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	ExpiresIn        int    `json:"expires_in"`
	RefreshExpiresIn int    `json:"refresh_token_expires_in"`
	Scope            string `json:"scope"`
	Error            string `json:"error"`
}

func (s *accessSession) setGitHub(exchange githubExchange, now time.Time) {
	s.githubToken, s.githubRefresh = exchange.AccessToken, exchange.RefreshToken
	s.githubExpires, s.refreshExpires = time.Time{}, time.Time{}
	if exchange.ExpiresIn > 0 {
		s.githubExpires = now.Add(time.Duration(exchange.ExpiresIn) * time.Second)
	}
	if exchange.RefreshExpiresIn > 0 {
		s.refreshExpires = now.Add(time.Duration(exchange.RefreshExpiresIn) * time.Second)
	}
}

// refreshSession runs under the session lock, so a rotating refresh token is
// consumed once even when two reads arrive together. Save the replacement
// before either read can reuse it. GitHub device grants need no client secret.
func (h *Handler) refreshSession(r *http.Request, hash string, session *accessSession) error {
	if session.githubExpires.IsZero() || h.now().Before(session.githubExpires.Add(-githubRefreshBefore)) {
		return nil
	}
	if session.githubRefresh == "" {
		if h.now().Before(session.githubExpires) {
			return nil
		}
		return h.endGitHubLocked(hash)
	}
	if !session.refreshExpires.IsZero() && !h.now().Before(session.refreshExpires) {
		return h.endGitHubLocked(hash)
	}
	var exchange githubExchange
	err := h.githubJSON(r, http.MethodPost, "https://github.com/login/oauth/access_token",
		url.Values{"client_id": {h.config.GitHubClientID}, "grant_type": {"refresh_token"},
			"refresh_token": {session.githubRefresh}}.Encode(), "", &exchange)
	var status githubStatusError
	if (errors.As(err, &status) && int(status) == http.StatusUnauthorized) || exchange.Error == "bad_refresh_token" || exchange.Error == "expired_token" || exchange.Error == "invalid_grant" {
		return h.endGitHubLocked(hash)
	}
	if err != nil {
		return err
	}
	if exchange.Error != "" || exchange.AccessToken == "" {
		return errors.New("GitHub refresh is unavailable")
	}
	session.setGitHub(exchange, h.now())
	h.sessions.ids[hash] = *session
	if err := h.saveSessions(); err != nil {
		// GitHub has already rotated the token. Do not serve a replacement
		// that cannot survive restart, or keep using the invalid old token.
		h.sessions.err = err
		return err
	}
	return nil
}

// endGitHubLocked keeps a hash-only tombstone until the factory session's own
// expiry so repeated requests and restarts still explain why sign-in ended.
func (h *Handler) endGitHubLocked(hash string) error {
	s := h.sessions.ids[hash]
	s.githubExpired = true
	s.githubToken, s.githubRefresh = "", ""
	h.sessions.ids[hash] = s
	h.releaseToken(profileHandle(s))
	if err := h.saveSessions(); err != nil {
		h.sessions.err = err
		return err
	}
	return errGitHubExpired
}

func (h *Handler) endGitHub(r *http.Request, token string) {
	h.sessions.mu.Lock()
	defer h.sessions.mu.Unlock()
	for hash, s := range h.sessions.ids {
		if s.githubToken == token {
			if err := h.endGitHubLocked(hash); !errors.Is(err, errGitHubExpired) {
				h.setAuthError(r, err)
				return
			}
		}
	}
	h.setAuthError(r, errGitHubExpired)
}
