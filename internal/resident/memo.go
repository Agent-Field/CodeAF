package resident

import (
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// A pass is written as a sequence of independent lanes, and that is deliberate:
// no lane may assume another one already asked the store something, because the
// order they run in is a scheduling detail and the one before it may have
// written. The cost of that independence is that the same read is taken two,
// three, a dozen times in one pass — every unresolved question, the whole
// active view, every taste verdict — and on the overwhelmingly common pass none
// of the lanes in between wrote a thing, so every one of those reads returns
// the bytes the last one returned.
//
// Hoisting the read by hand removes the cost and the guarantee together: the
// write in between is rare, but when it happens the later reader MUST see it,
// and a slice hoisted to the top of the pass silently would not. So the sharing
// is made conditional instead of unconditional, and the condition is the thing
// that already summarizes "has anything at all changed" — the journal
// watermark. Every write to this store appends to it; reading it is one indexed
// lookup through a prepared statement. A caller is handed the previous answer
// only where re-deriving would provably have produced the same one.
type journalMemo[T any] struct {
	filled bool
	seq    int64
	value  T
}

// read answers from the memo when the journal has not moved since it was
// filled, and from derive otherwise. A watermark that cannot be read is not an
// error here — it only means nothing may be reused, so the derivation runs.
func (m *journalMemo[T]) read(graph *store.Store, derive func() (T, error)) (T, error) {
	seq, err := graph.LatestEventSeq()
	if err != nil {
		return derive()
	}
	if m.filled && m.seq == seq {
		return m.value, nil
	}
	value, err := derive()
	if err != nil {
		var zero T
		return zero, err
	}
	m.filled, m.seq, m.value = true, seq, value
	return value, nil
}

// journalGate is the memo's shape aimed at a lane rather than at a value: a
// pass whose whole job is to make the world agree with the journal has nothing
// to do until the journal says something new.
//
// It exists because the change gate is not the whole story. A tick runs in full
// whenever anything at all is waiting — a clock deadline, the standing ceiling
// that fires every thirty seconds however still the store is — and every lane
// in it then re-derives from scratch. For a lane whose derivation is a scan of
// the fact shelf and a walk of the filesystem, that is the difference between
// asking the operating system twice a second and asking it twice a minute, and
// the two ask exactly the same question of exactly the same bytes.
type journalGate struct {
	ran bool
	seq int64
}

// due reports whether the journal has moved since this lane last ran. A
// watermark that cannot be read answers yes: a lane that cannot prove it is
// looking at unchanged state must look.
func (g *journalGate) due(graph *store.Store) bool {
	seq, err := graph.LatestEventSeq()
	if err != nil {
		return true
	}
	if g.ran && g.seq == seq {
		return false
	}
	// Stamped before the work, not after. A lane that journals something has
	// moved the watermark past its own stamp and runs once more, which is what
	// it would have done anyway.
	g.ran, g.seq = true, seq
	return true
}

// timedMemo holds a derivation for a fixed span of wall clock, for the one read
// the watermark cannot help with.
//
// The watermark works because the lanes that share a read run on passes where
// nothing was written between them. A lane that only runs on passes where
// something HAS been written gets no benefit from it at all — the memo would
// miss every single time — and the practice loop is exactly that lane. So its
// one very expensive derivation is held for a span instead, and the span is
// chosen against what reads it: practice fires at most twice a day, behind a
// twenty-minute idleness test and a one-minute charter poll, so a metric up to
// a minute old changes no decision that clock is capable of making.
type timedMemo[T any] struct {
	filled bool
	at     time.Time
	value  T
}

// read answers from the memo while it is younger than ttl. A zero ttl disables
// the memo entirely, which is what a test with a frozen clock wants.
func (m *timedMemo[T]) read(now time.Time, ttl time.Duration, derive func() (T, error)) (T, error) {
	if ttl > 0 && m.filled && !m.at.IsZero() && now.Sub(m.at) < ttl && !now.Before(m.at) {
		return m.value, nil
	}
	value, err := derive()
	if err != nil {
		var zero T
		return zero, err
	}
	m.filled, m.at, m.value = true, now, value
	return value, nil
}
