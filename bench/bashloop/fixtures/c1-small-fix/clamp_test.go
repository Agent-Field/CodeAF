package trim

import "testing"

func TestClamp(t *testing.T) {
	cases := []struct {
		name      string
		x, lo, hi float64
		want      float64
	}{
		{"inside", 2, 1, 3, 2},
		{"below comes back as the bottom", 0.5, 1, 3, 1},
		{"above comes back as the top", 5, 1, 3, 3},
		{"exactly the top", 3, 1, 3, 3},
		{"exactly the bottom", 1, 1, 3, 1},
		{"negative range below", -10, -5, -1, -5},
		{"negative range above", 0, -5, -1, -1},
	}
	for _, c := range cases {
		if got := Clamp(c.x, c.lo, c.hi); got != c.want {
			t.Errorf("Clamp(%v, %v, %v) = %v, want %v", c.x, c.lo, c.hi, got, c.want)
		}
	}
}
