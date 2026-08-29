package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// DecodeJSONObject accepts the small formatting failures common at provider
// boundaries: a code fence, or a sentence wrapped around an otherwise valid
// object. It still requires the selected object itself to be strict JSON.
//
// It lives here rather than beside any one caller because those failures are a
// property of the boundary, not of the pass that happens to be crossing it. The
// head learned this first — a router that silently falls back to a model
// without structured-output support answers in prose — and the planner paid for
// not knowing it: every contract call on a fenced-JSON model returned "invalid
// character 'B' looking for beginning of value", the money was spent, and the
// leaf ran with no working method and said nothing about it. One extractor, one
// tolerance, every structured call.
func DecodeJSONObject(text string, destination any) error {
	trimmed := trimCodeFence(strings.TrimSpace(text))
	if trimmed == "" {
		return errors.New("empty response")
	}
	candidate, err := firstJSONObject(trimmed)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(candidate, destination); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	return nil
}

func trimCodeFence(text string) string {
	if !strings.HasPrefix(text, "```") {
		return text
	}
	if newline := strings.IndexByte(text, '\n'); newline >= 0 {
		text = text[newline+1:]
	}
	text = strings.TrimSpace(text)
	if fence := strings.LastIndex(text, "```"); fence >= 0 {
		text = text[:fence]
	}
	return strings.TrimSpace(text)
}

// firstJSONObject finds the object in a reply, and refuses to find one INSIDE a
// truncated one.
//
// The second half of that sentence is a fix, not a nicety. A fan-out cut at the
// output ceiling looks like `{"parts":[{"title":"read the test"},{"title":"fix
// the fol` — an outer object that never closes, with complete objects nested in
// it. Scanning for the first balanced object found `{"title":"read the test"}`,
// decoded it happily into a reply struct that has no such field, and handed the
// caller a successful parse of an empty answer. The truncation vanished, the
// pass reported "no parts returned", and nothing anywhere said the reply had
// been cut off.
//
// So a brace that never closes ENDS the search. Every later brace is inside it
// by construction, and a fragment of an answer is not the answer. A balanced
// candidate that is not valid JSON is a different thing — prose with braces in
// it — and the scan carries on past that, which is the tolerance this file
// exists for.
func firstJSONObject(text string) ([]byte, error) {
	for start := 0; start < len(text); start++ {
		if text[start] != '{' {
			continue
		}
		candidate, closed := balancedObject(text, start)
		if !closed {
			break
		}
		if json.Valid(candidate) {
			return candidate, nil
		}
	}
	return nil, errors.New("response contains no JSON object")
}

func balancedObject(text string, start int) ([]byte, bool) {
	depth := 0
	inString := false
	escaped := false
	for index := start; index < len(text); index++ {
		character := text[index]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch character {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}
		switch character {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return []byte(text[start : index+1]), true
			}
		}
	}
	return nil, false
}

// UnclosedJSONObject answers the one question a truncated reply raises: did the
// model BEGIN an object and get cut off, or did it never start one?
//
// The two failures arrive at the same door and want opposite repairs. A reply
// that opened a brace and ran out of room is half bought — the expensive tokens
// are already in hand — and the right move is to ask for the rest of it. A reply
// with no brace anywhere spent its whole ceiling on something else (prose, or
// private deliberation) and there is nothing to continue; that one is asked
// again. Telling them apart is a structural reading of the text and never of the
// finish reason, which is the same law DecodeJSONObject is written under.
//
// It returns the text from the first unterminated brace to the end, which is
// exactly the prefix a continuation is appended to. It answers false whenever a
// complete object is present, so a caller that has already decoded never reaches
// it, and false when no brace was ever opened.
func UnclosedJSONObject(text string) (string, bool) {
	trimmed := trimCodeFence(strings.TrimSpace(text))
	if trimmed == "" {
		return "", false
	}
	if _, err := firstJSONObject(trimmed); err == nil {
		return "", false
	}
	start := strings.IndexByte(trimmed, '{')
	if start < 0 {
		return "", false
	}
	return trimmed[start:], true
}
