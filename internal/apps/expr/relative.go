package expr

import (
	"strconv"
	"strings"
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
	if d, ok := daysCall(text); ok {
		return sign * d, true
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

// dayWords are the named moments a query value may use: the start of a day
// in the clock's own zone, or the clock itself.
var dayWords = map[string]func(now time.Time) time.Time{
	"now":       func(now time.Time) time.Time { return now },
	"today":     func(now time.Time) time.Time { return DayStart(now) },
	"tomorrow":  func(now time.Time) time.Time { return DayStart(now).AddDate(0, 0, 1) },
	"yesterday": func(now time.Time) time.Time { return DayStart(now).AddDate(0, 0, -1) },
}

// DayStart is midnight of at's day in at's zone.
func DayStart(at time.Time) time.Time {
	y, m, d := at.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, at.Location())
}

// IsDayWord reports whether value names a whole day (today, tomorrow,
// yesterday), which an equality token matches as a range.
func IsDayWord(value string) bool { return value != "now" && dayWords[value] != nil }

// daysCall reads `days(n)`, the CEL helper, as the same duration here.
func daysCall(text string) (time.Duration, bool) {
	inner, ok := strings.CutPrefix(text, "days(")
	if !ok || !strings.HasSuffix(inner, ")") {
		return 0, false
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(inner, ")")), 64)
	if err != nil || n < 0 || n > 36500 {
		return 0, false
	}
	return time.Duration(n * float64(24*time.Hour)), true
}

// RelativeTime is now shifted by an Offset literal ("-7d" is a week ago), or
// a day word resolved against the same clock.
func RelativeTime(value string, now time.Time) (time.Time, bool) {
	if word := dayWords[value]; word != nil {
		return word(now), true
	}
	// `now+90d`, `today-days(7)`: a day word shifted by an Offset.
	if i := strings.IndexAny(value, "+-"); i > 0 && dayWords[value[:i]] != nil {
		d, ok := Offset(value[i:])
		return dayWords[value[:i]](now).Add(d), ok
	}
	d, ok := Offset(value)
	if !ok {
		return time.Time{}, false
	}
	return now.Add(d), true
}
