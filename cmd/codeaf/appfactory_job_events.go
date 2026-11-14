package main

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/guard"
)

var _ appwire.FactoryJobEventsReader = (*appBackend)(nil)

// FactoryEventsSince reads once; following is owned by the hub, beyond this call.
func (b *appBackend) FactoryEventsSince(ctx context.Context, p appwire.FactoryEventsSinceParams) (appwire.FactoryEvents, error) {
	h := b.factoryHub()
	out, err := h.readJobEvents(ctx, p, 0)
	if err == nil && out.Supported && p.Follow {
		h.jobEventsState().follow(b, h, p.Factory, out.Cursor)
	}
	return out, err
}

func (h *appFactoryHub) readJobEvents(ctx context.Context, p appwire.FactoryEventsSinceParams, wait int) (appwire.FactoryEvents, error) {
	out := appwire.FactoryEvents{Factory: p.Factory, Events: []appwire.FactoryEvent{}}
	limit := p.Limit
	if limit == 0 {
		limit = appwire.DefaultFactoryEvents
	}
	q := url.Values{"limit": {strconv.Itoa(limit)}}
	if p.After != nil {
		q.Set("after", strconv.FormatInt(*p.After, 10))
	}
	if wait > 0 {
		q.Set("wait", strconv.Itoa(wait))
	}
	var got api.JobEvents
	if err := h.read(ctx, p.Factory, "/v1/job-events?"+q.Encode(), appFactorySmall, &got); err != nil {
		if routeMissing(err) {
			return out, nil
		}
		return appwire.FactoryEvents{}, err
	}
	out.Supported, out.Cursor, out.Reset, out.More = true, got.Cursor, got.Reset, got.More
	for _, e := range got.Events {
		out.Events = append(out.Events, appwire.FactoryEvent{
			Factory: p.Factory, Seq: e.Seq, Kind: e.Kind, Job: e.Job, At: e.At,
			Title: e.Title, Group: e.Group, How: e.How, Reason: e.Reason,
			Need: e.Need, Code: e.Code, Step: e.Step, PRNumber: e.PRNumber, PRURL: e.PRURL,
		})
	}
	return out, nil
}

type appFactoryJobEvents struct {
	mu        sync.Mutex
	ctx       context.Context
	cancel    context.CancelFunc
	followers map[string]*appFactoryJobFollower
	wg        sync.WaitGroup
	// after lets retry tests advance the backoff without waiting on a clock.
	after func(time.Duration) <-chan time.Time
	// beforeRead lets tests hold a follower before its credential lookup.
	beforeRead func(context.Context)
}

type appFactoryJobFollower struct {
	cursor int64
}

func (h *appFactoryHub) jobEventsState() *appFactoryJobEvents {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.jobEvents == nil {
		ctx, cancel := context.WithCancel(context.Background())
		h.jobEvents = &appFactoryJobEvents{
			ctx: ctx, cancel: cancel, followers: map[string]*appFactoryJobFollower{}, after: time.After,
		}
		if h.closed {
			cancel()
		}
	}
	return h.jobEvents
}

func (h *appFactoryHub) stopJobEvents() {
	s := h.jobEventsState()
	s.stop()
	s.wg.Wait()
}

func (s *appFactoryJobEvents) stop() {
	// Cancel before taking the lock so a full wire queue releases its sender.
	s.cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	// Every admitted follower has joined the wait group before this returns.
}

func (s *appFactoryJobEvents) follow(b *appBackend, h *appFactoryHub, id string, cursor int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return
	}
	if f := s.followers[id]; f != nil {
		f.cursor = max(f.cursor, cursor)
		return
	}
	if len(s.followers) >= appwire.MaxFactoryConnections {
		return
	}
	f := &appFactoryJobFollower{cursor: cursor}
	s.followers[id] = f
	s.wg.Add(1)
	guard.Go("factory job outcomes", func() {
		defer s.wg.Done()
		defer s.remove(id, f)
		s.run(b, h, id, f)
	})
}

func (s *appFactoryJobEvents) remove(id string, f *appFactoryJobFollower) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.followers[id] == f {
		delete(s.followers, id)
	}
}

func (s *appFactoryJobEvents) cursor(f *appFactoryJobFollower) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return f.cursor
}

func (s *appFactoryJobEvents) run(b *appBackend, h *appFactoryHub, id string, f *appFactoryJobFollower) {
	delay := 2 * time.Second
	for s.ctx.Err() == nil {
		cursor := s.cursor(f)
		ctx, cancel := context.WithTimeout(s.ctx, appFactoryTimeout)
		if s.beforeRead != nil {
			s.beforeRead(ctx)
		}
		out, err := h.readJobEvents(ctx, appwire.FactoryEventsSinceParams{Factory: id, After: &cursor, Limit: appwire.DefaultFactoryEvents}, 15)
		cancel()
		if jobEventsEnded(s.ctx, out, err) || jobEventsDetached(h, id, err) {
			return
		}
		if err != nil {
			if !s.retry(delay) {
				return
			}
			delay = min(2*delay, time.Minute)
			continue
		}
		s.deliver(b, f, cursor, out)
		// A RESET THAT LEFT THE CURSOR WHERE IT WAS IS ANSWERED AGAIN AT ONCE,
		// wait or no wait: a Factory whose live record has a gap an older
		// writer left resets every read to the cursor it was asked. Read again
		// at once, the follower sent requests back to back, each one settling
		// every job on the Factory, until something closed the gap.
		if out.Reset && s.cursor(f) == cursor {
			if !s.retry(delay) {
				return
			}
			delay = min(2*delay, time.Minute)
			continue
		}
		delay = 2 * time.Second
	}
}

func jobEventsEnded(ctx context.Context, out appwire.FactoryEvents, err error) bool {
	if ctx.Err() != nil || (err == nil && !out.Supported) || errors.Is(err, context.Canceled) {
		return true
	}
	var wire *appwire.Error
	return errors.As(err, &wire) && (wire.Code == appwire.CodeNotFound || wire.Code == appwire.CodeClosed)
}

func jobEventsDetached(h *appFactoryHub, id string, err error) bool {
	var wire *appwire.Error
	if !errors.As(err, &wire) || wire.Code != appwire.CodeSignedOut {
		return false
	}
	// A detached poll can return signed_out after its credential was forgotten.
	_, _, err = h.lookup(id)
	return errors.As(err, &wire) && wire.Code == appwire.CodeNotFound
}

func (s *appFactoryJobEvents) retry(delay time.Duration) bool {
	select {
	case <-s.ctx.Done():
		return false
	case <-s.after(delay):
		return true
	}
}

func (s *appFactoryJobEvents) deliver(b *appBackend, f *appFactoryJobFollower, asked int64, out appwire.FactoryEvents) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return
	}
	if out.Reset {
		// A concurrent read may already have advanced beyond this request.
		if f.cursor == asked {
			f.cursor = out.Cursor
		}
		return
	}
	delivery := appModelNoticeDelivery{ctx: s.ctx, emit: b.inbox.next}
	for _, e := range out.Events {
		if e.Seq > f.cursor {
			delivery.Emit(appwire.EventFactoryEvent, e)
			f.cursor = e.Seq
		}
	}
	f.cursor = max(f.cursor, out.Cursor)
}
