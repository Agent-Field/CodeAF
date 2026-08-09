// Package runstate is the session-facing port of src/session/run-state.ts.
// The state machine itself lives in internal/engine/runner; this package wires
// its status callbacks to the matching session status service.
package runstate

import (
	"context"

	enginerunner "github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/runner"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/status"
)

type BusyError = enginerunner.BusyError
type Latch = enginerunner.Latch

func NewLatch() *Latch { return enginerunner.NewLatch() }

type Service[A any] struct {
	registry *enginerunner.Registry[A]
}

func New[A any](scope context.Context, statuses *status.Service) *Service[A] {
	var set enginerunner.Status
	if statuses != nil {
		set = func(sessionID, value string) {
			statuses.Set(sessionID, status.Info{Type: value})
		}
	}
	return &Service[A]{
		registry: enginerunner.NewRegistry[A](scope, set),
	}
}

func (service *Service[A]) AssertNotBusy(sessionID string) error {
	return service.registry.AssertNotBusy(sessionID)
}

func (service *Service[A]) Cancel(sessionID string) {
	service.registry.Cancel(sessionID)
}

func (service *Service[A]) EnsureRunning(
	ctx context.Context,
	sessionID string,
	onInterrupt func() (A, error),
	work enginerunner.Work[A],
) (A, error) {
	return service.registry.EnsureRunning(ctx, sessionID, onInterrupt, work)
}

func (service *Service[A]) StartShell(
	ctx context.Context,
	sessionID string,
	onInterrupt func() (A, error),
	work enginerunner.Work[A],
	ready *Latch,
) (A, error) {
	return service.registry.StartShell(ctx, sessionID, onInterrupt, work, ready)
}

func (service *Service[A]) Close(ctx context.Context) error {
	return service.registry.Close(ctx)
}
