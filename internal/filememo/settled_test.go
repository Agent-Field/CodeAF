package filememo

import (
	"testing"
	"time"
)

func TestTimestampsSettled(t *testing.T) {
	now := time.Unix(1700000000, 0)
	old := now.Add(-TimestampSettle)
	for _, test := range []struct {
		name              string
		modified, changed time.Time
		want              bool
	}{
		{"boundary", old, old, true},
		{"recent modification", now.Add(-time.Second), old, false},
		{"recent change", old, now.Add(-time.Second), false},
		{"future modification", now.Add(time.Second), old, false},
		{"future change", old, now.Add(time.Second), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := TimestampsSettled(now, test.modified, test.changed, TimestampSettle); got != test.want {
				t.Fatalf("settled=%v, want %v", got, test.want)
			}
		})
	}
}
