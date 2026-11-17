package serve

import (
	"net/http"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// setupInvite invites one person from a set-up card through the invite
// store People in Settings uses (invites.go), with their team and the
// workstreams the card puts them in, and answers the invite's id and link.
// An invite with no email is a link to copy, never a guess at an address.
func (h *Handler) setupInvite(r *http.Request, name, email, team string, workstreams []string) (string, string, error) {
	inv, url, err := h.createInvite(r, api.InviteNew{Email: email, Name: name, Team: team, Workstreams: workstreams})
	if err != nil {
		return "", "", err
	}
	return inv.ID, url, nil
}
