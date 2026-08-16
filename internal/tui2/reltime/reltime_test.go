package reltime

import (
	"strings"
	"testing"
	"time"
)

// now is the instant every case is measured against: a Tuesday afternoon, so
// "yesterday" is a Monday and the weekday rung has somewhere to land.
var now = time.Date(2026, time.August, 11, 14, 30, 0, 0, time.UTC)

// The ladder IS the specification, so it is written out rung by rung.
func TestShortLadder(t *testing.T) {
	cases := []struct {
		name string
		at   time.Time
		want string
	}{
		{"the same instant", now, "now"},
		{"nine seconds is still now", now.Add(-9 * time.Second), "now"},
		{"ten seconds starts counting", now.Add(-10 * time.Second), "10s"},
		{"seconds truncate, never round", now.Add(-59900 * time.Millisecond), "59s"},
		{"a minute exactly", now.Add(-time.Minute), "1m"},
		{"almost two minutes is one", now.Add(-119 * time.Second), "1m"},
		{"minutes to the hour", now.Add(-59 * time.Minute), "59m"},
		{"an hour exactly", now.Add(-time.Hour), "1h"},
		{"hours to the day", now.Add(-23 * time.Hour), "23h"},
		{"a day is yesterday", now.Add(-24 * time.Hour), "yesterday"},
		{"still yesterday at thirty hours", now.Add(-30 * time.Hour), "yesterday"},
		{"two days is a weekday", now.Add(-48 * time.Hour), "sun"},
		{"six days is still a weekday", now.AddDate(0, 0, -6), "wed"},
		{"seven days is a date", now.AddDate(0, 0, -7), "aug 4"},
		{"a month back", now.AddDate(0, -1, 0), "jul 11"},
		{"another year says so", now.AddDate(-1, 0, 0), "aug 11 2025"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Short(c.at, now); got != c.want {
				t.Fatalf("Short = %q, want %q", got, c.want)
			}
		})
	}
}

// Clock skew is real and "-3s" is not a reading. Anything ahead of now is now.
func TestShortClampsTheFuture(t *testing.T) {
	for _, ahead := range []time.Duration{time.Millisecond, time.Second, time.Hour, 400 * time.Hour} {
		if got := Short(now.Add(ahead), now); got != "now" {
			t.Fatalf("%s in the future read as %q, want \"now\"", ahead, got)
		}
	}
}

// A time nobody set is missing data, not a moment zero seconds ago (8.2.20).
func TestShortOnAZeroTimeIsEmpty(t *testing.T) {
	if got := Short(time.Time{}, now); got != "" {
		t.Fatalf("a zero time read as %q, want the empty reading", got)
	}
}

// The calendar rungs answer in the READER's location: the same instant is
// yesterday or today depending on where the reader is sitting.
func TestShortReadsTheCalendarInNowsLocation(t *testing.T) {
	east := time.FixedZone("east", 10*3600)
	// 23:00 UTC on the 10th is 09:00 on the 11th in +10, so from a 14:30 UTC
	// "now" it is 15h30m ago either way — the hour rung, in both zones.
	at := time.Date(2026, time.August, 10, 23, 0, 0, 0, time.UTC)
	if got := Short(at, now); got != "15h" {
		t.Fatalf("Short in UTC = %q, want \"15h\"", got)
	}
	// Past a day the calendar takes over, and the two zones disagree about
	// which calendar day the instant fell on — which is the point.
	older := time.Date(2026, time.August, 9, 23, 0, 0, 0, time.UTC)
	if got := Short(older, now); got != "sun" {
		t.Fatalf("Short in UTC = %q, want \"sun\"", got)
	}
	if got := Short(older, now.In(east)); got != "mon" {
		t.Fatalf("Short in +10 = %q, want \"mon\" — the instant landed on monday there", got)
	}
}

// Spring forward makes one calendar day 23 hours long. The day before it must
// still read as "yesterday" and not slip a rung.
func TestShortSurvivesADaylightSavingDay(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("no tzdata: %v", err)
	}
	// 2026-03-08 is the spring-forward day in New York, so the calendar day it
	// names is only 23 hours long. A gap that crosses it and clears a day is
	// where dividing the elapsed duration by 24 hours would answer "0 days".
	before := time.Date(2026, time.March, 7, 6, 0, 0, 0, ny)
	after := time.Date(2026, time.March, 8, 12, 0, 0, 0, ny)
	if got := Short(before, after); got != "yesterday" {
		t.Fatalf("across spring forward Short = %q, want \"yesterday\"", got)
	}
	// And one day further, across the same 23-hour day.
	next := time.Date(2026, time.March, 9, 12, 0, 0, 0, ny)
	if got := Short(before, next); got != "sat" {
		t.Fatalf("two days across spring forward Short = %q, want \"sat\"", got)
	}
}

func TestElapsedLadder(t *testing.T) {
	cases := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"nothing yet", 0, "0s"},
		{"a negative duration is zero", -5 * time.Second, "0s"},
		{"seconds", 12 * time.Second, "12s"},
		{"seconds truncate", 12900 * time.Millisecond, "12s"},
		{"a whole minute drops the zero", time.Minute, "1m"},
		{"minute and seconds", 72 * time.Second, "1m 12s"},
		{"minutes near the hour", 59*time.Minute + 59*time.Second, "59m 59s"},
		{"a whole hour drops the zero", time.Hour, "1h"},
		{"hours and minutes", 2*time.Hour + 5*time.Minute, "2h 5m"},
		{"seconds never reach the third rung", 2*time.Hour + 5*time.Minute + 59*time.Second, "2h 5m"},
		{"a whole day drops the zero", 24 * time.Hour, "1d"},
		{"days and hours", 27 * time.Hour, "1d 3h"},
		{"a long run", 100*24*time.Hour + 7*time.Hour, "100d 7h"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Elapsed(c.d); got != c.want {
				t.Fatalf("Elapsed = %q, want %q", got, c.want)
			}
		})
	}
}

// Two rungs, never three: no reading may carry three units.
func TestElapsedNeverShowsThreeRungs(t *testing.T) {
	for d := time.Duration(0); d < 72*time.Hour; d += 997 * time.Millisecond {
		if n := strings.Count(Elapsed(d), " "); n > 1 {
			t.Fatalf("Elapsed(%s) = %q carries %d separators", d, Elapsed(d), n)
		}
	}
}

// The two ladders share their first rung, so the same moment described two ways
// cannot disagree about the number of seconds that have passed.
func TestShortAndElapsedAgreeUnderAMinute(t *testing.T) {
	for d := nowBelow; d < time.Minute; d += time.Second {
		short, elapsed := Short(now.Add(-d), now), Elapsed(d)
		if short != elapsed {
			t.Fatalf("at %s Short says %q and Elapsed says %q", d, short, elapsed)
		}
	}
}

// A reading may never claim more time has passed than actually has: as the gap
// grows the number inside a rung only ever grows with it.
func TestShortNeverOverstatesTheGap(t *testing.T) {
	for d := time.Second; d < 24*time.Hour; d += 7 * time.Second {
		got := Short(now.Add(-d), now)
		if got == "now" {
			continue
		}
		unit := got[len(got)-1]
		n, scale := 0, time.Second
		switch unit {
		case 's':
			scale = time.Second
		case 'm':
			scale = time.Minute
		case 'h':
			scale = time.Hour
		default:
			t.Fatalf("at %s the reading %q left the numeric ladder", d, got)
		}
		for _, r := range got[:len(got)-1] {
			n = n*10 + int(r-'0')
		}
		if time.Duration(n)*scale > d {
			t.Fatalf("at %s the reading %q claims more time than passed", d, got)
		}
	}
}
