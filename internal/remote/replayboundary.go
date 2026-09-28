package remote

import (
	"github.com/Agent-Field/codeaf/internal/session"
	"sync"
)

func (c *Client) rememberReplay(cursor session.ReplayCursor, order uint64) {
	if cursor.Owner == "" {
		return
	}
	c.mu.Lock()
	if order < c.replayOrder {
		c.mu.Unlock()
		return
	}
	c.replayOrder = order
	if c.replayCursor.Owner != cursor.Owner || cursor.Turn > c.replayCursor.Turn {
		c.replayCursor = cursor
	}
	c.mu.Unlock()
}

// ReplayCovers is checked again on the surface loop: a Follow message can
// already be in flight when its atomic replay finishes on another goroutine.
func (a *Agent) ReplayCovers(ev session.Event) bool {
	if ev.ReplayObserved {
		return false
	}
	a.c.mu.Lock()
	defer a.c.mu.Unlock()
	return a.c.replayCursor.Covers(ev.ReplayCursor)
}

// Keep announcing before the first token. The predicate reads identity at
// admission, not at announcement: a hidden tab may receive it after completion.
func (c *Client) followStream(id uint64, said string) {
	s := c.stream(id)
	var discard sync.Once
	covered := func() bool {
		s.mu.Lock()
		cursor := s.replayCursor
		s.mu.Unlock()
		c.mu.Lock()
		owned := c.replayCursor.Covers(cursor)
		c.mu.Unlock()
		if owned {
			discard.Do(func() {
				go func() {
					for range s.events() {
					}
				}()
			})
		}
		return owned
	}
	c.follows(Following{Said: said, Events: s.events(), Covered: covered})
}

func (c *Client) hasReplayBoundary() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.replayCursor.Owner != ""
}
