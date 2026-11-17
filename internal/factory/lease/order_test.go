package lease

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// runsBefore is the queue's whole order: Marked, then p0..p3, then none, then
// oldest, then the lower number.
func TestRunsBefore(t *testing.T) {
	t0 := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Minute)
	mark := func(v string) *factory.JobPriority { return &factory.JobPriority{Value: v} }
	cases := []struct {
		name string
		a, b factory.Item
		want bool
	}{
		{"marked beats p0", factory.Item{ID: 2, Created: t1, Marked: true}, factory.Item{ID: 1, Created: t0, Priority: mark("urgent")}, true},
		{"p0 beats older p2", factory.Item{ID: 2, Created: t1, Priority: mark("urgent")}, factory.Item{ID: 1, Created: t0, Labels: []string{"p2"}}, true},
		{"p3 beats none", factory.Item{ID: 2, Created: t1, Labels: []string{"priority:low"}}, factory.Item{ID: 1, Created: t0}, true},
		{"none after p3", factory.Item{ID: 1, Created: t0}, factory.Item{ID: 2, Created: t1, Labels: []string{"p3"}}, false},
		{"a mark wins over a label", factory.Item{ID: 2, Created: t1, Priority: mark("low"), Labels: []string{"p0"}}, factory.Item{ID: 1, Created: t0, Labels: []string{"p1"}}, false},
		{"same priority, oldest first", factory.Item{ID: 2, Created: t0, Labels: []string{"p1"}}, factory.Item{ID: 1, Created: t1, Labels: []string{"p1"}}, true},
		{"same instant, lower number", factory.Item{ID: 1, Created: t0}, factory.Item{ID: 2, Created: t0}, true},
		{"never before itself", factory.Item{ID: 1, Created: t0}, factory.Item{ID: 1, Created: t0}, false},
	}
	for _, tc := range cases {
		if got := runsBefore(tc.a, tc.b); got != tc.want {
			t.Errorf("%s: runsBefore = %v, want %v", tc.name, got, tc.want)
		}
	}
}
