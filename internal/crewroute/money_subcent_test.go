package crewroute

import "testing"

func TestCrewMoneyKeepsTinyPositiveAmounts(t *testing.T) {
	for _, tc := range []struct {
		usd  float64
		want string
	}{
		{0, ""}, {-1, ""}, {0.000001, "<$0.0001"}, {0.0003, "$0.0003"},
		{0.009, "$0.0090"}, {0.1, "$0.100"}, {1, "$1.00"},
	} {
		if got := Money(tc.usd); got != tc.want {
			t.Errorf("Money(%g) = %q; want %q", tc.usd, got, tc.want)
		}
	}
}
