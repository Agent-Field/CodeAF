package automation

// trust.go is the standing-order half of an automation: the person ratifies
// one before it ever runs, and then lets it run alone only as far as it has
// earned.
//
// ── THE LAWS ──
//
//   - NOTHING RUNS BEFORE RATIFY. An automation an agent proposed is saved as
//     [StatusProposed] so it can be listed and changed, and the clock never
//     takes it: [Store.Due] reads active ones only. [Store.Ratify] is the one
//     door from proposed to active.
//
//   - A RUN OF WORK THAT IS NOT TRUSTED ASKS FIRST. When the clock comes to
//     start one, it parks the run as [PhaseAsking] instead, and the run waits
//     for the person's word ([Store.Answer]). Go starts it; skip passes it
//     over; not right passes it over and puts the ladder back to the bottom.
//     A reminder is a fixed line and a watch only looks, so neither asks.
//
//   - THE LADDER IS COUNTED, NEVER GUESSED. Asks first until the first go,
//     earning trust for the next ones, trusted alone after [TrustAfter] runs
//     the person said go to. One not right resets it.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Trust is how far the person lets an automation run alone.
type Trust string

const (
	// TrustAsksFirst asks before every run, and has had no go yet.
	TrustAsksFirst Trust = "asks_first"
	// TrustEarning asks before every run, and is counting the person's go's.
	TrustEarning Trust = "earning"
	// TrustTrusted runs alone. Empty reads as this.
	TrustTrusted Trust = "trusted"
)

// TrustAfter is how many runs the person says go to before one runs alone.
const TrustAfter = 5

// Scope is where an automation's results are said.
type Scope string

const (
	ScopeChat       Scope = "chat"
	ScopeCrew       Scope = "crew"
	ScopeEverywhere Scope = "everywhere"
	ScopeNotHere    Scope = "not_here"
)

// Reply is the person's word on a run that asked first.
type Reply string

const (
	ReplyGo       Reply = "go"
	ReplySkip     Reply = "skip"
	ReplyNotRight Reply = "not_right"
)

// LineSkipped and LineNotRight are what a passed-over run's row says.
const (
	LineSkipped  = "skipped by you"
	LineNotRight = "you said it was not right"
)

// Trusted reads the ladder, with empty as trusted.
func (a Automation) Trusted() bool { return a.Trust == "" || a.Trust == TrustTrusted }

// NeedsAsk reports whether a run of it waits for the person's go: work that is
// not yet trusted.
func (a Automation) NeedsAsk() bool { return !a.Trusted() && a.Action.Do != "" }

// rewrite reads one automation inside a write, lets change alter it, and saves
// it as the person's change: a new revision, so a clock holding an older copy
// is refused rather than written over.
func (s *Store) rewrite(id string, change func(a *Automation) error) (Automation, error) {
	now := s.now()
	var saved Automation
	err := s.write(func(tx *sql.Tx, seq int64) error {
		a, err := scanAutomation(tx.QueryRow(`SELECT doc, status, next_ms, seen, memo, revision FROM automations WHERE id = ?`, id))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := change(&a); err != nil {
			return err
		}
		a.Revision++
		a.Updated = now
		a.Last = nil
		doc, err := json.Marshal(a)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`UPDATE automations SET doc = ?, status = ?, next_ms = ?, revision = ?, seq = ? WHERE id = ?`,
			string(doc), string(a.Status), ms(a.Next), a.Revision, seq, id)
		saved = a
		return err
	})
	return saved, err
}

// Ratify is the person's yes to a proposed automation: it becomes active and
// wakes on its schedule from now. A one-time moment that passed while it was
// proposed runs once, late.
func (s *Store) Ratify(id string) (Automation, error) {
	now := s.now()
	return s.rewrite(id, func(a *Automation) error {
		if a.Status != StatusProposed {
			return fmt.Errorf("%s is not waiting to be agreed to", a.Title)
		}
		a.Status = StatusActive
		if a.Schedule.Repeats() {
			if a.Schedule.Anchor.IsZero() && a.Schedule.Interval() > 0 {
				a.Schedule.Anchor = now
			}
			next, err := a.Schedule.Next(now)
			if err != nil {
				return err
			}
			a.Next = next
		} else {
			a.Next = a.Schedule.At
		}
		return nil
	})
}

// Stop ends an automation for good. It keeps its history and wakes for
// nothing; the run in hand, if any, is asked to stop.
func (s *Store) Stop(id string) (Automation, error) {
	saved, err := s.rewrite(id, func(a *Automation) error {
		a.Status = StatusStopped
		a.Next = time.Time{}
		return nil
	})
	if err != nil {
		return saved, err
	}
	active, err := s.queryRuns(`WHERE automation_id = ? AND phase != ?`, id, string(PhaseOver))
	if err != nil {
		return saved, err
	}
	for _, run := range active {
		if run.Phase == PhaseAsking {
			_ = s.passOver(run, LineSkipped)
			continue
		}
		_ = s.RequestStop(run.ID)
	}
	return saved, nil
}

// SetTrust puts the ladder where the person says: trusted at once, or back to
// asking first.
func (s *Store) SetTrust(id string, trust Trust) (Automation, error) {
	return s.rewrite(id, func(a *Automation) error {
		switch trust {
		case TrustAsksFirst:
			a.Trust, a.Approved = TrustAsksFirst, 0
		case TrustTrusted:
			a.Trust = TrustTrusted
		default:
			return fmt.Errorf("trust cannot be set to %q", trust)
		}
		return nil
	})
}

// AskRun parks a queued run until the person says go.
func (s *Store) AskRun(id int64) error {
	return s.write(func(tx *sql.Tx, seq int64) error {
		_, err := tx.Exec(`UPDATE runs SET phase = ?, seq = ? WHERE id = ? AND phase = ?`, string(PhaseAsking), seq, id, string(PhaseQueued))
		return err
	})
}

// Asking is every run waiting for the person's word, oldest first.
func (s *Store) Asking() ([]Run, error) {
	return s.queryRuns(`WHERE phase = ? ORDER BY id`, string(PhaseAsking))
}

// Answer is the person's word on a run that asked first. Go queues it again
// with the person's grant, so the clock starts it on its next look, and counts
// one more step up the ladder; skip passes it over; not right passes it over
// and resets the ladder.
func (s *Store) Answer(runID int64, reply Reply) (Automation, error) {
	run, err := s.Run(runID)
	if err != nil {
		return Automation{}, err
	}
	if run.Phase != PhaseAsking {
		return Automation{}, errors.New("that run is not waiting for you")
	}
	switch reply {
	case ReplyGo:
		saved, err := s.rewrite(run.AutomationID, func(a *Automation) error {
			a.Granted = runID
			a.Approved++
			switch {
			case a.Approved >= TrustAfter:
				a.Trust = TrustTrusted
			default:
				a.Trust = TrustEarning
			}
			return nil
		})
		if err != nil {
			return saved, err
		}
		return saved, s.write(func(tx *sql.Tx, seq int64) error {
			_, err := tx.Exec(`UPDATE runs SET phase = ?, seq = ? WHERE id = ? AND phase = ?`, string(PhaseQueued), seq, runID, string(PhaseAsking))
			return err
		})
	case ReplySkip:
		if err := s.passOver(run, LineSkipped); err != nil {
			return Automation{}, err
		}
		return s.Get(run.AutomationID)
	case ReplyNotRight:
		if err := s.passOver(run, LineNotRight); err != nil {
			return Automation{}, err
		}
		return s.SetTrust(run.AutomationID, TrustAsksFirst)
	}
	return Automation{}, fmt.Errorf("%q is not an answer to a run", reply)
}

// passOver ends a run that asked first without running it.
func (s *Store) passOver(run Run, line string) error {
	return s.write(func(tx *sql.Tx, seq int64) error {
		_, err := tx.Exec(`UPDATE runs SET phase = ?, finished_ms = ?, outcome = ?, line = ?, seq = ? WHERE id = ? AND phase = ?`,
			string(PhaseOver), ms(s.now()), string(OutcomeSkipped), line, seq, run.ID, string(PhaseAsking))
		return err
	})
}
