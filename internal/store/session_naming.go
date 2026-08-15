package store

import "fmt"

// The read behind the room-name backfill.
//
// The scribe names a room after its first real exchange, and rooms made before
// it existed never got that pass — they sit in the rail wearing the honest
// placeholder forever, because the only moment that ever named a room was the
// end of a turn in it. Finding them is the question this answers, and it is a
// question with a precise shape: a room with no name of its own that HAS been
// spoken in from both sides. One without an answer in it is not a conversation
// yet and naming it would name the question rather than the room, which is the
// same rule the post-turn pass keeps.
//
// It is one query rather than a scan in the caller because the alternative is
// reading every room's messages to find out which handful qualify — the shape
// that makes a launch pause on a store with a year of rooms in it.

// UnnamedSessionsWithExchange lists the rooms a naming pass would still have
// something to say about: no title, at least one thing the person said, and at
// least one answer. Most recently active first, so a backfill that can only
// afford a few names the rooms a reader is most likely looking at; the older
// ones keep their turn for the next launch.
//
// limit bounds the answer because the caller is spending a model call per row
// and an unbounded list would be an unbounded bill.
func (s *Store) UnnamedSessionsWithExchange(limit int) ([]Session, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := s.db.Query(`
		SELECT s.id, s.title, s.tags, s.surface, s.created_at, s.last_active_at
		FROM sessions s
		WHERE s.title = ''
		  AND EXISTS (SELECT 1 FROM messages m WHERE m.session_id = s.id AND m.role = ?)
		  AND EXISTS (SELECT 1 FROM messages m WHERE m.session_id = s.id AND m.role = ?)
		ORDER BY s.last_active_at DESC, s.id
		LIMIT ?`, string(RoleUser), string(RoleAgent), limit)
	if err != nil {
		return nil, fmt.Errorf("list unnamed sessions: %w", err)
	}
	defer rows.Close()
	sessions := make([]Session, 0, limit)
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, fmt.Errorf("list unnamed sessions: %w", err)
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list unnamed sessions: %w", err)
	}
	return sessions, nil
}
