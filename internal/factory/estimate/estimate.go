// Package estimate is what a job will cost and how long it will take,
// MEASURED FROM THE FACTORY'S OWN FINISHED JOBS, never guessed by a model.
//
// A model judges what a model can judge (the complexity rubric, from the
// read or from Look closer); the dollars and minutes come from the jobs this
// factory already ran. An item's figure is the 10th to the 90th percentile of
// its nearest finished jobs, where about eight in ten of them landed: the same repository first, then every
// repository, near on the rubric's factors and the kind. A job with no read,
// or a read with too few similar jobs, is measured on its kind instead: the
// latest finished jobs of the same kind, read or not, in its repository and
// then in every repository. Every finished job counts. Fewer than [MinJobs]
// jobs on both bases is no figure at all, and the page says "no cost history
// yet".
//
// THE LEDGER IS THE FLOOR STORE. A finished job keeps its stream (spent,
// started, ended) and its rubric on its own document, so reading the store is
// reading every job that ever finished, with no second store to keep in step.
//
// Calibration is a backtest: each finished job is estimated from the jobs
// that had ended before it started, which is the figure the page could have
// shown, and its spend is checked against that range. When fewer than
// [Coverage] of them land inside, every range is widened by the spread of
// what jobs really spent against their median. Each basis is backtested and
// widened on its own, so a well-read history is not hidden by a kind pool
// that misses, nor the other way round.
package estimate

import (
	"math"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

const (
	// MinJobs is the fewest similar finished jobs a figure is made from.
	MinJobs = 5
	// Nearest is the most finished jobs one figure is made from.
	Nearest = 12
	// Far is the largest distance a finished job may be from the item and
	// still count as similar: a band is three rubric points wide.
	Far = 4
	// Coverage is the share of backtested jobs that must land inside their
	// range before ranges stop being widened.
	Coverage = 0.6
	// Low and High are the percentiles a range runs between: about eight in
	// ten similar jobs spent inside it.
	Low, High = 0.1, 0.9
)

// The bases a figure is made on.
const (
	// BasisRead is a figure from the jobs nearest on a read's rubric.
	BasisRead = "read"
	// BasisKind is a figure from the latest finished jobs of the same kind,
	// read or not: the page says it was not matched on a read.
	BasisKind = "kind"
)

// Done is one finished job as the ledger reads it.
type Done struct {
	ID      int
	Repo    string
	Kind    factory.Kind
	Rubric  *factory.Rubric
	Band    string
	Scouted bool
	Spent   float64
	Started time.Time
	Ended   time.Time
	Wall    time.Duration
	Steps   int
	Outcome string
	// at is where the job sits for the nearest-job search.
	at point
}

// Outcomes of a finished job.
const (
	OutcomeMerged   = "merged"
	OutcomeFailed   = "failed"
	OutcomeStopped  = "stopped"
	OutcomeFinished = "finished"
)

// Ledger is every finished job on the floor: a job whose run ended and that
// is not running again. A rerun starts a fresh stream, so a job is counted
// once, with what its latest run spent.
func Ledger(items []factory.Item) []Done {
	var done []Done
	for _, it := range items {
		s := it.Stream
		if s == nil || s.Ended.IsZero() || s.Started.IsZero() || factory.JobState(it) == "running" {
			continue
		}
		done = append(done, Done{
			ID: it.ID, Repo: it.Repo, Kind: it.Kind, Rubric: it.Triage.Rubric, Band: bandOf(it.Triage),
			Scouted: it.Triage.Rubric != nil && it.Triage.Rubric.From == factory.RubricFromScout,
			Spent:   factory.JobSpent(it), Started: s.Started, Ended: s.Ended, Wall: s.Ended.Sub(s.Started),
			Steps: len(s.Phases), Outcome: outcomeOf(it), at: pointOf(it),
		})
	}
	return done
}

func outcomeOf(it factory.Item) string {
	switch {
	case it.PullRequest != nil && it.PullRequest.State == factory.PRMerged:
		return OutcomeMerged
	case it.Failure != nil:
		return OutcomeFailed
	case it.DoneHow == "dropped" || it.State == factory.StateDismissed:
		return OutcomeStopped
	}
	return OutcomeFinished
}

// bandOf is the item's complexity band: its rubric's, or for a job read
// before the rubric existed, the band its read's size letter stands for.
func bandOf(t factory.Triage) string {
	if t.Rubric != nil {
		return t.Rubric.Band()
	}
	if t.SizeFrom != factory.SizeFromRead {
		return ""
	}
	for _, band := range []string{factory.BandSmall, factory.BandMedium, factory.BandLarge} {
		if factory.BandSize(band) == t.Size {
			return band
		}
	}
	return ""
}

// Range is one item's measured figure: dollars and minutes from the 10th to
// the 90th percentile of N similar finished jobs, in Repo ("" is every
// repository), on Basis. Widened says calibration widened it.
type Range struct {
	USDLo, USDHi float64
	MinLo, MinHi float64
	N            int
	Repo         string
	Basis        string
	Widened      bool
	// mid is the median spend, which calibration measures strays from.
	mid float64
}

// Calibration is the backtest's hit rate: Hit of Of finished jobs spent
// inside the range the jobs before them gave.
type Calibration struct {
	Hit, Of int
	// low and high are the 10th and 90th percentiles of what each
	// backtested job spent over its range's median: how far real spend
	// strays from the median, which a poor record widens ranges by.
	low, high float64
}

// Poor says the ranges miss too often to be shown unwidened.
func (c Calibration) Poor() bool {
	return c.Of >= MinJobs && float64(c.Hit) < Coverage*float64(c.Of)
}

// Book is the floor's ledger, read once, with its calibration. A stopped
// job's spend is what it spent before someone stopped it, not what the work
// cost, so it is counted as seen and never estimated from.
type Book struct {
	done []Done
	seen int
	cal  map[string]Calibration
}

// Open reads the ledger from the floor's items and backtests it.
func Open(items []factory.Item) Book {
	all := Ledger(items)
	done := slices.DeleteFunc(slices.Clone(all), func(d Done) bool { return d.Outcome == OutcomeStopped })
	return Book{done: done, seen: len(all), cal: backtest(done)}
}

// Jobs is how many finished jobs the ledger can see.
func (b Book) Jobs() int { return b.seen }

// JobsIn is how many finished jobs of one repository the ledger can see.
func (b Book) JobsIn(repo string) int {
	n := 0
	for _, d := range b.done {
		if strings.EqualFold(d.Repo, repo) {
			n++
		}
	}
	return n
}

// Calibration is the ledger's backtest over every basis.
func (b Book) Calibration() Calibration {
	var all Calibration
	for _, c := range b.cal {
		all.Hit, all.Of = all.Hit+c.Hit, all.Of+c.Of
	}
	return all
}

// CalibrationBy is the backtest of the figures made on one basis.
func (b Book) CalibrationBy(basis string) Calibration { return b.cal[basis] }

// For is the item's measured figure: near on its read when five similar
// read jobs finished, else on its kind; false below five on both.
func (b Book) For(it factory.Item) (Range, bool) {
	return b.calibrated(figure(pointOf(it), b.done))
}

// ForNew is a job's figure before it exists: on its kind in repo, then in
// every repository; false below five finished jobs of that kind.
func (b Book) ForNew(repo string, kind factory.Kind) (Range, bool) {
	return b.calibrated(byKind(repo, kind, b.done))
}

// calibrated widens a figure when its own basis misses too often.
func (b Book) calibrated(r Range, ok bool) (Range, bool) {
	if c := b.cal[r.Basis]; ok && c.Poor() {
		return c.widen(r), true
	}
	return r, ok
}

// Left is what a running job will likely spend from here to its end: the
// 10th to the 90th percentile of what similar finished jobs spent past the
// job's spend so far, over the jobs that spent at least that much. False
// while fewer than [MinJobs] such jobs finished.
func (b Book) Left(it factory.Item) (Range, bool) {
	spent := factory.JobSpent(it)
	var past []Done
	for _, d := range b.done {
		if d.Spent >= spent {
			d.Spent -= spent
			past = append(past, d)
		}
	}
	r, ok := figure(pointOf(it), past)
	r.MinLo, r.MinHi = 0, 0 // minutes past a spend are not what this measures
	return r, ok
}

// point is where a job sits for the nearest-job search: its repository, its
// kind, its band, and its five factor scores when it was scored.
type point struct {
	repo   string
	kind   factory.Kind
	band   int // -1 is no band
	scored bool
	scores [5]int
}

func pointOf(it factory.Item) point {
	p := point{repo: it.Repo, kind: it.Kind, band: bandRank(bandOf(it.Triage))}
	if r := it.Triage.Rubric; r != nil {
		p.scored = true
		for i, f := range r.Factors() {
			p.scores[i] = f.Score
		}
	}
	return p
}

// figure is the read match when there is one, else the kind pool.
func figure(p point, done []Done) (Range, bool) {
	if r, ok := rangeOf(p, done); ok {
		return r, true
	}
	return byKind(p.repo, p.kind, done)
}

func rangeOf(p point, done []Done) (Range, bool) {
	if p.band < 0 {
		return Range{}, false
	}
	inRepo, all := nearest(p, done)
	if len(inRepo) >= MinJobs {
		return spread(inRepo, p.repo, BasisRead), true
	}
	if len(all) >= MinJobs {
		return spread(all, "", BasisRead), true
	}
	return Range{}, false
}

// byKind is the figure from the latest [Nearest] finished jobs of the kind,
// in repo first and then in every repository.
func byKind(repo string, kind factory.Kind, done []Done) (Range, bool) {
	var inRepo, every []Done
	for _, d := range done {
		if d.Kind != kind {
			continue
		}
		every = append(every, d)
		if strings.EqualFold(d.Repo, repo) {
			inRepo = append(inRepo, d)
		}
	}
	if len(inRepo) >= MinJobs {
		return spread(latest(inRepo), inRepo[0].Repo, BasisKind), true
	}
	if len(every) >= MinJobs {
		return spread(latest(every), "", BasisKind), true
	}
	return Range{}, false
}

// latest is the [Nearest] jobs that ended last.
func latest(done []Done) []Done {
	s := slices.Clone(done)
	slices.SortStableFunc(s, func(a, b Done) int { return b.Ended.Compare(a.Ended) })
	return s[:min(len(s), Nearest)]
}

// spread is the 10th to the 90th percentile of what the jobs spent and took.
func spread(near []Done, repo, basis string) Range {
	usd, minutes := make([]float64, len(near)), make([]float64, len(near))
	for i, d := range near {
		usd[i], minutes[i] = d.Spent, math.Round(d.Wall.Minutes())
	}
	return Range{USDLo: quantile(usd, Low), USDHi: quantile(usd, High), mid: quantile(usd, 0.5),
		MinLo: quantile(minutes, Low), MinHi: quantile(minutes, High), N: len(near), Repo: repo, Basis: basis}
}

// nearest is the [Nearest] closest similar jobs in the probe's repository and
// in every repository, in one pass: similar jobs are bucketed by distance,
// which is a small whole number, so no sort is needed, and a bucket stops
// taking jobs once it alone holds [Nearest].
func nearest(p point, done []Done) (inRepo, all []Done) {
	var repo, every [Far + 1][]int
	for i := range done {
		dist, ok := distance(p, done[i].at)
		if !ok || dist > Far {
			continue
		}
		if len(every[dist]) < Nearest {
			every[dist] = append(every[dist], i)
		}
		if strings.EqualFold(done[i].Repo, p.repo) && len(repo[dist]) < Nearest {
			repo[dist] = append(repo[dist], i)
		}
	}
	return closest(repo, done), closest(every, done)
}

func closest(buckets [Far + 1][]int, done []Done) []Done {
	var out []Done
	for _, b := range buckets {
		for _, i := range b[:min(len(b), Nearest-len(out))] {
			out = append(out, done[i])
		}
	}
	return out
}

// distance is how far a finished job is from the probe: the factors' gaps
// when both were scored, three points a band otherwise (a band is three
// points wide), and two more for another kind of work.
func distance(p, d point) (int, bool) {
	if d.band < 0 {
		return 0, false
	}
	dist := 3 * abs(p.band-d.band)
	if p.scored && d.scored {
		dist = 0
		for i, s := range p.scores {
			dist += abs(s - d.scores[i])
		}
	}
	if p.kind != d.kind {
		dist += 2
	}
	return dist, true
}

func bandRank(band string) int {
	if band == "" {
		return -1
	}
	return slices.Index([]string{factory.BandSmall, factory.BandMedium, factory.BandLarge}, band)
}

// backtest estimates each finished job from the jobs that ended before it
// started, and counts, per basis, the ones whose spend fell inside.
//
// The ledger is sorted by end, so the jobs before one are a prefix of it.
func backtest(done []Done) map[string]Calibration {
	cal := map[string]Calibration{}
	ratios := map[string][]float64{}
	byEnd := slices.Clone(done)
	slices.SortStableFunc(byEnd, func(a, b Done) int { return a.Ended.Compare(b.Ended) })
	for _, d := range done {
		n, _ := slices.BinarySearchFunc(byEnd, d.Started, func(e Done, t time.Time) int { return e.Ended.Compare(t) })
		r, ok := figure(d.at, byEnd[:n])
		if !ok {
			continue
		}
		c := cal[r.Basis]
		c.Of++
		if d.Spent >= r.USDLo && d.Spent <= r.USDHi {
			c.Hit++
		}
		cal[r.Basis] = c
		if r.mid > 0 {
			ratios[r.Basis] = append(ratios[r.Basis], d.Spent/r.mid)
		}
	}
	for basis, rs := range ratios {
		c := cal[basis]
		c.low, c.high = quantile(rs, 0.1), quantile(rs, 0.9)
		cal[basis] = c
	}
	return cal
}

// widen stretches a range to the spread of what jobs really spent over their
// median: its low end down by the low ratio, its high end up by the high.
func (c Calibration) widen(r Range) Range {
	if c.high == 0 {
		return r
	}
	mid := r.mid
	r.USDLo = min(r.USDLo, mid*c.low)
	r.USDHi = max(r.USDHi, mid*c.high)
	r.Widened = true
	return r
}

// quantile is the nearest-rank q-quantile of values.
func quantile(values []float64, q float64) float64 {
	s := slices.Clone(values)
	slices.Sort(s)
	i := int(math.Ceil(q*float64(len(s)))) - 1
	return s[max(0, min(i, len(s)-1))]
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
