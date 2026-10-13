package runs

import (
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
)

// matchQuery applies QueryField tokens. The records service may not understand
// them yet, so the engine filters the page it got back. Tokens already applied
// by the store stay true, so filtering twice does not drop a real match.
func matchQuery(row bridge.Record, query string, now time.Time) bool {
	for _, token := range strings.Fields(query) {
		if token == "" || strings.HasPrefix(token, "sort:") {
			continue
		}
		if !matchToken(row, token, now) {
			return false
		}
	}
	return true
}

func matchToken(row bridge.Record, token string, now time.Time) bool {
	if name, ok := strings.CutPrefix(token, "has:"); ok {
		return asText(row.Fields[name]) != ""
	}
	field, op, value, ok := splitOp(token)
	if !ok {
		return true
	}
	if field == "id" {
		return matchColon(row.ID, op, value)
	}
	got := row.Fields[field]
	switch op {
	case ":":
		return matchColon(asText(got), op, value)
	case "<", ">", "<=", ">=":
		return matchCmp(got, op, value, now)
	default:
		return true
	}
}

func matchColon(got, op, value string) bool {
	if op != ":" {
		return false
	}
	var includes, excludes []string
	for _, part := range strings.Split(value, ",") {
		if part == "" {
			continue
		}
		if strings.HasPrefix(part, "-") {
			excludes = append(excludes, strings.TrimPrefix(part, "-"))
			continue
		}
		includes = append(includes, part)
	}
	for _, ex := range excludes {
		if starMatch(got, ex) {
			return false
		}
	}
	if len(includes) == 0 {
		return true
	}
	for _, in := range includes {
		if starMatch(got, in) {
			return true
		}
	}
	return false
}

func starMatch(got, pattern string) bool {
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(got, strings.TrimSuffix(pattern, "*"))
	}
	return got == pattern
}

func matchCmp(got any, op, value string, now time.Time) bool {
	if when, ok := relativeTime(value, now); ok {
		have, ok := asTime(got)
		if !ok {
			return false
		}
		switch op {
		case "<":
			return have.Before(when)
		case ">":
			return have.After(when)
		case "<=":
			return !have.After(when)
		case ">=":
			return !have.Before(when)
		}
	}
	left, okL := asFloat(got)
	right, okR := asFloat(value)
	if !okL || !okR {
		return false
	}
	return cmpFloat(left, op, right)
}

func relativeTime(value string, now time.Time) (time.Time, bool) {
	neg := strings.HasPrefix(value, "-")
	raw := strings.TrimPrefix(value, "-")
	d, ok := parseDur(raw)
	if !ok {
		return time.Time{}, false
	}
	if neg {
		return now.Add(-d), true
	}
	return now.Add(d), true
}

func asTime(value any) (time.Time, bool) {
	text := asText(value)
	if text == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, text); err == nil {
		return t, true
	}
	t, err := time.Parse("2006-01-02", text)
	return t, err == nil
}

func splitOp(token string) (field, op, value string, ok bool) {
	for _, candidate := range []string{"<=", ">=", "<", ">", ":"} {
		if i := strings.Index(token, candidate); i > 0 {
			return token[:i], candidate, token[i+len(candidate):], true
		}
	}
	return "", "", "", false
}
