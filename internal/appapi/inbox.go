package appapi

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

const inboxAnswerHold = 5 * time.Second

// InboxSource owns its waiting set and its resolver. A source must reject a
// stale answer itself; the derived list is never authority to resume work.
type InboxSource interface {
	InboxItems(context.Context) ([]InboxItem, error)
	AnswerInbox(context.Context, InboxItem, InboxAnswerRequest) (InboxAnswerResponse, error)
}

// Factory approvals already hold in their source server. They must not acquire
// another five-second delay when answered through the merged Inbox.
type inboxNativeHold interface {
	NativeInboxHold(InboxItem, InboxAnswerRequest) bool
	UndoInbox(context.Context, InboxItem, *AgentExpect) error
}
type inboxValidator interface {
	ValidateInbox(context.Context, InboxItem, InboxAnswerRequest) error
}
type inboxSourceResolution struct{ how, by string }

type inboxRow struct {
	item   InboxItem
	source InboxSource
	key    string
}
type inboxPending struct {
	row    inboxRow
	answer InboxAnswerRequest
	until  time.Time
	timer  *time.Timer
	native bool
}
type inboxState struct {
	sync.Mutex
	sources map[string]InboxSource
	rows    map[string]inboxRow
	pending map[string]*inboxPending
	now     func() time.Time
	after   func(time.Duration, func()) *time.Timer
	closed  bool
	done    chan struct{}
	cancel  context.CancelFunc
	ctx     context.Context
	watches map[string]context.CancelFunc
}

func (s *Server) startInbox() {
	ctx := s.options.Context
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	s.inbox = &inboxState{sources: map[string]InboxSource{}, rows: map[string]inboxRow{}, pending: map[string]*inboxPending{}, now: time.Now, after: time.AfterFunc, done: make(chan struct{}), cancel: cancel, ctx: ctx, watches: map[string]context.CancelFunc{}}
	if s.options.Agents != nil {
		s.inbox.sources["engine"] = &engineInboxSource{s: s, a: s.options.Agents}
	}
	go func() {
		defer close(s.inbox.done)
		tick := time.NewTicker(500 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if s.options.Agents != nil {
					select {
					case <-s.options.Agents.done:
						return
					default:
					}
				}
				_, _ = s.RefreshInbox(ctx)
			}
		}
	}()
}

// SetInboxSource is the attachment door for resource producers, including solo
// and team factories. Detaching withdraws their items on the next refresh.
func (s *Server) SetInboxSource(key string, source InboxSource) {
	s.inbox.Lock()
	defer s.inbox.Unlock()
	if s.inbox.closed {
		return
	}
	for id, pending := range s.inbox.pending {
		if pending.row.key == key {
			if pending.timer != nil {
				pending.timer.Stop()
			}
			delete(s.inbox.pending, id)
		}
	}
	for id, row := range s.inbox.rows {
		if row.key == key {
			delete(s.inbox.rows, id)
			_, _ = s.events.Publish(EventInboxResolved, InboxResolvedEvent{id, "no_longer_needed", "", s.inbox.now().UTC()})
		}
	}
	if stop := s.inbox.watches[key]; stop != nil {
		stop()
		delete(s.inbox.watches, key)
	}
	if source == nil {
		delete(s.inbox.sources, key)
	} else {
		s.inbox.sources[key] = source
		if watch, ok := source.(interface {
			WatchInbox(context.Context, func(FactoryEvent))
		}); ok {
			ctx, cancel := context.WithCancel(s.inbox.ctx)
			s.inbox.watches[key] = cancel
			go watch.WatchInbox(ctx, func(event FactoryEvent) {
				if ctx.Err() != nil {
					return
				}
				_, _ = s.events.Publish(EventFactoryEvent, event)
			})
		}
	}
}
func (s *Server) closeInbox() {
	s.inbox.cancel()
	s.inbox.Lock()
	s.inbox.closed = true
	for _, p := range s.inbox.pending {
		if p.timer != nil {
			p.timer.Stop()
		}
	}
	s.inbox.pending = map[string]*inboxPending{}
	s.inbox.Unlock()
	<-s.inbox.done
}

// RefreshInbox samples sources independently. An unavailable factory retains
// its last known rows: a network error must not claim somebody answered them.
func (s *Server) RefreshInbox(ctx context.Context) ([]InboxItem, error) {
	state := s.inbox
	state.Lock()
	defer state.Unlock()
	if state.closed {
		return nil, agentError(503, "not_allowed", "the engine is stopping")
	}
	next := map[string]inboxRow{}
	var restored []InboxUndoWindowEvent
	// Independent factories are sampled together, so one unreachable team
	// cannot add a separate network wait ahead of every healthy source.
	type sample struct {
		items []InboxItem
		err   error
	}
	samples := map[string]chan sample{}
	for key, source := range state.sources {
		result := make(chan sample, 1)
		samples[key] = result
		go func() { items, err := source.InboxItems(ctx); result <- sample{items, err} }()
	}
	for key, source := range state.sources {
		sampled := <-samples[key]
		items, err := sampled.items, sampled.err
		if err != nil {
			for id, row := range state.rows {
				if row.key == key {
					next[id] = row
				}
			}
			continue
		}
		for _, item := range items {
			if item.ID == "" || !inboxKind(item.Kind) {
				continue
			}
			item.Context.Before = inboxBefore(item.Context.Before)
			item.Path = append([]SourceRef{}, item.Path...)
			item.Verbs = append([]string{}, item.Verbs...)
			item.WhoCanAnswer = append([]string{}, item.WhoCanAnswer...)
			row := inboxRow{item: item, source: source, key: key}
			next[item.ID] = row
			if provider, ok := source.(interface {
				PendingInbox(InboxItem) (time.Time, bool)
			}); ok {
				if until, held := provider.PendingInbox(item); held && state.pending[item.ID] == nil && state.now().Before(until) {
					state.pending[item.ID] = &inboxPending{row: row, until: until, native: true}
					restored = append(restored, InboxUndoWindowEvent{item.ID, until})
				}
			}
		}
	}
	for id, row := range next {
		if _, ok := state.rows[id]; !ok {
			_, _ = s.events.Publish(EventInboxAdded, InboxAddedEvent{id, row.item.Kind})
		}
	}
	for _, event := range restored {
		_, _ = s.events.Publish(EventInboxUndoWindow, event)
	}
	for id, row := range state.rows {
		if _, ok := next[id]; !ok {
			how, by := "no_longer_needed", ""
			if _, attached := state.sources[row.key]; attached {
				if source, ok := row.source.(interface {
					ResolvedInbox(InboxItem) (string, string)
				}); ok {
					how, by = source.ResolvedInbox(row.item)
				}
			}
			if p := state.pending[id]; p != nil {
				if p.timer != nil {
					p.timer.Stop()
				}
				if p.native {
					how, by = "here", "you"
				}
				delete(state.pending, id)
			}
			_, _ = s.events.Publish(EventInboxResolved, InboxResolvedEvent{id, how, by, state.now().UTC()})
		}
	}
	state.rows = next
	out := make([]InboxItem, 0, len(next))
	for _, row := range next {
		out = append(out, row.item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}
func inboxKind(kind string) bool {
	switch kind {
	case "question", "clarification", "permission", "your_call", "approval_plan", "approval_proof", "approval_start", "budget", "out_of_rounds", "failed", "changes_requested", "conflict", "publish_failed", "runner_offline", "order_ask", "reminder":
		return true
	}
	return false
}
func inboxBefore(entries []DisplayEntry) []DisplayEntry {
	out := make([]DisplayEntry, 2)
	start := max(0, len(entries)-2)
	copy(out[2-(len(entries)-start):], entries[start:])
	return out
}

// AnswerInbox is also the source-surface door: chat and Inbox use one pending
// decision and one deadline. An ordinary duplicate is refused; HTTP keyed
// retries are replayed by the route's idempotency wrapper.
func (s *Server) AnswerInbox(ctx context.Context, id string, answer InboxAnswerRequest) (InboxAnswerResponse, error) {
	if answer.Choice == "" && answer.Words == "" && answer.Verb == "" && len(answer.Items) == 0 {
		return InboxAnswerResponse{}, agentError(422, "bad_input", "choose an answer or send words")
	}
	if _, err := s.RefreshInbox(ctx); err != nil {
		return InboxAnswerResponse{}, err
	}
	state := s.inbox
	state.Lock()
	defer state.Unlock()
	if state.closed {
		return InboxAnswerResponse{}, agentError(503, "not_allowed", "the engine is stopping")
	}
	row, ok := state.rows[id]
	if !ok {
		return InboxAnswerResponse{}, agentError(409, "already_answered", "this item is no longer waiting")
	}
	if state.pending[id] != nil {
		return InboxAnswerResponse{}, agentError(409, "conflict", "this answer is waiting for Undo")
	}
	if validator, ok := row.source.(inboxValidator); ok {
		if err := validator.ValidateInbox(ctx, row.item, answer); err != nil {
			return InboxAnswerResponse{}, err
		}
	}
	if snapshot, ok := row.source.(interface {
		SnapshotInboxAnswer(InboxItem, InboxAnswerRequest) InboxAnswerRequest
	}); ok {
		answer = snapshot.SnapshotInboxAnswer(row.item, answer)
	}
	p := &inboxPending{row: row, answer: answer, until: state.now().Add(inboxAnswerHold).UTC()}
	if native, ok := row.source.(inboxNativeHold); ok && native.NativeInboxHold(row.item, answer) {
		reply, err := row.source.AnswerInbox(ctx, row.item, answer)
		if err != nil {
			return reply, err
		}
		if reply.UndoUntil == nil {
			return InboxAnswerResponse{}, fmt.Errorf("source approval omitted its undo deadline")
		}
		p.until = *reply.UndoUntil
		p.native = true
		state.pending[id] = p
		_, _ = s.events.Publish(EventInboxUndoWindow, InboxUndoWindowEvent{id, p.until})
		return reply, nil
	}
	state.pending[id] = p
	p.timer = state.after(inboxAnswerHold, func() { s.finishInbox(id, p) })
	_, _ = s.events.Publish(EventInboxUndoWindow, InboxUndoWindowEvent{id, p.until})
	return InboxAnswerResponse{OK: true, UndoUntil: &p.until}, nil
}
func (s *Server) finishInbox(id string, p *inboxPending) {
	state := s.inbox
	state.Lock()
	defer state.Unlock()
	if state.closed || state.pending[id] != p {
		return
	}
	if state.now().Before(p.until) {
		p.timer = state.after(p.until.Sub(state.now()), func() { s.finishInbox(id, p) })
		return
	}
	delete(state.pending, id)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	items, err := p.row.source.InboxItems(ctx)
	if err != nil {
		_, _ = s.events.Publish(EventInboxAdded, InboxAddedEvent{id, p.row.item.Kind})
		return
	}
	found := false
	for _, item := range items {
		if item.ID == id {
			found = true
			break
		}
	}
	if !found {
		return
	}
	reply, err := p.row.source.AnswerInbox(ctx, p.row.item, p.answer)
	if err != nil || !reply.OK {
		_, _ = s.events.Publish(EventInboxAdded, InboxAddedEvent{id, p.row.item.Kind})
		return
	}
	delete(state.rows, id)
	_, _ = s.events.Publish(EventInboxResolved, InboxResolvedEvent{id, "here", "you", state.now().UTC()})
}
func (s *Server) UndoInbox(ctx context.Context, id string, expect *AgentExpect) error {
	state := s.inbox
	state.Lock()
	defer state.Unlock()
	p := state.pending[id]
	if p == nil || !state.now().Before(p.until) {
		return agentError(409, "conflict", "the five-second Undo window has ended")
	}
	if p.native {
		native := p.row.source.(inboxNativeHold)
		if err := native.UndoInbox(ctx, p.row.item, expect); err != nil {
			return err
		}
	} else {
		if expect != nil {
			check := p.answer
			check.Expect = expect
			if validator, ok := p.row.source.(inboxValidator); ok {
				if err := validator.ValidateInbox(ctx, p.row.item, check); err != nil {
					return err
				}
			}
		}
		if p.timer != nil {
			p.timer.Stop()
		}
	}
	delete(state.pending, id)
	_, _ = s.events.Publish(EventInboxAdded, InboxAddedEvent{id, p.row.item.Kind})
	return nil
}

// ResolveInbox reports source answers and withdrawals without a second answer
// store. Source lanes call this after their canonical resolver succeeds.
func (s *Server) ResolveInbox(id, how, by string) {
	s.inbox.Lock()
	defer s.inbox.Unlock()
	if _, ok := s.inbox.rows[id]; !ok {
		return
	}
	if p := s.inbox.pending[id]; p != nil {
		if p.timer != nil {
			p.timer.Stop()
		}
		delete(s.inbox.pending, id)
	}
	delete(s.inbox.rows, id)
	_, _ = s.events.Publish(EventInboxResolved, InboxResolvedEvent{id, how, by, s.inbox.now().UTC()})
}
