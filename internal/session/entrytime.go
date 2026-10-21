package session

import (
	"sync"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// WHEN A MESSAGE WAS WRITTEN IS A FACT OF ITS LINE.
//
// Every journal line carries the instant it was written, and a page that reads
// a conversation back wants that instant on each row: a reply that came an hour
// after the question should not claim the question's minute. The message itself
// has no clock, so the instant is kept beside it, keyed the way the
// presentation marks are, by the message's own first part.
//
// A message the file never stamped, and a message a compaction pass rebuilt,
// answers the zero time, which a surface reads as "not known".

// stampIndex is when each message's line was written.
type stampIndex struct {
	mu    sync.Mutex
	at    map[any]time.Time
	lines map[any]int
}

// stampKey is the message's identity for the index: its first part, or its
// first call when it carries no words, and nil when it carries neither.
func stampKey(message ai.Message) any {
	switch {
	case len(message.Content) > 0:
		return &message.Content[0]
	case len(message.ToolCalls) > 0:
		return &message.ToolCalls[0]
	}
	return nil
}

// remember records when the message was written. A zero instant is not kept.
func (s *stampIndex) remember(message ai.Message, at time.Time) {
	key := stampKey(message)
	if s == nil || key == nil || at.IsZero() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.at == nil {
		s.at = make(map[any]time.Time)
	}
	s.at[key] = at
}

// of answers when the message was written, or the zero time.
func (s *stampIndex) of(message ai.Message) time.Time {
	key := stampKey(message)
	if s == nil || key == nil {
		return time.Time{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.at[key]
}

// lineInstant reads a journal line's own timestamp, and the zero time for a
// line that has none or one this build cannot read.
func lineInstant(text string) time.Time {
	at, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return time.Time{}
	}
	return at
}

// written is when the message's line was written, through a journal that may
// be nil.
func (s *sessionFile) written(message ai.Message) time.Time {
	if s == nil {
		return time.Time{}
	}
	s.mu.Lock()
	stamps := s.stamps
	s.mu.Unlock()
	return stamps.of(message)
}

// rememberLine keeps a physical address beside the rebuilt message, so a
// consumer can resolve a recorded origin without matching the person's words.
func (s *stampIndex) rememberLine(message ai.Message, line int) {
	key := stampKey(message)
	if key == nil {
		return
	}
	if s.lines == nil {
		s.lines = make(map[any]int)
	}
	s.lines[key] = line
}

// journalLine is the physical line read for this message, absent on live entries.
func (s *sessionFile) journalLine(message ai.Message) int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	stamps := s.stamps
	s.mu.Unlock()
	if stamps == nil {
		return 0
	}
	stamps.mu.Lock()
	defer stamps.mu.Unlock()
	return stamps.lines[stampKey(message)]
}
