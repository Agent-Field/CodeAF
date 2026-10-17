package main

// ── codeaf app: TURNING A REPOSITORY ON OR OFF ──────────────────────────────
//
// The engine's half of appwire.FactoryRepoAdmin. It makes ONE write to ONE
// route of the factory's /v1 API, PUT /v1/repos/{owner}/{repo} with {"on": …},
// spelled here as a constant: the path and the verb are never taken from the
// app, and the owner and name are validated by appwire and escaped here. It
// does not edit the factory's repository configuration, write to GitHub or
// touch a job, a checkout or a folder: turning a repository off only removes it
// from the factory's watched list.
//
// WHO MAY: the OPERATOR, with the credential this app holds for that connection
// right now, asked again through GET /v1/me on every call. The machine's own
// solo factory is allowed (the app made its key and holds it in memory), which
// is why this file does not use whoHolds: that refuses the local row, because
// it has no sign-in to end. What is common is that a credential must be HELD.
// A connection with none is refused before a request leaves, a team factory
// as signed_out and the local row as not running for this app; nothing here
// asks the factory with no bearer, because a loopback team service answers a
// request with no bearer as its own operator, and nothing looks up the host's
// key or the terminal's attach record.
//
// A WRITE IS SENT ONCE. There is no retry. A connection that never opened is
// "unreachable" (nothing was sent). A request that may have arrived and got no
// answer, a 5xx, or a refusal WITHOUT the phase marker (api.PhaseHeader) is
// appwire.CodeUncertain: the factory saves the list and then records the
// event, and the second step can fail with a status that looks like a refusal.
// Only 200 or 204 is completed. Every wait is followed by a check that the
// connection still holds the same credential; an answer that arrives for a
// caller who has since been replaced reveals nothing.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

var _ appwire.FactoryRepoAdmin = (*appBackend)(nil)

const appFactoryRepoRoute = "/v1/repos/"

// heldWho resolves the connection and asks the factory who the credential held
// right now is. Unlike whoHolds it accepts the machine's own factory, and it
// never proceeds without a held credential.
func (b *appBackend) heldWho(ctx context.Context, id string) (adminWho, error) {
	h := b.factoryHub()
	row, token, err := h.target(id)
	if err != nil {
		return adminWho{}, err
	}
	if token == "" {
		if row.Local || row.ID == appLocalID {
			return adminWho{}, appwire.Errorf(appwire.CodeUnreachable, "the local factory is not running for this app; start it again")
		}
		return adminWho{}, appwire.Errorf(appwire.CodeSignedOut, "sign in to this factory")
	}
	r, err := h.send(ctx, row.URL, token, http.MethodGet, "/v1/me", nil, appFactorySmall, nil)
	if err != nil {
		return adminWho{}, err
	}
	// What the factory said describes the credential it was asked with. If that
	// is no longer the connection's, none of it is used.
	if err := h.staleCredential(id, token); err != nil {
		return adminWho{}, err
	}
	if r.status < 200 || r.status >= 300 {
		return adminWho{}, adminReadFailure(r)
	}
	var me api.Me
	if json.Unmarshal(r.body, &me) != nil {
		return adminWho{}, appwire.Errorf(appwire.CodeEngine, "the factory sent an answer this app cannot read")
	}
	return adminWho{row: row, token: token, me: me}, nil
}

// repoWho is heldWho for the operator alone.
func (b *appBackend) repoWho(ctx context.Context, id string) (adminWho, error) {
	who, err := b.heldWho(ctx, id)
	if err != nil {
		return adminWho{}, err
	}
	// THE FACTORY ANSWERS A NON-OPERATOR'S WRITE WITH 403 OR 401, which can read
	// as "signed out", so the refusal is made here, in words, before anything
	// is sent.
	if !who.me.Operator {
		return adminWho{}, appwire.Errorf(appwire.CodeForbidden, "only the operator can change which repositories the factory watches")
	}
	return who, nil
}

func (b *appBackend) FactoryRepoSet(ctx context.Context, p appwire.FactoryRepoSetParams) (appwire.FactoryRepoSetDone, error) {
	h := b.factoryHub()
	release, err := h.claimAdmin(p.Factory)
	if err != nil {
		return appwire.FactoryRepoSetDone{}, err
	}
	defer release()
	who, err := b.repoWho(ctx, p.Factory)
	if err != nil {
		return appwire.FactoryRepoSetDone{}, err
	}
	// The wait for who is over; the write is made only for the caller who asked.
	if !h.callerIs(p.Factory, who.token) {
		return appwire.FactoryRepoSetDone{}, appwire.Errorf(appwire.CodeConflict, "the sign-in changed while this was loading; try again")
	}
	owner, name, _ := strings.Cut(p.Repo, "/")
	path := appFactoryRepoRoute + url.PathEscape(owner) + "/" + url.PathEscape(name)
	body := struct {
		On bool `json:"on"`
	}{On: *p.On}
	r, err := h.linkDo(ctx, who, http.MethodPut, path, body, appFactorySmall)
	switch {
	case err == errLinkNotSent:
		return appwire.FactoryRepoSetDone{}, appwire.Errorf(appwire.CodeUnreachable, "the factory is not answering")
	case err != nil:
		return appwire.FactoryRepoSetDone{}, repoUncertain()
	}
	// Only a refusal that proves nothing was stored may be called one; it
	// reveals nothing scoped, so a caller who changed meanwhile is told so.
	if r.provedNoWrite() {
		if !h.callerIs(p.Factory, who.token) {
			return appwire.FactoryRepoSetDone{}, appwire.Errorf(appwire.CodeConflict, "the sign-in changed while this was loading; try again")
		}
		return appwire.FactoryRepoSetDone{}, linkRefusal(r, "that repository is not one this factory can see")
	}
	if !h.callerIs(p.Factory, who.token) || !completedAdminStatus(r.status) {
		// Completed for a caller who is no longer the connection's, or not
		// proven either way: claim neither, and name nothing.
		return appwire.FactoryRepoSetDone{}, repoUncertain()
	}
	h.touch(p.Factory)
	return appwire.FactoryRepoSetDone{Factory: p.Factory, Repo: p.Repo, On: *p.On}, nil
}

func repoUncertain() error {
	return appwire.Errorf(appwire.CodeUncertain, "the factory did not confirm the change; it may have happened, so read the repository list before trying again")
}
