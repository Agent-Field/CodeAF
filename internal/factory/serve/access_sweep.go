package serve

import (
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/factory/live"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

const grantSweepEvery = time.Minute

type grantSweeper struct {
	mu         sync.Mutex
	timer      *time.Timer
	generation uint64
	closed     bool
	callbacks  sync.WaitGroup
}

// sweepGrants tells every past or current viewer of a lapse as the decider
// sees it. Authorization does not wait for the event or the watermark.
func (h *Handler) sweepGrants(now time.Time) error {
	if h.config.Store == nil {
		return nil
	}
	return h.config.Store.SweepGrantLapses(now, func(lapses []store.GrantLapse) error {
		current := h.accessAt(now)
		events := make([]live.Event, 0, len(lapses))
		for _, lapse := range lapses {
			res, found, err := h.lookupAccessResource(lapse.Kind, lapse.ID)
			if err != nil {
				return err
			}
			if !found {
				continue
			}
			past := current.at(lapse.Earliest.Add(-time.Nanosecond))
			raw := current.tuplesFor(res.item)
			before := raw
			before.Grants = slices.DeleteFunc(slices.Clone(raw.Grants), func(g acl.Grant) bool {
				return g.CreatedAt.After(past.time())
			})
			policy := current.rules()
			audience := acl.Union(
				policy.Audience(past.time(), past.policyItem(res.item, before)),
				policy.Audience(now, current.policyItem(res.item, raw)),
			)
			events = append(events, h.accessEvent(res.ref, "", audience))
		}
		return live.AppendGrantLapses(h.eventRoot(), events...)
	})
}

func (h *Handler) startGrantSweep() {
	if h.config.Store == nil {
		return
	}
	h.sweepGrantsAndLog()
	h.scheduleGrantSweep()
}

func (h *Handler) sweepGrantsAndLog() {
	if err := h.sweepGrants(h.clock()); err != nil {
		slog.Error("could not announce expired grants", "error", err)
	}
}

func (h *Handler) scheduleGrantSweep() {
	s := &h.grantSweeper
	s.mu.Lock()
	defer s.mu.Unlock()
	h.armGrantSweepLocked(s)
}

func (h *Handler) armGrantSweepLocked(s *grantSweeper) {
	if s.closed || h.config.Store == nil {
		return
	}
	s.stopTimerLocked()
	generation := s.generation
	now := h.clock()
	delay := grantSweepEvery
	expiry, err := h.config.Store.NextGrantExpiry(now.Truncate(time.Millisecond))
	if err != nil {
		slog.Error("could not read the next grant expiry", "error", err)
	} else if !expiry.IsZero() {
		// Round up so a sub-millisecond expiry is covered by the integer watermark.
		deadline := expiry.Truncate(time.Millisecond)
		if deadline.Before(expiry) {
			deadline = deadline.Add(time.Millisecond)
		}
		delay = min(delay, max(0, deadline.Sub(now)))
	}

	s.callbacks.Add(1)
	s.timer = time.AfterFunc(delay, func() {
		defer s.callbacks.Done()
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.closed || generation != s.generation {
			return
		}
		s.timer = nil
		h.sweepGrantsAndLog()
		h.armGrantSweepLocked(s)
	})
}

// stopTimerLocked discards callbacks from a canceled generation. A callback
// that has already begun counts itself out after releasing the lock.
func (s *grantSweeper) stopTimerLocked() {
	s.generation++
	if s.timer != nil && s.timer.Stop() {
		s.callbacks.Done()
	}
	s.timer = nil
}

func (h *Handler) closeGrantSweep() {
	s := &h.grantSweeper
	s.mu.Lock()
	s.closed = true
	s.stopTimerLocked()
	s.mu.Unlock()
	s.callbacks.Wait()
}
