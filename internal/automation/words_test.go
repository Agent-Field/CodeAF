package automation

import (
	"testing"
	"time"
)

func TestDescribeSaysTheCommonShapesInWords(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	cases := map[string]string{
		"0 9 * * *":    "every day at 09:00",
		"0 9 * * 1":    "every Monday at 09:00",
		"30 8 * * 1-5": "every weekday at 08:30",
		"0 18 * * 1,4": "every Monday and Thursday at 18:00",
		"0 9 1 * *":    "on the 1st of every month at 09:00",
		"15 * * * *":   "every hour at :15",
		"*/15 * * * *": "every 15 minutes",
		"0 9 13 * 5":   "on the cron line 0 9 13 * 5",
		"15m":          "every 15 minutes",
		"2h":           "every 2 hours",
		"1d":           "every day",
	}
	for every, want := range cases {
		if got := Describe(Schedule{Every: every}, now); got != want {
			t.Errorf("%q reads %q, want %q", every, got, want)
		}
	}
	if got := Describe(Schedule{At: now.Add(6 * time.Hour)}, now); got != "today at 18:00" {
		t.Errorf("a moment today reads %q", got)
	}
	if got := Describe(Schedule{At: now.Add(21 * time.Hour)}, now); got != "tomorrow at 09:00" {
		t.Errorf("a moment tomorrow reads %q", got)
	}
}

func TestExactCarriesTheZoneOfACronLine(t *testing.T) {
	if got := Exact(Schedule{Every: "0 9 * * 1", Zone: "America/Toronto"}); got != "cron 0 9 * * 1 · America/Toronto" {
		t.Fatalf("exact = %q", got)
	}
	if got := Exact(Schedule{Every: "15m"}); got != "every 15m" {
		t.Fatalf("exact = %q", got)
	}
}
