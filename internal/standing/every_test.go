package standing

import (
	"strings"
	"testing"
	"time"
	// The zone database is embedded so the daylight-saving cases below are the
	// same on a developer's laptop, in CI, and inside a container with no
	// /usr/share/zoneinfo.
	_ "time/tzdata"
)

// utc is the plain clock most of the table uses: no zone story, just arithmetic.
func at(t *testing.T, text string, location *time.Location) time.Time {
	t.Helper()
	moment, err := time.ParseInLocation("2006-01-02T15:04", text, location)
	if err != nil {
		t.Fatalf("cannot read the moment %q: %v", text, err)
	}
	return moment
}

func TestParseEveryReadsCronLines(t *testing.T) {
	utc := time.UTC
	cases := []struct {
		name  string
		spec  string
		from  string
		want  string
		about string
	}{
		{name: "later today", spec: "0 9 * * *", from: "2026-08-20T08:00", want: "2026-08-20T09:00"},
		{name: "strictly after now", spec: "0 9 * * *", from: "2026-08-20T09:00", want: "2026-08-21T09:00"},
		{name: "a step inside the hour", spec: "*/15 * * * *", from: "2026-08-20T10:07", want: "2026-08-20T10:15"},
		{name: "a step across the hour", spec: "*/15 * * * *", from: "2026-08-20T10:45", want: "2026-08-20T11:00"},
		{name: "a weekday", spec: "0 9 * * 1", from: "2026-08-21T12:00", want: "2026-08-24T09:00"},
		{name: "a list of hours", spec: "30 6,18 * * *", from: "2026-08-20T07:00", want: "2026-08-20T18:30"},
		{name: "the first of the month", spec: "0 0 1 * *", from: "2026-08-20T00:00", want: "2026-09-01T00:00"},
		{name: "a range of hours", spec: "0 9-17 * * *", from: "2026-08-20T12:30", want: "2026-08-20T13:00"},
		{name: "sunday as zero", spec: "0 0 * * 0", from: "2026-08-20T00:00", want: "2026-08-23T00:00"},
		{name: "sunday as seven", spec: "0 0 * * 7", from: "2026-08-20T00:00", want: "2026-08-23T00:00"},
		{name: "a month away", spec: "15 3 1 1 *", from: "2026-02-01T00:00", want: "2027-01-01T03:15"},
		{name: "weekdays only", spec: "*/30 9-10 * * 1-5", from: "2026-08-22T12:00", want: "2026-08-24T09:00"},
		{name: "a step from a number", spec: "5/15 * * * *", from: "2026-08-20T10:06", want: "2026-08-20T10:20"},
		{
			name: "day of month or day of week", spec: "0 12 13 * 5", from: "2026-08-01T00:00", want: "2026-08-07T12:00",
			about: "when both day fields are restricted, either one matching is a match — the 7th is a Friday and comes before the 13th",
		},
		{name: "every minute", spec: "* * * * *", from: "2026-08-20T10:07", want: "2026-08-20T10:08"},
	}
	for _, one := range cases {
		t.Run(one.name, func(t *testing.T) {
			next, err := ParseEvery(one.spec)
			if err != nil {
				t.Fatalf("%q was refused: %v", one.spec, err)
			}
			got := next(at(t, one.from, utc))
			want := at(t, one.want, utc)
			if !got.Equal(want) {
				t.Fatalf("%q from %s answered %s, wanted %s%s", one.spec, one.from, got, want, "  "+one.about)
			}
		})
	}
}

func TestParseEveryReadsDurations(t *testing.T) {
	from := at(t, "2026-08-20T10:00", time.UTC)
	for _, one := range []struct {
		spec string
		want time.Duration
	}{
		{"1m", time.Minute},
		{"20m", 20 * time.Minute},
		{"2h", 2 * time.Hour},
		{"1h30m", 90 * time.Minute},
		{"24h", 24 * time.Hour},
	} {
		next, err := ParseEvery(one.spec)
		if err != nil {
			t.Fatalf("%q was refused: %v", one.spec, err)
		}
		if got := next(from); !got.Equal(from.Add(one.want)) {
			t.Fatalf("%q answered %s, wanted %s", one.spec, got, from.Add(one.want))
		}
	}
}

func TestParseEveryRefusesWhatItCannotRead(t *testing.T) {
	for _, spec := range []string{
		"",
		"   ",
		"30s",
		"999ms",
		"banana",
		"0 9 * *",
		"0 9 * * * *",
		"60 9 * * *",
		"0 24 * * *",
		"0 9 0 * *",
		"0 9 * 13 *",
		"0 9 * * 8",
		"0 9 * * mon",
		"*/0 * * * *",
		"5-1 * * * *",
		"0 9 * * ,",
	} {
		if _, err := ParseEvery(spec); err == nil {
			t.Fatalf("%q was accepted and should not have been", spec)
		} else if !strings.HasPrefix(err.Error(), "standing: ") {
			t.Fatalf("%q was refused with an unowned sentence: %v", spec, err)
		}
	}
}

// A rhythm is a WALL CLOCK rhythm: nine in the morning is nine in the morning
// on both sides of a clock change, which is the only reading a person means.
func TestParseEveryStepsThroughDaylightSaving(t *testing.T) {
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("no zone database here: %v", err)
	}
	next, err := ParseEvery("0 2 * * *")
	if err != nil {
		t.Fatal(err)
	}
	// 2026-03-08 has no two o'clock at all in New York, so a two o'clock
	// routine waits for the ninth rather than firing an hour early on the
	// eighth.
	got := next(at(t, "2026-03-07T03:00", newYork))
	want := at(t, "2026-03-09T02:00", newYork)
	if !got.Equal(want) {
		t.Fatalf("across the spring forward the next moment was %s, wanted %s", got, want)
	}

	// The ordinary case on either side of the change still lands on the wall
	// clock the person said.
	nine, err := ParseEvery("0 9 * * *")
	if err != nil {
		t.Fatal(err)
	}
	got = nine(at(t, "2026-03-07T10:00", newYork))
	want = at(t, "2026-03-08T09:00", newYork)
	if !got.Equal(want) {
		t.Fatalf("the morning after the spring forward was %s, wanted %s", got, want)
	}
	if hour := got.Hour(); hour != 9 {
		t.Fatalf("the wall clock said %d o'clock, wanted 9", hour)
	}

	// The hour that happens twice is the one place a wall-clock rhythm fires
	// twice. THE DAILY COUNT IS THE FLOOR UNDER THAT, not the parser.
	half, err := ParseEvery("30 1 * * *")
	if err != nil {
		t.Fatal(err)
	}
	first := half(at(t, "2026-11-01T00:00", newYork))
	second := half(first)
	if first.Hour() != 1 || second.Hour() != 1 {
		t.Fatalf("the fall back answered %s then %s; both should be one o'clock", first, second)
	}
	if gap := second.Sub(first); gap != time.Hour {
		t.Fatalf("the repeated hour was %s apart, wanted one hour", gap)
	}
}

// An impossible line is answered with the zero moment rather than a search that
// never ends.
func TestParseEveryGivesUpOnAMomentThatNeverComes(t *testing.T) {
	next, err := ParseEvery("0 0 30 2 *")
	if err != nil {
		t.Fatalf("the line is legal and should parse: %v", err)
	}
	if got := next(at(t, "2026-08-20T00:00", time.UTC)); !got.IsZero() {
		t.Fatalf("the thirtieth of February answered %s", got)
	}
}
