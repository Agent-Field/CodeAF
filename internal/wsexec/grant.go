package wsexec

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func (a *Adapter) authorizeLaunch(ctx context.Context, grantID string) (Grant, error) {
	g, err := a.store.GetGrant(ctx, grantID)
	if err != nil {
		return Grant{}, err
	}
	if g.Status != GrantActive {
		return Grant{}, fmt.Errorf("%w: grant is %s", ErrInvalid, g.Status)
	}
	return g, nil
}

func grantHas(g Grant, class string) bool {
	var classes []string
	if err := json.Unmarshal([]byte(g.ActionJSON), &classes); err != nil {
		return false
	}
	for _, got := range classes {
		if got == class {
			return true
		}
	}
	return false
}

func refuseModelPerson(rev SteerRevision) error {
	id := strings.TrimSpace(rev.PersonRequestID)
	if id == "" {
		return fmt.Errorf("%w: steer needs the original person request", ErrInvalid)
	}
	switch strings.ToLower(strings.ReplaceAll(id, "_", "")) {
	case OriginPerson, "fromperson", "from-person":
		return fmt.Errorf("%w: model-supplied person origin", ErrInvalid)
	}
	return nil
}
