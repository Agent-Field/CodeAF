package trigger

import (
	"context"
	"sync"

	"github.com/Agent-Field/codeaf/internal/factory/inbox"
	"github.com/Agent-Field/codeaf/pkg/codeaftrigger"
)

// Replier is the [inbox.Replier] of one registered source with a callback:
// each moment of a job the source filed becomes a delivery in the outbox.
// It never sends inline, so a slow callback can neither hold up nor fail the
// job; the outbox sends it, and sends it again until it is taken.
type Replier struct {
	Source string
	Out    *Outbox
}

var _ inbox.Replier = Replier{}

// events maps a moment to the webhook events it is. A merge is the job
// finishing too ([factory.JobState] is done once its pull request merged),
// and the PR follower tells only of the merge, so it is sent as both.
var events = map[inbox.EventKind][]string{
	inbox.EventFiled:    {codeaftrigger.EventFiled},
	inbox.EventStarted:  {codeaftrigger.EventStarted},
	inbox.EventQuestion: {codeaftrigger.EventQuestion},
	inbox.EventPROpened: {codeaftrigger.EventPROpened},
	inbox.EventMerged:   {codeaftrigger.EventMerged, codeaftrigger.EventDone},
	inbox.EventStuck:    {codeaftrigger.EventStuck},
	inbox.EventDone:     {codeaftrigger.EventDone},
	inbox.EventMessage:  {codeaftrigger.EventMessage},
}

// Reply queues ev for the job with key externalID. A moment it does not know
// queues nothing.
func (r Replier) Reply(_ context.Context, externalID string, ev inbox.Event) error {
	for _, name := range events[ev.Kind] {
		if _, err := r.Out.Enqueue(r.Source, codeaftrigger.Webhook{
			Event: name, ExternalID: externalID, Job: ev.JobID, Text: ev.Text, URL: ev.URL,
		}); err != nil {
			return err
		}
	}
	return nil
}

// Hub is one floor's Trigger API: its registry and its outbox, and the
// answerers it registered with the inbox.
type Hub struct {
	Registry Registry
	Out      *Outbox

	mu         sync.Mutex
	registered map[string]bool
}

// Open is the hub of the floor folder root.
func Open(root string) *Hub {
	out := OutboxAt(root)
	return &Hub{Registry: out.Registry, Out: out, registered: map[string]bool{}}
}

// Sync makes the inbox's answerers match the registry: one [Replier] for each
// source with a callback, and none for a source revoked or left without one.
// Call it at start and after every add or revoke.
func (h *Hub) Sync() error {
	all, err := h.Registry.Load()
	if err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	want := map[string]bool{}
	for _, s := range all {
		if s.Callback != "" {
			want[s.Name] = true
			inbox.RegisterReplier(s.Name, Replier{Source: s.Name, Out: h.Out})
		}
	}
	for name := range h.registered {
		if !want[name] {
			inbox.RegisterReplier(name, nil)
		}
	}
	h.registered = want
	return nil
}

// Close takes every answerer this hub registered away again.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for name := range h.registered {
		inbox.RegisterReplier(name, nil)
	}
	h.registered = map[string]bool{}
}
