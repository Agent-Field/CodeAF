package teams

import "testing"

// Contract 4.2 and 4.3: A sub-team share keeps a positive sub-cent cap.
func TestSubTeamShareKeepsSubCentCap(t *testing.T) {
	f := tree()
	for _, tc := range []struct{ cap, share, want float64 }{
		{0.001, 0.5, 0.0005},
		{0.009, 0.5, 0.0045},
		{5, 0.5, 2.5},
	} {
		if err := f.SetSettings("aaaaaaaaaaaa", func(s *Settings) {
			s.CapUSDDay = &tc.cap
			s.SubShare = &tc.share
		}); err != nil {
			t.Fatal(err)
		}
		if got := f.SubTeamCap("aaaaaaaaaaaa", defaults); got != tc.want {
			t.Errorf("cap %v share %v: sub-team got %v, want %v", tc.cap, tc.share, got, tc.want)
		}
	}
}
