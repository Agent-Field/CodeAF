package serve

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

const githubTimeout = 15 * time.Second
const deviceFlowLimit = 256

// githubSignInScope asks for the profile and for private repositories.
// Classic device flow has no read-only private-repository scope, and GitHub
// answers 404 for a private repository the token cannot see.
const githubSignInScope = "read:user repo"

// deviceFlows retains GitHub's secret until exchange or expiry. Only the
// factory's opaque flow secret leaves the service, bound to its original caller.
type deviceFlows struct {
	mu      sync.Mutex
	pending map[string]*deviceFlow
}

type deviceFlow struct {
	mu       sync.Mutex
	code     string
	caller   [32]byte
	expires  time.Time
	next     time.Time
	interval int
	tokenID  string
	consumed bool
}

func decodeGitHub(body io.Reader, out any) error {
	if err := json.NewDecoder(io.LimitReader(body, 1<<20)).Decode(out); err != nil {
		return errors.New("GitHub sign-in returned an invalid response")
	}
	return nil
}

func (h *Handler) startDevice(w http.ResponseWriter, r *http.Request) {
	if h.config.GitHubClientID == "" {
		h.fail(w, http.StatusNotImplemented, "GitHub device sign-in needs github_client_id in the factory host config")
		return
	}
	var body struct{}
	if !h.decode(w, r, &body, true) {
		return
	}
	// Bound pending flows before making a network call, so abandoned sign-ins
	// cannot retain unbounded state in a service that stays up for months.
	h.auth.mu.Lock()
	defer h.auth.mu.Unlock()
	now := h.now()
	for key, flow := range h.auth.pending {
		if !now.Before(flow.expires) {
			delete(h.auth.pending, key)
		}
	}
	if len(h.auth.pending) >= deviceFlowLimit {
		h.fail(w, http.StatusTooManyRequests, "too many pending GitHub sign-ins; try again after a code expires")
		return
	}
	var gh struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		Interval        int    `json:"interval"`
		ExpiresIn       int    `json:"expires_in"`
	}
	err := h.githubJSON(r, http.MethodPost, "https://github.com/login/device/code",
		url.Values{"client_id": {h.config.GitHubClientID}, "scope": {githubSignInScope}}.Encode(), "", &gh)
	if err != nil {
		h.fail(w, http.StatusBadGateway, err.Error())
		return
	}
	if gh.DeviceCode == "" || gh.UserCode == "" || gh.VerificationURI == "" || gh.ExpiresIn <= 0 || gh.Interval <= 0 {
		h.fail(w, http.StatusBadGateway, "GitHub sign-in returned an invalid device code")
		return
	}
	key, err := secret()
	if h.failed(w, err) {
		return
	}
	session, _ := h.credential(r)
	h.auth.pending[key] = &deviceFlow{code: gh.DeviceCode, caller: flowCaller(r),
		expires: now.Add(time.Duration(gh.ExpiresIn) * time.Second), next: now.Add(time.Duration(gh.Interval) * time.Second),
		interval: gh.Interval, tokenID: session.tokenID}
	h.write(w, http.StatusOK, api.Device{Factory: h.factory, DeviceCode: key, UserCode: gh.UserCode,
		VerificationURI: gh.VerificationURI, Interval: gh.Interval, ExpiresIn: gh.ExpiresIn})
}

func (h *Handler) pollDevice(w http.ResponseWriter, r *http.Request) {
	var body api.DevicePoll
	if !h.decode(w, r, &body, false) {
		return
	}
	h.auth.mu.Lock()
	flow := h.auth.pending[body.DeviceCode]
	h.auth.mu.Unlock()
	if flow == nil || flow.caller != flowCaller(r) {
		h.fail(w, http.StatusBadRequest, "unknown device code for this caller")
		return
	}
	flow.mu.Lock()
	defer flow.mu.Unlock()
	if flow.consumed {
		h.fail(w, http.StatusBadRequest, "device code was already exchanged")
		return
	}
	if !h.now().Before(flow.expires) {
		h.forgetDevice(body.DeviceCode, flow)
		h.fail(w, http.StatusGone, "expired_token")
		return
	}
	if h.now().Before(flow.next) {
		h.deviceWaiting(w, "authorization_pending", flow.interval)
		return
	}
	flow.next = h.now().Add(time.Duration(flow.interval) * time.Second)
	var exchange githubExchange
	err := h.githubJSON(r, http.MethodPost, "https://github.com/login/oauth/access_token",
		url.Values{"client_id": {h.config.GitHubClientID}, "device_code": {flow.code},
			"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}}.Encode(), "", &exchange)
	if err != nil {
		h.fail(w, http.StatusBadGateway, err.Error())
		return
	}
	switch exchange.Error {
	case "authorization_pending":
		h.deviceWaiting(w, exchange.Error, flow.interval)
		return
	case "slow_down":
		flow.interval += 5
		flow.next = h.now().Add(time.Duration(flow.interval) * time.Second)
		h.deviceWaiting(w, exchange.Error, flow.interval)
		return
	case "expired_token", "access_denied":
		h.forgetDevice(body.DeviceCode, flow)
		status := http.StatusGone
		if exchange.Error == "access_denied" {
			status = http.StatusForbidden
		}
		h.fail(w, status, exchange.Error)
		return
	}
	if exchange.Error != "" || exchange.AccessToken == "" {
		h.forgetDevice(body.DeviceCode, flow)
		h.fail(w, http.StatusBadGateway, "GitHub did not complete sign-in")
		return
	}
	// The GitHub exchange is single-use. A profile failure requires a new
	// device flow, rather than repeatedly exchanging a consumed code.
	h.forgetDevice(body.DeviceCode, flow)
	var user struct {
		Login     string `json:"login"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := h.githubJSON(r, http.MethodGet, "https://api.github.com/user", "", exchange.AccessToken, &user); err != nil {
		h.fail(w, http.StatusBadGateway, err.Error())
		return
	}
	handle := factory.NormalizeHandle(user.Login)
	if handle == "" {
		h.fail(w, http.StatusBadGateway, "GitHub sign-in returned no login")
		return
	}
	if !h.teamAllows(handle) {
		h.forgetDevice(body.DeviceCode, flow)
		h.fail(w, http.StatusForbidden, "this GitHub handle is not on the factory team")
		return
	}
	profile := api.Profile{Handle: handle, Name: user.Name, AvatarURL: user.AvatarURL,
		Scopes: strings.FieldsFunc(exchange.Scope, func(r rune) bool { return r == ',' || r == ' ' })}
	credential, err := secret()
	if h.failed(w, err) {
		return
	}
	id, err := secret()
	if h.failed(w, err) {
		return
	}
	now := h.now()
	session := accessSession{id: "session_" + id, expires: now.Add(accessSessionTTL), created: now, lastSeen: now,
		profile: &profile, tokenID: flow.tokenID, device: deviceLabel(r)}
	session.setGitHub(exchange, now)
	if flow.tokenID != "" {
		for _, row := range h.tokenRows() {
			if row.ID == flow.tokenID && row.InvitedLogin != "" && row.InvitedLogin != profile.Handle {
				h.fail(w, http.StatusForbidden, "this access link is for @"+row.InvitedLogin)
				return
			}
		}
		if err := h.bindDeviceProfile(r, flow.tokenID, profile, now); h.failed(w, err) {
			return
		}
	}
	h.sessions.mu.Lock()
	for key, old := range h.sessions.ids {
		if !now.Before(old.expires) {
			delete(h.sessions.ids, key)
		}
	}
	h.sessions.ids[sessionHash(credential)] = session
	err = h.saveSessions()
	if err != nil {
		delete(h.sessions.ids, sessionHash(credential))
	}
	h.sessions.mu.Unlock()
	if h.failed(w, err) {
		return
	}
	h.viewer.see(profile, now, now, exchange.AccessToken)
	h.setSessionCookie(w, r, credential, session)
	me := h.meBody(profile, session.id)
	me.Operator = profile.Handle == h.operatorLogin(r)
	h.write(w, http.StatusOK, api.SignedIn{Factory: h.factory, AccessToken: credential, TokenType: "bearer",
		ExpiresIn: int(accessSessionTTL.Seconds()), Me: me})
}

func (h *Handler) forgetDevice(key string, flow *deviceFlow) {
	flow.consumed = true
	h.auth.mu.Lock()
	delete(h.auth.pending, key)
	h.auth.mu.Unlock()
}

func (h *Handler) deviceWaiting(w http.ResponseWriter, state string, interval int) {
	h.write(w, http.StatusAccepted, struct {
		Factory  api.Factory `json:"factory"`
		Error    string      `json:"error"`
		Interval int         `json:"interval"`
	}{h.factory, state, interval})
}

// bindDeviceProfile records an operator claim only after GitHub proves the link's bound login.
func (h *Handler) bindDeviceProfile(r *http.Request, id string, profile api.Profile, now time.Time) error {
	return tokens.Bind(h.config.TokensPath, id, profile, now, func() error {
		if h.config.Record == nil {
			return nil
		}
		_, err := h.config.Record.Append(r.Context(), record.Event{
			Kind: record.OperatorClaimed, Handle: profile.Handle, Object: "factory",
			Actor:  record.Actor{Kind: "person", Name: "@" + profile.Handle, Operator: true},
			Detail: "claimed factory as operator",
		})
		return err
	})
}

// cancelDevice consumes only this caller's pending code; an in-flight poll holds the same lock.
func (h *Handler) cancelDevice(w http.ResponseWriter, r *http.Request) {
	var body api.DevicePoll
	if !h.decode(w, r, &body, false) {
		return
	}
	h.auth.mu.Lock()
	flow := h.auth.pending[body.DeviceCode]
	h.auth.mu.Unlock()
	if flow == nil || flow.caller != flowCaller(r) {
		h.fail(w, http.StatusBadRequest, "unknown device code for this caller")
		return
	}
	flow.mu.Lock()
	defer flow.mu.Unlock()
	if flow.consumed {
		h.fail(w, http.StatusBadRequest, "device code was already exchanged")
		return
	}
	h.forgetDevice(body.DeviceCode, flow)
	h.finish(w, nil)
}
