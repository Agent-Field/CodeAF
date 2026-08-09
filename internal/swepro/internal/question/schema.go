// Question schemas — port of src/question/index.ts:13-104 and
// src/question/schema.ts:1-17 (swe-pro 3b25a1a).
package question

import (
	"bytes"
	"encoding/json"

	idpkg "github.com/Agent-Field/swe-pro-go/internal/id"
)

// QuestionID is the branded string used to identify a pending question.
type QuestionID string

// Option is one selectable response.
type Option struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// Info describes a question presented to a user.
type Info struct {
	Question string   `json:"question"`
	Header   string   `json:"header"`
	Options  []Option `json:"options"`
	Multiple *bool    `json:"multiple,omitempty"`
	Custom   *bool    `json:"custom,omitempty"`
}

// Prompt is the tool-facing question shape before the custom-answer flag is
// added.
type Prompt struct {
	Question string   `json:"question"`
	Header   string   `json:"header"`
	Options  []Option `json:"options"`
	Multiple *bool    `json:"multiple,omitempty"`
}

// Tool links a question request to its originating tool call.
type Tool struct {
	MessageID string `json:"messageID"`
	CallID    string `json:"callID"`
}

// Request is the payload of question.asked.
type Request struct {
	ID        QuestionID `json:"id"`
	SessionID string     `json:"sessionID"`
	Questions []Info     `json:"questions"`
	Tool      *Tool      `json:"tool,omitempty"`
}

// Answer contains the selected labels for one question.
type Answer []string

// Reply is the HTTP/tool reply body.
type Reply struct {
	Answers []Answer `json:"answers"`
}

// Replied is the payload of question.replied.
type Replied struct {
	SessionID string     `json:"sessionID"`
	RequestID QuestionID `json:"requestID"`
	Answers   []Answer   `json:"answers"`
}

// Rejected is the payload of question.rejected.
type Rejected struct {
	SessionID string     `json:"sessionID"`
	RequestID QuestionID `json:"requestID"`
}

// AscendingQuestionID mirrors QuestionID.ascending.
func AscendingQuestionID(given ...string) (QuestionID, error) {
	value, err := idpkg.Ascending("question", given...)
	return QuestionID(value), err
}

// SchemaAccepts mirrors either Effect Schema decoding or the generated zod
// static for each exported question schema. The zod mode additionally applies
// the Identifier prefix overrides used by the branded ID strings.
func SchemaAccepts(kind, mode string, raw json.RawMessage) bool {
	if mode != "effect" && mode != "zod" {
		return false
	}
	zod := mode == "zod"
	switch kind {
	case "option":
		return acceptsOption(raw)
	case "info":
		return acceptsInfo(raw)
	case "prompt":
		return acceptsPrompt(raw)
	case "tool":
		return acceptsTool(raw, zod)
	case "request":
		return acceptsRequest(raw, zod)
	case "answer":
		return acceptsAnswer(raw)
	case "reply":
		return acceptsReply(raw)
	case "questionID":
		value, ok := rawString(raw)
		return ok && (!zod || idpkg.SchemaAccepts("question", value))
	default:
		return false
	}
}

func acceptsOption(raw json.RawMessage) bool {
	object, ok := rawObject(raw)
	if !ok {
		return false
	}
	_, labelOK := requiredString(object, "label")
	_, descriptionOK := requiredString(object, "description")
	return labelOK && descriptionOK
}

func acceptsInfo(raw json.RawMessage) bool {
	object, ok := rawObject(raw)
	if !ok || !acceptsBase(object) {
		return false
	}
	return optionalBool(object, "custom")
}

func acceptsPrompt(raw json.RawMessage) bool {
	object, ok := rawObject(raw)
	return ok && acceptsBase(object)
}

func acceptsBase(object map[string]json.RawMessage) bool {
	if _, ok := requiredString(object, "question"); !ok {
		return false
	}
	if _, ok := requiredString(object, "header"); !ok {
		return false
	}
	options, ok := rawArray(object["options"])
	if !ok {
		return false
	}
	for _, option := range options {
		if !acceptsOption(option) {
			return false
		}
	}
	return optionalBool(object, "multiple")
}

func acceptsTool(raw json.RawMessage, zod bool) bool {
	object, ok := rawObject(raw)
	if !ok {
		return false
	}
	messageID, ok := requiredString(object, "messageID")
	if !ok || (zod && !idpkg.SchemaAccepts("message", messageID)) {
		return false
	}
	_, ok = requiredString(object, "callID")
	return ok
}

func acceptsRequest(raw json.RawMessage, zod bool) bool {
	object, ok := rawObject(raw)
	if !ok {
		return false
	}
	requestID, ok := requiredString(object, "id")
	if !ok || (zod && !idpkg.SchemaAccepts("question", requestID)) {
		return false
	}
	sessionID, ok := requiredString(object, "sessionID")
	if !ok || (zod && !idpkg.SchemaAccepts("session", sessionID)) {
		return false
	}
	questions, ok := rawArray(object["questions"])
	if !ok {
		return false
	}
	for _, question := range questions {
		if !acceptsInfo(question) {
			return false
		}
	}
	if tool, exists := object["tool"]; exists && !acceptsTool(tool, zod) {
		return false
	}
	return true
}

func acceptsAnswer(raw json.RawMessage) bool {
	answers, ok := rawArray(raw)
	if !ok {
		return false
	}
	for _, answer := range answers {
		if _, ok := rawString(answer); !ok {
			return false
		}
	}
	return true
}

func acceptsReply(raw json.RawMessage) bool {
	object, ok := rawObject(raw)
	if !ok {
		return false
	}
	answers, ok := rawArray(object["answers"])
	if !ok {
		return false
	}
	for _, answer := range answers {
		if !acceptsAnswer(answer) {
			return false
		}
	}
	return true
}

func rawObject(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, false
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &value); err != nil || value == nil {
		return nil, false
	}
	return value, true
}

func rawArray(raw json.RawMessage) ([]json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, false
	}
	var value []json.RawMessage
	if err := json.Unmarshal(trimmed, &value); err != nil {
		return nil, false
	}
	return value, true
}

func rawString(raw json.RawMessage) (string, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '"' {
		return "", false
	}
	var value string
	if json.Unmarshal(trimmed, &value) != nil {
		return "", false
	}
	return value, true
}

func requiredString(object map[string]json.RawMessage, key string) (string, bool) {
	raw, exists := object[key]
	if !exists {
		return "", false
	}
	return rawString(raw)
}

func optionalBool(object map[string]json.RawMessage, key string) bool {
	raw, exists := object[key]
	if !exists {
		return true
	}
	trimmed := bytes.TrimSpace(raw)
	if !bytes.Equal(trimmed, []byte("true")) && !bytes.Equal(trimmed, []byte("false")) {
		return false
	}
	var value bool
	return json.Unmarshal(trimmed, &value) == nil
}
