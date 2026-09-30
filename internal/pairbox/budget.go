package pairbox

import (
	"context"
	"time"
)

// window counts uses inside a span, so a limit is "no more than n in the last
// minute" and not a bucket that refills in lumps.
type window struct{ at []time.Time }

// take records a use now unless max uses already fall inside span, in which
// case it answers how long until the oldest one leaves the window.
func (w *window) take(now time.Time, span time.Duration, max int) (time.Duration, bool) {
	w.trim(now, span)
	if len(w.at) >= max {
		return w.at[0].Add(span).Sub(now), false
	}
	w.at = append(w.at, now)
	return 0, true
}

func (w *window) trim(now time.Time, span time.Duration) {
	keep := 0
	for keep < len(w.at) && !now.Before(w.at[keep].Add(span)) {
		keep++
	}
	w.at = w.at[keep:]
}

// budget is what one network has used: creations, writes and open polls.
type budget struct {
	creates, writes window
	polls           int
}

const (
	createSpan = time.Hour
	writeSpan  = time.Minute
	pollRetry  = time.Second
)

func (b *budget) allowCreate(now time.Time, l Limits) error {
	return refuse(b.creates.take(now, createSpan, l.CreatePerHour))
}

func (b *budget) allowWrite(now time.Time, l Limits) error {
	return refuse(b.writes.take(now, writeSpan, l.WritePerMinute))
}

func refuse(retry time.Duration, ok bool) error {
	if ok {
		return nil
	}
	return RateLimited{RetryAfter: max(retry, time.Second)}
}

// idle reports whether the budget holds nothing worth remembering.
func (b *budget) idle(now time.Time) bool {
	b.creates.trim(now, createSpan)
	b.writes.trim(now, writeSpan)
	return b.polls == 0 && len(b.creates.at) == 0 && len(b.writes.at) == 0
}

// budget is the network's account, made on first use. The caller holds mu.
func (m *Memory) budget(ctx context.Context) *budget {
	peer := peerOf(ctx)
	b, ok := m.peers[peer]
	if !ok {
		b = &budget{}
		m.peers[peer] = b
	}
	return b
}

// enterPoll takes one of the network's concurrent poll slots and answers how to
// give it back.
func (m *Memory) enterPoll(ctx context.Context) (func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b := m.budget(ctx)
	if b.polls >= m.limits.PollsPerIP {
		return nil, RateLimited{RetryAfter: pollRetry}
	}
	b.polls++
	return func() {
		m.mu.Lock()
		b.polls--
		m.mu.Unlock()
	}, nil
}
