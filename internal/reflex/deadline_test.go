package reflex

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

type deadlineCompleter struct {
	t        *testing.T
	cap      time.Duration
	deadline time.Time
	calls    int
	role     lane.Role
}

func (c *deadlineCompleter) CompleteWithMessages(ctx context.Context, _ []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	c.calls++
	if c.role != "" && provider.RoleFrom(ctx) != c.role {
		c.t.Fatalf("role=%q want%q", provider.RoleFrom(ctx), c.role)
	}
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > c.cap {
		c.t.Fatalf("helper has no bounded deadline: %v %v", deadline, ok)
	}
	if c.calls == 1 {
		c.deadline = deadline
	} else if !deadline.Equal(c.deadline) {
		c.t.Fatalf("repair reset deadline: %v -> %v", c.deadline, deadline)
	}
	if c.calls == 2 {
		return nil, context.DeadlineExceeded
	}
	return &ai.Response{Choices: []ai.Choice{{Message: ai.Message{Content: []ai.ContentPart{{Type: "text", Text: "not json"}}}}}}, nil
}

func TestRecallSharesTheInteractiveDeadlineAcrossRepair(t *testing.T) {
	c := &deadlineCompleter{t: t, cap: lane.VisiblePatience, role: lane.RoleRecall}
	_, err := Route(context.Background(), c, "which preference applies", nil)
	if !errors.Is(err, context.DeadlineExceeded) || c.calls != 2 {
		t.Fatalf("calls=%d err=%v", c.calls, err)
	}
}

func TestOtherReflexOperationsShareTierPatienceAcrossRepair(t *testing.T) {
	c := &deadlineCompleter{t: t, cap: roles.PatienceFor(roles.RoleReflex)}
	err := ask(context.Background(), c, "classify", "input", func(string) error { return errors.New("invalid") })
	if !errors.Is(err, context.DeadlineExceeded) || c.calls != 2 {
		t.Fatalf("calls=%d err=%v", c.calls, err)
	}
}

func TestRecallPreservesAShorterCallerDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c := &deadlineCompleter{t: t, cap: time.Second}
	_, _ = Route(ctx, c, "which preference applies", nil)
	want, _ := ctx.Deadline()
	if !c.deadline.Equal(want) {
		t.Fatalf("caller deadline changed: %v -> %v", want, c.deadline)
	}
}
