package judge

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// ErrNoAnswer is every way a reply can fail to be an answer. IT IS NEVER A
// YES: the intake reads any error from the judge as no, so a model that wrote
// prose, a truncated object or a "yes" of its own invention starts nothing.
var ErrNoAnswer = errors.New("judge: the model's reply is not an answer")

// answer is the one shape a reply may have. Pointers tell a key that was
// absent from one that was false or empty.
type answer struct {
	Yes *bool   `json:"yes"`
	Why *string `json:"why"`
}

// Parse reads a reply STRICTLY: it must be exactly one JSON object with
// exactly the keys "yes" (a boolean) and "why" (a non-empty string), and
// nothing before or after it. The one courtesy is a single code fence around
// the whole reply (```json … ```), which cheap models add after being told not
// to; prose around the object, a second object, an extra key, a "yes" spelled
// as a string or a number, and an empty reason are all [ErrNoAnswer].
//
// The reason is folded onto one line and cut to [WhyMost] runes.
func Parse(reply string) (yes bool, why string, err error) {
	body := unfence(strings.TrimSpace(reply))
	if !strings.HasPrefix(body, "{") {
		return false, "", ErrNoAnswer
	}
	dec := json.NewDecoder(strings.NewReader(body))
	dec.DisallowUnknownFields()
	var a answer
	if err := dec.Decode(&a); err != nil {
		return false, "", ErrNoAnswer
	}
	// Anything after the object, even a second object, is not this shape.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return false, "", ErrNoAnswer
	}
	if a.Yes == nil || a.Why == nil {
		return false, "", ErrNoAnswer
	}
	why = clip(oneLine(*a.Why), WhyMost)
	if why == "" {
		return false, "", ErrNoAnswer
	}
	if dupKeys(body) {
		return false, "", ErrNoAnswer
	}
	return *a.Yes, why, nil
}

// unfence removes one code fence wrapping the whole reply, and nothing else.
func unfence(s string) string {
	if !strings.HasPrefix(s, "```") || !strings.HasSuffix(s, "```") || len(s) < 6 {
		return s
	}
	inner := strings.TrimSpace(strings.TrimSuffix(s[3:], "```"))
	if len(inner) >= 4 && strings.EqualFold(inner[:4], "json") {
		inner = inner[4:] // the fence's language word
	}
	return strings.TrimSpace(inner)
}

// dupKeys says whether the object names a key twice. encoding/json keeps the
// last of two, so `{"yes": false, "why": "…", "yes": true}` would otherwise
// read as a yes the first half of the reply denied.
func dupKeys(obj string) bool {
	dec := json.NewDecoder(strings.NewReader(obj))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return true
	}
	seen := map[string]bool{}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return true
		}
		key, _ := t.(string)
		if seen[key] {
			return true
		}
		seen[key] = true
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return true
		}
	}
	return false
}
