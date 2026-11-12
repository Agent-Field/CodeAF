package api

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/live"
)

// FixtureLiveEvents gives app and contract tests the same small payloads as
// store writes, plus the accepted person actions and a machine transition.
func FixtureLiveEvents(now time.Time) []live.Event {
	var events []live.Event
	for _, it := range factory.Fixture(now).Items {
		events = append(events, live.Changes(factory.Item{}, it, now)...)
	}
	failed := factory.Item{ID: 9, State: factory.StateLanded, PublishStatus: &factory.Publishing{State: "failed", Code: "no_push_permission", Attempt: 1, At: now}}
	events = append(events, live.Changes(factory.Item{}, failed, now)...)
	events = append(events,
		live.New("approved", 10, LiveEvent{Job: 10, By: "santosh"}),
		live.New("taken", 2, LiveEvent{Job: 2, By: "santosh"}),
		live.New("machine", 2, LiveEvent{Name: "laptop", Status: MachineBusy}),
	)
	return events
}
