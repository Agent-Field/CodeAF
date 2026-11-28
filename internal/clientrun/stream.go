package clientrun

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/clientproto"
)

// Stream coalesces streaming chat text deltas to clientproto.StreamCoalesce
// (250 ms) with a leading-edge publication for the first token of a message or
// the first token after a 250 ms quiet gap (design 7.1 and 7.3, contract L1).
type Stream struct {
	mu      sync.Mutex
	chats   map[string]*streamPartial
	now     func() time.Time
	after   func(time.Duration, func()) func()
	changed func()
	closed  bool
}

type streamPartial struct {
	timerVersion uint64
	current      appwire.Entry
	visible      appwire.Entry
	at           time.Time
	lastToken    time.Time
	cancel       func()
	text         strings.Builder
}

// NewStream returns an engine-agnostic stream coalescer that calls changed
// whenever a leading or trailing partial becomes visible.
func NewStream(now func() time.Time, after func(time.Duration, func()) func(), changed func()) *Stream {
	if now == nil {
		now = time.Now
	}
	if after == nil {
		after = func(d time.Duration, fn func()) func() {
			timer := time.AfterFunc(d, fn)
			return func() { timer.Stop() }
		}
	}
	return &Stream{
		chats:   map[string]*streamPartial{},
		now:     now,
		after:   after,
		changed: changed,
	}
}

// Delta records one streaming turn delta and triggers changed immediately on
// the leading edge or schedules a trailing 250 ms publication.
func (s *Stream) Delta(d appwire.TurnDelta) {
	if d.Kind != appwire.DeltaText || d.Text == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if s.chats == nil {
		s.chats = map[string]*streamPartial{}
	}
	p := s.chats[d.Agent]
	if p == nil {
		p = &streamPartial{}
		s.chats[d.Agent] = p
	}
	now := s.now()
	first := p.current.Turn != d.Turn || p.current.Msg == nil || *p.current.Msg != d.Msg
	if first {
		p.text.Reset()
		msg := d.Msg
		p.current = appwire.Entry{
			ID:    fmt.Sprintf("live-%d-%d", d.Turn, d.Msg),
			Agent: d.Agent,
			Turn:  d.Turn,
			Msg:   &msg,
			Kind:  appwire.EntryAnswer,
			At:    now,
		}
	}
	quiet := !p.lastToken.IsZero() && now.Sub(p.lastToken) >= clientproto.StreamCoalesce
	p.lastToken = now
	p.text.WriteString(d.Text)
	publish := func(at time.Time) {
		p.current.Text = p.text.String()
		p.visible = p.current
		p.at = at
		if s.changed != nil {
			s.changed()
		}
	}
	if wait := clientproto.StreamCoalesce - now.Sub(p.at); first || quiet || wait <= 0 {
		// A token can obtain the lock before a delayed timer callback. Cancel
		// that timer and invalidate callbacks already waiting for this lock.
		if p.cancel != nil {
			p.cancel()
			p.cancel = nil
			p.timerVersion++
		}
		publish(now)
	} else if p.cancel == nil {
		p.timerVersion++
		version := p.timerVersion
		p.cancel = s.after(wait, func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.closed || s.chats[d.Agent] != p || p.timerVersion != version {
				return
			}
			p.cancel = nil
			publish(s.now())
		})
	}
}

// Entry returns the latest coalesced partial entry for agent, or the zero
// Entry when none is active.
func (s *Stream) Entry(agent string) appwire.Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.chats[agent]; p != nil {
		return p.visible
	}
	return appwire.Entry{}
}

// Flush promotes any buffered streaming text into the visible partial at once
// so an on:true watching transition publishes the latest token immediately.
func (s *Stream) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	now := s.now()
	for _, p := range s.chats {
		if p.cancel != nil {
			p.cancel()
			p.cancel = nil
			p.timerVersion++
		}
		if p.text.Len() > 0 && p.visible.Text != p.text.String() {
			p.current.Text = p.text.String()
			p.visible = p.current
			p.at = now
		}
	}
}

// Clear drops any active partial for agent and cancels its trailing timer.
func (s *Stream) Clear(agent string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.chats[agent]; p != nil && p.cancel != nil {
		p.cancel()
	}
	delete(s.chats, agent)
}

// Stop cancels all pending timers and prevents further publications.
func (s *Stream) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	for _, p := range s.chats {
		if p.cancel != nil {
			p.cancel()
		}
	}
	s.chats = nil
}
