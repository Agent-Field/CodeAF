package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// ConversationExcerptBytes bounds the text carried by one search hit or neighbour.
// ConversationReadBytes gives an explicitly opened exchange more room without
// replaying a whole conversation into the model's context.
const (
	ConversationSearchDefault = 8
	ConversationSearchMax     = 20
	ConversationQueryWords    = 32
	ConversationExcerptBytes  = 400
	ConversationReadBytes     = MaxMessageBytes
)

// FindConversationMessages keeps ranking in the existing index but extracts the
// matching passage rather than the beginning of a possibly unrelated paragraph.
// Quoted Unicode tokens are data, never FTS operators. No query means browsing
// one explicitly named conversation; it never silently lists the whole store.
func (s *Store) FindConversationMessages(ctx context.Context, terms, sessionID, excludeSessionID string, limit int) ([]MessageHit, error) {
	if limit <= 0 {
		limit = ConversationSearchDefault
	}
	if limit > ConversationSearchMax {
		limit = ConversationSearchMax
	}
	fields := strings.FieldsFunc(terms, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	tokens := make([]string, 0, len(fields))
	for _, field := range fields {
		tokens = append(tokens, `"`+field+`"`)
		if len(tokens) == ConversationQueryWords {
			break
		}
	}
	query := strings.Join(tokens, " OR ")
	sessionID = strings.TrimSpace(sessionID)
	if query == "" && (strings.TrimSpace(terms) != "" || sessionID == "") {
		return nil, nil
	}
	from, body, where, order := "messages m", "m.body", "m.body <> ''", "m.seq DESC"
	var args []any
	if query != "" {
		from = "messages_fts JOIN messages m ON m.seq = messages_fts.rowid"
		body = "snippet(messages_fts, 2, '', '', ' … ', 20)"
		where += " AND messages_fts MATCH ?"
		order = "bm25(messages_fts, 0.0, 0.0, 1.0), m.seq DESC"
		args = append(args, query)
	}
	if sessionID != "" {
		where += " AND m.session_id = ?"
		args = append(args, sessionID)
	}
	if sessionID == "" && strings.TrimSpace(excludeSessionID) != "" {
		where += " AND m.session_id <> ?"
		args = append(args, strings.TrimSpace(excludeSessionID))
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, "SELECT m.seq, m.ts, m.session_id, m.role, "+body+" FROM "+from+" WHERE "+where+" ORDER BY "+order+" LIMIT ?", args...)
	if err != nil {
		return nil, fmt.Errorf("find conversation messages: %w", err)
	}
	return conversationRows(rows, ConversationExcerptBytes)
}

// ConversationExchange reads an anchor and its actual neighbours in one thread.
// Sequence numbers belong to the whole journal, so arithmetic on seq would mix
// conversations or miss neighbours whenever another writer posted in between.
func (s *Store) ConversationExchange(ctx context.Context, sessionID string, seq int64, radius, byteLimit int) ([]MessageHit, error) {
	if radius < 0 {
		radius = 0
	}
	if radius > 2 {
		radius = 2
	}
	if byteLimit <= 0 || byteLimit > ConversationReadBytes {
		byteLimit = ConversationReadBytes
	}
	var owner string
	if err := s.db.QueryRowContext(ctx, "SELECT session_id FROM messages WHERE seq = ?", seq).Scan(&owner); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if strings.TrimSpace(sessionID) != owner {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT seq, ts, session_id, role, body FROM (
 SELECT * FROM (SELECT seq, ts, session_id, role, body FROM messages WHERE session_id = ? AND seq < ? ORDER BY seq DESC LIMIT ?)
 UNION ALL SELECT seq, ts, session_id, role, body FROM messages WHERE session_id = ? AND seq = ?
 UNION ALL SELECT * FROM (SELECT seq, ts, session_id, role, body FROM messages WHERE session_id = ? AND seq > ? ORDER BY seq LIMIT ?)
 ) ORDER BY seq`, owner, seq, radius, owner, seq, owner, seq, radius)
	if err != nil {
		return nil, fmt.Errorf("read conversation exchange: %w", err)
	}
	hits, err := conversationRows(rows, byteLimit)
	for i := range hits {
		if hits[i].Seq != seq {
			hits[i].Body = bounded(hits[i].Body, ConversationExcerptBytes)
		}
	}
	return hits, err
}

// conversationRows shares timestamp parsing and byte bounds between search and
// exact reads so either route carries the same evidence and provenance.
func conversationRows(rows *sql.Rows, byteLimit int) ([]MessageHit, error) {
	defer rows.Close()
	var hits []MessageHit
	now := time.Now()
	for rows.Next() {
		var hit MessageHit
		var timestamp string
		if err := rows.Scan(&hit.Seq, &timestamp, &hit.SessionID, &hit.Role, &hit.Body); err != nil {
			return nil, err
		}
		parsed, err := parseTime(timestamp)
		if err != nil {
			return nil, err
		}
		hit.Time, hit.Age = parsed, AgeLabel(parsed, now)
		hit.Body = bounded(hit.Body, byteLimit)
		hits = append(hits, hit)
	}
	return hits, rows.Err()
}
