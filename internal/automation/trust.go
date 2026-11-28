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
//     the person said go to. One not right resets it. "Go, and stop asking"
//     ([ReplyAlways]) is a go that also climbs the whole ladder at once.
//
//   - ONE RUN WAITS PER ORDER. The clock passes over a slot that comes while a
//     run of the order is waiting, and [Store.AskRun] is the store's own
//     guard behind it: a second run that comes to wait beside one already
//     waiting is folded into it ([LineFolded]) rather than asked about. Five
//     asks for the same work would be five go's to give and the same work run
//     five times.
//
//   - TRUSTING AN ORDER RELEASES THE RUN WAITING ON IT. Nobody is left being
//     asked about an order that no longer asks: [Store.SetTrust] to trusted
//     starts the waiting run, and a go that says "always" starts it too.

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
	ReplyGo Reply = "go"
	// ReplyAlways is "Go, and stop asking": this run goes, as a go does and
	// counted as one, and the order is trusted from now on, so its next runs
	// start alone.
	ReplyAlways   Reply = "always"
	ReplySkip     Reply = "skip"
	ReplyNotRight Reply = "not_right"
)

// LineSkipped and LineNotRight are what a passed-over run's row says;
// LineFolded is what a run says that came to wait beside one already waiting
// for the person, and was folded into it.
const (
	LineSkipped  = "skipped by you"
	LineNotRight = "you said it was not right"
	LineFolded   = "folded into the run already waiting for you"
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
	return s.rewriteIn(id, func(_ *sql.Tx, _ int64, a *Automation) error { return change(a) })
}

// rewriteIn is [Store.rewrite] for a change that must write runs in the same
// transaction as the automation, so no reader sees one without the other.
func (s *Store) rewriteIn(id string, change func(tx *sql.Tx, seq int64, a *Automation) error) (Automation, error) {
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
		if err := change(tx, seq, &a); err != nil {
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
//
// TRUSTING RELEASES THE RUN WAITING ON THE ORDER. An order that no longer asks
// must not leave a question open about it, so the run that was waiting for the
// person's go is queued as a go would queue it, in the same write as the
// trust. It is the person's trust and not a go: the ladder's count of go's
// does not move. A paused order keeps its waiting run waiting, because the
// person's pause said not to run it.
func (s *Store) SetTrust(id string, trust Trust) (Automation, error) {
	return s.rewriteIn(id, func(tx *sql.Tx, seq int64, a *Automation) error {
		switch trust {
		case TrustAsksFirst:
			a.Trust, a.Approved = TrustAsksFirst, 0
		case TrustTrusted:
			a.Trust = TrustTrusted
			return s.releaseWaiting(tx, seq, a)
		default:
			return fmt.Errorf("trust cannot be set to %q", trust)
		}
		return nil
	})
}

// releaseWaiting queues the oldest run of a trusted, active order that is
// waiting for the person's go, and folds any other into it.
func (s *Store) releaseWaiting(tx *sql.Tx, seq int64, a *Automation) error {
	if a.Status != StatusActive {
		return nil
	}
	var first int64
	err := tx.QueryRow(`SELECT id FROM runs WHERE automation_id = ? AND phase = ? ORDER BY id LIMIT 1`, a.ID, string(PhaseAsking)).Scan(&first)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	a.Granted = first
	if _, err := tx.Exec(`UPDATE runs SET phase = ?, seq = ? WHERE id = ? AND phase = ?`, string(PhaseQueued), seq, first, string(PhaseAsking)); err != nil {
		return err
	}
	return s.foldWaiting(tx, seq, a.ID, first)
}

// foldWaiting ends every run of the order that is waiting for the person's go,
// except keep, as folded into that one.
func (s *Store) foldWaiting(tx *sql.Tx, seq int64, automationID string, keep int64) error {
	_, err := tx.Exec(`UPDATE runs SET phase = ?, finished_ms = ?, outcome = ?, line = ?, seq = ? WHERE automation_id = ? AND phase = ? AND id != ?`,
		string(PhaseOver), ms(s.now()), string(OutcomeSkipped), LineFolded, seq, automationID, string(PhaseAsking), keep)
	return err
}

// AskRun parks a queued run until the person says go, and keeps when.
//
// ONE RUN WAITS PER ORDER. The clock already passes over a slot that comes
// while a run is waiting; this is the guard in the store itself, for the run
// that got past it (a "run now" that landed in the same moment as a slot, or
// an older build's). A run that would wait beside one already waiting is not
// parked: it ends at once as [LineFolded], and the run waiting keeps its id and
// the time it began to wait, so an answer on its way to it still lands.
func (s *Store) AskRun(id int64) error {
	now := s.now()
	return s.write(func(tx *sql.Tx, seq int64) error {
		var beside int
		if err := tx.QueryRow(`SELECT count(*) FROM runs WHERE phase = ? AND id != ? AND automation_id = (SELECT automation_id FROM runs WHERE id = ?)`,
			string(PhaseAsking), id, id).Scan(&beside); err != nil {
			return err
		}
		if beside > 0 {
			_, err := tx.Exec(`UPDATE runs SET phase = ?, finished_ms = ?, outcome = ?, line = ?, seq = ? WHERE id = ? AND phase = ?`,
				string(PhaseOver), ms(now), string(OutcomeSkipped), LineFolded, seq, id, string(PhaseQueued))
			return err
		}
		res, err := tx.Exec(`UPDATE runs SET phase = ?, seq = ? WHERE id = ? AND phase = ?`, string(PhaseAsking), seq, id, string(PhaseQueued))
		if err != nil {
			return err
		}
		if parked, _ := res.RowsAffected(); parked == 0 {
			return nil
		}
		return noteAsked(tx, id, now)
	})
}

// Asking is every run waiting for the person's word, oldest first, each with
// when it began to wait.
func (s *Store) Asking() ([]Run, error) {
	return s.queryRuns(`WHERE phase = ? ORDER BY id`, string(PhaseAsking))
}

// Answer is the person's word on a run that asked first. Go queues it again
// with the person's grant, so the clock starts it on its next look, and counts
// one more step up the ladder; always is a go that also trusts the order, so
// it never asks again; skip passes it over; not right passes it over and
// resets the ladder.
//
// A GO RUNS THE WORK ONCE. Any other run of the same order still waiting (an
// older build could leave several) is folded into the one that was answered,
// so the same work is never asked about, or run, twice.
func (s *Store) Answer(runID int64, reply Reply) (Automation, error) {
	run, err := s.Run(runID)
	if err != nil {
		return Automation{}, err
	}
	if run.Phase != PhaseAsking {
		return Automation{}, errors.New("that run is not waiting for you")
	}
	switch reply {
	case ReplyGo, ReplyAlways:
		saved, err := s.rewrite(run.AutomationID, func(a *Automation) error {
			a.Granted = runID
			a.Approved++
			switch {
			case reply == ReplyAlways || a.Approved >= TrustAfter:
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
			if _, err := tx.Exec(`UPDATE runs SET phase = ?, seq = ? WHERE id = ? AND phase = ?`, string(PhaseQueued), seq, runID, string(PhaseAsking)); err != nil {
				return err
			}
			return s.foldWaiting(tx, seq, run.AutomationID, runID)
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
