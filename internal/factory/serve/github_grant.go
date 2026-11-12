package serve

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

// grantGitHub accepts a separate app device-flow token only from the claimed operator.
// The server proves the token's login and never exposes it in a response or Activity.
func (h *Handler) grantGitHub(w http.ResponseWriter, r *http.Request) {
	if !h.requireOperator(w, r) || !h.requireVerb(w, h.config.TokensPath != "") {
		return
	}
	operator := h.signedInOperator(r)
	if operator == "" {
		h.fail(w, http.StatusForbidden, "claim the factory operator link first")
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	if !h.decode(w, r, &body, false) {
		return
	}
	if body.Token == "" || strings.ContainsAny(body.Token, " \t\r\n") {
		h.fail(w, http.StatusUnprocessableEntity, "a GitHub token is one word")
		return
	}
	login, err := h.readGrantLogin(r, body.Token)
	if err != nil {
		h.fail(w, http.StatusBadGateway, err.Error())
		return
	}
	if factory.NormalizeHandle(login) != operator {
		h.fail(w, http.StatusForbidden, "the GitHub read grant must belong to the operator")
		return
	}
	grant := factorygithub.ReadGrant{Token: body.Token, Login: operator, GrantedAt: h.now()}
	if h.failed(w, h.keepReadGrant(r, grant)) {
		return
	}
	h.github(w, r)
}

func (h *Handler) signedInOperator(r *http.Request) string {
	session, ok := h.credential(r)
	if !ok || session.profile == nil || !strings.HasPrefix(session.id, "session_") {
		return ""
	}
	operator := h.operatorLogin(r)
	if session.profile.Handle != operator {
		return ""
	}
	return operator
}

func (h *Handler) keepReadGrant(r *http.Request, grant factorygithub.ReadGrant) error {
	if err := factorygithub.SaveReadGrant(factorygithub.GrantPath(h.config.TokensPath), grant); err != nil {
		return err
	}
	if err := h.operatorEvent(r, record.GitHubGranted, "GitHub", "read issues and repositories"); err != nil {
		return err
	}
	if h.config.Store != nil {
		factorygithub.Nudge(h.config.Store)
	}
	return nil
}

// pollGitHub is POST /v1/github/poll, "Check GitHub now": it wakes the poll
// on this factory's machine at once ([factorygithub.Nudge]) and answers
// without waiting for the read. The read itself is the poll's ordinary one,
// so what it finds arrives the way every read does, and the queue's github
// source says it is reading (polling) and then when it last read well
// (polled). Nudges while a read is in flight fold into one more read after
// it, so pressing twice costs one read, not two. A factory with no store has
// no poll to wake.
func (h *Handler) pollGitHub(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.config.Store != nil) {
		return
	}
	factorygithub.Nudge(h.config.Store)
	h.finish(w, nil)
}

// readGrantLogin checks identity and refuses known write-capable classic OAuth scopes.
// GitHub App tokens have no OAuth scopes; their Issues and Metadata read permissions
// are configured in the app, and this credential is only handed to read clients.
func (h *Handler) readGrantLogin(r *http.Request, token string) (string, error) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "codeaf-factory")
	req.Header.Set("Authorization", "Bearer "+token)
	client := h.githubClient()
	response, err := client.Do(req)
	if err != nil {
		return "", errors.New("GitHub read grant is unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", githubStatusError(response.StatusCode)
	}
	for _, scope := range strings.FieldsFunc(response.Header.Get("X-OAuth-Scopes"), func(r rune) bool { return r == ',' || r == ' ' }) {
		if scope != "read:user" && scope != "user:email" && scope != "read:org" {
			return "", errors.New("GitHub read grant needs read-only permissions; use a GitHub App with Issues and Metadata read access")
		}
	}
	var user struct {
		Login string `json:"login"`
	}
	err = decodeGitHub(response.Body, &user)
	return user.Login, err
}
