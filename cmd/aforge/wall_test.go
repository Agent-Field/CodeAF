package main

import (
	"testing"
	"time"
)

// `aforge do --timeout 5m` was a parse error because the flag was an integer
// of seconds. It is a duration now, and a bare number is still seconds for one
// release, so no script that passed `-timeout 900` notices (#376).
func TestTheWallTakesAUnitAndStillTakesBareSeconds(t *testing.T) {
	for _, tc := range []struct {
		text string
		want time.Duration
	}{
		{"5m", 5 * time.Minute},
		{"2h", 2 * time.Hour},
		{"90s", 90 * time.Second},
		{"300", 300 * time.Second},
		{" 900 ", 900 * time.Second},
	} {
		got, err := parseWall(tc.text)
		if err != nil || got != tc.want {
			t.Fatalf("parseWall(%q) = %v, %v; want %v", tc.text, got, err, tc.want)
		}
	}
	// 9223372037 seconds is one more than a duration can hold; multiplied out
	// it wrapped to a wall of 145 ms that passed every check.
	for _, text := range []string{"5x", "", "0", "-5", "0s", "later", "9223372037", "99999999999999"} {
		if _, err := parseWall(text); err == nil {
			t.Fatalf("parseWall(%q) accepted a wall that is not one", text)
		}
	}
}
