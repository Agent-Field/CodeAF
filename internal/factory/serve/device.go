package serve

import (
	"context"
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
	consumed, joinLocked := false, false
	defer func() {
		if joinLocked {
			h.joinMu.Unlock()
		}
	}()
	if flow.tokenID != "" {
		for _, row := range h.tokenRows() {
			if row.ID == flow.tokenID && row.InvitedLogin != "" && row.InvitedLogin != profile.Handle {
				h.fail(w, http.StatusForbidden, "this access link is for @"+row.InvitedLogin)
				return
			}
			// The link is judged again now that GitHub says who this is: it may
			// have been revoked, expired or filled while they were signing in.
			// Someone already counted is not turned away for being late.
			if row.ID == flow.tokenID && row.Human() && !row.Has(profile.Handle) {
				if words := linkRefusal(row.Reason(now)); words != "" {
					h.fail(w, http.StatusForbidden, words)
					return
				}
			}
		}
		// A join link must not be consumed by a person whose GitHub sees no watched repository.
		for _, row := range h.tokenRows() {
			if row.ID != flow.tokenID || !row.AccessLink || row.Operator || h.seam.Load == nil {
				continue
			}
			snap, loadErr := h.seam.Load()
			if h.failed(w, loadErr) {
				return
			}
			if h.config.Store != nil {
				watched, e := h.config.Store.Repos()
				if h.failed(w, e) {
					return
				}
				snap = withWatchedRepos(snap, watched)
			}
			allowed := len(snap.Repos) == 0
			for _, repo := range snap.Repos {
				status, e := h.githubStatus(r, "GET", "https://api.github.com/repos/"+repo.Name, exchange.AccessToken)
				if e != nil {
					h.fail(w, 502, "GitHub repository access is unavailable")
					return
				}
				if status == 200 {
					allowed = true
					break
				}
			}
			if !allowed {
				h.fail(w, 403, linkNoRepoWords)
				return
			}
		}
		// One sign-in at a time per factory is between taking a use and
		// keeping or returning it, so a rollback can see every other session
		// of the same person and never uncounts one that succeeded.
		h.joinMu.Lock()
		joinLocked = true
		var err error
		consumed, err = h.bindDeviceProfile(r, flow.tokenID, profile, now)
		if words := joinRefusal(err); words != "" {
			h.fail(w, http.StatusForbidden, words)
			return
		}
		if h.failed(w, err) {
			return
		}
	}
	// The link's sign-in is judged by what this very session would see, while
	// the use is still ours to return. The session is NOT in the store yet, so
	// no save by anyone else can write it down before the verdict: the check
	// reads it from the request context. The verdict is the one the card
	// shows; it is never asked a second time later.
	if joinLocked && h.humanLink(flow.tokenID) {
		check := r.WithContext(context.WithValue(r.Context(), trialSessionKey{}, &session))
		check.Header = r.Header.Clone()
		check.Header.Del("Cookie")
		check.Header.Del("Authorization")
		seen := api.FactoryAccess{State: "joined"}
		h.accessRepos(&seen, check)
		if seen.State == "no_access" {
			if consumed {
				h.giveBackUse(flow.tokenID, profile.Handle)
			}
			h.fail(w, http.StatusForbidden, linkNoRepoWords)
			return
		}
		if receipt, ok := r.Context().Value(joinReceiptKey{}).(*joinReceipt); ok {
			receipt.repos = seen.Factory.Repos
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
	if err != nil {
		// No session was made, so the use this sign-in took goes back.
		if consumed {
			h.giveBackUse(flow.tokenID, profile.Handle)
		}
		h.failed(w, err)
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
// The first result is whether a use of a human link was spent; a person who
// was already in, a plain token and an operator claim spend none.
func (h *Handler) bindDeviceProfile(r *http.Request, id string, profile api.Profile, now time.Time) (bool, error) {
	joined, err := tokens.Join(h.config.TokensPath, id, profile, now, func() error {
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
	return joined.Consumed, err
}

type joinReceiptKey struct{}

// trialSessionKey carries a sign-in that is not stored yet, so its repository
// check can run with its own GitHub credential without the session ever
// reaching the store (and so the disk) before the check passes.
type trialSessionKey struct{}

// joinReceipt carries the repositories pollDevice already judged to the
// access card that called it, so the card does not look a second time.
type joinReceipt struct{ repos []string }

func (h *Handler) humanLink(id string) bool {
	for _, row := range h.tokenRows() {
		if row.ID == id {
			return row.Human()
		}
	}
	return false
}

// giveBackUse returns the use a failed sign-in took. Another live session of
// the same person on the same link means they are still in, so nothing goes
// back. Call with joinMu held.
func (h *Handler) giveBackUse(tokenID, handle string) {
	h.sessions.mu.Lock()
	for _, s := range h.sessions.ids {
		if s.tokenID == tokenID && s.profile != nil && s.profile.Handle == handle && h.sessionUsable(s) {
			h.sessions.mu.Unlock()
			return
		}
	}
	h.sessions.mu.Unlock()
	_ = tokens.Unjoin(h.config.TokensPath, tokenID, handle)
}

// The sentences a refused join carries. link_state reads the 403 body for
// them, so these are the one spelling.
const (
	linkRevokedWords = "This link was revoked."
	linkExpiredWords = "This link has expired."
	linkUsedUpWords  = "This link has no uses left."
	linkNoRepoWords  = "Your GitHub cannot see this factory's repositories."
)

func linkRefusal(reason string) string {
	switch reason {
	case "revoked":
		return linkRevokedWords
	case "expired":
		return linkExpiredWords
	case "used_up":
		return linkUsedUpWords
	}
	return ""
}

// joinRefusal maps a typed refusal from tokens.Join to its sentence, or "".
func joinRefusal(err error) string {
	switch {
	case errors.Is(err, tokens.ErrRevoked):
		return linkRevokedWords
	case errors.Is(err, tokens.ErrExpired):
		return linkExpiredWords
	case errors.Is(err, tokens.ErrUsedUp):
		return linkUsedUpWords
	}
	return ""
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
