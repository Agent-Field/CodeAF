package relayserve

import (
	"context"
	"crypto/ed25519"
	"sync"
	"time"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
)

// memberTimer has one deadline per identity. A new ingress wakes a quiescent
// group; repeated ingress cannot postpone its already armed maintenance turn.
// Timers stop after the shared all-voter sleep protocol, without idle polling.
type memberTimer struct {
	mu         sync.Mutex
	turn       sync.Mutex
	stop       func() bool
	generation uint64
	closed     bool
	started    time.Time
	after      func(time.Duration, func()) func() bool
	anchor     ed25519.PrivateKey
	cursor     string
}

func (d *nativeNamespace) armMembers() {
	m := &d.timer
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.stop != nil {
		return
	}
	m.generation++
	generation := m.generation
	m.stop = m.after(time.Duration(cs.TickEveryMillis)*time.Millisecond, func() { d.maintainMembers(generation) })
}
func (d *nativeNamespace) maintainMembers(generation uint64) {
	m := &d.timer
	m.turn.Lock()
	defer m.turn.Unlock()
	m.mu.Lock()
	if m.closed || generation != m.generation {
		m.mu.Unlock()
		return
	}
	m.stop = nil
	m.mu.Unlock()
	ctx := context.Background()
	pending, err := d.host.PendingEffects(ctx)
	if err == nil && pending {
		m.cursor, err = d.effects.Replay(ctx, d.now().UnixMilli(), d.links, m.cursor)
	}
	// An unavailable outbox read cannot suppress the election clock. Its own
	// authority checks still precede delivery when a leader becomes available.
	after, maintenanceErr := d.host.Maintain(ctx, max(int64(0), d.now().Sub(m.started).Milliseconds()), d.now().UnixMilli(), m.anchor)
	if err != nil || maintenanceErr != nil || after != 0 {
		d.armMembers()
	}
}
func (d *nativeNamespace) stopMembers() {
	m := &d.timer
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	m.generation++
	if m.stop != nil {
		m.stop()
		m.stop = nil
	}
}
