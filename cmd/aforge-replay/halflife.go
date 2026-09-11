package main

import (
	"math"
	"sort"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/lane"
)

// ── THE TWO NUMBERS THIS PROGRAM IS NOT ALLOWED TO CHOOSE ───────────────────
//
// Two candidates need a constant each — how fast a picture should forget, and
// how sure a filter may become — and a constant chosen here would be exactly the
// band-aid this instrument exists to stop other people shipping. So both are
// MEASURED FROM THE SAME LOG being replayed, by the two paragraphs below, and
// both are printed in the report beside the table they produced.

// changeHalfLife is how long one measurement of a machine keeps half of what it
// says.
//
// IT IS THE LOG'S OWN CHANGE-POINT RATE, read as a variogram. Two answers from
// one machine a few seconds apart write at nearly the same rate; two a day apart
// are unrelated, and the difference between them is the whole spread of that
// machine's behaviour. So the mean gap between two answers rises with the lag
// between them and then flattens, and the lag at which it has risen HALF the way
// to flat is the moment a measurement has told you half of what it will ever
// tell you. That is a half-life in the only sense this program needs one, and it
// is measured rather than picked.
//
// It is read on the WRITING RATE and not on the first token, deliberately: a
// first-token wait carries the length of the prompt behind it, so two of them
// differ for a reason that is not the machine changing, and the quantity whose
// regime shifts is the one the 2026-09-11 ninefold collapse moved.
func changeHalfLife(measured *world) time.Duration {
	lags := lagLadder()
	gaps := make([]float64, len(lags))
	counts := make([]int, len(lags))
	apart := 0.0
	drawn := 0
	for _, draws := range measured.draws {
		rates := logRates(draws)
		if len(rates) < 4 {
			continue
		}
		apart += scatter(rates) * float64(len(rates))
		drawn += len(rates)
		for i := range rates {
			for j := i + 1; j < len(rates); j++ {
				between := rates[j].at.Sub(rates[i].at)
				if between > lags[len(lags)-1] {
					break
				}
				slot := sort.Search(len(lags), func(k int) bool { return lags[k] >= between })
				if slot >= len(lags) {
					break
				}
				gaps[slot] += math.Abs(rates[j].value - rates[i].value)
				counts[slot]++
			}
		}
	}
	if drawn == 0 || counts[0] == 0 {
		return lane.HalfLife
	}
	independent := apart / float64(drawn)
	nearest := gaps[0] / float64(counts[0])
	halfway := nearest + (independent-nearest)/2
	for slot, lag := range lags {
		if counts[slot] == 0 {
			continue
		}
		if gaps[slot]/float64(counts[slot]) >= halfway {
			return lag
		}
	}
	// A LOG THAT NEVER REACHES HALFWAY IS A LOG WITH NOTHING TO FORGET, and the
	// honest answer there is the belief's own forgetting rather than a figure
	// this program invented to fill the hole.
	return lane.HalfLife
}

// lagLadder is the set of lags the variogram is read at: a doubling from half a
// minute to just over two hours. It is a ladder of DATA and not of cases — each
// rung is a lag, the rungs have no names, and the answer is whichever rung the
// measurement lands on.
//
// BOTH ENDS ARE TIED TO SOMETHING RATHER THAN CHOSEN. The bottom rung is half a
// minute because a shorter one reads the same answer twice — a model call takes
// tens of seconds, so two draws closer together than that are usually one hedge
// or one retry, and their agreement is a fact about our own fan-out and not
// about how fast a machine drifts. The top is the horizon past which this
// instrument stops believing evidence at all — `stopBelieving`, which is
// `forgotten × lane.HalfLife` and the tree's own hundred minutes — with one
// doubling of headroom, so the ladder can see the variogram flatten rather than
// running out exactly where the answer would be. A half-life the ladder could
// only report beyond that is one nothing downstream would act on anyway.
func lagLadder() []time.Duration {
	ladder := make([]time.Duration, 0, 9)
	for lag := 30 * time.Second; lag <= 2*stopBelieving; lag *= 2 {
		ladder = append(ladder, lag)
	}
	return ladder
}

// stamped is one measurement with its moment, which is all either reading here
// needs of a draw.
type stamped struct {
	at    time.Time
	value float64
}

// logRates is a machine's writing rates in the log domain, in time order.
// The log domain because a rate is multiplicative — a machine that halves is as
// far from itself as one that doubles — which is the same reason
// [lane.Posterior] holds its beliefs there.
func logRates(draws []draw) []stamped {
	held := make([]stamped, 0, len(draws))
	for _, one := range draws {
		if one.rate > 0 {
			held = append(held, stamped{at: one.at, value: math.Log(one.rate)})
		}
	}
	return held
}

// scatter is how far one measurement typically sits from another picked at
// random, which is the level a variogram flattens out at. It is twice the mean
// distance from the median rather than a sampled pairing, so that the figure is
// the same every run and needs no generator.
func scatter(held []stamped) float64 {
	if len(held) < 2 {
		return 0
	}
	values := make([]float64, len(held))
	for index, one := range held {
		values[index] = one.value
	}
	middle := quantile(values, 0.5)
	total := 0.0
	for _, value := range values {
		total += math.Abs(value - middle)
	}
	return 2 * total / float64(len(values))
}

// processNoise is the smallest variance the `current+Q` candidate's filter is
// allowed to hold about a machine's median.
//
// IT IS THE MACHINE'S OWN DAY-TO-DAY MOVEMENT, measured. For every machine the
// log has seen on more than one day, take its median writing rate on each day
// and the variance of those medians across days: that is how far the subject
// itself moves while nothing about the measurement changes. The figure reported
// is the median of that across machines, so one machine having a strange week
// cannot set the floor for everybody.
//
// A KALMAN FILTER WITH NO PROCESS NOISE IS A FILTER THAT BELIEVES THE WORLD
// HOLDS STILL. Its variance falls with every observation and never rises, so
// after an afternoon of answers it is very sure where a machine's median is —
// and it is exactly that certainty which makes it slow to accept that the
// machine has collapsed. The floor says: however much has been seen, never claim
// to know a machine's median better than the machine's own days differ.
func processNoise(measured *world) float64 {
	var spreads []float64
	for _, draws := range measured.draws {
		byDay := map[string][]float64{}
		for _, one := range draws {
			if one.rate > 0 {
				day := one.at.Format("2006-01-02")
				byDay[day] = append(byDay[day], math.Log(one.rate))
			}
		}
		if len(byDay) < 2 {
			continue
		}
		medians := make([]float64, 0, len(byDay))
		for _, rates := range byDay {
			medians = append(medians, quantile(rates, 0.5))
		}
		spreads = append(spreads, variance(medians))
	}
	if len(spreads) == 0 {
		return 0
	}
	return quantile(spreads, 0.5)
}

func variance(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}
	mean := 0.0
	for _, value := range values {
		mean += value
	}
	mean /= float64(len(values))
	total := 0.0
	for _, value := range values {
		total += (value - mean) * (value - mean)
	}
	return total / float64(len(values)-1)
}
