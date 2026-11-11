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
	mu sync.Mutex
	at map[any]time.Time
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

// retain keeps only the instants of messages something still holds. Like a
// presentation mark, a stamp's key holds the message's whole part array alive;
// keeping stamps for folded or rewound messages would keep their words too.
func (s *stampIndex) retain(live ...[]ai.Message) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.at) == 0 {
		return
	}
	kept := make(map[any]time.Time)
	for _, messages := range live {
		for index := range messages {
			if key := stampKey(messages[index]); key != nil {
				if at, ok := s.at[key]; ok {
					kept[key] = at
				}
			}
		}
	}
	s.at = kept
}

// retainStamps follows the live transcript and its queues at the same boundaries
// as the presentation index, after any departing region has been shaped.
func (s *sessionFile) retainStamps(live ...[]ai.Message) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stamps.retain(live...)
}

// rememberStamp puts back the instant a message was written, for a message a
// boundary let go of and an undo is handing back ([Agent.UndoRewind]): the
// stamps field is read under the file's lock everywhere else.
func (s *sessionFile) rememberStamp(message ai.Message, at time.Time) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.stamps == nil {
		s.stamps = &stampIndex{}
	}
	stamps := s.stamps
	s.mu.Unlock()
	stamps.remember(message, at)
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
