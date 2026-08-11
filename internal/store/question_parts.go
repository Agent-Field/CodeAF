package store

import (
	"encoding/json"
	"strconv"
	"strings"
)

// The end of the smuggling, on the producing side (Part 2.11, 13.3 bug 1).
//
// A question has always existed in five places at once: a durable question row,
// a message row, a JSON blob inside that message's BODY, a typed options column
// beside it that nothing read, and an FTS copy of the lot. The surface that had
// to draw it recovered its components with a brace scanner over the prose, and
// the moment a renderer drew the journal honestly the blob appeared on screen as
// an agent's own words.
//
// This file is the one place that decides what a question message carries, and
// it decides it once for every producer — the head's own asks, the resident's,
// the compiler's askbacks, a worker's — because the smuggling was never one
// caller's habit. Two products come out of it:
//
//   - PARTS, which are the contract. A text part carrying the prompt alone, and
//     a question part carrying how the ask is drawn, what it is about, and which
//     lifecycle row it belongs to. The options are NOT copied into them: they
//     are Message.Options on the same row, already typed and already the thing a
//     reply is validated against.
//   - A BODY, which stays humane. Old chat reads the same rows (11.1) and reads
//     them out of the body alone, so the body keeps saying what it said — as
//     numbered prose wherever prose carries the whole meaning, and, for a
//     consent question whose default and no-free-text rule prose cannot carry,
//     as the payload it has always carried. Belt and suspenders: parts are what
//     a surface should read, the body is what nothing is broken by.
//
// QuestionMessageBody in questions.go remains the payload encoder. This file is
// its reader and its humane alternative, and the two live one package apart from
// nothing so that the encoding is never guessed at from the outside again.

// questionOptionBullet is the numbered spelling a humane body uses. It is the
// glyph the surfaces already render options with, and it is what the existing
// chat's numbered fallback parses, so a body written this way loses nothing on
// the way to a reader that has never heard of parts.
const questionOptionBullet = "▸ "

// QuestionParts is the render contract for one ask: the prompt as prose, and the
// ask as types. It is the ONLY constructor callers should use, so that "which
// blocks does a question message carry" has one answer rather than one per
// producer.
//
// A prompt that is empty produces no text part rather than an empty one — the
// part list stays a description of what is actually there.
func QuestionParts(prompt string, question QuestionPart) []MessagePart {
	parts := make([]MessagePart, 0, 2)
	if trimmed := strings.TrimSpace(prompt); trimmed != "" {
		parts = append(parts, TextPart(trimmed))
	}
	return append(parts, QuestionBlock(question))
}

// HumaneQuestionBody renders an ask as prose a person can read without any
// machinery at all: the prompt, then one numbered line per option.
//
// This is what replaces the fenced JSON on every ask whose whole meaning
// survives the trip. What does NOT survive is a preselected default and a
// refusal of free text, because a numbered list cannot say either — which is
// exactly why QuestionMessageBody is still used for those, and why this function
// is chosen by QuestionBodyFor rather than by a caller guessing.
func HumaneQuestionBody(prompt string, options []QuestionOption) string {
	prompt = strings.TrimSpace(prompt)
	rows := make([]string, 0, len(options))
	for index, option := range options {
		label := strings.TrimSpace(option.Label)
		if label == "" {
			continue
		}
		if hint := strings.TrimSpace(option.Hint); hint != "" {
			label += " — " + hint
		}
		// The number is the answer: a reply of "3" selects options[2] on every
		// surface, so the number a person reads has to be the option's position
		// in the durable list rather than its position among the ones that had
		// a label.
		rows = append(rows, questionOptionBullet+strconv.Itoa(index+1)+". "+label)
	}
	if len(rows) == 0 {
		return prompt
	}
	// One blank line under the prompt, then one option to a line: the block
	// reads as a list to a person and parses as numbered options to the reader
	// that has never heard of parts.
	return strings.TrimSpace(prompt + "\n\n" + strings.Join(rows, "\n"))
}

// QuestionBodyFor is the body one ask should be journaled with, and the rule it
// applies is about the READER that cannot see parts rather than about taste.
//
// A numbered list carries a prompt and some choices completely. It cannot carry
// which choice stands if the person says nothing, and it cannot carry that an
// answer outside the list is refused. Both of those are consent semantics on the
// questions that have them, and 11.1 says the existing chat keeps working — so a
// question that depends on either keeps the payload it has always carried, and
// every other question stops carrying one.
func QuestionBodyFor(prompt string, options []QuestionOption, config QuestionConfig) string {
	allowFree := true
	if config.AllowFree != nil {
		allowFree = *config.AllowFree
	}
	if config.Kind == QuestionConfirm || strings.TrimSpace(config.Default) != "" || !allowFree {
		return QuestionMessageBody(prompt, options, config)
	}
	return HumaneQuestionBody(prompt, options)
}

// QuestionPrompt recovers the prose from a body this package wrote. It is a
// decoder, not a scanner: QuestionMessageBody and HumaneQuestionBody are the two
// spellings that exist, both are written here, and this reverses exactly those
// two and nothing else.
//
// It exists because the durable question row stores its RENDERED body as its
// text, so the prompt a text part should carry has to be read back out of it —
// and reading it back in one owned place is the difference between a codec and
// the brace scanner this whole file exists to retire.
func QuestionPrompt(body string) string {
	if fence := strings.Index(body, "\n\n```\n{"); fence >= 0 {
		body = body[:fence]
	}
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), questionOptionBullet) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// questionBodyPayload is the shape QuestionMessageBody writes. It is declared
// again here rather than shared with the encoder because the encoder's struct is
// an anonymous literal inside that function; keeping the reader's copy explicit
// makes the wire form visible at both ends.
type questionBodyPayload struct {
	Kind      QuestionKind     `json:"kind"`
	Default   string           `json:"default"`
	Category  QuestionCategory `json:"category"`
	AllowFree bool             `json:"allowFree"`
}

// readQuestionBody recovers the drawing contract from a body this package wrote.
// A body with no payload is not a failure: it is a humane body, and everything
// the payload would have said is either derivable from the options or is the
// conservative default.
func readQuestionBody(body string) (questionBodyPayload, bool) {
	open := strings.Index(body, "\n\n```\n{")
	if open < 0 {
		return questionBodyPayload{}, false
	}
	encoded := body[open+len("\n\n```\n"):]
	if close := strings.Index(encoded, "\n```"); close >= 0 {
		encoded = encoded[:close]
	}
	var payload questionBodyPayload
	if err := json.Unmarshal([]byte(encoded), &payload); err != nil {
		return questionBodyPayload{}, false
	}
	return payload, true
}

// PartsForQuestion is the durable question row rendered as blocks. It is what
// the surfacing path attaches to the message it posts, so that EVERY producer of
// a durable question — this package's callers, the resident, the compiler, a
// worker — emits the same contract without any of them knowing it exists.
//
// Everything it says comes off the row or off the body the row already holds.
// Nothing is invented, and nothing is copied that the message already carries:
// the options ride the message's own typed column, exactly as they always have.
func PartsForQuestion(question AgentQuestion) []MessagePart {
	payload, hasPayload := readQuestionBody(question.Text)
	kind := payload.Kind
	if !validQuestionKind(kind) {
		// No payload, or one written before kinds existed. A question with
		// choices is a choice; a question without any is free text.
		kind = QuestionText
		if len(question.Options) > 0 {
			kind = QuestionChoose
		}
	}
	allowFree := len(question.Options) == 0
	if hasPayload {
		allowFree = payload.AllowFree
	}
	class := question.Class
	if class != QuestionInformational {
		class = QuestionConsent
	}
	category := question.Category
	if category == "" {
		category = payload.Category
	}
	answer := strings.TrimSpace(question.DefaultAnswer)
	if answer == "" {
		answer = strings.TrimSpace(payload.Default)
	}
	return QuestionParts(QuestionPrompt(question.Text), QuestionPart{
		Seq:       question.Seq,
		Kind:      kind,
		Class:     class,
		Category:  category,
		Default:   answer,
		AllowFree: allowFree,
		NodeID:    strings.TrimSpace(question.OriginNodeID),
		CharterID: strings.TrimSpace(question.OriginCharterID),
	})
}
