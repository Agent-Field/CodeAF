package serve

import (
	"context"
	"errors"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

// NewOperatorLink is the serve startup door and deliberately returns no host
// access token. It is [Handler.MintOperatorLink] then [Handler.InviteOperatorLink].
func (h *Handler) NewOperatorLink(ctx context.Context, recovery bool) (api.AccessLink, error) {
	row, err := h.MintOperatorLink(recovery)
	if err != nil {
		return api.AccessLink{}, err
	}
	return h.InviteOperatorLink(ctx, row)
}

// MintOperatorLink writes the operator link's row without asking GitHub
// anything, so serve can say ready with the row in place. An empty row means
// no link is due: not a team factory, or already claimed outside recovery.
func (h *Handler) MintOperatorLink(recovery bool) (tokens.Token, error) {
	if h.mode() != api.FactoryTeam {
		return tokens.Token{}, nil
	}
	claim, err := tokens.LoadOperator(h.config.TokensPath)
	if err != nil || (claim.Login != "" && !recovery) {
		return tokens.Token{}, err
	}
	if h.config.TokensPath == "" {
		return tokens.Token{}, errors.New("operator access links need a host token store")
	}
	return tokens.RotateOperator(h.config.TokensPath, claim.Login, h.now(), recovery)
}

// InviteOperatorLink answers the link to print for a minted row. On a server
// that takes no passwords it first names the host's GitHub login on a row that
// has none, which may wait on GitHub, so that secret is never shown unbound.
//
// A SERVER THAT TAKES PASSWORDS NEVER NAMES ITS OPERATOR LINK FOR A GITHUB
// LOGIN. A named link takes no password (signUp, reasonGitHubLink), so a server
// that merely held a GitHub token (GH_TOKEN, a grant in its home) could only be
// claimed by that token's GitHub account, and a password operator never (film
// D14). Like every invitation, the operator link makes a username and password
// account by default, and GitHub stays a way in (2646-org-password): the secret
// in the link is the proof, as it is on a server with no GitHub at all. A
// recovery link still names the operator who already claimed (RotateOperator).
func (h *Handler) InviteOperatorLink(ctx context.Context, row tokens.Token) (api.AccessLink, error) {
	if row.ID == "" {
		return api.AccessLink{}, nil
	}
	if row.InvitedLogin == "" && h.seam.GitHub != nil && !h.passwordsOn() {
		link, err := h.seam.GitHub(ctx)
		if err != nil {
			return api.AccessLink{}, err
		}
		row.InvitedLogin = factory.NormalizeHandle(link.Login)
		if err := tokens.InviteOperator(h.config.TokensPath, row.ID, row.InvitedLogin); err != nil {
			return api.AccessLink{}, err
		}
	}
	out := linkWire(row, h.now())
	out.URL = accessLinkURL(h.factory.Address, row)
	return out, nil
}
