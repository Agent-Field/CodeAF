package main

// ── codeaf app: USERNAME + PASSWORD ON AN ATTACHED ORGANIZATION ─────────────
//
// The engine's half of appwire.FactoryPassword. A sign-up or a reset goes
// through the factory's /v1/factory-access exactly as an access link's GitHub
// sign-in does (accessWith), and a sign-in again goes to /v1/auth/password.
// Every session the factory grants is held the way access() holds one: in
// h.sessions, kept for the app's core by keepSessionLocked, and never put in
// an answer.
//
// THE PASSWORD IS REGISTERED WITH redact AND trace THE MOMENT IT ARRIVES, so
// any line any part of this process writes afterwards has it taken out; it is
// held only for the one request, and no error here is built from it.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/trace"
)

var _ appwire.FactoryPassword = (*appBackend)(nil)

// appSignInMiss is how long a factory whose /v1/about could not be read is
// left unasked, so a list of unreachable factories does not wait twice.
const appSignInMiss = 30 * time.Second

type appFactorySignInCap struct {
	methods []string
	expires time.Time
}

// coverSecret takes a password out of everything this process writes while
// it is being used. It is a passing secret: the latest few stay covered, and
// one under eight bytes is not registered, since it would redact ordinary
// words (the server refuses it anyway; nothing here logs it).
func coverSecret(values ...string) {
	for _, v := range values {
		redact.RegisterPassing(v)
		trace.SecretPassing(v)
	}
}

// signInFor is how a person signs in to origin, from a cached /v1/about.
// nil means the factory has not said, which reads as GitHub only.
func (h *appFactoryHub) signInFor(ctx context.Context, origin string) []string {
	if methods, ok := h.cachedSignIn(origin); ok {
		return methods
	}
	methods, read := h.readSignIn(ctx, origin)
	h.mu.Lock()
	defer h.mu.Unlock()
	life := appFactoryFlowLife
	if !read {
		life = appSignInMiss
	}
	h.rememberSignInForLocked(origin, methods, life)
	return methods
}

// cachedSignIn is what origin last said about sign-in, while it is fresh.
func (h *appFactoryHub) cachedSignIn(origin string) ([]string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	c, ok := h.signInCaps[origin]
	if !ok || !h.now().Before(c.expires) {
		return nil, false
	}
	return c.methods, true
}

func (h *appFactoryHub) readSignIn(ctx context.Context, origin string) ([]string, bool) {
	r, err := h.send(ctx, origin, "", http.MethodGet, "/v1/about", nil, appFactorySmall, nil)
	if err != nil || r.status != http.StatusOK {
		return nil, false
	}
	var about struct {
		SignIn []string `json:"sign_in"`
	}
	if json.Unmarshal(r.body, &about) != nil {
		return nil, false
	}
	return cleanSignIn(about.SignIn), true
}

// cleanSignIn keeps only the two words a client acts on, once each, in order.
func cleanSignIn(in []string) []string {
	if in == nil {
		return nil
	}
	out := []string{}
	for _, m := range in {
		if (m == "password" || m == "github") && !containsString(out, m) {
			out = append(out, m)
		}
	}
	return out
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// rememberSignInLocked keeps what an access answer said. Callers hold h.mu.
func (h *appFactoryHub) rememberSignInLocked(origin string, methods []string) {
	h.rememberSignInForLocked(origin, cleanSignIn(methods), appFactoryFlowLife)
}

func (h *appFactoryHub) rememberSignInForLocked(origin string, methods []string, life time.Duration) {
	if h.signInCaps == nil {
		h.signInCaps = map[string]appFactorySignInCap{}
	}
	if _, ok := h.signInCaps[origin]; !ok && len(h.signInCaps) >= appwire.MaxFactoryFlows {
		now := h.now()
		for k, c := range h.signInCaps {
			if !now.Before(c.expires) {
				delete(h.signInCaps, k)
			}
		}
		if len(h.signInCaps) >= appwire.MaxFactoryFlows {
			return
		}
	}
	h.signInCaps[origin] = appFactorySignInCap{methods: methods, expires: h.now().Add(life)}
}

func (b *appBackend) FactoryAccessSignUp(ctx context.Context, p appwire.FactoryAccessSignUpParams) (appwire.FactoryAccess, error) {
	coverSecret(p.Password)
	return b.factoryHub().accessWith(ctx, p.Caller, p.Link, api.FactoryAccessRequest{Verb: "sign_up",
		Username: p.Username, Password: p.Password, Name: strings.TrimSpace(p.Name)})
}

func (b *appBackend) FactoryAccessReset(ctx context.Context, p appwire.FactoryAccessResetParams) (appwire.FactoryAccess, error) {
	coverSecret(p.Password)
	return b.factoryHub().accessWith(ctx, p.Caller, p.Link, api.FactoryAccessRequest{Verb: "reset", Password: p.Password})
}

// passwordFailure is the wire error for a password door's refusal: the
// factory's own sentence, which never names the password.
func passwordFailure(r appFactoryReply) error {
	var body struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(r.body, &body)
	words := strings.TrimSpace(redact.Secrets(body.Error))
	if len(words) > 300 {
		words = words[:300]
	}
	switch r.status {
	case http.StatusUnauthorized, http.StatusForbidden:
		if words == "" {
			words = "That username and password don't match."
		}
		return appwire.Errorf(appwire.CodeForbidden, "%s", words)
	case http.StatusNotFound:
		return appwire.Errorf(appwire.CodeNotFound, "this organization does not take passwords")
	case http.StatusConflict:
		// The password was changed by another sign-in meanwhile.
		if words == "" {
			words = "Your password was changed meanwhile; sign in again."
		}
		return appwire.Errorf(appwire.CodeConflict, "%s", words)
	}
	return appFactoryFailure(r)
}

func (b *appBackend) FactoryPasswordSignIn(ctx context.Context, p appwire.FactoryPasswordSignInParams) (appwire.FactoryPasswordSignedIn, error) {
	coverSecret(p.Password)
	h := b.factoryHub()
	row, _, err := h.target(p.Factory)
	if err != nil {
		return appwire.FactoryPasswordSignedIn{}, err
	}
	if row.Local || row.ID == appLocalID {
		return appwire.FactoryPasswordSignedIn{}, appwire.Errorf(appwire.CodeConflict, "this machine's own factory has no sign-in")
	}
	// No bearer: the password is the whole proof, and the session it makes
	// replaces whatever this connection held.
	// The device token, when this machine has one, lets the factory count the
	// try by this machine rather than against the username's ceiling.
	r, err := h.send(ctx, row.URL, "", http.MethodPost, "/v1/auth/password",
		api.PasswordSignIn{Username: p.Username, Password: p.Password, Device: h.deviceFor(row.ID)}, appFactorySmall, nil)
	if err != nil {
		return appwire.FactoryPasswordSignedIn{}, err
	}
	if r.status != http.StatusOK {
		return appwire.FactoryPasswordSignedIn{}, passwordFailure(r)
	}
	var signed api.PasswordSignedIn
	if json.Unmarshal(r.body, &signed) != nil || signed.AccessToken == "" {
		return appwire.FactoryPasswordSignedIn{}, appwire.Errorf(appwire.CodeEngine, "the factory sent an answer this app cannot read")
	}
	h.holdDevice(row.ID, signed.DeviceToken)
	h.holdAt(row.ID, row.URL, signed.AccessToken, time.Duration(signed.ExpiresIn)*time.Second)
	h.touch(row.ID)
	me := signed.Me
	return appwire.FactoryPasswordSignedIn{State: appwire.FactorySignedInState, Me: appwire.NewFactoryMe(me)}, nil
}

func (b *appBackend) FactoryPasswordChange(ctx context.Context, p appwire.FactoryPasswordChangeParams) error {
	coverSecret(p.Current, p.New)
	h := b.factoryHub()
	row, token, err := h.credentialed(ctx, p.Factory)
	if err != nil {
		return err
	}
	if token == "" {
		return appwire.Errorf(appwire.CodeSignedOut, "sign in to this factory")
	}
	r, err := h.send(ctx, row.URL, token, http.MethodPost, "/v1/me/password",
		api.PasswordChange{Current: p.Current, New: p.New}, appFactorySmall, nil)
	if err != nil {
		// The change may or may not have been made; the person can sign in
		// with whichever password works.
		return appwire.Errorf(appwire.CodeUncertain, "the factory did not confirm the change; try signing in with the new password")
	}
	switch r.status {
	case http.StatusNoContent:
		return nil
	case http.StatusOK:
		// A change revokes every device token of the old password; the answer
		// carries this machine's new one, kept beside the sign-in that stays.
		var changed api.PasswordChanged
		if json.Unmarshal(r.body, &changed) == nil && changed.DeviceToken != "" {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.holdDeviceLocked(row.ID, changed.DeviceToken)
			if s, ok := h.sessions[row.ID]; ok && s.token == token {
				h.keepSessionLocked(row.ID, row.URL, s)
			}
		}
		return nil
	case http.StatusForbidden, http.StatusConflict:
		return passwordFailure(r)
	}
	return appFactoryFailure(r)
}
