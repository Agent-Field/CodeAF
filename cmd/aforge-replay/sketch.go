package main

import (
	"math"
	"sort"
	"time"
)

// ── A PICTURE THAT FORGETS ──────────────────────────────────────────────────
//
// The quantile candidate needs the SHAPE of what a machine has been doing and
// needs it to forget at the rate the world changes. Two small types do the whole
// of that: a decayed sample, and a decayed count.

// forgotten is how many half-lives a measurement is kept for. It is not a
// capacity anybody chose: after ten half-lives an observation carries about one
// thousandth of the weight of a fresh one (2⁻¹⁰ ≈ 1/1024), which cannot move any
// quantile that has a hundred fresher observations beside it. Keeping it longer
// is memory spent on arithmetic that rounds away.
const forgotten = 10

// sketch is an exponentially decayed picture of one quantity.
//
// IT KEEPS THE OBSERVATIONS AND NOT A HISTOGRAM, which is the cheaper choice
// here and the more honest one: a histogram needs a bin width, a bin width is a
// resolution at which two different measurements are declared the same, and
// nothing in this log says what that resolution should be. Ten half-lives of
// observations is a few hundred numbers per machine, and a weighted quantile
// over them is exact.
type sketch struct {
	when  []time.Time
	value []float64
}

func newSketch() *sketch { return &sketch{} }

// add folds one measurement in, and drops everything too old to matter.
func (s *sketch) add(at time.Time, value float64, halfLife time.Duration) {
	if !(value > 0) || halfLife <= 0 {
		return
	}
	s.when = append(s.when, at)
	s.value = append(s.value, value)
	oldest := at.Add(-forgotten * halfLife)
	keep := 0
	for index, when := range s.when {
		if when.Before(oldest) {
			continue
		}
		s.when[keep], s.value[keep] = s.when[index], s.value[index]
		keep++
	}
	s.when, s.value = s.when[:keep], s.value[:keep]
}

// at is the quantile of what this machine has been doing, as of a moment, with
// every observation weighted by how much of itself it has left.
//
// The weight is 2^(−age/halfLife) — the same exponential forgetting
// internal/lane's own beliefs age by — so an observation from one half-life ago
// counts half, and one from an hour ago on a ten-minute half-life counts about a
// sixty-fourth. It answers zero when nothing is left, which the caller reads as
// "no opinion" rather than as a fast machine.
func (s *sketch) at(now time.Time, share float64, halfLife time.Duration) float64 {
	if len(s.value) == 0 || halfLife <= 0 {
		return 0
	}
	type weighed struct{ value, weight float64 }
	held := make([]weighed, 0, len(s.value))
	total := 0.0
	for index, value := range s.value {
		weight := decay(now.Sub(s.when[index]), halfLife)
		if weight <= 0 {
			continue
		}
		held = append(held, weighed{value: value, weight: weight})
		total += weight
	}
	if total <= 0 {
		return 0
	}
	sort.Slice(held, func(i, j int) bool { return held[i].value < held[j].value })
	want, running := share*total, 0.0
	for _, one := range held {
		running += one.weight
		if running >= want {
			return one.value
		}
	}
	return held[len(held)-1].value
}

// fading is a count that forgets at the same rate, which is all the availability
// axis needs: how many requests this machine was recently given, and how many it
// recently answered.
type fading struct {
	weight float64
	at     time.Time
}

func (f *fading) add(at time.Time, by float64, halfLife time.Duration) {
	f.weight = f.weight*decay(at.Sub(f.at), halfLife) + by
	f.at = at
}

func (f *fading) left(now time.Time, halfLife time.Duration) float64 {
	return f.weight * decay(now.Sub(f.at), halfLife)
}

// decay is how much of itself an observation has left after a stretch of time.
// A negative stretch — two rows of one moment, read out of order — is no decay
// at all rather than a weight that grows.
func decay(since, halfLife time.Duration) float64 {
	if halfLife <= 0 || since <= 0 {
		return 1
	}
	return math.Exp2(-since.Seconds() / halfLife.Seconds())
}
