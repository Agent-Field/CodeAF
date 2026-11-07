package trigger

import (
	"context"
	"errors"

	"github.com/Agent-Field/codeaf/internal/factory/inbox"
)

// SinkName is the name the webhook sink is registered under on the bus.
const SinkName = "webhook"

// events maps a moment to the webhook events it is. A merge is the job
// finishing too ([factory.JobState] is done once its pull request merged),
// and the PR follower tells only of the merge, so it is sent as both.
var events = map[inbox.EventKind][]string{
	inbox.EventFiled:    {EventFiled},
	inbox.EventStarted:  {EventStarted},
	inbox.EventQuestion: {EventQuestion},
	inbox.EventPROpened: {EventPROpened},
	inbox.EventMerged:   {EventMerged, EventDone},
	inbox.EventStuck:    {EventStuck},
	inbox.EventDone:     {EventDone},
	inbox.EventMessage:  {EventMessage},
}

// EventsOf is the webhook events a moment is sent as, in order; none for a
// moment this version does not send. Another sink that speaks the same event
// names (AgentField's execution progress) reads them here.
func EventsOf(kind inbox.EventKind) []string { return events[kind] }

// WebhookSink is the [inbox.Sink] of the signed webhooks: a moment of a job
// whose source has a webhook becomes deliveries in the outbox. IT NEVER SENDS
// INLINE, so a slow receiver can neither hold up nor fail the job.
type WebhookSink struct {
	Out *Outbox
}

var _ inbox.Sink = WebhookSink{}

// Send queues m when its source has a webhook, and nothing otherwise.
func (s WebhookSink) Send(_ context.Context, m inbox.Moment) error {
	if _, err := s.Out.Webhooks.Get(m.Source); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	for _, name := range EventsOf(m.Kind) {
		if _, err := s.Out.Enqueue(m.Source, Body{
			Event: name, ExternalID: m.ExternalID, Job: m.JobID, Text: m.Text, URL: m.URL,
		}); err != nil {
			return err
		}
	}
	return nil
}

// Hub is one floor's webhooks and outbox.
type Hub struct {
	Webhooks Webhooks
	Out      *Outbox
}

// Open is the hub of the floor folder root, its sink registered on the bus.
func Open(root string) *Hub {
	out := OutboxAt(root)
	inbox.RegisterSink(SinkName, WebhookSink{Out: out})
	return &Hub{Webhooks: out.Webhooks, Out: out}
}

// Close takes the sink off the bus.
func (h *Hub) Close() { inbox.RegisterSink(SinkName, nil) }
