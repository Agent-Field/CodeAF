package factory

import "math"

// PolicyMoneyCompare compares whole micro-dollars so decimal sums and caps
// make the same decision at start, while continuing and on a policy read.
func PolicyMoneyCompare(a, b float64) int {
	a, b = math.Round(a*1e6), math.Round(b*1e6)
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
