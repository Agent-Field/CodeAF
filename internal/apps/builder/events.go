package builder

import (
	"context"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
)

// emit publishes one build event stamped with the clock and the spend so far.
func (s *Service) emit(b *build, ev bridge.BuildEvent) {
	ev.Cost = b.cost()
	ev.At = s.now().UTC().Format(time.RFC3339Nano)
	s.all.Hub.Publish(bridge.TopicBuild(b.app), ev)
}

func (s *Service) step(b *build, step bridge.BuildStep, status bridge.BuildStatus, log string) {
	s.emit(b, bridge.BuildEvent{Step: step, Status: status, Log: log})
}

// setState moves the app and tells the desk's state topic.
func (s *Service) setState(ctx context.Context, b *build, state bridge.AppState) error {
	if err := s.all.Store.SetState(ctx, b.app, state); err != nil {
		return err
	}
	detail, err := s.all.Store.Get(ctx, b.app)
	if err == nil {
		s.all.Hub.Publish(bridge.TopicState(b.app), detail.AppSummary)
	}
	return nil
}
