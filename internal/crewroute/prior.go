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
//  1. THE EVIDENCE TABLE (prior.json, embedded). Measured: 22 real GitHub
//     issues, fourteen narrow fixes and eight open-ended pieces of work, every
//     crew's result scored blind by two reviewers on a ten-point mergeability
//     scale. A crew's score is read here as the SUM of what its three seats
//     add, split so the measured crews add back up exactly:
//
//     bugfix     all glm-5.3-flash   6.57  = 4.00 worker + 1.00 planner + 1.57 checker
//     bugfix     all kimi-k3         6.86  = 4.20 worker + 1.09 planner + 1.57 checker
//     openended  all glm-5.3-flash   3.60  = 2.00 worker + 0.60 planner + 1.00 checker
//     openended  + kimi-k3 checker   7.60  =                              + 5.00 checker
//     openended  + v4-flash checker  5.70  =                              + 3.10 checker
//
//     The split between the worker and the planner of one crew is a choice the
//     evidence does not make — both moved together — and it is made in the
//     worker's favour because the worker carries the work. The checker's share
//     on a fix is the SAME for both models because the evidence says a strong
//     checker adds nothing to a narrow fix.
//
//  2. THE CATALOG'S OWN FIGURES, for a model nobody measured. Its published
//     intelligence, coding and agentic indexes, weighed the way each seat uses
//     a model, place it above or below the middle of what was measured for
//     that seat — and NEVER ABOVE THE BEST MEASURED MODEL, because a model
//     nobody has watched do the work is not believed to do it better than one
//     somebody has. The published indexes predicted the measured checkers
//     badly (the weakest-indexed model was the second-best checker), which is
//     why they only move an unmeasured model half as far as the measured
//     spread, and why a measured row always wins a tie.
//
// The cost of a seat is the seat's token shape — how much it reads fresh, how
// much it reads back from a warm cache, how much it writes on an ordinary task
// — priced at the route's published per-token prices. The three shapes were
// fitted so the measured crews cost what they cost: all-flash about $0.023 a
// task, all-kimi about $0.35, flash with a kimi checker about $0.117.

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
	byID     map[string]priorModel
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
			t.spread[seatKey{class, seat}] = spread{median: median, best: qs[len(qs)-1], half: half}
		}
	}
}

// Lineage is the id a model is measured under: lowercase, with a thinking
// level, a floating alias's `~` and `-latest`, and a dated snapshot suffix
// taken off. `deepseek/deepseek-v4-flash-0731` is the same lineage as the
// `deepseek/deepseek-v4-flash` the table measured, and a measurement of one is
// read for the other — a dated build is the same weights under a pinned name
// far more often than it is a different model.
//
// It is spelled with string operations rather than a pattern because it runs
// for every candidate in every seat of every decision, and a decision's budget
// is two milliseconds against a catalog of several hundred rows.
func Lineage(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	id = strings.TrimPrefix(id, "~")
	if at := strings.LastIndex(id, ":"); at > 0 {
		switch id[at+1:] {
		case "low", "medium", "high", "minimal", "max":
			id = id[:at]
		}
	}
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
	if c, ok := t.measured[cellKey{class, seat, Lineage(m.ID)}]; ok {
		return c.Quality, true
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
	if q > sp.best {
		q = sp.best
	}
	return q, false
}

// indexScore reads a model's published indexes the way one seat uses a model.
// An index a row does not publish is left out and the weights of the rest are
// renormalised; a row that publishes none reads as a full scale below the
// middle, because a model with no published figure is a model nobody has
// described, which is not a reason to believe it is average.
func indexScore(seat Seat, m Model) float64 {
	weights := seatWeight[seat]
	values := [3]float64{m.Intelligence, m.Coding, m.Agentic}
	var sum, weight float64
	for i, v := range values {
		if v <= 0 || weights[i] == 0 {
			continue
		}
		sum += v * weights[i]
		weight += weights[i]
	}
	if weight == 0 {
		return indexRef - indexScale
	}
	return sum / weight
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
// [Route] for why this is the knee of the measured front.
func Knee() float64 { return prior().Knee }
