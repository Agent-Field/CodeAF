package jscompat

import "testing"

// Cross-checked against V8 (bun -e): tie cases where Go's half-even differs.
func TestToFixedMatchesV8(t *testing.T) {
	cases := []struct {
		f    float64
		d    int
		want string
	}{
		{0.125, 2, "0.13"},
		{-0.125, 2, "-0.13"},
		{1.005, 2, "1.00"}, // 1.005 is 1.00499...96 in binary — V8 rounds DOWN
		{0, 2, "0.00"},
		{2.675, 2, "2.67"}, // 2.674999... in binary
		{5, 0, "5"},
		{1.5, 0, "2"},
		{2.5, 0, "3"}, // half-up, not half-even
	}
	for _, c := range cases {
		if got := ToFixed(c.f, c.d); got != c.want {
			t.Errorf("ToFixed(%v, %d) = %q, want %q", c.f, c.d, got, c.want)
		}
	}
}
