package expr

import (
	"testing"
	"time"
)

func TestOffsetSharesTheExpressionUnits(t *testing.T) {
	tests := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"7d", 7 * 24 * time.Hour, true}, {"-7d", -7 * 24 * time.Hour, true}, {"+2w", 14 * 24 * time.Hour, true},
		{"24h", 24 * time.Hour, true}, {"30m", 30 * time.Minute, true}, {"0d", 0, true},
		{"7", 0, false}, {"d", 0, false}, {"7x", 0, false}, {"-", 0, false}, {"2026-01-01", 0, false}, {"x7d", 0, false},
	}
	for _, tt := range tests {
		got, ok := Offset(tt.in)
		if ok != tt.ok || got != tt.want {
			t.Errorf("Offset(%q) = %v, %v; want %v, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestRelativeTimeTakesMomentsWithArithmetic(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	day := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	tests := map[string]time.Time{
		"now": now, "today": day, "tomorrow": day.AddDate(0, 0, 1), "yesterday": day.AddDate(0, 0, -1),
		"now+90d": now.AddDate(0, 0, 90), "now-days(7)": now.AddDate(0, 0, -7), "today+1d": day.AddDate(0, 0, 1),
		"days(2)": now.AddDate(0, 0, 2), "-days(0.5)": now.Add(-12 * time.Hour),
	}
	for in, want := range tests {
		if got, ok := RelativeTime(in, now); !ok || !got.Equal(want) {
			t.Errorf("RelativeTime(%q) = %v, %v; want %v", in, got, ok, want)
		}
	}
	for _, bad := range []string{"now+", "soon", "now+x", "days(x)", "2026-10-13"} {
		if _, ok := RelativeTime(bad, now); ok {
			t.Errorf("RelativeTime(%q) must not resolve", bad)
		}
	}
}
