package serve

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
	"github.com/Agent-Field/codeaf/internal/filelock"
)

// endedLinkListed is how long an ended human link stays in the operator's list.
const endedLinkListed = 30 * 24 * time.Hour

// linkWire is the list row. It carries counts and public handles, never the
// secret: that is shown once, by createAccessLink.
func linkWire(tok tokens.Token, now time.Time) api.AccessLink {
	out := api.AccessLink{Operator: tok.Operator, ID: tok.ID, CreatedAt: tok.Created, ExpiresAt: tok.Expires, CreatedBy: tok.InvitedBy, Login: tok.InvitedLogin, Name: tok.InvitedName, Revoked: tok.Revoked}
	if !tok.Human() {
		return out
	}
	out.MaxUses, out.Used, out.Remaining = tok.Limit(), tok.Used(), tok.Remaining()
	counted := tok.Counted()
	out.Counted = &counted
	out.Status = "open"
	if reason := tok.Reason(now); reason != "" {
		out.Status = reason
		ended := tok.EndedAt(now)
		out.EndedAt = &ended
	}
	for _, j := range tok.Members() {
		out.UsedBy = append(out.UsedBy, j.Profile.Handle)
	}
	return out
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
	now := h.now()
	for _, tok := range rows {
		// Ended links stay listed, greyed, for thirty days.
		if tok.Human() && tok.Reason(now) != "" && now.Sub(tok.EndedAt(now)) > endedLinkListed {
			continue
		}
		out.Links = append(out.Links, linkWire(tok, now))
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
	now := h.now()
	if !body.ExpiresAt.IsZero() && !now.Before(body.ExpiresAt) {
		h.fail(w, http.StatusUnprocessableEntity, "expiry must be in the future")
		return
	}
	if body.ExpiresAt.After(now.Add(tokens.HumanLinkTTL)) {
		h.fail(w, http.StatusUnprocessableEntity, "an access link lasts at most 7 days")
		return
	}
	if body.ExpiresAt.IsZero() {
		body.ExpiresAt = now.Add(tokens.HumanLinkTTL)
	}
	if body.MaxUses == 0 {
		body.MaxUses = 1
	}
	if body.MaxUses < 1 || body.MaxUses > tokens.MaxLinkPeople {
		h.fail(w, http.StatusUnprocessableEntity, fmt.Sprintf("a link admits one person or up to 2 to %d people", tokens.MaxLinkPeople))
		return
	}
	if body.Login != "" && body.MaxUses > 1 {
		h.fail(w, http.StatusUnprocessableEntity, "a link for one GitHub login admits one person")
		return
	}
	tok, err := tokens.Mint()
	if h.failed(w, err) {
		return
	}
	tok.Created, tok.Expires, tok.InvitedBy = now, body.ExpiresAt, h.operatorLogin(r)
	tok.InvitedLogin, tok.InvitedName = body.Login, body.Name
	tok.AccessLink, tok.MaxUses = true, body.MaxUses
	clearPhase(w)
	if h.failed(w, os.MkdirAll(filepath.Dir(h.config.TokensPath), 0700)) {
		return
	}
	if h.failed(w, h.mutateLink(func() error { return tokens.Add(h.config.TokensPath, tok) })) {
		return
	}
	if h.failed(w, h.operatorEvent(r, record.LinkCreated, "link:"+tok.ID, "Sign in with GitHub through an access link")) {
		return
	}
	out := linkWire(tok, now)
	// The app consumes the fragment. Current clients check it in a POST body;
	// legacy clients still include the full link in a GET query.
	out.URL = accessLinkURL(h.factory.Address, tok)
	h.write(w, http.StatusCreated, struct {
		Factory api.Factory    `json:"factory"`
		Link    api.AccessLink `json:"link"`
	}{h.factory, out})
}

func (h *Handler) revokeAccessLink(w http.ResponseWriter, r *http.Request) {
	if !h.requireOperator(w, r) || !h.requireVerb(w, h.config.TokensPath != "") {
		return
	}
	id := r.PathValue("id")
	var revoked tokens.Token
	clearPhase(w)
	err := h.mutateLink(func() error {
		rows, err := tokens.Load(h.config.TokensPath)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if row.ID == id {
				revoked = row
			}
		}
		return tokens.RemoveAt(h.config.TokensPath, id, h.now())
	})
	if errors.Is(err, tokens.ErrNotFound) {
		// RemoveAt returns ErrNotFound before it changes a row (an unknown id, or
		// one already revoked), so this refusal is again a proven no-write.
		w.Header().Set(api.PhaseHeader, api.PhasePreMutation)
		h.fail(w, http.StatusNotFound, "access link not found")
		return
	}
	if h.failed(w, err) {
		return
	}
	// A link with its own use limit stops admitting people and leaves those
	// who joined in; ending a person is the separate operator action in People.
	// An older link keeps its rule: sessions made through it stop immediately,
	// including their runners.
	if !revoked.Counted() {
		for _, member := range revoked.Members() {
			if h.failed(w, h.revokePerson(r, member.Profile.Handle)) {
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

// accessLinkURL keeps startup and app-created links on the same join door.
func accessLinkURL(address string, tok tokens.Token) string {
	query := url.Values{"url": {address}}
	if tok.Operator {
		query.Set("operator", "true")
	}
	return "codeaf://factory/join?" + query.Encode() + "#token=" + url.QueryEscape(tok.Token)
}
