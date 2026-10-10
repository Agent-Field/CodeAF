package automation

import (
	"strings"
	"testing"
	"time"
)

func zone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("no zone data for %s: %v", name, err)
	}
	return loc
}

// THE NIGHT THE CLOCKS FALL BACK, A 01:30 LINE FIRES ONCE. The schedule this
// replaces returned a moment in the past for every instant of the repeated
// hour, so a pass every five minutes fired a daily item eight times
// (audit-notes/standing-and-schedules.md §3.2).
func TestCronFiresOnceOnTheNightTheClocksFallBack(t *testing.T) {
	loc := zone(t, "America/Toronto")
	s := Schedule{Every: "30 1 * * *", Zone: "America/Toronto"}
	due, err := s.Next(time.Date(2026, 10, 31, 12, 0, 0, 0, loc))
	if err != nil {
		t.Fatal(err)
	}
	var fired []string
	for now := time.Date(2026, 11, 1, 0, 0, 0, 0, loc); now.Before(time.Date(2026, 11, 1, 4, 0, 0, 0, loc)); now = now.Add(time.Minute) {
		if !now.Before(due) {
			fired = append(fired, now.Format("15:04 MST"))
			if due, err = s.Next(now); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(fired) != 1 || fired[0] != "01:30 EDT" {
		t.Fatalf("fired %v on 2026-11-01, want exactly [01:30 EDT]", fired)
	}
}

// NEXT IS ALWAYS LATER, BY THE CLOCK AND BY THE WALL. Walked minute by minute
// across both of a year's transitions in zones north and south of the equator,
// one with a forty-five-minute offset and one with no daylight saving at all.
func TestCronNextIsAlwaysLaterAcrossEveryTransition(t *testing.T) {
	specs := []string{"30 1 * * *", "0 * * * *", "*/15 * * * *", "30 2 * * *", "0 0 * * *", "45 23 * * 0"}
	windows := []struct {
		zone       string
		start, end time.Time
	}{
		{"America/Toronto", time.Date(2026, 3, 7, 22, 0, 0, 0, time.UTC), time.Date(2026, 3, 9, 4, 0, 0, 0, time.UTC)},
		{"America/Toronto", time.Date(2026, 10, 31, 22, 0, 0, 0, time.UTC), time.Date(2026, 11, 2, 4, 0, 0, 0, time.UTC)},
		{"Europe/London", time.Date(2026, 10, 24, 22, 0, 0, 0, time.UTC), time.Date(2026, 10, 26, 4, 0, 0, 0, time.UTC)},
		{"Australia/Sydney", time.Date(2026, 4, 4, 10, 0, 0, 0, time.UTC), time.Date(2026, 4, 5, 22, 0, 0, 0, time.UTC)},
		{"Pacific/Chatham", time.Date(2026, 4, 4, 10, 0, 0, 0, time.UTC), time.Date(2026, 4, 5, 22, 0, 0, 0, time.UTC)},
		{"Asia/Kolkata", time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)},
	}
	for _, w := range windows {
		loc := zone(t, w.zone)
		for _, spec := range specs {
			s := Schedule{Every: spec, Zone: w.zone}
			for now := w.start; now.Before(w.end); now = now.Add(7 * time.Minute) {
				next, err := s.Next(now)
				if err != nil {
					t.Fatalf("%s %q at %v: %v", w.zone, spec, now, err)
				}
				if !next.After(now) {
					t.Fatalf("%s %q: next(%v) = %v is not after it", w.zone, spec, now.In(loc), next.In(loc))
				}
				if !wallOf(next.In(loc)).after(wallOf(now.In(loc))) {
					t.Fatalf("%s %q: next(%v) = %v went backwards on the wall clock", w.zone, spec, now.In(loc), next.In(loc))
				}
			}
		}
	}
}

// In spring the missing hour has no minutes, so a 02:30 line waits a day rather
// than firing an hour early.
func TestCronSkipsTheHourThatDoesNotExist(t *testing.T) {
	loc := zone(t, "America/Toronto")
	s := Schedule{Every: "30 2 * * *", Zone: "America/Toronto"}
	next, err := s.Next(time.Date(2026, 3, 7, 12, 0, 0, 0, loc))
	if err != nil {
		t.Fatal(err)
	}
	// The 7th's 02:30 is already behind noon and the 8th has no 02:30 at all,
	// so the honest answer is the 9th.
	if got := next.In(loc).Format("2006-01-02 15:04 MST"); got != "2026-03-09 02:30 EDT" {
		t.Fatalf("next = %s, want 2026-03-09 02:30 EDT", got)
	}
}

// A RHYTHM SAID IN ONE ZONE STAYS IN IT. The machine — or a remote host — may
// be anywhere; nine in Toronto is nine in Toronto.
func TestCronIsReadInTheAutomationsOwnZone(t *testing.T) {
	loc := zone(t, "America/Toronto")
	s := Schedule{Every: "0 9 * * 1", Zone: "America/Toronto"}
	next, err := s.Next(time.Date(2026, 10, 9, 15, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if got := next.In(loc).Format("Mon 15:04"); got != "Mon 09:00" {
		t.Fatalf("next = %s", got)
	}
}

func TestCronEitherDayRuleWhenBothAreRestricted(t *testing.T) {
	line, err := parseCron("0 9 13 * 5")
	if err != nil {
		t.Fatal(err)
	}
	friday := time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC)      // a Friday, the 9th
	thirteenth := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC) // a Tuesday
	tuesday := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	if !line.matchesDate(friday) || !line.matchesDate(thirteenth) || line.matchesDate(tuesday) {
		t.Fatal("day-of-month and day-of-week must OR when both are restricted")
	}
}

// THE GRID IS THE RHYTHM. A run that started late, or a clock that was away,
// never moves the next slot later: every slot is anchor + k×interval.
func TestIntervalStaysOnItsGrid(t *testing.T) {
	anchor := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	s := Schedule{Every: "15m", Anchor: anchor}
	cases := []struct {
		after time.Time
		want  time.Time
	}{
		{anchor.Add(-time.Hour), anchor},
		{anchor, anchor.Add(15 * time.Minute)},
		{anchor.Add(17 * time.Minute), anchor.Add(30 * time.Minute)},
		{anchor.Add(30 * time.Minute), anchor.Add(45 * time.Minute)},
		{anchor.Add(5*time.Hour + 2*time.Second), anchor.Add(5*time.Hour + 15*time.Minute)},
	}
	for _, c := range cases {
		got, err := s.Next(c.after)
		if err != nil {
			t.Fatal(err)
		}
		if !got.Equal(c.want) {
			t.Fatalf("next(%v) = %v, want %v", c.after, got, c.want)
		}
	}
}

func TestFirstIsAfterNowForARhythmAndTheMomentForAOneOff(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	every := Schedule{Every: "1h", Anchor: now}
	if first, _ := every.First(now); !first.Equal(now.Add(time.Hour)) {
		t.Fatalf("first = %v", first)
	}
	at := now.Add(3 * time.Hour)
	once := Schedule{At: at}
	if first, _ := once.First(now); !first.Equal(at) {
		t.Fatalf("first = %v", first)
	}
	if next, _ := once.Next(at.Add(time.Minute)); !next.IsZero() {
		t.Fatalf("a one-time schedule has no next, got %v", next)
	}
}

func TestParseIntervalReadsDaysAndRefusesUnderAMinute(t *testing.T) {
	if d, err := ParseInterval("2d"); err != nil || d != 48*time.Hour {
		t.Fatalf("2d = %v, %v", d, err)
	}
	if d, err := ParseInterval("90m"); err != nil || d != 90*time.Minute {
		t.Fatalf("90m = %v, %v", d, err)
	}
	for _, bad := range []string{"30s", "0d", "-1h", "soon", ""} {
		if _, err := ParseInterval(bad); err == nil {
			t.Fatalf("%q was accepted", bad)
		}
	}
}

func TestValidateRefusesWhatCouldNeverWake(t *testing.T) {
	cases := map[string]Schedule{
		"never happens":   {Every: "0 0 30 2 *"},
		"no moment":       {},
		"both":            {At: time.Now(), Every: "1h"},
		"unknown zone":    {Every: "0 9 * * *", Zone: "Mars/Olympus"},
		"bad field count": {Every: "0 9 * *"},
		"backwards range": {Every: "0 17-9 * * *"},
	}
	for name, s := range cases {
		if err := s.Validate(); err == nil {
			t.Errorf("%s: accepted %+v", name, s)
		}
	}
	if err := (Schedule{Every: "0 9 * * 1-5", Zone: "America/Toronto"}).Validate(); err != nil {
		t.Fatalf("a weekday line was refused: %v", err)
	}
	if err := (Schedule{Every: "0 9 * * 1-5", Zone: "Mars/Olympus"}).Validate(); err == nil || !strings.Contains(err.Error(), "zone") {
		t.Fatalf("an unknown zone should be named, got %v", err)
	}
}
