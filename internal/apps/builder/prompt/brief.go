package prompt

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
)

// briefKeys is the design brief's shape, in the order a product designer
// answers it: the PLAN call writes it first and every later call reads it.
var briefKeys = []struct{ Key, Label string }{
	{"users", "Who uses it"},
	{"job", "The job"},
	{"object", "Primary object"},
	{"daily_view", "Daily view"},
	{"states", "Key states"},
	{"actions", "Top actions"},
	{"agent", "App agent"},
}

// Brief is the design brief as text, one "Label: value" line per answer. The
// model may send it as an object (the asked shape) or as plain text; either
// becomes the same lines, so a loose reply never fails the plan.
type Brief string

// UnmarshalJSON accepts a string, or an object whose values are strings or
// string lists. Known keys come first in briefKeys order, others sorted.
func (b *Brief) UnmarshalJSON(raw []byte) error {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		*b = Brief(strings.TrimSpace(text))
		return nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		*b = ""
		return nil
	}
	var lines []string
	for _, k := range briefKeys {
		if v := briefValue(fields[k.Key]); v != "" {
			lines = append(lines, k.Label+": "+v)
		}
		delete(fields, k.Key)
	}
	extra := make([]string, 0, len(fields))
	for key := range fields {
		extra = append(extra, key)
	}
	sort.Strings(extra)
	for _, key := range extra {
		if v := briefValue(fields[key]); v != "" {
			lines = append(lines, key+": "+v)
		}
	}
	*b = Brief(strings.Join(lines, "\n"))
	return nil
}

func briefValue(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text)
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return strings.Join(list, "; ")
	}
	var compact bytes.Buffer
	if json.Compact(&compact, raw) != nil {
		return ""
	}
	return compact.String()
}
