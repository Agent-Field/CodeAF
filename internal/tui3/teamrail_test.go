package tui3

import (
	"strings"
	"testing"
	"time"
)

// A Traffic row stays on a compact age at every span. A task row and a home
// session print a date past thirty days, and that date is the wrong clock in
// a margin of a few cells.
func TestTrafficAgeStaysCompactPastAMonth(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	for _, c := range []struct {
		name string
		span time.Duration
		want string
	}{
		{"29d", 29 * day, "29d"},
		{"30d", 30 * day, "30d"},
		{"90d", 90 * day, "12w"},
		{"400d", 400 * day, "1y"},
	} {
		if got := trafficAgeAt(now.Add(-c.span), now); got != c.want {
			t.Errorf("%s reads %q, want %q", c.name, got, c.want)
		}
	}
	// The shared ladder still prints a date at the month. This row does not
	// borrow it.
	if got := sinceAt(now.Add(-30*day), now); !strings.Contains(got, " ") {
		t.Errorf("sinceAt at 30d reads %q, want a date", got)
	}
}
