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

func firstJSONObject(text string) ([]byte, error) {
	for start := 0; start < len(text); start++ {
		if text[start] != '{' {
			continue
		}
		if candidate, ok := balancedObject(text, start); ok && json.Valid(candidate) {
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
