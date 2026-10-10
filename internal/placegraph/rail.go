package placegraph

import (
	"fmt"
	"time"
)

// The rail's Open section (design Places §10a P-10a-9..15, §6d P-6d-6/7): the
// places a person has gone to and not put away, newest first, below the Pinned
// section. It is global across windows (DESIGN-QUESTIONS 2), so it lives in
// places.json beside Pinned.

// OpenIdle is how long an Open row may go untouched before Sweep closes it.
const OpenIdle = 12 * time.Hour

// MaxOpen bounds the section so a runaway caller cannot grow the file; the
// oldest rows fall off first.
const MaxOpen = 200

// OpenRow is one place in the Open section. Closed means the person closed it
// while work in it was still running: it stays, muted, until that work settles.
type OpenRow struct {
	PlaceID   string    `json:"placeId"`
	TouchedAt time.Time `json:"touchedAt"`
	Closed    bool      `json:"closed,omitempty"`
}

// Busy reports whether a place still has work running in it. The store knows
// nothing about sessions, so the caller supplies the answer; nil means idle.
type Busy func(placeID string) bool

func (b Busy) is(id string) bool { return b != nil && b(id) }

// pruneOpen drops rows whose place is missing or archived, pinned places (they
// live in Pinned, never in Open) and duplicates. Open is soft state, so damage
// here is silently healed instead of refusing the document.
func (st *State) pruneOpen(places map[string]*Place) {
	if len(st.Open) == 0 {
		return
	}
	seen := map[string]bool{}
	out := st.Open[:0:0]
	for _, r := range st.Open {
		p := places[r.PlaceID]
		if p == nil || p.Archived || seen[r.PlaceID] || containsString(st.Pinned, r.PlaceID) {
			continue
		}
		seen[r.PlaceID] = true
		out = append(out, r)
	}
	if len(out) > MaxOpen {
		out = out[:MaxOpen]
	}
	st.Open = out
}

func (st *State) dropOpen(id string) {
	out := st.Open[:0:0]
	for _, r := range st.Open {
		if r.PlaceID != id {
			out = append(out, r)
		}
	}
	st.Open = out
}

// soft applies fn to a fresh copy and writes it WITHOUT a revision bump or a
// receipt, the way TouchOpened does, so rail upkeep never breaks an undo.
func (s *Store) soft(fn func(st *State, now time.Time) error) error {
	release, err := s.acquire()
	if err != nil {
		return err
	}
	defer release()
	cur, err := s.loadLocked()
	if err != nil {
		return err
	}
	st := cur.clone()
	if err := fn(st, s.opts.Now().UTC()); err != nil {
		return err
	}
	if _, err := validateState(st, false); err != nil {
		return err
	}
	return s.writeLocked(st)
}

// Visit records that a place was gone to: it stamps LastOpenedAt and, unless the
// place is pinned, moves it to the top of Open (reopening it if it was closed).
func (s *Store) Visit(id string) error {
	return s.soft(func(st *State, now time.Time) error {
		p, err := st.needPlace(id)
		if err != nil {
			return err
		}
		if p.Archived {
			return ErrArchived
		}
		p.LastOpenedAt = now
		if containsString(st.Pinned, id) {
			return nil
		}
		st.dropOpen(id)
		st.Open = append([]OpenRow{{PlaceID: id, TouchedAt: now}}, st.Open...)
		return nil
	})
}

// Close puts a place away. Busy: the row stays, Closed, until it settles (Sweep
// removes it). Pinned places have no Open row and stay pinned.
func (s *Store) Close(id string, busy Busy) error {
	return s.soft(func(st *State, _ time.Time) error {
		for i := range st.Open {
			if st.Open[i].PlaceID != id {
				continue
			}
			if busy.is(id) {
				st.Open[i].Closed = true
			} else {
				st.dropOpen(id)
			}
			return nil
		}
		return nil
	})
}

// Sweep drops Closed rows that have settled and Open rows untouched for OpenIdle
// that are not busy. Pinned places are never in Open, so they never auto-close,
// and nothing here follows a parent edge: a pinned parent pulls no children in.
func (s *Store) Sweep(now time.Time, busy Busy) error {
	return s.soft(func(st *State, _ time.Time) error {
		out := st.Open[:0:0]
		for _, r := range st.Open {
			switch {
			case busy.is(r.PlaceID):
			case r.Closed:
				continue
			case now.Sub(r.TouchedAt) >= OpenIdle:
				continue
			}
			out = append(out, r)
		}
		st.Open = out
		return nil
	})
}

// Reorder sets the Pinned order to ids. Ids that are not pinned are ignored;
// pinned ids not listed keep their relative order after the listed ones.
func (s *Store) Reorder(ids []string) (Receipt, error) {
	return s.mutate(func(st *State, _ time.Time) (*change, error) {
		before := append([]string{}, st.Pinned...)
		next := make([]string, 0, len(before))
		for _, id := range ids {
			if containsString(before, id) && !containsString(next, id) {
				next = append(next, id)
			}
		}
		for _, id := range before {
			if !containsString(next, id) {
				next = append(next, id)
			}
		}
		if equalStrings(before, next) {
			return nil, nil
		}
		st.Pinned = next
		return &change{ActionReorder, fmt.Sprint(len(next))}, nil
	})
}
