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
// front, sorted by bill; Presets and AtKnob read picks off that front.
package crewpick

import (
	"math"
	"sort"
	"strings"
)

// A Candidate is one model up for a seat. Prices are dollars per 1M tokens.
//
// A candidate missing any of the three indexes — the zero value, which is how
// an unpublished index reads — or carrying a zero prompt price is not a
// candidate at all: it never occupies a seat, never clears a floor, and
// raises no ceiling. A model that publishes no cache-read price says so with
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
}

// Floor is the share of a seat's best quality in the family that a candidate
// must reach to stay in the running for that seat. A model that cannot do
// the job is not cheap, whatever it costs.
const Floor = 0.80

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
// differently in a stronger pool. A row that is not a candidate at all, and
// any row against a pool that has no candidates to raise a scale, score
// zero.
func SeatQuality(c Candidate, shape SeatShape, pool []Candidate) float64 {
	if !isCandidate(c) {
		return 0
	}
	maxI, maxC, maxA := maxima(pool)
	return quality(c, shape, maxI, maxC, maxA)
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
// break by id order — worker, then high, then mastermind — which is what
// makes the front a property of the candidates rather than of their order.
func Front(candidates []Candidate, shapes map[Seat]SeatShape, fam Family) []Crew {
	maxI, maxC, maxA := maxima(candidates)
	workers := shortlist(candidates, shapes[Worker], fam, maxI, maxC, maxA)
	highs := shortlist(candidates, shapes[High], fam, maxI, maxC, maxA)
	minds := shortlist(candidates, shapes[Mastermind], fam, maxI, maxC, maxA)
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
		return crews[i].Quality > crews[j].Quality
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

// Presets reads the three named picks off a front: frugal is the cheapest
// crew, max the dearest, and balanced the knee between them — the crew
// farthest above the straight line from one end of the front to the other in
// (ln bill, quality), which is where money stops buying quality in earnest.
// A front of one crew is all three; an empty front is none.
func Presets(front []Crew) (frugal, balanced, max Crew) {
	if len(front) == 0 {
		return Crew{}, Crew{}, Crew{}
	}
	frugal, balanced, max = front[0], knee(front), front[len(front)-1]
	return frugal, balanced, max
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

// knee returns the front's knee: the crew farthest above the straight line
// from the first crew to the last in (ln bill, quality). Ties go to the
// earlier crew, and a front whose two ends share a bill has no line to be
// above — its best crew carries the name.
func knee(front []Crew) Crew {
	x0, y0 := math.Log(front[0].Bill), front[0].Quality
	x1, y1 := math.Log(front[len(front)-1].Bill), front[len(front)-1].Quality
	if x1 == x0 {
		return front[len(front)-1]
	}
	pick := front[0]
	best := math.Inf(-1)
	for _, crew := range front {
		d := (crew.Quality - y0) - (y1-y0)*(math.Log(crew.Bill)-x0)/(x1-x0)
		if d > best {
			pick, best = crew, d
		}
	}
	return pick
}

// A seatPick is a candidate already priced and scored for one seat.
type seatPick struct {
	c       Candidate
	cost    float64
	quality float64
}

// shortlist returns a seat's running: the candidates that pass family and the
// seat's needs, kept when their quality reaches Floor of the family's best
// for the seat, in id order.
func shortlist(candidates []Candidate, shape SeatShape, fam Family, maxI, maxC, maxA float64) []seatPick {
	var running []seatPick
	top := 0.0
	for _, c := range candidates {
		if !eligible(c, shape, fam) {
			continue
		}
		pick := seatPick{c: c, cost: SeatCost(c, shape), quality: quality(c, shape, maxI, maxC, maxA)}
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

// quality is SeatQuality against pre-read maxima.
func quality(c Candidate, shape SeatShape, maxI, maxC, maxA float64) float64 {
	if maxI <= 0 || maxC <= 0 || maxA <= 0 {
		return 0
	}
	w := shape.Weights
	return 100 * (w[0]*c.Intelligence/maxI + w[1]*c.Coding/maxC + w[2]*c.Agentic/maxA)
}

// maxima reads the pool's best value of each index — the scale quality is
// measured against. Only real candidates raise a ceiling.
func maxima(pool []Candidate) (maxI, maxC, maxA float64) {
	for _, c := range pool {
		if !isCandidate(c) {
			continue
		}
		maxI = math.Max(maxI, c.Intelligence)
		maxC = math.Max(maxC, c.Coding)
		maxA = math.Max(maxA, c.Agentic)
	}
	return maxI, maxC, maxA
}

// isCandidate reports whether a row is a candidate at all: all three indexes
// present and a prompt price above zero. Everything downstream —
// eligibility, the floor, the maxima — starts from this.
func isCandidate(c Candidate) bool {
	return c.Intelligence > 0 && c.Coding > 0 && c.Agentic > 0 && c.PromptPrice > 0
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
