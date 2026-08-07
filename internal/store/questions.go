package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
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
		SELECT q.seq, q.ts, q.session_id, q.role, q.body, q.node_id, q.command_seq, q.question_seq, q.options
		FROM messages q
		WHERE (q.session_id = ? OR q.session_id = '') AND q.role = ? AND q.seq < ?
		  AND json_array_length(q.options) > 0
		  AND (q.question_seq = 0 OR EXISTS (
		      SELECT 1 FROM agent_questions aq
		      WHERE aq.seq = q.question_seq AND aq.status IN (?, ?)
		  ))
		  AND NOT EXISTS (
		      SELECT 1 FROM messages u
		      WHERE u.session_id = ? AND u.role = ?
		        AND u.seq > q.seq AND u.seq < ?
		  )
		ORDER BY q.seq DESC LIMIT 1`, sessionID, RoleAgent, beforeSeq,
		QuestionPending, QuestionAsked, sessionID, RoleUser, beforeSeq)
	var message Message
	var timestamp, options string
	if err := row.Scan(&message.Seq, &timestamp, &message.SessionID, &message.Role,
		&message.Body, &message.NodeID, &message.CommandSeq, &message.QuestionSeq, &options); err != nil {
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

// QuestionMessageBody renders a selectable askback the way every surface can
// read it: the prompt in plain text for transcripts, followed by the
// structured JSON payload the TUI's question components parse. Options carry
// numeric keys, so a selection replies "N" and selects options[N-1] in every
// surface; the durable option rows on the message remain the continuation and
// validation source.
type QuestionKind string

const (
	QuestionChoose  QuestionKind = "choose"
	QuestionConfirm QuestionKind = "confirm"
	QuestionText    QuestionKind = "text"
)

// QuestionConfig selects the structured component spelling. The zero value is
// the existing choose question with free text enabled.
type QuestionConfig struct {
	Kind      QuestionKind
	Default   string
	AllowFree *bool
}

func QuestionMessageBody(prompt string, options []QuestionOption, configs ...QuestionConfig) string {
	prompt = strings.TrimSpace(prompt)
	config := QuestionConfig{Kind: QuestionChoose}
	allowFree := true
	if len(configs) > 0 {
		config = configs[0]
		if config.Kind == "" {
			config.Kind = QuestionChoose
		}
		if config.AllowFree != nil {
			allowFree = *config.AllowFree
		}
	}
	if len(options) == 0 && config.Kind != QuestionText {
		config.Kind = QuestionText
	}
	type payloadOption struct {
		Key   string `json:"key"`
		Label string `json:"label"`
		Hint  string `json:"hint,omitempty"`
	}
	payload := struct {
		Kind      QuestionKind    `json:"kind"`
		Prompt    string          `json:"prompt"`
		Options   []payloadOption `json:"options"`
		Default   string          `json:"default,omitempty"`
		AllowFree bool            `json:"allowFree"`
	}{Kind: config.Kind, Prompt: prompt, Default: config.Default, AllowFree: allowFree}
	for index, option := range options {
		payload.Options = append(payload.Options, payloadOption{
			Key: strconv.Itoa(index + 1), Label: strings.TrimSpace(option.Label), Hint: strings.TrimSpace(option.Hint),
		})
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return prompt
	}
	return prompt + "\n\n```\n" + string(encoded) + "\n```"
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
		option.Hint = strings.TrimSpace(option.Hint)
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
