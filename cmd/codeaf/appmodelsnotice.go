package main

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/guard"
)

// appModelNoticeDelivery lets Close cancel a sender waiting on the wire queue.
// Recorders may implement only Emit; the production wire supports cancellation.
type appModelNoticeDelivery struct {
	ctx  context.Context
	emit appwire.Emitter
}

func (d appModelNoticeDelivery) Emit(event string, data any) {
	if d.ctx.Err() != nil {
		return
	}
	if emit, ok := d.emit.(interface {
		EmitContext(context.Context, string, any)
	}); ok {
		emit.EmitContext(d.ctx, event, data)
		return
	}
	d.emit.Emit(event, data)
}

// watchModelCompletions joins the terminal's service-model notifications once
// per app lifetime. The initial lazy catalog has its own completion door, so
// one cancellable waiter covers it before any provider warm can finish.
func (b *appBackend) watchModelCompletions(proc *v3Process) {
	b.locked(func() {
		if b.closing || b.stopModelNotices != nil {
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		// Model notices do not change Inbox state. Queue them to its final
		// emitter, retaining the wire's cancellable delivery door.
		box := appwire.NewOutbox(appModelNoticeDelivery{ctx: ctx, emit: b.inbox.next})
		tell := func(string, string) {
			// Only admission is serialized with Close; stdout never owns b.mu.
			b.locked(func() {
				if !b.closing {
					box.Event(appwire.EventModelsChanged, appwire.Empty{})
				}
			})
		}
		remove := proc.registerServiceNotice(tell)
		done := make(chan struct{})
		b.stopModelNotices = func() {
			cancel()
			remove()
			<-done
			box.Close()
		}
		// A catalog already in hand needs no notification: the first list can
		// read it. A cancelled context makes this readiness probe nonblocking.
		ready, stopReady := context.WithCancel(context.Background())
		stopReady()
		if proc.Models == nil || proc.Models.Warmed(ready) {
			close(done)
			return
		}
		guard.Go("app/models-completion", func() {
			defer close(done)
			if proc.Models.Warmed(ctx) && ctx.Err() == nil {
				tell("", "")
			}
		})
	})
}
