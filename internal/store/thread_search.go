package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// messages_fts is the index the conversation never had.
//
// The messages table carried exactly one index — (session_id, seq) — and there
// was no FTS table over it at all. Four other things in this database are
// searchable (the graph, the notebook, charters, services) and the one surface
// the product's whole premise rests on was not: anything that was only ever
// SAID, and never spliced into a job, was unreachable by every search in the
// product, forever. The head sees ten to twenty recent messages, so "remember
// that pricing analysis from January?" was answered from a window that did not
// contain January and a notebook that had never heard of it.
//
// It is a materialized view rather than an external-content table, for the same
// reason graph_fts is: replacement is explicit, it happens inside the event
// transaction that wrote the message, and Rebuild reproduces it by replay
// through the ordinary view function.
//
// session_id and role ride along unindexed so a hit can say where and from whom
// without a second query, and so a caller can scope a search to one thread.
const messagesFTSSchema = `
CREATE VIRTUAL TABLE messages_fts USING fts5(
    session_id UNINDEXED,
    role UNINDEXED,
    body
);
`

// migrateMessagesFTS creates the index and backfills it from the message view.
//
// The backfill is the point of the whole migration: a resident three months old
// is precisely the one whose conversation is worth searching, and an index that
// only covered what was said after the upgrade would be honest about nothing.
// The message view is itself journal-derived, so a one-time copy is sufficient;
// later Rebuilds recreate the index by replaying message events.
func migrateMessagesFTS(db *sql.DB) error {
	var exists int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND name = 'messages_fts'`).Scan(&exists); err != nil {
		return err
	}
	if exists != 0 {
		return nil
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(messagesFTSSchema); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO messages_fts (rowid, session_id, role, body)
		SELECT seq, session_id, role, body FROM messages`); err != nil {
		return err
	}
	return tx.Commit()
}

// refreshMessageFTS replaces one index row from the authoritative message view.
// It runs inside the event transaction, so searchable conversation can never get
// ahead of or lag behind the event that wrote it.
func refreshMessageFTS(tx *sql.Tx, seq int64) error {
	if _, err := tx.Exec(`DELETE FROM messages_fts WHERE rowid = ?`, seq); err != nil {
		return err
	}
	_, err := tx.Exec(`
		INSERT INTO messages_fts (rowid, session_id, role, body)
		SELECT seq, session_id, role, body FROM messages WHERE seq = ?`, seq)
	return err
}

// MessageWindowFloor is the seq of the keep-th newest message in a session:
// everything below it is conversation the front desk can no longer see.
//
// It exists so a caller can ask the one question that separates "they are
// talking about what we just said" from "they are asking me to remember". Zero
// means the session is still short enough that nothing has fallen out of sight,
// which is the same as saying there is nothing to remember yet.
func (s *Store) MessageWindowFloor(sessionID string, keep int) (int64, error) {
	if keep <= 0 || strings.TrimSpace(sessionID) == "" {
		return 0, nil
	}
	var floor int64
	err := s.db.QueryRow(`
		SELECT seq FROM messages WHERE session_id = ?
		ORDER BY seq DESC LIMIT 1 OFFSET ?`, sessionID, keep-1).Scan(&floor)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("message window floor: %w", err)
	}
	return floor, nil
}

// MessageHit is one remembered line of conversation with enough around it to be
// quoted honestly: who said it, when, and in which thread.
type MessageHit struct {
	// Complete is true only when the reader returned the entire indexed body.
	// Older excerpt-only readers leave it false.
	Complete  bool
	Seq       int64
	SessionID string
	Role      Role
	Body      string
	Time      time.Time
	Age       string
}

// messageSearchBytes bounds one rendered hit. A search result is a pointer back
// into a conversation, not a replay of it.
const messageSearchBytes = 400

// SearchMessages finds conversation by its words, newest first among equals.
//
// Ranking is bm25 with recency as the tiebreak rather than as a term: the whole
// failure this read exists for is a question about something old, and a search
// that quietly prefers recent lines answers it with the same window the caller
// already had. A session filter narrows to one thread; empty searches every
// thread, which is what a person means by "we talked about this once".
func (s *Store) SearchMessages(terms, sessionID string, limit int) ([]MessageHit, error) {
	if limit <= 0 {
		limit = 8
	}
	if limit > 50 {
		limit = 50
	}
	query := ftsQueryFrom(terms)
	if query == "" {
		return nil, nil
	}
	where := `messages_fts MATCH ? AND m.body <> ''`
	args := []any{query}
	if session := strings.TrimSpace(sessionID); session != "" {
		where += ` AND m.session_id = ?`
		args = append(args, session)
	}
	args = append(args, limit)
	rows, err := s.db.Query(`
		SELECT m.seq, m.ts, m.session_id, m.role, m.body
		FROM messages_fts
		JOIN messages AS m ON m.seq = messages_fts.rowid
		WHERE `+where+`
		ORDER BY bm25(messages_fts, 0.0, 0.0, 1.0), m.seq DESC
		LIMIT ?`, args...)
	if err != nil {
		// Hostile FTS syntax is a miss, exactly as it is in Recall — but a miss
		// this read is allowed to SAY, which is the other half of the fix.
		return nil, nil
	}
	defer rows.Close()
	now := time.Now()
	hits := make([]MessageHit, 0, limit)
	for rows.Next() {
		var hit MessageHit
		var timestamp, role string
		if err := rows.Scan(&hit.Seq, &timestamp, &hit.SessionID, &role, &hit.Body); err != nil {
			return nil, fmt.Errorf("search messages: %w", err)
		}
		parsed, err := parseTime(timestamp)
		if err != nil {
			return nil, fmt.Errorf("search messages: %w", err)
		}
		hit.Role = Role(role)
		hit.Time = parsed
		hit.Age = AgeLabel(parsed, now)
		hit.Body = bounded(strings.TrimSpace(hit.Body), messageSearchBytes)
		hits = append(hits, hit)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search messages: %w", err)
	}
	return hits, nil
}

// ConversationHit is one remembered line with the NAME of the thread it was
// said in — the one fact [MessageHit] does not carry and the one a result on a
// search page cannot be drawn without.
//
// Everything else a row needs is already on the hit underneath: the session id
// to open, the bounded body to quote, the instant to sort on and the age to
// print.
type ConversationHit struct {
	MessageHit
	// Title is the conversation's name (internal/session's title.go, kept in the
	// sessions table). Empty for a thread nobody has named yet, or one that
	// posted messages without ever opening a session row — which is unknown and
	// not "untitled": a page draws the project or the first line instead, and
	// never a word this store made up.
	Title string
}

// SearchConversations finds conversation by its words ACROSS EVERY THREAD, with
// each hit carrying the name of the thread it came from.
//
// IT IS ONE FTS QUERY AND IT STAYS ONE. The title comes from a LEFT JOIN in the
// same statement rather than a lookup per hit, which is the difference between a
// search page and a search page that opens fifty connections to draw fifty rows
// (see internal/store's memory snapshot for the same defect written down). The
// ranking, the bound on one quoted body, and the tolerance of hostile FTS syntax
// are all [Store.SearchMessages]'s and deliberately not restated here.
//
// There is no session filter: a search PLACE is the question "we talked about
// this once", and the answer to it is the whole machine. A caller that wants one
// thread already has [Store.SearchMessages].
func (s *Store) SearchConversations(terms string, limit int) ([]ConversationHit, error) {
	if limit <= 0 {
		limit = 8
	}
	if limit > 50 {
		limit = 50
	}
	query := ftsQueryFrom(terms)
	if query == "" {
		return nil, nil
	}
	rows, err := s.db.Query(`
		SELECT m.seq, m.ts, m.session_id, m.role, m.body, COALESCE(c.title, '')
		FROM messages_fts
		JOIN messages AS m ON m.seq = messages_fts.rowid
		LEFT JOIN sessions AS c ON c.id = m.session_id
		WHERE messages_fts MATCH ? AND m.body <> ''
		ORDER BY bm25(messages_fts, 0.0, 0.0, 1.0), m.seq DESC
		LIMIT ?`, query, limit)
	if err != nil {
		// Hostile FTS syntax is a miss, exactly as it is above.
		return nil, nil
	}
	defer rows.Close()
	now := time.Now()
	hits := make([]ConversationHit, 0, limit)
	for rows.Next() {
		var hit ConversationHit
		var timestamp, role string
		if err := rows.Scan(&hit.Seq, &timestamp, &hit.SessionID, &role, &hit.Body, &hit.Title); err != nil {
			return nil, fmt.Errorf("search conversations: %w", err)
		}
		parsed, err := parseTime(timestamp)
		if err != nil {
			return nil, fmt.Errorf("search conversations: %w", err)
		}
		hit.Role = Role(role)
		hit.Time = parsed
		hit.Age = AgeLabel(parsed, now)
		hit.Body = bounded(strings.TrimSpace(hit.Body), messageSearchBytes)
		hit.Title = strings.TrimSpace(hit.Title)
		hits = append(hits, hit)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search conversations: %w", err)
	}
	return hits, nil
}
