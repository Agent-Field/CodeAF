package tui3

import "testing"

func TestMemoryCountsOmitZeroParts(t *testing.T) {
	for _, tc := range []struct {
		kept, gone, outside int
		want                string
	}{
		{0, 2, 0, "2 let go"},
		{0, 0, 0, ""},
		{1, 0, 0, "1 kept"},
		{0, 0, 3, "3 more in memory"},
		{1, 2, 3, "1 kept · 3 more in memory · 2 let go"},
	} {
		if got := memoryCounts(tc.kept, tc.gone, tc.outside); got != tc.want {
			t.Errorf("counts(%d, %d, %d) = %q; want %q", tc.kept, tc.gone, tc.outside, got, tc.want)
		}
	}
}
