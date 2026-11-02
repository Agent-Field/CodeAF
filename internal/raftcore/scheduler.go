package raftcore

import (
	"slices"

	"github.com/Agent-Field/codeaf/internal/clusterstate"
)

// ScheduleInput is an internal host call under the same member queue as Step.
// Now is elapsed monotonic milliseconds, never accumulated missed tick debt.
// Busy includes work, assignments, holds, ingress requests and outbox work.
// Marker and acknowledgments come from durable application/authenticated peers;
// they are never accepted from a public coordination request.
type ScheduleInput struct {
	Now    int64                   `json:"now"`
	Busy   bool                    `json:"busy"`
	Marker clusterstate.Quiescence `json:"marker"`
	Acks   []SleepAck              `json:"acks"`
}
type SleepAck struct {
	Node  uint64 `json:"node"`
	Index uint64 `json:"index"`
	Term  uint64 `json:"term"`
}
type SchedulePlan struct {
	Barrier  bool     `json:"barrier"`
	Tick     bool     `json:"tick"`
	Prepare  bool     `json:"prepare"`
	Sleeping bool     `json:"sleeping"`
	Wake     []uint64 `json:"wake"`
	After    int64    `json:"after"`
}
type scheduling struct {
	started   bool
	now       int64
	active    int64
	next      int64
	marker    uint64
	cancelled uint64
	sleeping  bool
	wake      bool
}

// wakeScheduling invalidates the observed sleep generation before processing
// an operation. A delayed final acknowledgment cannot put that generation back
// to sleep. Core reads still use ReadOnlySafe, even with a remembered leader.
func (c *Core) wakeScheduling() {
	s := &c.schedule
	s.cancelled = max(s.cancelled, s.marker, c.applied)
	s.sleeping = false
	s.active = s.now
	s.next = s.now + clusterstate.TickEveryMillis
	s.wake = true
	c.quorumIndex, c.quorumTerm = 0, 0
}

// Schedule suppresses scheduling only after ALL current voters have durably
// applied one committed marker. It does not use TickQuiesced, a clock lease or
// a reduced quorum. An unreachable member retains its ordinary active timer.
func (c *Core) Schedule(in ScheduleInput) (SchedulePlan, error) {
	var p SchedulePlan
	if c.stopped {
		return p, ErrStopped
	}
	if c.pending != nil {
		return p, ErrBusy
	}
	if in.Now < 0 || in.Now > int64(clusterstate.MaxSafeInteger)-clusterstate.IdleAfterMillis {
		return p, ErrConfig
	}
	s := &c.schedule
	if !s.started {
		s.started = true
		s.now = in.Now
		s.active = in.Now
		s.next = in.Now + clusterstate.TickEveryMillis
	}
	// A backward host sample cannot move a deadline into the past.
	s.now = max(s.now, in.Now)
	conf := Configuration(c.store.conf)
	voters := slices.Clone(conf.Voters)
	for _, id := range conf.Outgoing {
		if !slices.Contains(voters, id) {
			voters = append(voters, id)
		}
	}
	slices.Sort(voters)
	if s.wake {
		p.Wake = voters
		s.wake = false
	}
	last, err := c.store.LastIndex()
	if err != nil {
		return p, err
	}
	busy := in.Busy || conf.Joint() || len(c.reads) != 0 || last != c.applied || c.status().Commit != c.applied
	m := in.Marker
	valid := m.Index != 0 && m.Index == c.applied && m.Term == c.status().Term && m.Barrier+1 == m.Index && slices.Equal(m.Voters, voters)
	if valid {
		s.marker = max(s.marker, m.Index)
	}
	if busy {
		s.cancelled = max(s.cancelled, s.marker, c.applied)
		s.active = s.now
	}
	if (busy || !valid) && s.sleeping {
		c.wakeScheduling()
		p.Wake = voters
		s.wake = false
	}
	if !busy && valid && m.Index > s.cancelled {
		complete := len(in.Acks) == len(voters)
		seen := map[uint64]bool{}
		for _, ack := range in.Acks {
			if !slices.Contains(voters, ack.Node) || seen[ack.Node] || ack.Index != m.Index || ack.Term != m.Term {
				complete = false
			}
			seen[ack.Node] = true
		}
		if complete {
			s.sleeping = true
		}
	}
	p.Sleeping = s.sleeping
	if s.sleeping {
		return p, nil
	}
	p.After = max(int64(1), s.next-s.now)
	if s.now >= s.next {
		p.Tick = true
		s.next = s.now + clusterstate.TickEveryMillis
		p.After = clusterstate.TickEveryMillis
	}
	// Fresh ReadIndex is required before preparing the durable marker. Host
	// activity checks must also cover application intents and unexpired holds.
	eligible := !busy && c.leader() && s.now-s.active >= clusterstate.IdleAfterMillis && (!valid || m.Index <= s.cancelled)
	fresh := c.quorumTerm == c.status().Term && c.quorumIndex >= c.applied
	p.Barrier = eligible && !fresh
	p.Prepare = eligible && fresh
	return p, nil
}
