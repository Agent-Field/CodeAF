// Package tally keeps additive sufficient statistics for observations so
// that two sheets recorded on different machines can be merged by addition,
// in any order, and give the same answer as one sheet that saw everything.
// It is standard library only and touches no network, no disk and no clock.
// The only strings a sheet holds are the metric, role, model and dim labels
// an observation carries.
package tally

// Cell is the sufficient statistic recorded for one address: how many
// observations were seen, their sum, and the sum of their squares. Two
// cells for the same address add field by field.
type Cell struct {
	N     int64
	Sum   float64
	SumSq float64
}

// Mean returns the mean of the observations, or 0 when the cell is empty.
func (c Cell) Mean() float64 {
	if c.N == 0 {
		return 0
	}
	return c.Sum / float64(c.N)
}

// Var returns the sample variance of the observations. It is 0 when the
// cell holds fewer than two observations, and it is never negative:
// rounding in the sum-of-squares formula is clamped to zero.
func (c Cell) Var() float64 {
	if c.N < 2 {
		return 0
	}
	v := (c.SumSq - c.Sum*c.Sum/float64(c.N)) / float64(c.N-1)
	if v < 0 {
		return 0
	}
	return v
}
