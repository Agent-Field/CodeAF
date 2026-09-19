package wscollab

import (
	"context"
	"fmt"
	"strings"
)

// Invocation is one bounded model call that produced a contribution. Distinct
// attributed speakers need distinct invocations — a coordinator writing both
// sides of a planner/critic exchange is not this record (J19).
type Invocation struct {
	ID       string
	ActorID  string
	Role     string
	Source   string
	Evidence []string
}

func (inv Invocation) validate() error {
	if strings.TrimSpace(inv.ID) == "" {
		return fmt.Errorf("%w: invocation id is empty", ErrInvalid)
	}
	if strings.TrimSpace(inv.ActorID) == "" {
		return fmt.Errorf("%w: invocation actor is empty", ErrInvalid)
	}
	if strings.TrimSpace(inv.Role) == "" {
		return fmt.Errorf("%w: invocation role is empty", ErrInvalid)
	}
	return nil
}

// Contribute records one participant's own invocation and delivers it into
// the discussion as OriginAgent. The source chat is evidence, not a wake.
func (r *Router) Contribute(ctx context.Context, discussionID string, inv Invocation, body string) (Receipt, error) {
	if err := inv.validate(); err != nil {
		return Receipt{}, err
	}
	if strings.TrimSpace(discussionID) == "" {
		return Receipt{}, fmt.Errorf("%w: discussion id is empty", ErrInvalid)
	}
	if err := r.rememberInvocation(ctx, inv); err != nil {
		return Receipt{}, err
	}
	msg := Message{
		From:       inv.Source,
		ActorID:    inv.ActorID,
		Role:       inv.Role,
		Body:       body,
		Discussion: discussionID,
	}
	if msg.From == "" {
		msg.From = inv.ActorID
	}
	got, err := r.Deliver(ctx, OriginAgent, msg, []string{discussionID})
	if err != nil {
		return Receipt{}, err
	}
	return got[0], nil
}

func (r *Router) rememberInvocation(ctx context.Context, inv Invocation) error {
	have, err := r.store.GetInvocation(ctx, inv.ID)
	if err == nil {
		if have.ActorID != inv.ActorID {
			return fmt.Errorf("%w: invocation %s already speaks as %s", ErrInvalid, inv.ID, have.ActorID)
		}
		return nil
	}
	if err != ErrNotFound {
		return err
	}
	return r.store.PutInvocation(ctx, inv)
}
