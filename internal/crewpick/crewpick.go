// Package crewpick picks one model per seat for a three-seat crew by reading
// the pareto front of expected task bill against crew quality.
//
// It deliberately imports nothing from the rest of the tree and touches no
// disk and no network: the caller owns the candidate list and the seat
// shapes, and the same candidates give the same front however often they are
// asked about and in whatever order they arrive.
//
// The law it implements: every (worker, high, mastermind) combination the
// candidates can field is scored by the bill its seats' token volumes run up
// and by the mean of its three seat qualities. A candidate is in the running
// for a seat only when it meets the family and the seat's needs and its
// quality reaches Floor of that seat's best in the family, and a crew is
// refused outright when its high seat comes from the worker's own vendor.
// The crews that no other crew beats on both bill and quality survive as the
// front, sorted by bill; Presets, AtStake and AtKnob read picks off that
// front.
//
// A pick is a decision with units. The person's one dial is the STAKE: what
// one failed task costs them, in dollars. A crew's loss at a stake is its
// expected bill for a task plus the stake times the chance the task is not
// accepted, and the pick at a stake is the crew on the front with the least
// loss. The three named presets are three stakes (docs/design/model-pool/
// pareto-crewing.tex, the decision section): the knee of the front that
// balanced used to name implied a stake nobody was shown, and one that moved
// with the price level while a person's stake does not.
//
// A candidate that publishes some but not all of its three indexes is scored
// on the ones it publishes: each missing index is estimated from the call's
// candidates that publish both it and one of its measured ones — the donors —
// never above the call's largest measured value of that index, and the
// result says which of a pick's indexes were estimated. A candidate that
// publishes none of its indexes, or no price at all, is out of the running.
package crewpick

import (
	"math"
	"slices"
	"sort"
	"strings"
)

// An Index is one of the three capability indexes quality is read from —
// intelligence, coding, agentic — in the order the seat weights and a
// candidate's reading carry them.
type Index int

const (
	Intelligence Index = iota
	Coding
	Agentic
)

// A Candidate is one model up for a seat. Prices are dollars per 1M tokens.
//
// A candidate carrying none of the three indexes — zero values, which is how
// unpublished indexes read — or carrying a zero prompt price is not a
// candidate at all: it never occupies a seat, never clears a floor, and
// raises no ceiling. A candidate carrying some but not all of its indexes is
// scored on the ones it publishes, each missing one estimated from the
// donors among its fellow candidates, and the result says which of a pick's
// indexes those were. A model that publishes no cache-read price says so with
// HasCacheRead false and pays the prompt price on the cache share too.
type Candidate struct {
	// ID names the candidate; the text before its first slash is the vendor.
	ID string
	// Open marks a candidate the open family may draw from.
	Open bool
	// Intelligence, Coding and Agentic are the three capability indexes
	// quality is read from.
	Intelligence float64
	Coding       float64
	Agentic      float64
	// PromptPrice, CompletionPrice and CacheReadPrice are the dollars per
	// 1M tokens of each kind.
	PromptPrice     float64
	CompletionPrice float64
	CacheReadPrice  float64
	// HasCacheRead reports whether CacheReadPrice is published at all.
	HasCacheRead bool
	// Context is the model's context window, in tokens.
	Context int
	// Images and Tools are what the model can take and call.
	Images bool
	Tools  bool
}

// A Seat is one of a crew's three roles: the worker carries the volume of
// the work, the high seat judges and repairs what the worker writes, and the
// mastermind spends a little context steering the whole task.
type Seat int

const (
	Worker Seat = iota
	High
	Mastermind
)

// A SeatShape is how one seat uses whatever model sits it: the weights
// quality is read from, the token mix cost is computed on, what the model must
// be able to take and call, the window it must have, and the share of a
// task's tokens the seat burns.
type SeatShape struct {
	// Weights reads quality from the three indexes: intelligence, coding,
	// agentic.
	Weights [3]float64
	// InOut is the seat's input tokens per output token; the completion
	// price is spread over it.
	InOut float64
	// CacheShare is the share of the seat's input tokens served from cache.
	CacheShare float64
	// NeedTools and NeedImages are what a model must take and call to sit
	// the seat.
	NeedTools  bool
	NeedImages bool
	// MinContext is the window a model must have to sit the seat.
	MinContext int
	// Volume is the seat's share of a task's tokens, and the weight its
	// cost carries in a crew's bill.
	Volume float64
}

// DefaultShapes returns the three seat shapes crews are picked for. The
// worker takes half its quality from the agentic index and reads barely
// anything but cached input; the high seat judges the three indexes evenly,
// needs tools and images both, and sees the same token mix on a sliver of the
// volume; the mastermind reads intelligence and agentic only, on a terse
// ten-in-one mix with no cache and a fraction of the volume.
func DefaultShapes() map[Seat]SeatShape {
	return map[Seat]SeatShape{
		Worker: {
			Weights:    [3]float64{0.2, 0.3, 0.5},
			InOut:      100,
			CacheShare: 0.75,
			NeedTools:  true,
			MinContext: 200_000,
			Volume:     0.90,
		},
		High: {
			Weights:    [3]float64{1.0 / 3.0, 1.0 / 3.0, 1.0 / 3.0},
			InOut:      100,
			CacheShare: 0.75,
			NeedTools:  true,
			NeedImages: true,
			MinContext: 200_000,
			Volume:     0.08,
		},
		Mastermind: {
			Weights:    [3]float64{0.6, 0, 0.4},
			InOut:      10,
			MinContext: 200_000,
			Volume:     0.02,
		},
	}
}

// A TaskUsage is what one task spent on each seat, in tokens: the evidence a
// seat's shape is learned from. A seat the task never seated is absent.
type TaskUsage map[Seat]SeatTokens

// SeatTokens is one seat's tokens on one task.
type SeatTokens struct {
	In  int64
	Out int64
}

// ShapeWeightAt is the number of tasks at which the seats' measured shape
// carries the same weight as the defaults: at N tasks the measurement is worth
// half of each learned figure and the default the other half, the same
// N/(N+ShapeWeightAt) shrinkage the quality prior uses, so one strange task
// cannot move a bill and thirty ordinary ones can.
const ShapeWeightAt = 30

// ShapesFrom learns the seats' shapes from what real tasks spent: each seat's
// Volume moves from the default towards the mean share of a task's tokens the
// seat took, and its InOut towards the seat's input tokens per output token
// over every task, each by N/(N+ShapeWeightAt) with N the tasks that carried
// the seat. Everything else about a shape — its weights, its needs, its cache
// share, which no ledger row can see — is the default's. A task with no
// tokens teaches nothing, and no tasks at all leaves the defaults untouched.
// The shipped defaults put eight percent of a task on the high seat; on the
// bench's 133 task-door runs the seat's mean share was thirteen percent with
// a long tail, and the bill that difference hid is what this exists to see.
func ShapesFrom(defaults map[Seat]SeatShape, tasks []TaskUsage) map[Seat]SeatShape {
	type tally struct {
		n       int
		share   float64
		in, out float64
	}
	tallies := map[Seat]*tally{}
	for _, task := range tasks {
		var total float64
		for _, t := range task {
			total += float64(t.In + t.Out)
		}
		if total <= 0 {
			continue
		}
		for seat, t := range task {
			if t.In+t.Out <= 0 {
				continue
			}
			ty := tallies[seat]
			if ty == nil {
				ty = &tally{}
				tallies[seat] = ty
			}
			ty.n++
			ty.share += float64(t.In+t.Out) / total
			ty.in += float64(t.In)
			ty.out += float64(t.Out)
		}
	}
	shapes := make(map[Seat]SeatShape, len(defaults))
	for seat, shape := range defaults {
		ty := tallies[seat]
		if ty == nil || ty.n == 0 {
			shapes[seat] = shape
			continue
		}
		w := float64(ty.n) / float64(ty.n+ShapeWeightAt)
		shape.Volume = (1-w)*shape.Volume + w*ty.share/float64(ty.n)
		if ty.out > 0 {
			shape.InOut = (1-w)*shape.InOut + w*ty.in/ty.out
		}
		shapes[seat] = shape
	}
	return shapes
}

// A Family is which candidates a crew may be drawn from.
type Family int

const (
	// Open draws only from candidates marked Open.
	Open Family = iota
	// All draws from every candidate.
	All
)

// A Crew is one (worker, high, mastermind) pick. Bill is the expected dollars
// per 1M task tokens once each seat's share of the task is paid for at its
// seat cost; Quality is the mean of the three seat qualities on a 0-100
// scale.
type Crew struct {
	Worker     string
	High       string
	Mastermind string
	Bill       float64
	Quality    float64
	// Estimated marks, for the model in each seat, which of its indexes
	// were scored by estimate rather than measurement: the row is the seat
	// and the column the index. A crew picked on measured indexes alone is
	// all false.
	Estimated [3][3]bool
	// Measured marks, for the model in each seat, whether a pool's own
	// rating for that model entered the seat's quality: the row is the seat
	// and the value says the seat's model carried a rating with a positive
	// observation count. A crew picked with no prior, or whose seats the
	// prior never rated, is all false.
	Measured [3]bool
}

// Floor is the share of a seat's best quality in the family that a candidate
// must reach to stay in the running for that seat. A model that cannot do
// the job is not cheap, whatever it costs.
const Floor = 0.80

// PriorWeightAt is the observation count at which a pool's own rating for a
// model carries the same weight in a seat's quality as the catalog's
// published indexes: at N observations the rating is worth half the seat and
// the catalog the other half. A rating with no observations never enters.
const PriorWeightAt = 30

// A Rating is a pool's measured quality for one model on one seat, on the
// same 0-100 scale quality is read on, with the number of observations behind
// it.
type Rating struct {
	Mean float64
	N    int
}

// A Prior is a pool's measured quality per seat, keyed by canonical model id:
// the rating a seat's quality is blended towards, by how many observations
// back it. A nil Prior is no prior at all, and every seat keeps the catalog
// quality.
type Prior map[Seat]map[string]Rating

// A Cell is one measurement a caller hands PriorFromCells: a role, a model id
// and the pool's mean quality with the observations behind it. It is the shape
// a measurement document reads into, so crewpick need not import the reader.
type Cell struct {
	Role  string
	Model string
	Mean  float64
	N     int
}

// seatWords names the roles a Cell's role word maps to a seat by, folded to
// one spelling so a document's own case never matters. Any other word names
// no seat and is ignored.
var seatWords = map[string]Seat{
	"worker":     Worker,
	"high":       High,
	"mastermind": Mastermind,
}

// PriorFromCells reads a pool's measurements into a Prior: a cell names a
// seat by its role word, a model by an id that canonical resolves to the
// canonical id a candidate is looked up by, and carries a mean and a count.
// A cell whose role names no seat, or whose count is below minInstalls, is
// ignored, and so is a count of zero or less. A nil canonical leaves the id
// as it stands. Two cells that name one seat and one model — a graded source
// and a seeded one, or two spellings one canonical resolves together — fold
// into one rating, their means weighted by their counts and the counts added,
// in whatever order they arrive. When no cell survives the prior is nil,
// which is the same as no prior at all.
//
// The floor here keeps counting rows rather than the installs a measurement
// document carries beside them: the reader applies the document's floor
// before these cells arrive — on installs where a cell spells them — and
// rows cannot be fewer than the installs that produced them, so a cell that
// floor kept meets this one too. The cells built outside a document carry
// rows alone, so an installs field on Cell would sit unset on every caller,
// and there is none.
func PriorFromCells(cells []Cell, minInstalls int, canonical func(string) string) Prior {
	var prior Prior
	for _, c := range cells {
		seat, ok := seatWords[strings.ToLower(strings.TrimSpace(c.Role))]
		if !ok || c.N < minInstalls || c.N <= 0 {
			continue
		}
		model := c.Model
		if canonical != nil {
			model = canonical(model)
		}
		if prior == nil {
			prior = Prior{}
		}
		if prior[seat] == nil {
			prior[seat] = map[string]Rating{}
		}
		if got, ok := prior[seat][model]; ok {
			n := got.N + c.N
			prior[seat][model] = Rating{Mean: (float64(got.N)*got.Mean + float64(c.N)*c.Mean) / float64(n), N: n}
			continue
		}
		prior[seat][model] = Rating{Mean: c.Mean, N: c.N}
	}
	return prior
}

// MergePriors folds two priors into one. Seat by seat, a rating both hold for
// a model is combined by observation count — the two means weighted by the
// counts behind them, the counts added — and a rating either holds alone is
// carried whole. A nil prior on either side is the other, and two nils are
// nil. The priors given are read and never changed, so the answer is never
// one of them.
func MergePriors(a, b Prior) Prior {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	merged := make(Prior)
	for seat, ratings := range a {
		for model, r := range ratings {
			if merged[seat] == nil {
				merged[seat] = map[string]Rating{}
			}
			merged[seat][model] = r
		}
	}
	for seat, ratings := range b {
		for model, r := range ratings {
			got, ok := merged[seat][model]
			if !ok {
				if merged[seat] == nil {
					merged[seat] = map[string]Rating{}
				}
				merged[seat][model] = r
				continue
			}
			n := got.N + r.N
			if n <= 0 {
				merged[seat][model] = Rating{}
				continue
			}
			merged[seat][model] = Rating{
				Mean: (float64(got.N)*got.Mean + float64(r.N)*r.Mean) / float64(n),
				N:    n,
			}
		}
	}
	return merged
}

// SeatCost is the seat's expected cost of a candidate, in dollars per 1M
// input-equivalent tokens: the prompt price blended with the cache-read
// price by the seat's cache share, plus the completion price spread over the
// seat's input-to-output ratio. A candidate that publishes no cache-read
// price pays the prompt price on the cache share too.
func SeatCost(c Candidate, shape SeatShape) float64 {
	cache := c.PromptPrice
	if c.HasCacheRead {
		cache = c.CacheReadPrice
	}
	return (1-shape.CacheShare)*c.PromptPrice + shape.CacheShare*cache + c.CompletionPrice/shape.InOut
}

// SeatQuality is the candidate's quality for the seat on a 0-100 scale: each
// index counted against the pool's best for that index and weighted by the
// seat's weights. The pool sets the scale, so the same candidate scores
// differently in a stronger pool. A missing index is estimated from the
// pool's donors exactly as in Front. A row that is not a candidate at all,
// and any row against a pool with no candidates to raise a scale, score
// zero.
func SeatQuality(c Candidate, shape SeatShape, pool []Candidate) float64 {
	return SeatQualityWith(c, shape, pool, Worker, nil)
}

// SeatQualityWith is SeatQuality with the pool's prior blended in for the seat
// given: a rating the prior holds for the candidate's canonical id, with a
// positive observation count, moves the seat's quality towards the rating's
// mean by N/(N+PriorWeightAt), and a seat the prior never rates — or rates
// with no observations — keeps the catalog quality unchanged.
func SeatQualityWith(c Candidate, shape SeatShape, pool []Candidate, seat Seat, prior Prior) float64 {
	if !isCandidate(c) {
		return 0
	}
	scale := maxima(pool)
	r := fill(c, readTally(pool), scale)
	q, _ := blendQuality(c.ID, quality(r.idx, shape, scale), seat, prior)
	return q
}

// Front returns the pareto front of (bill, quality) over every crew the
// candidates can field in the family, sorted by bill: a crew on it is beaten
// by no other crew on both, and every crew off it is beaten by one on it.
//
// shapes must carry an entry for every seat. Per seat the running is
// narrowed twice before crews are enumerated — family and the seat's needs
// first, then Floor against that seat's best in the family — and a crew is
// refused while its high seat comes from the worker's own vendor, so the two
// seats that see the same work never share one shop. Ties in (bill, quality)
// break to the crew scored on fewer estimated indexes, and ties past that by
// id order — worker, then high, then mastermind — which is what makes the
// front a property of the candidates rather than of their order.
func Front(candidates []Candidate, shapes map[Seat]SeatShape, fam Family) []Crew {
	return FrontWith(candidates, shapes, fam, nil)
}

// FrontWith is Front with the pool's prior blended into each seat's quality: a
// rating the prior holds for a seat's model, with a positive observation
// count, moves that seat's quality towards the rating's mean by
// N/(N+PriorWeightAt), the seat's measured flag says so, and the floor and the
// crew's quality read the blended value. A nil prior leaves every seat on the
// catalog quality, exactly as Front does. The front is a property of the
// candidates and the prior, never of the order they arrive in.
func FrontWith(candidates []Candidate, shapes map[Seat]SeatShape, fam Family, prior Prior) []Crew {
	scale := maxima(candidates)
	t := readTally(candidates)
	readings := make([]reading, 0, len(candidates))
	for _, c := range candidates {
		readings = append(readings, fill(c, t, scale))
	}
	workers := shortlist(readings, shapes[Worker], fam, scale, Worker, prior)
	highs := shortlist(readings, shapes[High], fam, scale, High, prior)
	minds := shortlist(readings, shapes[Mastermind], fam, scale, Mastermind, prior)
	vw := shapes[Worker].Volume
	vh := shapes[High].Volume
	vm := shapes[Mastermind].Volume

	crews := make([]Crew, 0, len(workers)*len(highs)*len(minds))
	for _, w := range workers {
		for _, h := range highs {
			if vendor(w.c.ID) == vendor(h.c.ID) {
				continue
			}
			for _, m := range minds {
				crews = append(crews, Crew{
					Worker:     w.c.ID,
					High:       h.c.ID,
					Mastermind: m.c.ID,
					Bill:       vw*w.cost + vh*h.cost + vm*m.cost,
					Quality:    (w.quality + h.quality + m.quality) / 3,
					Estimated:  [3][3]bool{w.est, h.est, m.est},
					Measured:   [3]bool{w.measured, h.measured, m.measured},
				})
			}
		}
	}

	// The front: cheapest bill first, best quality first within a bill, and
	// a crew survives when its quality beats everything cheaper so far.
	sort.SliceStable(crews, func(i, j int) bool {
		if crews[i].Bill != crews[j].Bill {
			return crews[i].Bill < crews[j].Bill
		}
		if crews[i].Quality != crews[j].Quality {
			return crews[i].Quality > crews[j].Quality
		}
		return estimateCount(crews[i]) < estimateCount(crews[j])
	})
	var front []Crew
	best := math.Inf(-1)
	for _, crew := range crews {
		if crew.Quality > best+1e-9 {
			front = append(front, crew)
			best = crew.Quality
		}
	}
	return front
}

// Two of the three named presets are stakes, in dollars — what one failed
// task costs the person. The words are the paper's (docs/design/model-pool/
// pareto-crewing.tex, product semantics): frugal is a failed task that is an
// annoyance, balanced one that costs about ten minutes of a person. Max is
// not a stake: it is the best crew on the front whatever it bills, the one
// word that stays a superlative, because a person who says max has said the
// bill is not the point. A custom stake is any number.
const (
	StakeFrugal   = 1.0
	StakeBalanced = 10.0
)

// TypicalTaskTokens is the token count a crew's bill is read over when a
// caller has no better figure for the task in hand: the median of the 133
// task-door runs on the bench with a per-seat token split, taken 2026-09-17
// (quartiles 2.3M and 11.4M). Bill is dollars per 1M task tokens, so this is
// what turns it into dollars per task, which is the unit a stake is in.
const TypicalTaskTokens = 4_400_000

// TaskBill is the crew's expected bill for one task of the tokens given, in
// dollars: Bill is per 1M task tokens.
func TaskBill(c Crew, tokens float64) float64 {
	return c.Bill * tokens / 1e6
}

// Loss is the crew's expected loss at a stake for one task of the tokens
// given: the task's bill plus the stake times the chance the task is not
// accepted, read as one minus the crew's quality on its 0-1 scale. Quality
// is on the acceptance scale by construction — a measured rating is the share
// of graded tasks accepted, times 100, and the catalog's index blend is the
// prior for it — so the two terms are in the same dollars.
func Loss(c Crew, stake, tokens float64) float64 {
	return TaskBill(c, tokens) + stake*(1-c.Quality/100)
}

// AtStake reads one pick off the front at a stake: the crew with the least
// loss for a task of the tokens given, and the cheaper one when two tie. A
// stake of zero is the cheapest crew and a stake past every bill is the best
// one; between them, as the stake rises the pick walks up the front and never
// back down, because a crew that was worth more at a lower stake is worth at
// least that at a higher one. An empty front picks nothing.
func AtStake(front []Crew, stake, tokens float64) Crew {
	if len(front) == 0 {
		return Crew{}
	}
	pick := front[0]
	best := Loss(pick, stake, tokens)
	for _, crew := range front[1:] {
		if loss := Loss(crew, stake, tokens); loss < best-1e-12 {
			pick, best = crew, loss
		}
	}
	return pick
}

// Presets reads the three named picks off a front: frugal at StakeFrugal and
// balanced at StakeBalanced, each for a task of TypicalTaskTokens, and max the
// best crew on the front, which is its last because the front is sorted by
// bill with quality strictly rising. A front of one crew is all three; an
// empty front is none.
func Presets(front []Crew) (frugal, balanced, max Crew) {
	if len(front) == 0 {
		return Crew{}, Crew{}, Crew{}
	}
	return AtStake(front, StakeFrugal, TypicalTaskTokens),
		AtStake(front, StakeBalanced, TypicalTaskTokens),
		front[len(front)-1]
}

// AtKnob reads one pick off the front at knob k in [0, 1]: the budget runs
// geometrically from the frugal bill to the max bill — lo*(hi/lo)^k — and
// the pick is the best-quality crew whose bill fits it, so k of 0 is frugal
// and k of 1 is max. An empty front picks nothing.
func AtKnob(front []Crew, k float64) Crew {
	if len(front) == 0 {
		return Crew{}
	}
	lo, hi := front[0].Bill, front[len(front)-1].Bill
	budget := lo * math.Pow(hi/lo, k)
	var pick Crew
	for _, crew := range front {
		if crew.Bill <= budget*(1+1e-9) {
			pick = crew
		}
	}
	return pick
}

// A seatPick is a candidate already priced and scored for one seat, on the
// candidate's reading: measured and estimated indexes alike. measured says
// whether the pool's own rating for the seat entered the quality.
type seatPick struct {
	c        Candidate
	idx      [3]float64
	est      [3]bool
	cost     float64
	quality  float64
	measured bool
}

// shortlist returns a seat's running: the candidates that pass family and the
// seat's needs, kept when their quality reaches Floor of the family's best
// for the seat, in id order. The seat's quality is read with the pool's prior
// blended in, and measured marks the picks the prior touched.
func shortlist(readings []reading, shape SeatShape, fam Family, scale [3]float64, seat Seat, prior Prior) []seatPick {
	var running []seatPick
	top := 0.0
	for _, r := range readings {
		if !eligible(r.c, shape, fam) {
			continue
		}
		q, measured := blendQuality(r.c.ID, quality(r.idx, shape, scale), seat, prior)
		pick := seatPick{c: r.c, idx: r.idx, est: r.est, cost: SeatCost(r.c, shape), quality: q, measured: measured}
		running = append(running, pick)
		top = math.Max(top, pick.quality)
	}
	var kept []seatPick
	for _, pick := range running {
		if pick.quality >= Floor*top {
			kept = append(kept, pick)
		}
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].c.ID < kept[j].c.ID })
	return kept
}

// quality is SeatQuality against pre-read maxima, on a candidate's reading.
func quality(idx [3]float64, shape SeatShape, scale [3]float64) float64 {
	if scale[Intelligence] <= 0 || scale[Coding] <= 0 || scale[Agentic] <= 0 {
		return 0
	}
	w := shape.Weights
	return 100 * (w[Intelligence]*idx[Intelligence]/scale[Intelligence] +
		w[Coding]*idx[Coding]/scale[Coding] +
		w[Agentic]*idx[Agentic]/scale[Agentic])
}

// blendQuality blends the pool's prior for one seat into the catalog quality:
// a rating the prior holds for the model on the seat, with a positive
// observation count, carries weight N/(N+PriorWeightAt) against the catalog's
// own quality, and the second result reports whether the rating entered. A
// seat the prior never rates, or rates with no observations, keeps the
// catalog quality and reads measured false.
func blendQuality(model string, catalog float64, seat Seat, prior Prior) (float64, bool) {
	if prior == nil {
		return catalog, false
	}
	rating, ok := prior[seat][model]
	if !ok || rating.N <= 0 {
		return catalog, false
	}
	w := float64(rating.N) / float64(rating.N+PriorWeightAt)
	return (1-w)*catalog + w*rating.Mean, true
}

// maxima reads the pool's largest measured value of each index — the scale
// quality is measured against and the ceiling an estimate never crosses.
// Only real candidates raise a ceiling, and a row that publishes only some
// of its indexes raises only those.
func maxima(pool []Candidate) (scale [3]float64) {
	for _, c := range pool {
		if !isCandidate(c) {
			continue
		}
		v := measured(c)
		for i, val := range v {
			scale[i] = math.Max(scale[i], val)
		}
	}
	return scale
}

// measured is the candidate's three indexes as it published them: an
// unpublished index reads as zero.
func measured(c Candidate) [3]float64 {
	return [3]float64{c.Intelligence, c.Coding, c.Agentic}
}

// A tally is one call's donor statistics: for every ordered pair of indexes,
// how many candidates — the donors — carry both measured, and the median of
// the first over the second among them. A missing index is estimated from
// these.
type tally struct {
	count  [3][3]int
	median [3][3]float64
}

// readTally reads the pool's tally; the donors of a pair of indexes are the
// candidates carrying both measured.
func readTally(pool []Candidate) tally {
	var ratios [3][3][]float64
	for _, c := range pool {
		if !isCandidate(c) {
			continue
		}
		v := measured(c)
		for _, x := range [3]Index{Intelligence, Coding, Agentic} {
			for _, y := range [3]Index{Intelligence, Coding, Agentic} {
				if x != y && v[x] > 0 && v[y] > 0 {
					ratios[x][y] = append(ratios[x][y], v[x]/v[y])
				}
			}
		}
	}
	var t tally
	for _, x := range [3]Index{Intelligence, Coding, Agentic} {
		for _, y := range [3]Index{Intelligence, Coding, Agentic} {
			t.count[x][y] = len(ratios[x][y])
			if len(ratios[x][y]) >= 3 {
				t.median[x][y] = medianOf(ratios[x][y])
			}
		}
	}
	return t
}

// medianOf is the middle of the values in order, or the mean of the two
// middles when there is no single middle.
func medianOf(values []float64) float64 {
	slices.Sort(values)
	n := len(values)
	if n%2 == 1 {
		return values[n/2]
	}
	return (values[n/2-1] + values[n/2]) / 2
}

// A reading is a candidate's three indexes ready for scoring — the measured
// ones as published, the missing ones estimated from the call's donors —
// with est remembering which of the three were estimated rather than
// measured.
type reading struct {
	c   Candidate
	idx [3]float64
	est [3]bool
}

// fill reads the candidate's three indexes for scoring. A measured index is
// taken as published; a missing one is estimated from the ones present: each
// present index Y offers Y times the tally's median of the missing index over
// Y among the donors carrying both — or, when fewer than three donors carry
// the pair, equal standing, the missing index read where its present one
// stands, X over the pool's largest X equal to Y over the pool's largest Y —
// and the mean of the offers is kept, never above the pool's largest measured
// value of the missing index. A row that is no candidate at all fills to
// zeros.
func fill(c Candidate, t tally, scale [3]float64) reading {
	var r reading
	if !isCandidate(c) {
		return r
	}
	r.c = c
	v := measured(c)
	r.idx = v
	for _, x := range [3]Index{Intelligence, Coding, Agentic} {
		if v[x] > 0 {
			continue
		}
		var offers []float64
		for _, y := range [3]Index{Intelligence, Coding, Agentic} {
			if y == x || v[y] <= 0 {
				continue
			}
			if t.count[x][y] >= 3 {
				offers = append(offers, v[y]*t.median[x][y])
			} else {
				offers = append(offers, v[y]/scale[y]*scale[x])
			}
		}
		sum := 0.0
		for _, offer := range offers {
			sum += offer
		}
		r.idx[x] = math.Min(sum/float64(len(offers)), scale[x])
		r.est[x] = true
	}
	return r
}

// estimateCount reads how many of a crew's picks were scored on estimated
// indexes — the tiebreak between crews that tie on bill and quality.
func estimateCount(c Crew) int {
	n := 0
	for _, seat := range c.Estimated {
		for _, est := range seat {
			if est {
				n++
			}
		}
	}
	return n
}

// isCandidate reports whether a row is a candidate at all: at least one of
// the three indexes present and a prompt price above zero. Everything
// downstream — eligibility, the floor, the maxima — starts from this.
func isCandidate(c Candidate) bool {
	return (c.Intelligence > 0 || c.Coding > 0 || c.Agentic > 0) && c.PromptPrice > 0
}

// eligible reports whether a candidate may sit the seat: family first, then
// what the seat needs the model to take and call, then the window it needs.
func eligible(c Candidate, shape SeatShape, fam Family) bool {
	if !isCandidate(c) {
		return false
	}
	if fam == Open && !c.Open {
		return false
	}
	if shape.NeedTools && !c.Tools {
		return false
	}
	if shape.NeedImages && !c.Images {
		return false
	}
	return c.Context >= shape.MinContext
}

// vendor is the id text before the first slash, or the whole id when there is
// no slash.
func vendor(id string) string {
	if i := strings.Index(id, "/"); i >= 0 {
		return id[:i]
	}
	return id
}
