package expr

import (
	"strconv"
	"time"
)

// Offset reads a signed duration literal in the same units CEL expressions
// accept (7d, 2w, 24h, 30m; an optional + or -), so queries, rules and Check
// share ONE duration grammar instead of each keeping its own.
func Offset(text string) (time.Duration, bool) {
	sign := time.Duration(1)
	switch {
	case len(text) > 0 && text[0] == '-':
		sign, text = -1, text[1:]
	case len(text) > 0 && text[0] == '+':
		text = text[1:]
	}
	if len(text) < 2 {
		return 0, false
	}
	unit, multiplier := durationUnit(text[len(text)-1])
	n, err := strconv.ParseUint(text[:len(text)-1], 10, 32)
	if multiplier == 0 || err != nil || text[0] < '0' || text[0] > '9' {
		return 0, false
	}
	base := time.Minute
	if unit == 'h' {
		base = time.Hour
	}
	return sign * time.Duration(n*multiplier) * base, true
}

// RelativeTime is now shifted by an Offset literal: "-7d" is a week ago.
func RelativeTime(value string, now time.Time) (time.Time, bool) {
	d, ok := Offset(value)
	if !ok {
		return time.Time{}, false
	}
	return now.Add(d), true
}
