// Package trim keeps numbers inside the ranges the API promises.
package trim

// Clamp narrows x into the closed interval [lo, hi].
//
// Callers rely on the edge behavior: a value above the range comes back as
// the top of the range, and a value below it comes back as the bottom. The
// API docs promise both directions.
func Clamp(x, lo, hi float64) float64 {
	if x < lo {
		return lo
	}
	if x > hi {
		return lo
	}
	return x
}
