package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

const phonePartialInterval = 500 * time.Millisecond

// appClientStream exposes only the latest published partial per chat. A
// trailing timer makes even a lone token visible without waiting for another.
type appClientStream struct {
	mu     sync.Mutex
	chats  map[string]*appClientPartial
	last   map[string]time.Time
	closed bool
}

type appClientPartial struct {
	current appwire.Entry
	visible appwire.Entry
	at      time.Time
	timer   *time.Timer
}

func (s *appClientStream) delta(d appwire.TurnDelta, changed func()) {
	if d.Kind != appwire.DeltaText || d.Text == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if s.chats == nil {
		s.chats = map[string]*appClientPartial{}
		s.last = map[string]time.Time{}
	}
	p := s.chats[d.Agent]
	if p == nil {
		p = &appClientPartial{at: s.last[d.Agent]}
		s.chats[d.Agent] = p
	}
	if p.current.Turn != d.Turn || p.current.Msg == nil || *p.current.Msg != d.Msg {
		msg := d.Msg
		p.current = appwire.Entry{ID: fmt.Sprintf("live-%d-%d", d.Turn, d.Msg), Agent: d.Agent, Turn: d.Turn, Msg: &msg, Kind: appwire.EntryAnswer, At: time.Now()}
	}
	p.current.Text += d.Text
	publish := func() {
		p.visible = p.current
		p.at = time.Now()
		s.last[d.Agent] = p.at
		changed()
	}
	if wait := phonePartialInterval - time.Since(p.at); wait <= 0 {
		publish()
	} else if p.timer == nil {
		p.timer = time.AfterFunc(wait, func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.closed || s.chats[d.Agent] != p {
				return
			}
			p.timer = nil
			publish()
		})
	}
}

func (s *appClientStream) entry(agent string) appwire.Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.chats[agent]; p != nil {
		return p.visible
	}
	return appwire.Entry{}
}

func (s *appClientStream) clear(agent string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.chats[agent]; p != nil && p.timer != nil {
		p.timer.Stop()
	}
	delete(s.chats, agent)
}

func (s *appClientStream) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	for _, p := range s.chats {
		if p.timer != nil {
			p.timer.Stop()
		}
	}
	s.chats = nil
	s.last = nil
}

// appClientEngine keeps the ordinary appwire types while including the
// streamed answer that has not reached the durable transcript yet.
type appClientEngine struct{ *appBackend }

func (e appClientEngine) Entries(id, before string, limit int) ([]appwire.Entry, bool, error) {
	rows, more, err := e.appBackend.Entries(id, before, limit)
	if err != nil || before != "" {
		return rows, more, err
	}
	partial := e.clientStream.entry(id)
	if partial.Text == "" {
		return rows, more, nil
	}
	for _, row := range rows {
		if row.Turn == partial.Turn && row.Msg != nil && partial.Msg != nil && *row.Msg == *partial.Msg {
			return rows, more, nil
		}
	}
	rows = append(rows, partial)
	if limit > 0 && len(rows) > limit {
		rows = rows[len(rows)-limit:]
		more = true
	}
	return rows, more, nil
}
