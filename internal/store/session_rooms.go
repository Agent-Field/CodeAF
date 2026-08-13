package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Rooms as places, and the three reads a surface needs to keep them honest.
//
// The sessions table above is a projection of every room that has ever been
// named, which is exactly right for a list and exactly wrong for the two
// questions a window asks at launch: WHICH room am I coming back to, and which
// of these rooms is a room at all. A launch that could not answer the first
// minted a fresh id every time it opened; a `+ new room` that could not answer
// the second minted a second empty room on top of the empty room already
// standing there. Five "untitled room" rows is what those two questions look
// like unanswered — not a naming failure, a MINTING failure (rail-rooms
// grooming A2, design-law-v2 §2: "launch resumes the last thread; empty rooms
// are reused/reaped").
//
// All three reads are here rather than in the caller because all three are one
// query each and none of them is a policy: what a room is, whether anybody has
// spoken in it, and which one was spoken in last are facts the journal already
// holds.

// sessionDiscardedPayload is a room being taken back on the wire — one that was
// opened, never spoken in, and never named.
//
// It carries the id alone. There is no reason field: the only condition under
// which a room may be discarded is the one ReapEmptySessions checks, so a
// reason would be a sentence restating the rule that let the event be written.
type sessionDiscardedPayload struct {
	SessionID string `json:"session_id"`
}

// LatestSession names the room a launch comes back to.
//
// The honest answer is "the room the PERSON was last in", and the journal has
// two candidates for it that disagree in exactly one case. The newest session
// row by activity is the wrong one: a job delivering at 03:00 into the room
// that commissioned it raises that room's activity mark, and coming back to
// that room the next morning would be the machine deciding where the
// conversation continues. The newest USER message is the right one — a room a
// person spoke in last is the conversation they were having, whether they left
// it by switching rooms in the window or by closing the lid.
//
// The fallback below it is for a store with rooms and no words in any of them:
// the newest row by activity, so a launch that follows an opened-but-unspoken
// room still comes back to it rather than opening a second one beside it.
//
// False means there is no room to come back to at all, which is only true of a
// journal nobody has ever spoken into. A caller mints then, and only then.
func (s *Store) LatestSession() (Session, bool, error) {
	var spoken string
	err := s.db.QueryRow(`
		SELECT session_id FROM messages
		WHERE role = ? AND session_id <> ''
		ORDER BY seq DESC LIMIT 1`, string(RoleUser)).Scan(&spoken)
	switch {
	case err == nil:
		session, readErr := readSession(s.db, spoken)
		if readErr == nil {
			return session, true, nil
		}
		if !errors.Is(readErr, sql.ErrNoRows) {
			return Session{}, false, fmt.Errorf("latest session: %w", readErr)
		}
		// A room spoken in whose row is missing cannot happen through any live
		// path — every message ensures its session — so this is a store that was
		// edited under us. The id is still the true answer to the question.
		return Session{ID: spoken}, true, nil
	case !errors.Is(err, sql.ErrNoRows):
		return Session{}, false, fmt.Errorf("latest session: %w", err)
	}

	session, err := scanSession(s.db.QueryRow(`
		SELECT id, title, tags, surface, created_at, last_active_at
		FROM sessions ORDER BY last_active_at DESC, id LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, fmt.Errorf("latest session: %w", err)
	}
	return session, true, nil
}

// EmptySessions lists every room that was opened and never became a
// conversation — no message of any kind, and no name of its own — newest
// first.
//
// A title is part of the test and not an afterthought: a room somebody
// deliberately named is a place they made on purpose, and emptying it is not
// the same as never having used it. The scribe never names a room with nothing
// in it, so the only titles this excludes are the ones a person typed.
func (s *Store) EmptySessions() ([]Session, error) {
	rows, err := s.db.Query(`
		SELECT s.id, s.title, s.tags, s.surface, s.created_at, s.last_active_at
		FROM sessions s
		WHERE s.title = ''
		  AND NOT EXISTS (SELECT 1 FROM messages m WHERE m.session_id = s.id)
		ORDER BY s.last_active_at DESC, s.id`)
	if err != nil {
		return nil, fmt.Errorf("list empty sessions: %w", err)
	}
	defer rows.Close()
	sessions := make([]Session, 0)
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, fmt.Errorf("list empty sessions: %w", err)
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list empty sessions: %w", err)
	}
	return sessions, nil
}

// OpenOrReuseSession is the `+ new room` door, and the whole of the difference
// between it and OpenSession is the question it asks first: is there already an
// empty unnamed room to walk into?
//
// An empty room is not a document — it has no content to lose and no name to
// confuse with another — so two of them are indistinguishable to a reader and
// the second one is pure accumulation. Reusing is therefore not a compromise on
// "new": the room it hands back is exactly as new as the one it would have
// minted, and it journals nothing, which is the honest record of a request that
// changed nothing.
//
// The boolean says which happened, for a caller that wants to word it.
func (s *Store) OpenOrReuseSession(id, surface string) (Session, bool, error) {
	empty, err := s.EmptySessions()
	if err != nil {
		return Session{}, false, err
	}
	if len(empty) > 0 {
		return empty[0], true, nil
	}
	opened, err := s.OpenSession(id, "", surface)
	if err != nil {
		return Session{}, false, err
	}
	return opened, false, nil
}

// ReapEmptySessions takes back the empty unnamed rooms that accumulated before
// the two doors above existed, and returns the ids it discarded.
//
// It keeps two rooms out of the reap on purpose. The newest empty one stays
// because an empty room is what `+ new room` just made and the next launch must
// not delete the place somebody is standing in; keep stays because it is the
// room this launch resolved, and a window may not open onto a row it is about
// to remove. Everything older than both is a room that was opened, never
// spoken in, and left behind.
//
// The delete is journaled rather than performed quietly, because the sessions
// table is a projection: a bare DELETE would be undone by the next
// `aforge rebuild`, and a projection that disagrees with the journal is the one
// thing this store does not have. A discarded room replays as discarded.
func (s *Store) ReapEmptySessions(keep string) ([]string, error) {
	keep = strings.TrimSpace(keep)
	empty, err := s.EmptySessions()
	if err != nil {
		return nil, err
	}
	if len(empty) < 2 {
		return nil, nil
	}
	discarded := make([]string, 0, len(empty)-1)
	// The head of the list is the newest empty room and is never reaped.
	for _, session := range empty[1:] {
		if session.ID == keep {
			continue
		}
		discarded = append(discarded, session.ID)
	}
	if len(discarded) == 0 {
		return nil, nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("reap empty sessions: %w", err)
	}
	defer tx.Rollback()
	for _, id := range discarded {
		// Re-checked inside the transaction: another window may have spoken into
		// this room between the read above and this write, and a room with words
		// in it is not an empty room any more.
		var spoken int
		if err := tx.QueryRow(
			`SELECT EXISTS (SELECT 1 FROM messages WHERE session_id = ?)`, id).Scan(&spoken); err != nil {
			return nil, fmt.Errorf("reap empty sessions: %w", err)
		}
		if spoken != 0 {
			continue
		}
		payload := sessionDiscardedPayload{SessionID: id}
		// The room is not a node, the same shape OpenSession's mint uses.
		if _, _, err := appendEvent(tx, "", EventSessionDiscarded, payload); err != nil {
			return nil, fmt.Errorf("reap empty sessions: %w", err)
		}
		if err := applySessionDiscarded(tx, payload); err != nil {
			return nil, fmt.Errorf("reap empty sessions: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("reap empty sessions: %w", err)
	}
	return discarded, nil
}

// applySessionDiscarded is the projection write for a room being taken back.
//
// A missing row is not an error the way a missing rename target is: replay runs
// after Rebuild has dropped the table, and a room discarded before it was ever
// re-minted by a message simply has nothing to delete. A message that arrives
// AFTER the discard mints the row again through ensureSessionTx, which is the
// honest answer — somebody spoke in it, so it exists.
func applySessionDiscarded(tx *sql.Tx, payload sessionDiscardedPayload) error {
	id := strings.TrimSpace(payload.SessionID)
	if id == "" {
		return fmt.Errorf("session discarded without an id")
	}
	_, err := tx.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}
