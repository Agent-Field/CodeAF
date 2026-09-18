package store

import "testing"

// A GATE IS DONE ON A PASS, OR WHEN THE CHECKS ARE ALL THAT IS MISSING. A
// coverage finding that stands beside any other shortfall has found the work
// itself short, and the moment it anchors would come before the work was
// finished.
func TestAGateIsDoneOnlyWhenTheChecksAreAllThatIsMissing(t *testing.T) {
	for _, c := range []struct {
		name string
		gate DeliveryGate
		want bool
	}{
		{"a pass", DeliveryGate{Pass: true}, true},
		{"an unexercised finding alone", DeliveryGate{Unexercised: []string{"the flag turns on"}}, true},
		{"an unasserted finding alone", DeliveryGate{Unasserted: []string{"the flag turns off"}}, true},
		{"nothing found at all", DeliveryGate{}, false},
		{"a coverage finding beside a failing check of its own", DeliveryGate{Unexercised: []string{"a"}, OwnFailing: []string{"TestX"}}, false},
		{"a coverage finding beside an unbound consumer", DeliveryGate{Unexercised: []string{"a"}, Unbound: []string{"caller"}}, false},
		{"a coverage finding beside a constraint", DeliveryGate{Unasserted: []string{"a"}, Constraint: []string{"no new dependency"}}, false},
		{"a coverage finding on a mechanical failure", DeliveryGate{Unexercised: []string{"a"}, Mechanical: true}, false},
	} {
		if got := c.gate.Done(); got != c.want {
			t.Errorf("%s: Done() = %t, want %t", c.name, got, c.want)
		}
	}
}
