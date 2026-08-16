package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// MessageTail is the end of one room's conversation, oldest first.
//
// Messages is the TAILING primitive: a lens remembers the last seq it rendered
// and asks for what came after, so its window opens at the front and its limit
// bounds how far forward it reads. That is exactly the wrong shape for the one
// question this answers — "what were we saying in there" — where the front of a
// day-old room is the part nobody wants and the limit would cut off the part
// they do. Paging forward to find the end would mean one query per two hundred
// messages to throw all but the last handful away.
//
// So this reads backward and hands the result back in reading order. The
// decoding is the same decoding, because a message that came out of this query
// has to be indistinguishable from a message that came out of that one.
func (s *Store) MessageTail(sessionID string, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 200
	}
	where := ``
	args := []any{}
	if sessionID != "" {
		where = ` WHERE session_id = ?`
		args = append(args, sessionID)
	}
	args = append(args, limit)
	rows, err := s.db.Query(`
		SELECT seq, ts, session_id, role, body, attachments, model, node_id, command_seq, question_seq, answers_seq, options, brief, progress, parts
		FROM messages`+where+` ORDER BY seq DESC LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("message tail: %w", err)
	}
	defer rows.Close()

	messages := make([]Message, 0, limit)
	for rows.Next() {
		var message Message
		var timestamp, attachments, options, brief, progress string
		// The same RawBytes discipline the forward read uses: the driver hands
		// over its own buffer and the legacy parts check reads it without
		// copying, valid exactly until the next row.
		var parts sql.RawBytes
		if err := rows.Scan(&message.Seq, &timestamp, &message.SessionID,
			&message.Role, &message.Body, &attachments, &message.Model, &message.NodeID,
			&message.CommandSeq, &message.QuestionSeq, &message.Answers, &options,
			&brief, &progress, &parts); err != nil {
			return nil, fmt.Errorf("message tail: %w", err)
		}
		if err := decodeQuestionOptions(options, &message.Options); err != nil {
			return nil, fmt.Errorf("message tail: %w", err)
		}
		if err := decodeBrief(brief, &message.Brief); err != nil {
			return nil, fmt.Errorf("message tail: %w", err)
		}
		if err := decodeMessageProgress(progress, &message.Progress); err != nil {
			return nil, fmt.Errorf("message tail: %w", err)
		}
		decodeMessageParts(parts, message.Seq, &message.Parts)
		at, err := parseTime(timestamp)
		if err != nil {
			return nil, fmt.Errorf("message tail: parse time: %w", err)
		}
		message.Time = at
		if err := json.Unmarshal([]byte(attachments), &message.Attachments); err != nil {
			return nil, fmt.Errorf("message tail: decode attachments: %w", err)
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("message tail: %w", err)
	}
	// Read newest first so the LIMIT lands on the right end; handed back oldest
	// first because a transcript is read in the order it was said.
	for left, right := 0, len(messages)-1; left < right; left, right = left+1, right-1 {
		messages[left], messages[right] = messages[right], messages[left]
	}
	return messages, nil
}
