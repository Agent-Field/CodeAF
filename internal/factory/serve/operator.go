package serve

import (
	"context"
	"errors"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

// NewOperatorLink is the serve startup door and deliberately returns no host access token.
func (h *Handler) NewOperatorLink(ctx context.Context, recovery bool) (api.AccessLink, error) {
	if h.mode() != api.FactoryTeam {
		return api.AccessLink{}, nil
	}
	claim, err := tokens.LoadOperator(h.config.TokensPath)
	if err != nil || (claim.Login != "" && !recovery) {
		return api.AccessLink{}, err
	}
	if h.config.TokensPath == "" {
		return api.AccessLink{}, errors.New("operator access links need a host token store")
	}
	login := claim.Login
	if login == "" && h.seam.GitHub != nil {
		link, err := h.seam.GitHub(ctx)
		if err != nil {
			return api.AccessLink{}, err
		}
		login = factory.NormalizeHandle(link.Login)
	}
	tok, err := tokens.RotateOperator(h.config.TokensPath, login, h.now(), recovery)
	if err != nil || tok.ID == "" {
		return api.AccessLink{}, err
	}
	out := linkWire(tok)
	out.URL = accessLinkURL(h.factory.Address, tok)
	return out, nil
}
