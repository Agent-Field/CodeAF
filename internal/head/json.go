package head

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// decodeJSONObject accepts the small formatting failures common at provider
// boundaries: a code fence or a sentence wrapped around an otherwise valid
// object. It still requires the selected object itself to be strict JSON.
func decodeJSONObject(text string, destination any) error {
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
