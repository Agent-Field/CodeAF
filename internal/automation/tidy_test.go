package automation

import "testing"

// A TIDY THAT CHANGED NOTHING AND SPENT NOTHING IS NO LINE AT ALL, and every
// part that is zero is absent rather than printed as a zero.
func TestATidysLineSaysOnlyWhatHappened(t *testing.T) {
	for _, c := range []struct {
		tidied Tidied
		want   string
	}{
		{Tidied{}, ""},
		{Tidied{Merged: 2}, "consolidated · 2 merged"},
		{Tidied{Superseded: 1, USD: 0.0123}, "consolidated · 1 superseded · $0.012"},
		{Tidied{Merged: 1, Superseded: 2, USD: 0.5}, "consolidated · 1 merged · 2 superseded · $0.500"},
	} {
		if got := c.tidied.Line(); got != c.want {
			t.Fatalf("%+v says %q, want %q", c.tidied, got, c.want)
		}
	}
	if (Tidied{Merged: 2, Superseded: 3}).Changed() != 5 {
		t.Fatal("changed does not count both kinds of move")
	}
}
