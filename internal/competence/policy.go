package competence

import (
	"errors"
	"math"
	"time"
)

// Class is where one scope stands for one model.
type Class string

const (
	Strong   Class = "strong"
	Frontier Class = "frontier"
	Weak     Class = "weak"
	Stale    Class = "stale"
)

// Classes is every class, in the order a map draws them.
var Classes = []Class{Strong, Frontier, Weak, Stale}

// Policy is every boundary the grouping reads. THE THRESHOLDS ARE READ OFF
// CHECKS ALONE: the only inputs are how many results a scope has, how many of
// them passed, and when the last one was.
type Policy struct {
	// StaleAfter is how old a scope's newest result may be before the scope is
	// stale. Inside it, only results inside it are counted.
	StaleAfter time.Duration
	// StrongAtLeast and WeakAtMost are the pass-rate bounds, passed over
	// results, where an undone result is not a pass.
	StrongAtLeast float64
	WeakAtMost    float64
	// MinResults is how many results a scope needs before it may be called
	// strong or weak; fewer is frontier, because a scope with two results has
	// not said enough to be either.
	MinResults int
}

// DefaultPolicy is the engine's grouping: thirty days, four in five, two in five,
// three results.
func DefaultPolicy() Policy {
	return Policy{
		StaleAfter:    30 * 24 * time.Hour,
		StrongAtLeast: 0.8,
		WeakAtMost:    0.4,
		MinResults:    3,
	}
}

func (p Policy) validate() error {
	rate := func(v float64) bool { return v >= 0 && v <= 1 && !math.IsNaN(v) }
	if p.StaleAfter <= 0 || p.MinResults <= 0 || !rate(p.StrongAtLeast) || !rate(p.WeakAtMost) ||
		p.WeakAtMost >= p.StrongAtLeast {
		return errors.New("a competence policy needs a positive staleness and result floor, and weak below strong in [0,1]")
	}
	return nil
}

// classify is one scope's class from its counted results. Stale is decided by
// the caller, which is the one that knows whether anything was recent.
func (p Policy) classify(results, passed int) Class {
	if results < p.MinResults {
		return Frontier
	}
	rate := float64(passed) / float64(results)
	switch {
	case rate >= p.StrongAtLeast:
		return Strong
	case rate <= p.WeakAtMost:
		return Weak
	default:
		return Frontier
	}
}
