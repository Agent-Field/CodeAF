package main

// ── codeaf app: PASSKEYS ON AN ATTACHED ORGANIZATION ────────────────────────
//
// The engine's half of appwire.FactoryPasskeys. A ceremony runs in the
// person's browser on the organization server's own page; the engine starts
// it, keeps the verifier, and polls the server for the session the way it
// polls GitHub's device flow.
//
// THE VERIFIER NEVER LEAVES THIS PROCESS. It is 32 bytes from crypto/rand,
// registered with redact and trace the moment it is made, sent only to the
// server's poll and cancel, and never put in an answer. The server was given
// only its SHA-256.
//
// THE SESSION IS HELD AS EVERY OTHER IS: a done sign-in or reset is kept for
// the connection it named (holdAt), a done join attaches the organization
// and keeps its session exactly as accessWith does, and neither ever crosses
// the wire. An add signs nobody in; the person was signed in already.
//
// THE URL IS CHECKED BEFORE A CLIENT OPENS IT: it must be on the very origin
// the engine asked, so a server cannot send the person's browser anywhere
// else.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/trace"
)

var _ appwire.FactoryPasskeys = (*appBackend)(nil)

// appPasskeyFlow is one ceremony this engine started.
type appPasskeyFlow struct {
	caller   string
	purpose  string
	origin   string
	conn     string // the connection for sign_in and add; "" for a join
	verifier string
	expires  time.Time
	busy     bool
}

func (h *appFactoryHub) passkeyFlowsLocked() map[string]*appPasskeyFlow {
	if h.passkeyFlows == nil {
		h.passkeyFlows = map[string]*appPasskeyFlow{}
	}
	now := h.now()
	for id, f := range h.passkeyFlows {
		if !f.busy && !now.Before(f.expires) {
			delete(h.passkeyFlows, id)
		}
	}
	return h.passkeyFlows
}

// passkeyVerifier is a fresh verifier and the challenge the server is given.
func passkeyVerifier() (verifier, challenge string, err error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", "", appwire.Errorf(appwire.CodeEngine, "the engine could not make a passkey sign-in")
	}
	sum := sha256.Sum256(raw[:])
	verifier = base64.RawURLEncoding.EncodeToString(raw[:])
	redact.Register(verifier)
	trace.Secret(verifier)
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

// passkeyURLOn is whether raw is a ceremony page on origin itself.
func passkeyURLOn(raw, origin string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !strings.HasPrefix(u.Path, "/passkey/") {
		return false
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost")) {
		return false
	}
	return u.Scheme+"://"+strings.ToLower(u.Host) == origin
}

func (b *appBackend) FactoryPasskeyStart(ctx context.Context, p appwire.FactoryPasskeyStartParams) (appwire.FactoryPasskeyStarted, error) {
	h := b.factoryHub()
	var origin, token, conn string
	switch p.Purpose {
	case appwire.FactoryPasskeyJoin:
		o, _, err := appFactoryJoin(p.Caller, p.Link)
		if err != nil {
			return appwire.FactoryPasskeyStarted{}, err
		}
		origin = o
	case appwire.FactoryPasskeySignIn:
		row, _, err := h.target(p.Factory)
		if err != nil {
			return appwire.FactoryPasskeyStarted{}, err
		}
		if row.Local || row.ID == appLocalID {
			return appwire.FactoryPasskeyStarted{}, appwire.Errorf(appwire.CodeConflict, "this machine's own factory has no sign-in")
		}
		origin, conn = row.URL, row.ID
	case appwire.FactoryPasskeyAdd:
		row, held, err := h.credentialed(ctx, p.Factory)
		if err != nil {
			return appwire.FactoryPasskeyStarted{}, err
		}
		if held == "" {
			return appwire.FactoryPasskeyStarted{}, appwire.Errorf(appwire.CodeSignedOut, "sign in to this factory")
		}
		origin, token, conn = row.URL, held, row.ID
	}
	if err := h.passkeyRoom(); err != nil {
		return appwire.FactoryPasskeyStarted{}, err
	}
	verifier, challenge, err := passkeyVerifier()
	if err != nil {
		return appwire.FactoryPasskeyStarted{}, err
	}
	r, err := h.send(ctx, origin, token, http.MethodPost, "/v1/auth/passkey/start", api.PasskeyStart{Purpose: p.Purpose, Link: p.Link,
		Username: p.Username, Name: strings.TrimSpace(p.Name), CodeChallenge: challenge}, appFactorySmall, nil)
	if err != nil {
		return appwire.FactoryPasskeyStarted{}, err
	}
	if r.status != http.StatusCreated {
		return appwire.FactoryPasskeyStarted{}, appFactoryFailure(r)
	}
	var started api.PasskeyStarted
	if json.Unmarshal(r.body, &started) != nil || started.Flow == "" || len(started.Flow) > 128 {
		return appwire.FactoryPasskeyStarted{}, appwire.Errorf(appwire.CodeEngine, "the factory sent an answer this app cannot read")
	}
	if !passkeyURLOn(started.URL, origin) {
		return appwire.FactoryPasskeyStarted{}, appwire.Errorf(appwire.CodeEngine, "the factory's passkey page is not on its own address")
	}
	expires := started.ExpiresAt
	if expires.IsZero() || expires.After(h.now().Add(appFactoryFlowLife)) {
		expires = h.now().Add(appFactoryFlowLife)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.passkeyFlowsLocked()[started.Flow] = &appPasskeyFlow{caller: p.Caller, purpose: p.Purpose, origin: origin, conn: conn,
		verifier: verifier, expires: expires}
	return appwire.FactoryPasskeyStarted{Flow: started.Flow, URL: started.URL, ExpiresAt: started.ExpiresAt}, nil
}

// passkeyRoom refuses a new flow while the engine is stopping or its table
// of waiting flows is full.
func (h *appFactoryHub) passkeyRoom() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return appwire.Errorf(appwire.CodeClosed, "the engine is stopping")
	}
	if len(h.passkeyFlowsLocked()) >= appwire.MaxFactoryFlows {
		return appwire.Errorf(appwire.CodeBusy, "too many passkey sign-ins are waiting")
	}
	return nil
}

// claimPasskeyFlow is the caller's own flow, marked busy so one poll at a
// time speaks for it.
func (h *appFactoryHub) claimPasskeyFlow(flow, caller string) (appPasskeyFlow, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	f := h.passkeyFlowsLocked()[flow]
	if f == nil || f.caller != caller {
		return appPasskeyFlow{}, appwire.Errorf(appwire.CodeNotFound, "no such passkey sign-in")
	}
	if f.busy {
		return appPasskeyFlow{}, appwire.Errorf(appwire.CodeBusy, "this passkey sign-in is already being checked")
	}
	f.busy = true
	return *f, nil
}

func (h *appFactoryHub) releasePasskeyFlow(flow string, end bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if f := h.passkeyFlows[flow]; f != nil {
		f.busy = false
		if end {
			delete(h.passkeyFlows, flow)
		}
	}
}

func (b *appBackend) FactoryPasskeyPoll(ctx context.Context, p appwire.FactoryPasskeyPollParams) (appwire.FactoryPasskeyPolled, error) {
	h := b.factoryHub()
	f, err := h.claimPasskeyFlow(p.Flow, p.Caller)
	if err != nil {
		return appwire.FactoryPasskeyPolled{}, err
	}
	r, err := h.send(ctx, f.origin, "", http.MethodPost, "/v1/auth/passkey/poll", api.PasskeyPoll{Flow: p.Flow, Verifier: f.verifier}, appFactorySmall, nil)
	if err != nil {
		h.releasePasskeyFlow(p.Flow, false)
		return appwire.FactoryPasskeyPolled{}, err
	}
	var polled api.PasskeyPolled
	if r.status != http.StatusOK {
		h.releasePasskeyFlow(p.Flow, false)
		return appwire.FactoryPasskeyPolled{}, appFactoryFailure(r)
	}
	if json.Unmarshal(r.body, &polled) != nil {
		h.releasePasskeyFlow(p.Flow, false)
		return appwire.FactoryPasskeyPolled{}, appwire.Errorf(appwire.CodeEngine, "the factory sent an answer this app cannot read")
	}
	message := strings.TrimSpace(redact.Secrets(polled.Message))
	if len(message) > 300 {
		message = message[:300]
	}
	switch polled.State {
	case api.PasskeyPending:
		h.releasePasskeyFlow(p.Flow, false)
		return appwire.FactoryPasskeyPolled{State: appwire.FactoryPasskeyPending}, nil
	case api.PasskeyExpired, api.PasskeyRefused:
		h.releasePasskeyFlow(p.Flow, true)
		return appwire.FactoryPasskeyPolled{State: polled.State, Message: message}, nil
	case api.PasskeyDone:
	default:
		h.releasePasskeyFlow(p.Flow, true)
		return appwire.FactoryPasskeyPolled{}, appwire.Errorf(appwire.CodeEngine, "the factory sent an answer this app cannot read")
	}
	// Done. The flow ends whatever happens next: the server answers a done
	// flow once.
	defer h.releasePasskeyFlow(p.Flow, true)
	if f.purpose == appwire.FactoryPasskeyAdd {
		return appwire.FactoryPasskeyPolled{State: appwire.FactoryPasskeyDone, Factory: f.conn}, nil
	}
	if polled.AccessToken == "" {
		return appwire.FactoryPasskeyPolled{}, appwire.Errorf(appwire.CodeEngine, "the factory finished the sign-in but sent no session")
	}
	redact.Register(polled.AccessToken)
	trace.Secret(polled.AccessToken)
	lives := time.Duration(polled.ExpiresIn) * time.Second
	if f.conn != "" {
		h.holdAt(f.conn, f.origin, polled.AccessToken, lives)
		h.touch(f.conn)
		return appwire.FactoryPasskeyPolled{State: appwire.FactoryPasskeyDone, Factory: f.conn}, nil
	}
	// A join: attach the organization and keep its session, as accessWith
	// does for a sign-up.
	name := ""
	if polled.Factory != nil {
		name = polled.Factory.Name
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return appwire.FactoryPasskeyPolled{}, appwire.Errorf(appwire.CodeClosed, "the engine is stopping")
	}
	if err := h.load(); err != nil {
		return appwire.FactoryPasskeyPolled{}, err
	}
	id := h.idForLocked(f.origin)
	known := false
	for _, row := range h.rows {
		known = known || row.ID == id
	}
	if !known && len(h.rows) >= appwire.MaxFactoryConnections {
		return appwire.FactoryPasskeyPolled{}, appwire.Errorf(appwire.CodeBusy, "this app is attached to as many factories as it can be (%d)", appwire.MaxFactoryConnections)
	}
	s := appFactorySession{token: polled.AccessToken}
	if lives > 0 {
		s.expires = h.now().Add(lives)
	}
	h.sessions[id] = s
	h.keepSessionLocked(id, f.origin, s)
	if err := h.attachLocked(id, f.origin, name); err != nil {
		return appwire.FactoryPasskeyPolled{}, err
	}
	return appwire.FactoryPasskeyPolled{State: appwire.FactoryPasskeyDone, Factory: id}, nil
}

func (b *appBackend) FactoryPasskeyCancel(ctx context.Context, p appwire.FactoryPasskeyPollParams) error {
	h := b.factoryHub()
	f, err := h.claimPasskeyFlow(p.Flow, p.Caller)
	if err != nil {
		// Nothing of this caller's is waiting: cancelled already.
		return nil
	}
	defer h.releasePasskeyFlow(p.Flow, true)
	// Best effort: the server's flow ends on its own in ten minutes anyway.
	_, _ = h.send(ctx, f.origin, "", http.MethodPost, "/v1/auth/passkey/cancel", api.PasskeyPoll{Flow: p.Flow, Verifier: f.verifier}, appFactorySmall, nil)
	return nil
}

func (b *appBackend) FactoryPasskeyList(ctx context.Context, p appwire.FactoryPasskeyListParams) (appwire.FactoryPasskeyList, error) {
	h := b.factoryHub()
	row, token, err := h.credentialed(ctx, p.Factory)
	if err != nil {
		return appwire.FactoryPasskeyList{}, err
	}
	r, err := h.send(ctx, row.URL, token, http.MethodGet, "/v1/me/passkeys", nil, appFactorySmall, nil)
	if err != nil {
		return appwire.FactoryPasskeyList{}, err
	}
	if r.status != http.StatusOK {
		return appwire.FactoryPasskeyList{}, appFactoryFailure(r)
	}
	var list api.PasskeyList
	if json.Unmarshal(r.body, &list) != nil {
		return appwire.FactoryPasskeyList{}, appwire.Errorf(appwire.CodeEngine, "the factory sent an answer this app cannot read")
	}
	out := appwire.FactoryPasskeyList{Passkeys: []appwire.FactoryPasskey{}, Password: list.Password}
	for _, pk := range list.Passkeys {
		out.Passkeys = append(out.Passkeys, appwire.FactoryPasskey{ID: pk.ID, Name: pk.Name, CreatedAt: pk.CreatedAt,
			LastUsedAt: pk.LastUsedAt, Synced: pk.Synced})
	}
	return out, nil
}

func (b *appBackend) FactoryPasskeyRemove(ctx context.Context, p appwire.FactoryPasskeyRemoveParams) error {
	h := b.factoryHub()
	row, token, err := h.credentialed(ctx, p.Factory)
	if err != nil {
		return err
	}
	r, err := h.send(ctx, row.URL, token, http.MethodPost, "/v1/me/passkeys/remove", api.PasskeyRemove{ID: p.ID}, appFactorySmall, nil)
	if err != nil {
		return appwire.Errorf(appwire.CodeUncertain, "the factory did not confirm the removal; list your passkeys again")
	}
	switch r.status {
	case http.StatusNoContent, http.StatusOK:
		return nil
	case http.StatusConflict:
		var body struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(r.body, &body)
		words := strings.TrimSpace(body.Error)
		if words == "" || len(words) > 300 {
			words = "This is your only way to sign in. Add a password or another passkey first."
		}
		return appwire.Errorf(appwire.CodeConflict, "%s", words)
	}
	return appFactoryFailure(r)
}
