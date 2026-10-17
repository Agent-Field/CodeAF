package main

// ── codeaf app: SIGNING OUT, AND REVOKING EVERY SESSION ─────────────────────
//
// The engine's half of appwire.FactoryAdmin. Each method makes ONE write to
// ONE route of the factory's /v1 API, spelled here as a constant; the path and
// the verb are never taken from the app.
//
// SIGN-OUT ENDS THE SIGN-IN THIS APP HOLDS AND NOTHING ELSE. The engine asks
// the factory who the held credential is (GET /v1/me), takes the id of that
// session from the answer, and deletes exactly that id (DELETE
// /v1/me/sessions/{id}). The id is read, used once and dropped inside this
// file: it is never an input, never an output, and never in an error. The
// connection's row stays, so the person can sign in again.
//
// REVOKE-ALL IS THE OPERATOR'S OWN REQUEST. It is POST /v1/access/revoke-sessions
// with the credential this app already holds for that factory, which the
// factory accepts only from its operator. Nothing is substituted: no host key
// is looked up, no loopback-owner fallback is used (a connection with no
// credential is refused before anything is sent), and a caller the factory has
// not told us is the operator is refused without a request.
//
// A WRITE IS SENT ONCE. There is no retry. A connection that never opened is
// "unreachable" (nothing was sent); a request that may have arrived and got no
// answer, or got a 5xx, is appwire.CodeUncertain and claims neither outcome.
// The held credential is dropped ONLY after the factory said yes, and only if
// it is still the credential that was used (a sign-in completed meanwhile is
// kept). An uncertain outcome keeps it: dropping a credential the factory may
// still honour would leave a live sign-in nobody can end.
//
// NOTHING BRINGS A SIGN-OUT BACK. A dropped credential is replaced only by a
// person signing in or attaching with a key. The machine's own factory is
// refused here, so restarting or resuming it (which makes a new key for it)
// never undoes a sign-out, and this file never reads the terminal's attach
// record or the host key.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

var _ appwire.FactoryAdmin = (*appBackend)(nil)

const (
	appFactoryRevokeRoute   = "/v1/access/revoke-sessions"
	appFactorySessionsRoute = "/v1/me/sessions/"
)

// adminWho is the held credential, as the factory describes it.
type adminWho struct {
	row   appFactoryRow
	token string
	me    api.Me
}

// ownSession is the id of the sign-in the held credential is, or "" when it
// is a key. It stays inside this file.
func (w adminWho) ownSession() string {
	if appwire.FactorySessionCanEnd(w.me.SessionID) {
		return w.me.SessionID
	}
	return ""
}

// rowOf is the saved row id names, without waking the local factory.
func (h *appFactoryHub) rowOf(id string) (appFactoryRow, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return appFactoryRow{}, appwire.Errorf(appwire.CodeClosed, "the engine is stopping")
	}
	if err := h.load(); err != nil {
		return appFactoryRow{}, err
	}
	for _, row := range h.rows {
		if row.ID == id {
			return row, nil
		}
	}
	return appFactoryRow{}, appwire.Errorf(appwire.CodeNotFound, "no such factory")
}

// claimAdmin lets one admin write per connection be in flight, so a double
// press is one request. The release is idempotent.
func (h *appFactoryHub) claimAdmin(id string) (release func(), err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.adminBusy == nil {
		h.adminBusy = map[string]bool{}
	}
	if h.adminBusy[id] {
		return nil, appwire.Errorf(appwire.CodeBusy, "that is already being done; wait for it to finish")
	}
	h.adminBusy[id] = true
	return func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.adminBusy, id)
	}, nil
}

// dropSession forgets the credential held for id, but only if it is still
// token. A newer sign-in is never forgotten by an older write's answer.
func (h *appFactoryHub) dropSession(id, token string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if s, ok := h.sessions[id]; ok && s.token == token {
		delete(h.sessions, id)
		h.dropHeldLocked(id)
	}
}

// whoHolds resolves the connection and asks the factory who the held
// credential is. It refuses the machine's own factory and a connection with
// nothing held before any request leaves.
func (b *appBackend) whoHolds(ctx context.Context, id string) (adminWho, error) {
	h := b.factoryHub()
	row, err := h.rowOf(id)
	if err != nil {
		return adminWho{}, err
	}
	if row.Local || row.ID == appLocalID {
		return adminWho{}, appwire.Errorf(appwire.CodeConflict, "the factory on this machine belongs to this app and has no sign-in to end")
	}
	row, token, err := h.target(id)
	if err != nil {
		return adminWho{}, err
	}
	if token == "" {
		return adminWho{}, appwire.Errorf(appwire.CodeSignedOut, "sign in to this factory")
	}
	var me api.Me
	r, err := h.send(ctx, row.URL, token, http.MethodGet, "/v1/me", nil, appFactorySmall, nil)
	if err != nil {
		return adminWho{}, err
	}
	if r.status < 200 || r.status >= 300 {
		return adminWho{}, adminReadFailure(r)
	}
	if json.Unmarshal(r.body, &me) != nil {
		return adminWho{}, appwire.Errorf(appwire.CodeEngine, "the factory sent an answer this app cannot read")
	}
	h.touch(id)
	return adminWho{row: row, token: token, me: me}, nil
}

// adminWrite sends one request with the credential whoHolds resolved, once.
// A reply of any status is returned for the caller to read; the error is for a
// request with no reply at all.
func (h *appFactoryHub) adminWrite(ctx context.Context, w adminWho, method, path, what string) (appFactoryReply, error) {
	req, err := http.NewRequestWithContext(ctx, method, w.row.URL+path, nil)
	if err != nil {
		return appFactoryReply{}, appwire.Errorf(appwire.CodeBadParams, "that is not a request a factory can take")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "codeaf-app")
	req.Header.Set("Authorization", "Bearer "+w.token)
	resp, err := h.client.Do(req)
	if err != nil {
		// The Go error names the address and, for a sign-out, the session's id,
		// so only the fact is passed on. Only a refused connection proves
		// nothing was sent.
		var op *net.OpError
		if errors.As(err, &op) && op.Op == "dial" {
			return appFactoryReply{}, appwire.Errorf(appwire.CodeUnreachable, "the factory is not answering")
		}
		return appFactoryReply{}, appwire.Errorf(appwire.CodeUncertain, "the factory did not confirm %s; it may have happened, so check whether you are still signed in before trying again", what)
	}
	defer resp.Body.Close()
	// A status was received, so whether the factory acted is known; an
	// unreadable body only costs the words.
	data, _ := io.ReadAll(io.LimitReader(resp.Body, appFactorySmall+1))
	if int64(len(data)) > appFactorySmall {
		data = nil
	}
	return appFactoryReply{status: resp.StatusCode, header: resp.Header, body: data}, nil
}

// adminReadFailure keeps only the status and the documented expiry reason.
// A remote error may echo the private session path or other server state;
// arbitrary error prose never belongs in this narrow public admin result.
func adminReadFailure(r appFactoryReply) error {
	var detail struct {
		Reason string `json:"reason"`
	}
	if r.status == http.StatusUnauthorized {
		_ = json.Unmarshal(r.body, &detail)
	}
	r.body = nil
	if detail.Reason == api.ReasonGitHubExpired {
		r.body = []byte(`{"reason":"github_expired"}`)
	}
	return appFactoryFailure(r)
}

// adminFailure is the wire error for a refusal of an admin write. ONLY A REPLY
// THAT CARRIES api.PhaseHeader is a refusal: the factory changes its sessions
// and then records the event, and a failure of that second step comes back as a
// 403, 404 or 5xx. Any other failure may follow the change, so it claims neither
// outcome.
func adminFailure(r appFactoryReply, what string) error {
	switch {
	case r.header.Get(api.PhaseHeader) != api.PhasePreMutation || r.status < 400 || (r.status >= 500 && r.status != http.StatusNotImplemented):
		return appwire.Errorf(appwire.CodeUncertain, "the factory failed while %s; it may have happened, so check whether you are still signed in before trying again", what)
	case r.status == http.StatusForbidden:
		return appwire.Errorf(appwire.CodeForbidden, "the factory does not let you do that")
	case r.status == http.StatusNotImplemented:
		return appwire.Errorf(appwire.CodeConflict, "this factory does not provide that action")
	}
	return adminReadFailure(r)
}

// The documented synchronous routes complete with 200 or 204. Accepted,
// partial and other unexpected success statuses do not prove the action ended.
func completedAdminStatus(status int) bool {
	return status == http.StatusOK || status == http.StatusNoContent
}

func (b *appBackend) FactorySignOut(ctx context.Context, p appwire.FactoryAdminParams) (appwire.FactoryAdminDone, error) {
	h := b.factoryHub()
	release, err := h.claimAdmin(p.Factory)
	if err != nil {
		return appwire.FactoryAdminDone{}, err
	}
	defer release()
	who, err := b.whoHolds(ctx, p.Factory)
	if err != nil {
		return appwire.FactoryAdminDone{}, err
	}
	session := who.ownSession()
	if session == "" {
		return appwire.FactoryAdminDone{}, appwire.Errorf(appwire.CodeConflict, "this connection uses a key, not a sign-in, so there is nothing to sign out of")
	}
	r, err := h.adminWrite(ctx, who, http.MethodDelete, appFactorySessionsRoute+url.PathEscape(session), "the sign-out")
	if err != nil {
		return appwire.FactoryAdminDone{}, err
	}
	if !completedAdminStatus(r.status) {
		return appwire.FactoryAdminDone{}, adminFailure(r, "signing out")
	}
	h.dropSession(p.Factory, who.token)
	h.touch(p.Factory)
	return appwire.FactoryAdminDone{Factory: p.Factory, Action: appwire.AdminActSignOut, SignedOut: true}, nil
}

func (b *appBackend) FactoryRevokeSessions(ctx context.Context, p appwire.FactoryAdminParams) (appwire.FactoryAdminDone, error) {
	h := b.factoryHub()
	release, err := h.claimAdmin(p.Factory)
	if err != nil {
		return appwire.FactoryAdminDone{}, err
	}
	defer release()
	who, err := b.whoHolds(ctx, p.Factory)
	if err != nil {
		return appwire.FactoryAdminDone{}, err
	}
	// THE FACTORY ANSWERS A NON-OPERATOR WITH 401, which reads as "signed out",
	// so the refusal is made here, in words, before anything is sent.
	if !who.me.Operator {
		return appwire.FactoryAdminDone{}, appwire.Errorf(appwire.CodeForbidden, "only the operator can sign everyone out")
	}
	r, err := h.adminWrite(ctx, who, http.MethodPost, appFactoryRevokeRoute, "revoking sessions")
	if err != nil {
		return appwire.FactoryAdminDone{}, err
	}
	if !completedAdminStatus(r.status) {
		return appwire.FactoryAdminDone{}, adminFailure(r, "revoking sessions")
	}
	// The caller's own sign-in died with the rest. A key does not: it is the
	// factory's own credential, not a session, and is kept.
	out := appwire.FactoryAdminDone{Factory: p.Factory, Action: appwire.AdminActRevokeSessions}
	if who.ownSession() != "" {
		h.dropSession(p.Factory, who.token)
		out.SignedOut = true
	}
	// Whoever held a made invitation for a retry no longer has the standing to.
	h.dropHeld(p.Factory)
	h.touch(p.Factory)
	return out, nil
}
