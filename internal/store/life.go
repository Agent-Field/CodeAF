package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// A job's life is longer than its current attempt, and until this read existed
// nothing could say so.
//
// `started_at` is a CLAIM stamp. Release sets it back to NULL (lifecycle.go) and
// the next Start writes a new one, so on a node that was restarted the column
// answers "how long has this attempt been going" while every reader in the
// product was asking "how long has this work been going". A job thirty-two
// minutes and six finished parts deep, whose root had been restarted a minute
// earlier, therefore read as a job that had just started — and a head composing
// a sentence out of that read said exactly that to the person watching the
// thirty-two minutes tick by. The read was wrong before the sentence was.
//
// The stamp that does not move is the journal's own: the event that admitted the
// node. It is written once, at splice, and no lifecycle transition can rewrite
// it. Attempt is beside it because the two facts only make sense together — a
// long clock with no attempt count invites "then why has nothing happened", and
// an attempt count with a short clock is the defect this file exists to close.

// JobLife is one node's whole run history: when the work was admitted, how many
// attempts it has taken, and when the attempt now in flight started.
//
// ADMITTED IS THE JOB'S CLOCK; ATTEMPTSTARTED IS THE ATTEMPT'S. Both are
// returned because both are true, and a caller that needs to say "restarted a
// minute ago" is asking a different question from one that needs to say "at it
// for thirty-two minutes". What no caller may do is spend the second where the
// first was asked for, which is the whole of the incident behind this type.
type JobLife struct {
	NodeID string
	Status Status
	// Admitted is the journal stamp of the event that created this node — the
	// moment the work was spliced in. Zero only for a node whose creating event
	// predates the journal it was rebuilt from, which is absence and not "now".
	Admitted time.Time
	// Attempt is the node's claim counter: 1 is the first go at it, 2 the first
	// restart. Zero means never claimed.
	Attempt uint64
	// AttemptStarted is the CURRENT attempt's start stamp, the column a restart
	// resets. It is carried so a caller can say when the restart happened, never
	// so it can pass for the job's age.
	AttemptStarted time.Time
	FinishedAt     time.Time
}

// Restarted reports work that has been picked up more than once. It is the
// presence bit for the attempt clause: a job on its first attempt says nothing
// about attempts at all, because "first attempt" is noise on every job that has
// never gone wrong.
func (life JobLife) Restarted() bool { return life.Attempt > 1 }

// Live reports work that has been admitted and has not stopped.
func (life JobLife) Live() bool {
	return life.FinishedAt.IsZero() && !terminal(life.Status)
}

// Elapsed is how long this work has been going, measured from admission and NOT
// from the current attempt. A job still running is measured against now; a job
// that settled is measured to its finish. The second return is presence: a node
// with no admission stamp has no clock, which is a different sentence from a
// clock reading zero.
func (life JobLife) Elapsed(now time.Time) (time.Duration, bool) {
	return span(life.Admitted, life.FinishedAt, life.Live(), now)
}

// SinceRestart is how long the current attempt has been going. It is absent on
// work that has only ever had one attempt, because there is no restart to date.
func (life JobLife) SinceRestart(now time.Time) (time.Duration, bool) {
	if !life.Restarted() {
		return 0, false
	}
	return span(life.AttemptStarted, life.FinishedAt, life.Live(), now)
}

// AttemptWords is the attempt count as a phrase, empty on a first attempt.
// Ordinals rather than "attempt 2" because the reader is a person or a model
// composing for one, and neither says "attempt 2" out loud.
func (life JobLife) AttemptWords() string {
	switch {
	case life.Attempt <= 1:
		return ""
	case life.Attempt == 2:
		return "second attempt"
	case life.Attempt == 3:
		return "third attempt"
	case life.Attempt == 4:
		return "fourth attempt"
	default:
		return fmt.Sprintf("%dth attempt", life.Attempt)
	}
}

// NodeLife reads one node's run history in a single query.
//
// The admission stamp comes off the journal row the node's created_seq already
// points at — the same join surgery.go has been doing inline to age a search
// result — so this read invents no new bookkeeping and cannot disagree with the
// event log. The second return is presence.
func (s *Store) NodeLife(id string) (JobLife, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return JobLife{}, false, fmt.Errorf("node life: %w: empty id", ErrInvalid)
	}
	var (
		life                        JobLife
		admitted, started, finished sql.NullString
	)
	err := s.db.QueryRow(`
		SELECT node.status, node.attempt, node.started_at, node.finished_at, admitted.ts
		FROM nodes AS node
		LEFT JOIN events AS admitted ON admitted.seq = node.created_seq
		WHERE node.id = ?`, id).
		Scan(&life.Status, &life.Attempt, &started, &finished, &admitted)
	if errors.Is(err, sql.ErrNoRows) {
		return JobLife{}, false, nil
	}
	if err != nil {
		return JobLife{}, false, fmt.Errorf("node life %q: %w", id, err)
	}
	life.NodeID = id
	for _, stamp := range []struct {
		raw   sql.NullString
		field *time.Time
		what  string
	}{
		{admitted, &life.Admitted, "admission"},
		{started, &life.AttemptStarted, "start"},
		{finished, &life.FinishedAt, "finish"},
	} {
		if !stamp.raw.Valid || stamp.raw.String == "" {
			continue
		}
		at, parseErr := parseTime(stamp.raw.String)
		if parseErr != nil {
			return JobLife{}, false, fmt.Errorf("node life %q: parse %s time: %w", id, stamp.what, parseErr)
		}
		*stamp.field = at
	}
	return life, true, nil
}

// notFiledAway is the SQL predicate for "this row is not history".
//
// Folding files SETTLED work away, and every verb below already refuses work
// that has settled — so `folded = 0` beside a status gate was redundant on
// every row except the one it must not refuse: a node that is still running,
// claimed or pending under a lineage that was filed. On that row the redundant
// clause turned "stop that" into "there is no such work", which is the shape a
// read defect takes when it reaches a person as a decision.
const notFiledAway = `(folded = 0 OR status NOT IN ('done', 'failed', 'cancelled'))`
