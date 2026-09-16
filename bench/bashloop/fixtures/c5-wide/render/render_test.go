package render

import "testing"

func TestBar(t *testing.T) {
	cases := []struct {
		n, width int
		want     string
	}{
		{2, 5, "##---"},
		{0, 5, "-----"},
		{5, 5, "#####"},
		{9, 5, "#####"},
		{1, 1, "#"},
		{0, 1, "-"},
	}
	for _, c := range cases {
		if got := Bar(c.n, c.width); got != c.want {
			t.Errorf("Bar(%d, %d) = %q, want %q", c.n, c.width, got, c.want)
		}
	}
}
