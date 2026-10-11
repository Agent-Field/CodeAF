package serve

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// callerHandle resolves credentials before consulting the host's GitHub login.
// An unsigned invite names its inviter without pretending that person is here.
func (h *Handler) callerHandle(r *http.Request) (string, error) {
	if r.Context().Value(workerKey{}) != nil {
		return requestWorker(r).Handle, nil
	}
	if session, ok := h.credential(r); ok {
		if session.profile != nil {
			return session.profile.Handle, nil
		}
		if session.tokenID != "" && session.invitedBy != "" {
			return "invited:" + session.invitedBy, nil
		}
	}
	return h.seam.PersonHandle(r.Context())
}

func (h *Handler) meBody(profile api.Profile, id, by string) api.Me {
	if profile.Scopes == nil {
		profile.Scopes = []string{}
	}
	return api.Me{Factory: h.factory, CodeafVersion: h.factory.Version, Contract: api.Contract,
		Profile: profile, SessionID: id, InvitedBy: by, Machines: []api.Machine{}, TakesUnassigned: map[string]bool{},
		Access: h.accessMode(), PollingEvery: h.pollingEvery(), ServedBy: h.servedBy()}
}

func (h *Handler) teamAllows(handle string) bool {
	if len(h.config.Team) == 0 {
		return true
	}
	for _, member := range h.config.Team {
		if factory.NormalizeHandle(member) == handle {
			return true
		}
	}
	return false
}

// flowCaller binds an exchange to the credential that requested it. The device
// code is a second secret, so even two local clients cannot guess each other's flow.
func flowCaller(r *http.Request) [32]byte {
	if token := bearer(r); token != "" {
		return sha256.Sum256([]byte("bearer:" + token))
	}
	if cookie, err := r.Cookie(accessCookieName); err == nil {
		return sha256.Sum256([]byte("cookie:" + cookie.Value))
	}
	return sha256.Sum256([]byte("loopback"))
}

func (h *Handler) setSessionCookie(w http.ResponseWriter, r *http.Request, credential string, session accessSession) {
	http.SetCookie(w, &http.Cookie{Name: accessCookieName, Value: credential, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil,
		MaxAge: int(accessSessionTTL.Seconds()), Expires: session.expires})
}

// githubJSON only speaks to GitHub's fixed URLs and does not follow redirects
// with credentials. Tests replace the transport, never the published endpoint.
func (h *Handler) githubJSON(r *http.Request, method, endpoint, body, token string, out any) error {
	req, err := http.NewRequestWithContext(r.Context(), method, endpoint, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "codeaf-factory")
	if body != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := h.githubClient()
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("GitHub sign-in is unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub sign-in returned HTTP %d", response.StatusCode)
	}
	return decodeGitHub(response.Body, out)
}

// githubClient is the one HTTP client every GitHub call uses. Tests replace
// the transport and keep the published URLs. Redirects are not followed, so
// a credential cannot be replayed onto a different host.
func (h *Handler) githubClient() http.Client {
	client := http.Client{Timeout: githubTimeout}
	if h.config.GitHubHTTPClient != nil {
		client = *h.config.GitHubHTTPClient
	}
	if client.Timeout == 0 {
		client.Timeout = githubTimeout
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return client
}

// githubStatus is a GitHub status code. A transport failure is an error;
// 404 and 403 are answers, not failures, so the caller can cache them.
func (h *Handler) githubStatus(r *http.Request, method, endpoint, token string) (int, error) {
	req, err := http.NewRequestWithContext(r.Context(), method, endpoint, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "codeaf-factory")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := h.githubClient()
	response, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	return response.StatusCode, nil
}
