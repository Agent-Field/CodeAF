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
