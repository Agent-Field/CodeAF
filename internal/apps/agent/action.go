package agent

import (
	"encoding/json"
	"regexp"
	"strings"
)

// ShapeThisText is the exact answer to a request that changes the app (rule 02).
const ShapeThisText = "That changes the app"

// action is one model step: speak, call a tool, or hand the request to Shape.
type action struct {
	Say       string         `json:"say,omitempty"`
	Tool      string         `json:"tool,omitempty"`
	Args      map[string]any `json:"args,omitempty"`
	ShapeThis bool           `json:"shape_this,omitempty"`
	dropped   int
}

// actionSchema is sent with every call; providers without schema support
// still get the same contract through the system prompt.
var actionSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "say": {"type": "string"},
    "tool": {"type": "string"},
    "args": {"type": "object"},
    "shape_this": {"type": "boolean"}
  }
}`)

// parseAction accepts prose around the protocol, but only one action can run
// before the model sees its result. Protocol fragments never become speech.
func parseAction(text string) action {
	var first action
	found := false
	for i := 0; i < len(text); i++ {
		if text[i] != '{' {
			continue
		}
		end, complete := objectEnd(text, i)
		if !complete {
			continue
		}
		a, valid := decodeAction(text[i:end])
		if !valid {
			continue
		}
		i = end - 1
		if found {
			first.dropped++
			continue
		}
		first, found = a, true
	}
	if !found {
		return action{Say: safeText(text)}
	}
	first.Say = safeText(first.Say)
	return first
}

func decodeAction(body string) (action, bool) {
	var a action
	if json.Unmarshal([]byte(body), &a) != nil {
		return action{}, false
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal([]byte(body), &fields)
	_, says := fields["say"]
	return a, a.Tool != "" || a.ShapeThis || (says && string(fields["say"]) != "null")
}

// objectEnd balances braces outside strings, including escaped quotes and
// backslashes, so a record's text cannot end an action early.
func objectEnd(text string, start int) (int, bool) {
	depth, quoted, escaped := 0, false, false
	for i := start; i < len(text); i++ {
		c := text[i]
		if quoted {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				quoted = false
			}
			continue
		}
		switch c {
		case '"':
			quoted = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1, true
			}
		}
	}
	return len(text), false
}

var actionKey = regexp.MustCompile(`(?i)(?:"(?:tool|args|say|shape_this)"?\s*(?::|$)|\b(?:tool|args|say|shape_this)\s*:)`)

// safeText also handles malformed actions: executing one requires valid JSON,
// while hiding one only requires a protocol-looking key. This is used at the
// stream boundary as well as on the parser's plain-answer fallback.
func safeText(text string) string {
	var out strings.Builder
	removed := false
	for i := 0; i < len(text); {
		if text[i] != '{' {
			out.WriteByte(text[i])
			i++
			continue
		}
		end, complete := objectEnd(text, i)
		fragment := text[i:end]
		_, valid := decodeAction(fragment)
		if valid || actionKey.MatchString(fragment) {
			removed = true
			i = end
			continue
		}
		if complete {
			out.WriteString(fragment)
			i = end
			continue
		}
		out.WriteByte(text[i])
		i++
	}
	if !removed {
		return strings.TrimSpace(out.String())
	}
	var lines []string
	for _, line := range strings.Split(out.String(), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "```") {
			lines = append(lines, line)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
