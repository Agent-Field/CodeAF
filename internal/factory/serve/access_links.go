package serve

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
	"github.com/Agent-Field/codeaf/internal/filelock"
)

func linkWire(tok tokens.Token) api.AccessLink {
	return api.AccessLink{ID: tok.ID, CreatedAt: tok.Created, ExpiresAt: tok.Expires, CreatedBy: tok.InvitedBy, Login: tok.InvitedLogin, Name: tok.InvitedName, Revoked: tok.Revoked}
}

func (h *Handler) accessLinks(w http.ResponseWriter, r *http.Request) {
	if !h.requireOperator(w, r) {
		return
	}
	rows, err := tokens.Load(h.config.TokensPath)
	if h.failed(w, err) {
		return
	}
	out := api.AccessLinks{Factory: h.factory, Links: []api.AccessLink{}}
	for _, tok := range rows {
		out.Links = append(out.Links, linkWire(tok))
	}
	h.write(w, http.StatusOK, out)
}

func (h *Handler) createAccessLink(w http.ResponseWriter, r *http.Request) {
	if !h.requireOperator(w, r) || !h.requireVerb(w, h.config.TokensPath != "") {
		return
	}
	var body api.CreateAccessLink
	if !h.decode(w, r, &body, true) {
		return
	}
	body.Login = factory.NormalizeHandle(body.Login)
	if body.Login != "" && !personHandle.MatchString(body.Login) {
		h.fail(w, http.StatusUnprocessableEntity, "login must be a GitHub handle")
		return
	}
	if !body.ExpiresAt.IsZero() && !h.now().Before(body.ExpiresAt) {
		h.fail(w, http.StatusUnprocessableEntity, "expiry must be in the future")
		return
	}
	tok, err := tokens.Mint()
	if h.failed(w, err) {
		return
	}
	tok.Created, tok.Expires, tok.InvitedBy = h.now(), body.ExpiresAt, h.operatorLogin(r)
	tok.InvitedLogin, tok.InvitedName = body.Login, body.Name
	tok.AccessLink = true
	if h.failed(w, os.MkdirAll(filepath.Dir(h.config.TokensPath), 0700)) {
		return
	}
	if h.failed(w, h.mutateLink(func() error { return tokens.Add(h.config.TokensPath, tok) })) {
		return
	}
	if h.failed(w, h.operatorEvent(r, record.LinkCreated, "link:"+tok.ID, "Sign in with GitHub through an access link")) {
		return
	}
	out := linkWire(tok)
	// The fragment is consumed by the app; it is never sent as a URL query to the server.
	out.URL = "codeaf://factory/join?url=" + url.QueryEscape(h.factory.Address) + "#token=" + url.QueryEscape(tok.Token)
	h.write(w, http.StatusCreated, struct {
		Factory api.Factory    `json:"factory"`
		Link    api.AccessLink `json:"link"`
	}{h.factory, out})
}

func (h *Handler) revokeAccessLink(w http.ResponseWriter, r *http.Request) {
	if !h.requireOperator(w, r) || !h.requireVerb(w, h.config.TokensPath != "") {
		return
	}
	rows, err := tokens.Load(h.config.TokensPath)
	if h.failed(w, err) {
		return
	}
	err = h.mutateLink(func() error { return tokens.Remove(h.config.TokensPath, r.PathValue("id")) })
	if errors.Is(err, tokens.ErrNotFound) {
		h.fail(w, http.StatusNotFound, "access link not found")
		return
	}
	if h.failed(w, err) {
		return
	}
	// Sessions created through the link stop working immediately, including their runners.
	for _, tok := range rows {
		if tok.ID == r.PathValue("id") && tok.Profile != nil {
			if h.failed(w, h.revokePerson(r, tok.Profile.Handle)) {
				return
			}
		}
	}
	h.finish(w, h.operatorEvent(r, record.LinkRevoked, "link:"+r.PathValue("id"), "access link revoked"))
}

// mutateLink shares the CLI and GitHub-binding lock so concurrent link changes cannot overwrite each other.
func (h *Handler) mutateLink(change func() error) error {
	f, err := os.OpenFile(h.config.TokensPath+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := filelock.Lock(f, true, false); err != nil {
		return err
	}
	defer filelock.Unlock(f)
	return change()
}
