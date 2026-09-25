package crewroute

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strings"
	"sync"
)

// WHAT A SEAT IS WORTH, BEFORE THIS INSTALL HAS RUN ANYTHING.
//
// The router needs, for every class of work, every seat and every model it
// might sit there, two numbers: how much that model in that seat adds to the
// work's quality, and what it costs. This file is where both come from, in the
// order they are trusted:
//
//  1. THE EVIDENCE TABLE (prior.json, embedded). Measured crews per class of
//     work (docs/design/model-pool/pareto-crewing.pdf). A crew's quality is
//     read here as the SUM of what its three seats add, split so the table's
//     crews add back up exactly; the cells are prior.json's own.
//
//     The split between the worker and the planner of one crew is a choice
//     the table does not make, and it is made in the worker's favour because
//     the worker carries the work. The checker's share on a fix is the SAME
//     for every model, because the rule is that a narrow fix does not pay for
//     a stronger checker.
//
//  2. THE CATALOG'S OWN FIGURES, for a model nobody measured. Its published
//     intelligence, coding and agentic indexes, weighed the way each seat uses
//     a model, place it above or below the middle of what was measured for
//     that seat — and NEVER AS HIGH AS THE MEASURED MIDDLE, because a model
//     nobody has watched do the work is not believed to do it as well as one
//     somebody has. One that reads far under the worst measured model does
//     not sit the seat at all ([table.credible]), and when it is weighed its
//     cost is never taken as less than the cheapest measured model's
//     ([table.costFloor]): a price of zero is not evidence of anything. A
//     published index is a weaker signal than a measured row, which is why
//     the indexes only move an unmeasured model half as far as the measured
//     spread, and why a measured row always wins a tie.
//
// The cost of a seat is the seat's token shape — how much it reads fresh, how
// much it reads back from a warm cache, how much it writes on an ordinary task
// — priced at the route's published per-token prices. The three shapes are
// fitted so the table's crews cost what prior.json's costs say.

//go:embed prior.json
var priorJSON []byte

// shape is one seat's tokens on an ordinary task.
type shape struct {
	Prompt     float64 `json:"prompt"`
	Cached     float64 `json:"cached"`
	Completion float64 `json:"completion"`
}

// priorModel is a catalog row as the table snapshotted it, so a measured model
// can be priced and scored on a machine whose catalog has not arrived yet.
type priorModel struct {
	ID           string  `json:"id"`
	Open         bool    `json:"open"`
	Prompt       float64 `json:"prompt"`
	Completion   float64 `json:"completion"`
	CacheRead    float64 `json:"cache_read"`
	Intelligence float64 `json:"intelligence"`
	Coding       float64 `json:"coding"`
	Agentic      float64 `json:"agentic"`
	Context      int     `json:"context"`
}

// cell is one measured (class, seat, model) contribution.
type cell struct {
	Class   Class   `json:"class"`
	Seat    Seat    `json:"seat"`
	Model   string  `json:"model"`
	Quality float64 `json:"quality"`
	N       int     `json:"n"`
}

// costCell is one measured seat cost: dollars per task.
type costCell struct {
	Class Class   `json:"class"`
	Seat  Seat    `json:"seat"`
	Model string  `json:"model"`
	USD   float64 `json:"usd"`
	N     int     `json:"n"`
}

// table is prior.json read.
type table struct {
	Measured string           `json:"measured"`
	Source   string           `json:"source"`
	Knee     float64          `json:"knee_per_usd"`
	Shapes   map[Seat]shape   `json:"shapes"`
	Models   []priorModel     `json:"models"`
	Cells    []cell           `json:"cells"`
	measured map[cellKey]cell // lineage-folded, the Other class derived
	spread   map[seatKey]spread
	floors   map[seatKey]float64
	byID     map[string]priorModel
	// Costs are what a seat measurably cost per task, by class and model;
	// costs is them lineage-folded with Other derived, and costScale each
	// class and seat's measured-to-shape ratio, for a model nobody measured.
	Costs     []costCell `json:"costs"`
	costs     map[cellKey]float64
	costScale map[seatKey]float64
	// rescue is a table asked for a seat's last rungs ([Request.Rescue]).
	rescue bool
}

type cellKey struct {
	class Class
	seat  Seat
	model string
}

type seatKey struct {
	class Class
	seat  Seat
}

// spread is what was measured for one class and seat: the middle, the best,
// and the half-width an unmeasured model may move across.
type spread struct {
	median float64
	best   float64
	worst  float64
	half   float64
}

// The catalog-prior constants, spelled once. An index score of indexRef reads
// as the measured middle, indexScale points moves a model the whole measured
// half-width, and an unmeasured model moves only unseenShrink of that.
// minHalf keeps a seat whose measured models all scored the same (the checker
// on a fix) from treating every unmeasured model as their equal: the worst
// indexes still cost something.
const (
	indexRef     = 55.0
	indexScale   = 10.0
	unseenShrink = 0.5
	minHalf      = 0.5
	// unseenMargin is how far under the measured middle an unmeasured model's
	// ceiling sits, as a share of the seat's half-width. An unmeasured model is
	// believed at most a little WORSE than the middle of what was watched doing
	// the work: on a seat where one model was measured, the middle is that
	// model, and a stranger that tied it would win on price alone — which is
	// how a free variant nobody measured took a worker seat and died in its
	// first second.
	unseenMargin = 0.25
	// unseenFloor is how far under the worst measured model an unmeasured one
	// may read and still sit the seat, as a share of the half-width. Below it
	// the published figures say the model is weaker than anything measured,
	// and a seat is not a place to find out.
	unseenFloor = 1.0
)

var (
	loaded     *table
	loadOnce   sync.Once
	seatWeight = map[Seat][3]float64{
		// intelligence, coding, agentic — how each seat uses a model. The worker
		// holds a long agentic loop, the planner thinks once and writes a plan,
		// the checker reads work against its acceptance and needs all three.
		Worker:  {0.2, 0.3, 0.5},
		Planner: {0.6, 0.0, 0.4},
		Checker: {1.0 / 3, 1.0 / 3, 1.0 / 3},
	}
)

// prior is the table, read once. A table that does not parse is a build that
// must not ship, so it panics at first use the way a malformed embedded asset
// does everywhere else in this tree — and a test reads it on every run.
func prior() *table {
	loadOnce.Do(func() {
		var t table
		if err := json.Unmarshal(priorJSON, &t); err != nil {
			panic("crewroute: prior.json: " + err.Error())
		}
		t.index()
		loaded = &t
	})
	return loaded
}

// index folds the measured cells by lineage, derives the Other class as the
// average of the two measured classes, and reads each seat's spread.
func (t *table) index() {
	t.byID = make(map[string]priorModel, len(t.Models))
	for _, m := range t.Models {
		t.byID[Lineage(m.ID)] = m
	}
	t.measured = make(map[cellKey]cell, len(t.Cells)*2)
	for _, c := range t.Cells {
		c.Model = Lineage(c.Model)
		t.measured[cellKey{c.Class, c.Seat, c.Model}] = c
	}
	t.spread = map[seatKey]spread{}
	t.spreads(Bugfix, OpenEnded)
	// OTHER IS THE AVERAGE OF THE TWO MEASURED CLASSES, per seat and model,
	// because nothing measured work that changes nothing in particular. A
	// model measured in one class only is averaged against the OTHER class's
	// measured middle rather than read as its one number: kimi measured as a
	// fix's worker is not thereby measured as everybody's worker.
	for _, c := range t.Cells {
		key := cellKey{Other, c.Seat, Lineage(c.Model)}
		if _, done := t.measured[key]; done {
			continue
		}
		var sum float64
		var n int
		for _, class := range []Class{Bugfix, OpenEnded} {
			if held, ok := t.measured[cellKey{class, c.Seat, key.model}]; ok {
				sum += held.Quality
				n += held.N
			} else {
				sum += t.spread[seatKey{class, c.Seat}].median
			}
		}
		t.measured[key] = cell{Class: Other, Seat: c.Seat, Model: key.model, Quality: sum / 2, N: n}
	}
	t.spreads(Other)
	t.floors = t.floorsOf()
	t.indexCosts()
}

// indexCosts folds the measured seat costs by lineage, derives Other as the
// mean of the classes that measured a seat, and reads each class and seat's
// scale: the median of measured cost over the flat shape's estimate.
func (t *table) indexCosts() {
	t.costs = map[cellKey]float64{}
	sums, counts := map[cellKey]float64{}, map[cellKey]int{}
	for _, c := range t.Costs {
		key := cellKey{c.Class, c.Seat, Lineage(c.Model)}
		t.costs[key] = c.USD
		other := cellKey{Other, c.Seat, key.model}
		sums[other] += c.USD
		counts[other]++
	}
	for key, sum := range sums {
		t.costs[key] = sum / float64(counts[key])
	}
	ratios := map[seatKey][]float64{}
	for key, usd := range t.costs {
		m, ok := t.byID[key.model]
		if !ok {
			continue
		}
		model := Model{ID: m.ID, PromptPrice: m.Prompt, CompletionPrice: m.Completion, CacheReadPrice: m.CacheRead}
		if flat := t.seatCost(key.seat, model); flat > 0 {
			ratios[seatKey{key.class, key.seat}] = append(ratios[seatKey{key.class, key.seat}], usd/flat)
		}
	}
	t.costScale = map[seatKey]float64{}
	for key, rs := range ratios {
		sort.Float64s(rs)
		t.costScale[key] = rs[len(rs)/2]
	}
}

// estCost is what a seat is expected to cost per task, for the estimate a
// task's line shows: the cost prior.json holds for this model in this seat and
// class where the table has it — a model's own verbosity included — and the
// flat token shape scaled by the class and seat's cost ratio otherwise.
//
// IT IS THE ESTIMATE, NOT THE WEIGHT. Routing weighs [table.seatCost], the
// figure the knee was set against; this is what a person is told to expect.
func (t *table) estCost(class Class, seat Seat, m Model) float64 {
	if usd, ok := t.costs[cellKey{class, seat, Lineage(m.ID)}]; ok {
		return usd
	}
	scale := t.costScale[seatKey{class, seat}]
	if scale <= 0 {
		scale = 1
	}
	return t.seatCost(seat, m) * scale
}

// spreads reads the measured spread of every seat for the classes given.
func (t *table) spreads(classes ...Class) {
	for _, class := range classes {
		for _, seat := range Seats {
			var qs []float64
			for key, c := range t.measured {
				if key.class == class && key.seat == seat {
					qs = append(qs, c.Quality)
				}
			}
			if len(qs) == 0 {
				continue
			}
			sort.Float64s(qs)
			median := qs[len(qs)/2]
			if len(qs)%2 == 0 {
				median = (qs[len(qs)/2-1] + qs[len(qs)/2]) / 2
			}
			half := (qs[len(qs)-1] - qs[0]) / 2
			if half < minHalf {
				half = minHalf
			}
			t.spread[seatKey{class, seat}] = spread{median: median, best: qs[len(qs)-1], worst: qs[0], half: half}
		}
	}
}

// Lineage is the id a model is measured under: its canonical identity across
// every provider's spelling ([CanonicalOf]) — lowercase, a provider's
// namespace and a route suffix (`:free`, `:nitro`) taken off, a thinking
// level, a floating alias's `~` and `-latest`, and a dated snapshot suffix
// taken off. `deepseek/deepseek-v4-flash-0731` is the same lineage as the
// `deepseek/deepseek-v4-flash` the table measured, and so is its free pool:
// a route is not a model. A quantised local copy keeps its variant after `@`,
// so it is never mistaken for the model it was squeezed from.
func Lineage(id string) string { return CanonicalOf(id).String() }

// lineageTail is [Lineage]'s own rules on an id already read by
// [CanonicalOf]: `-latest` and a dated snapshot suffix taken off.
func lineageTail(id string) string {
	id = strings.TrimSuffix(id, "-latest")
	if at := strings.LastIndex(id, "-"); at > 0 {
		if tail := id[at+1:]; (len(tail) == 4 || len(tail) == 8) && allDigits(tail) {
			id = id[:at]
		}
	}
	return id
}

// allDigits is whether a word is made of ASCII digits only.
func allDigits(word string) bool {
	for i := 0; i < len(word); i++ {
		if word[i] < '0' || word[i] > '9' {
			return false
		}
	}
	return word != ""
}

// quality is what one model adds in one seat for one class of work, and
// whether that number was measured.
func (t *table) quality(class Class, seat Seat, m Model) (float64, bool) {
	canon := CanonicalOf(m.ID)
	if c, ok := t.measured[cellKey{class, seat, canon.String()}]; ok {
		return c.Quality, true
	}
	if canon.Variant != "" {
		// A QUANTISED COPY INHERITS ITS MODEL'S EVIDENCE AT A DISCOUNT, and
		// reads as unmeasured: the number is borrowed, not watched.
		if c, ok := t.measured[cellKey{class, seat, canon.ID}]; ok {
			return c.Quality * quantDiscount, false
		}
	}
	sp, ok := t.spread[seatKey{class, seat}]
	if !ok {
		return 0, false
	}
	move := (indexScore(seat, m) - indexRef) / indexScale
	if move > 1 {
		move = 1
	}
	if move < -1 {
		move = -1
	}
	q := sp.median + move*sp.half*unseenShrink
	if ceiling := sp.median - unseenMargin*sp.half; q > ceiling {
		q = ceiling
	}
	return q, false
}

// credible is whether an unmeasured model's reading is high enough to sit a
// seat at all: no lower than a half-width under the worst model measured
// there. A measured model is always credible — its number is evidence.
func (t *table) credible(class Class, seat Seat, q float64, measured bool) bool {
	if measured {
		return true
	}
	sp, ok := t.spread[seatKey{class, seat}]
	if !ok {
		return false
	}
	return q >= sp.worst-unseenFloor*sp.half-1e-9
}

// costFloor is the least an UNMEASURED model is taken to cost in a seat when
// the router weighs it: the cheapest metered cost of a model MEASURED in that
// seat for that class of work.
//
// A PRICE OF ZERO IS NOT EVIDENCE OF VALUE. A free pool, or a model on a route
// this table cannot price, reads as costing nothing, and quality minus λ·0
// beats every measured model that costs a cent. The floor says the stranger is
// at best as cheap as the cheapest model that was actually watched doing this
// work — and a measured model on a subscription or a local route still costs
// what its route costs, because there the zero IS the evidence.
func (t *table) costFloor(class Class, seat Seat) float64 {
	return t.floors[seatKey{class, seat}]
}

// floorsOf reads every class and seat's cost floor once, when the table is
// indexed: a decision asks for them once per candidate per seat.
func (t *table) floorsOf() map[seatKey]float64 {
	floors := map[seatKey]float64{}
	for key := range t.measured {
		pm, ok := t.byID[key.model]
		if !ok {
			continue
		}
		m := Model{PromptPrice: pm.Prompt, CompletionPrice: pm.Completion, CacheReadPrice: pm.CacheRead}
		sk := seatKey{key.class, key.seat}
		if c, held := floors[sk]; !held || t.seatCost(key.seat, m) < c {
			floors[sk] = t.seatCost(key.seat, m)
		}
	}
	return floors
}

// IsMeasured is whether the evidence table measured this model's lineage in
// any seat — the models the router trusts before this install has run any.
func IsMeasured(id string) bool {
	_, ok := prior().byID[Lineage(id)]
	return ok
}

// indexScore reads a model's published indexes the way one seat uses a model.
//
// AN INDEX A ROW DOES NOT PUBLISH COUNTS AGAINST IT. Each missing index is read
// as a full scale below the middle ([indexRef] − [indexScale]) at its seat's
// weight — never renormalised away. Renormalising let a model that published
// one flattering index and nothing else read as though it had published three:
// a free model with only a coding figure outranked models whose agentic figure
// was known and middling, and took a worker seat it could not hold.
func indexScore(seat Seat, m Model) float64 {
	weights := seatWeight[seat]
	values := [3]float64{m.Intelligence, m.Coding, m.Agentic}
	var sum, weight float64
	for i, v := range values {
		if weights[i] == 0 {
			continue
		}
		if v <= 0 {
			v = indexRef - indexScale
		}
		sum += v * weights[i]
		weight += weights[i]
	}
	if weight == 0 {
		return indexRef - indexScale
	}
	return sum / weight
}

// agenticKnown is whether an unmeasured model may be believed in a seat that
// runs an agent loop — the worker and the checker — at all: it must publish
// the agentic index those seats weigh most. The planner writes a plan once and
// is read on what it publishes.
func agenticKnown(seat Seat, m Model) bool {
	return seat == Planner || m.Agentic > 0
}

// evidenceKnown is whether an unmeasured model publishes ANY index the seat
// weighs. A model that publishes none is read at the catalog floor for every
// index, which is still credible — and then wins a seat on price alone the
// moment λ leans cheap: a planner rung went to a model with no index at all
// because it was the cheapest thing with a context window. Nothing measured
// it and nothing published about it, so there is nothing to rank it by, and
// it is not a first pick nor a rung; only a seat's last-rung rescue takes it.
func evidenceKnown(seat Seat, m Model) bool {
	weights := seatWeight[seat]
	for i, v := range [3]float64{m.Intelligence, m.Coding, m.Agentic} {
		if weights[i] > 0 && v > 0 {
			return true
		}
	}
	return false
}

// seatCost is what one model costs in one seat on an ordinary task, at the
// prices it publishes. A route that bills nothing per token (a subscription,
// a local model) is priced by the caller, not here.
func (t *table) seatCost(seat Seat, m Model) float64 {
	s := t.Shapes[seat]
	cached := m.CacheReadPrice
	if cached <= 0 {
		// A provider that publishes no cache-read price is paid the prompt
		// price on what it reads back — the dearer reading, never a free one.
		cached = m.PromptPrice
	}
	return s.Prompt*m.PromptPrice + s.Cached*cached + s.Completion*m.CompletionPrice
}

// Snapshot is a measured model as the table priced and described it, for a
// caller whose catalog has not arrived: the router's measured rows are then
// still candidates rather than nothing. ok is false for a model the table
// does not carry.
func Snapshot(id string) (Model, bool) {
	pm, ok := prior().byID[Lineage(id)]
	if !ok {
		return Model{}, false
	}
	return Model{
		ID: pm.ID, Open: pm.Open, PromptPrice: pm.Prompt, CompletionPrice: pm.Completion,
		CacheReadPrice: pm.CacheRead, Intelligence: pm.Intelligence, Coding: pm.Coding,
		Agentic: pm.Agentic, Context: pm.Context, Tools: true,
	}, true
}

// Measured lists the ids of the models the table measured, sorted, for a
// caller that wants to offer them when nothing else is known.
func Measured() []string {
	t := prior()
	ids := make([]string, 0, len(t.Models))
	for _, m := range t.Models {
		ids = append(ids, m.ID)
	}
	sort.Strings(ids)
	return ids
}

// Knee is the default price of a quality point, in points per dollar — see
// [Route] for how the knee is read.
func Knee() float64 { return prior().Knee }
