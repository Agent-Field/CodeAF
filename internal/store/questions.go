package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// PendingQuestion returns the latest selectable askback this user message can
// answer. An intervening user turn consumes a question even when it was free
// text, preventing old choices from capturing unrelated conversation.
func (s *Store) PendingQuestion(sessionID string, beforeSeq int64) (Message, bool, error) {
	if beforeSeq <= 0 {
		return Message{}, false, nil
	}
	row := s.db.QueryRow(`
		SELECT q.seq, q.ts, q.session_id, q.role, q.body, q.node_id, q.command_seq, q.options
		FROM messages q
		WHERE q.session_id = ? AND q.role = ? AND q.seq < ?
		  AND json_array_length(q.options) > 0
		  AND NOT EXISTS (
		      SELECT 1 FROM messages u
		      WHERE u.session_id = q.session_id AND u.role = ?
		        AND u.seq > q.seq AND u.seq < ?
		  )
		ORDER BY q.seq DESC LIMIT 1`, sessionID, RoleAgent, beforeSeq, RoleUser, beforeSeq)
	var message Message
	var timestamp, options string
	if err := row.Scan(&message.Seq, &timestamp, &message.SessionID, &message.Role,
		&message.Body, &message.NodeID, &message.CommandSeq, &options); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Message{}, false, nil
		}
		return Message{}, false, fmt.Errorf("pending question: %w", err)
	}
	if err := decodeQuestionOptions(options, &message.Options); err != nil {
		return Message{}, false, fmt.Errorf("pending question: %w", err)
	}
	at, err := parseTime(timestamp)
	if err != nil {
		return Message{}, false, fmt.Errorf("pending question: parse time: %w", err)
	}
	message.Time = at
	return message, true, nil
}

func normalizeQuestionOptions(options []QuestionOption) ([]QuestionOption, error) {
	if len(options) == 0 {
		return nil, nil
	}
	if len(options) > 20 {
		return nil, fmt.Errorf("%w: too many question options", ErrInvalid)
	}
	normalized := make([]QuestionOption, 0, len(options))
	for _, option := range options {
		option.Label = strings.TrimSpace(option.Label)
		option.Value = strings.TrimSpace(option.Value)
		if option.Label == "" {
			return nil, fmt.Errorf("%w: empty question option", ErrInvalid)
		}
		normalized = append(normalized, option)
	}
	return normalized, nil
}

func decodeQuestionOptions(raw string, target *[]QuestionOption) error {
	var options []QuestionOption
	if err := json.Unmarshal([]byte(raw), &options); err != nil {
		return err
	}
	normalized, err := normalizeQuestionOptions(options)
	if err != nil {
		return err
	}
	*target = normalized
	return nil
}
