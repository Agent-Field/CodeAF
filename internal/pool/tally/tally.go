// Package tally keeps additive sufficient statistics for observations so
// that two sheets recorded on different machines can be merged by addition,
// in any order, and give the same answer as one sheet that saw everything.
// It is standard library only and touches no network, no disk and no clock.
// The only strings a sheet holds are the metric, role, model and dim labels
// an observation carries.
package tally

import (
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// maxNameLen bounds every label a sheet stores.
const maxNameLen = 128

// validName reports whether s may be stored: at most maxNameLen bytes, no
// newline, and no path separator — except that a model id may carry the
// single "/" between vendor and name.
func validName(s string, model bool) bool {
	if len(s) > maxNameLen {
		return false
	}
	slashes := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\n', '\\':
			return false
		case '/':
			if !model {
				return false
			}
			slashes++
			if slashes > 1 {
				return false
			}
		}
	}
	return true
}

// lp prefixes s with its byte length, so an address key built by
// concatenating labels is unambiguous whatever bytes the labels hold.
func lp(s string) string {
	return strconv.Itoa(len(s)) + ":" + s
}

// dimsKey canonicalises the dim labels of an address: keys sorted, every
// key and value length-prefixed. Nil and an empty map give the same key,
// and the order the keys were inserted in never matters.
func dimsKey(dims map[string]string) string {
	keys := slices.Sorted(maps.Keys(dims))
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(lp(k))
		b.WriteString(lp(dims[k]))
	}
	return b.String()
}

// cellAddr is the full address of a cell, kept so the sheet can name it
// again in a document.
type cellAddr struct {
	metric, role, model string
	dims                map[string]string
}

// key is the unambiguous form of the address.
func (a cellAddr) key() string {
	return lp(a.metric) + lp(a.role) + lp(a.model) + dimsKey(a.dims)
}

type cellEntry struct {
	addr cellAddr
	cell Cell
}

// winKey addresses one direction of a paired comparison.
type winKey struct {
	role, winner, loser string
}

// Sheet keeps additive sufficient statistics: one cell per address of
// metric, role, model and dims, and paired-comparison tallies per role.
// A Sheet is safe for concurrent use by multiple goroutines, and the zero
// value is an empty sheet ready to use.
type Sheet struct {
	mu    sync.Mutex
	cells map[string]cellEntry
	wins  map[winKey]int64
}

// New returns an empty sheet.
func New() *Sheet {
	return &Sheet{}
}

// copyDims returns a private copy of dims, nil for nil or empty.
func copyDims(dims map[string]string) map[string]string {
	if len(dims) == 0 {
		return nil
	}
	out := make(map[string]string, len(dims))
	for k, v := range dims {
		out[k] = v
	}
	return out
}

// Observe records one observation x of metric for role and model, under
// the optional dim labels (for example quant=fp8). Nil dims and an empty
// map are the same address, and the order the labels were inserted in
// never matters. An observation that is NaN or infinite is ignored, and so
// is the whole call when any label is longer than 128 bytes or contains a
// newline or a path separator other than the single "/" a model id carries
// between vendor and name.
func (s *Sheet) Observe(metric, role, model string, dims map[string]string, x float64) {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return
	}
	if !validName(metric, false) || !validName(role, false) || !validName(model, true) {
		return
	}
	for k, v := range dims {
		if !validName(k, false) || !validName(v, false) {
			return
		}
	}
	a := cellAddr{metric: metric, role: role, model: model, dims: copyDims(dims)}
	k := a.key()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cells == nil {
		s.cells = make(map[string]cellEntry)
	}
	e, ok := s.cells[k]
	if !ok {
		e.addr = a
	}
	e.cell.N++
	e.cell.Sum += x
	e.cell.SumSq += x * x
	s.cells[k] = e
}

// Cell returns the cell recorded for the address, and whether any
// observation was recorded for it. Nil dims and an empty map are the same
// address, and the order the labels were inserted in never matters.
func (s *Sheet) Cell(metric, role, model string, dims map[string]string) (Cell, bool) {
	k := cellAddr{metric: metric, role: role, model: model, dims: dims}.key()
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.cells[k]
	return e.cell, ok
}

// Win records one paired comparison in which winner beat loser for role.
// The call is ignored when a name is empty, when winner and loser are
// equal, or when a name is longer than 128 bytes or contains a newline or
// a path separator other than the single "/" a model id carries between
// vendor and name.
func (s *Sheet) Win(role, winner, loser string) {
	if role == "" || winner == "" || loser == "" || winner == loser {
		return
	}
	if !validName(role, false) || !validName(winner, true) || !validName(loser, true) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.wins == nil {
		s.wins = make(map[winKey]int64)
	}
	s.wins[winKey{role: role, winner: winner, loser: loser}]++
}

// Wins returns the paired comparisons recorded for role in both directions
// between a and b.
func (s *Sheet) Wins(role, a, b string) (aOverB, bOverA int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.wins[winKey{role: role, winner: a, loser: b}],
		s.wins[winKey{role: role, winner: b, loser: a}]
}

// winCount is one snapshot entry of the paired-comparison tallies.
type winCount struct {
	key winKey
	n   int64
}

// Merge adds other into s: afterwards s holds what it held plus what
// other holds, and other is left unmodified. Merging a sheet into itself
// doubles it. A nil sheet on either side is a no-op.
func (s *Sheet) Merge(other *Sheet) {
	if s == nil || other == nil {
		return
	}
	if other == s {
		s.mu.Lock()
		cells, wins := s.snapshotLocked()
		s.applyLocked(cells, wins)
		s.mu.Unlock()
		return
	}
	other.mu.Lock()
	cells, wins := other.snapshotLocked()
	other.mu.Unlock()

	s.mu.Lock()
	s.applyLocked(cells, wins)
	s.mu.Unlock()
}

// snapshotLocked copies the sheet's cells and wins.
func (s *Sheet) snapshotLocked() ([]cellEntry, []winCount) {
	cells := make([]cellEntry, 0, len(s.cells))
	for _, e := range s.cells {
		cells = append(cells, e)
	}
	wins := make([]winCount, 0, len(s.wins))
	for k, n := range s.wins {
		wins = append(wins, winCount{key: k, n: n})
	}
	return cells, wins
}

// applyLocked adds snapshot cells and wins to the sheet.
func (s *Sheet) applyLocked(cells []cellEntry, wins []winCount) {
	if s.cells == nil {
		s.cells = make(map[string]cellEntry, len(cells))
	}
	for _, e := range cells {
		k := e.addr.key()
		cur, ok := s.cells[k]
		if !ok {
			cur = cellEntry{addr: cellAddr{
				metric: e.addr.metric,
				role:   e.addr.role,
				model:  e.addr.model,
				dims:   copyDims(e.addr.dims),
			}}
		}
		cur.cell.N += e.cell.N
		cur.cell.Sum += e.cell.Sum
		cur.cell.SumSq += e.cell.SumSq
		s.cells[k] = cur
	}
	if s.wins == nil {
		s.wins = make(map[winKey]int64, len(wins))
	}
	for _, w := range wins {
		s.wins[w.key] += w.n
	}
}

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

// cellDoc and winDoc are the JSON shapes of one cell and one win; doc is
// the whole document. Fields unknown to this package, here and inside an
// entry, are ignored when reading.
type cellDoc struct {
	Metric string            `json:"metric"`
	Role   string            `json:"role"`
	Model  string            `json:"model"`
	Dims   map[string]string `json:"dims"`
	N      int64             `json:"n"`
	Sum    float64           `json:"sum"`
	SumSq  float64           `json:"sumsq"`
}

type winDoc struct {
	Role   string `json:"role"`
	Winner string `json:"winner"`
	Loser  string `json:"loser"`
	Wins   int64  `json:"wins"`
}

type doc struct {
	Schema int       `json:"schema"`
	Cells  []cellDoc `json:"cells"`
	Wins   []winDoc  `json:"wins"`
}

// schemaVersion is the only document schema this package writes and reads.
const schemaVersion = 1

// MarshalJSON encodes the sheet deterministically: sheets holding the same
// statistics give byte-identical documents, whatever order anything was
// inserted in. Cells are sorted by metric, role, model and dim labels,
// wins by role, winner and loser, dim labels by key. The document carries
// a top-level "schema": 1.
func (s *Sheet) MarshalJSON() ([]byte, error) {
	s.mu.Lock()
	cells, wins := s.snapshotLocked()
	s.mu.Unlock()

	docs := make([]cellDoc, 0, len(cells))
	for _, e := range cells {
		dims := e.addr.dims
		if dims == nil {
			dims = map[string]string{}
		}
		docs = append(docs, cellDoc{
			Metric: e.addr.metric,
			Role:   e.addr.role,
			Model:  e.addr.model,
			Dims:   dims,
			N:      e.cell.N,
			Sum:    e.cell.Sum,
			SumSq:  e.cell.SumSq,
		})
	}
	slices.SortFunc(docs, func(a, b cellDoc) int {
		if c := strings.Compare(a.Metric, b.Metric); c != 0 {
			return c
		}
		if c := strings.Compare(a.Role, b.Role); c != 0 {
			return c
		}
		if c := strings.Compare(a.Model, b.Model); c != 0 {
			return c
		}
		return strings.Compare(dimsKey(a.Dims), dimsKey(b.Dims))
	})
	wdocs := make([]winDoc, 0, len(wins))
	for _, w := range wins {
		wdocs = append(wdocs, winDoc{Role: w.key.role, Winner: w.key.winner, Loser: w.key.loser, Wins: w.n})
	}
	slices.SortFunc(wdocs, func(a, b winDoc) int {
		if c := strings.Compare(a.Role, b.Role); c != 0 {
			return c
		}
		if c := strings.Compare(a.Winner, b.Winner); c != 0 {
			return c
		}
		return strings.Compare(a.Loser, b.Loser)
	})
	return json.Marshal(doc{Schema: schemaVersion, Cells: docs, Wins: wdocs})
}

// UnmarshalJSON replaces the sheet's contents with the document. Fields
// unknown to this package, at the top level and inside an entry, are
// ignored. A document whose schema is greater than 1 is refused with an
// error.
func (s *Sheet) UnmarshalJSON(data []byte) error {
	var d doc
	if err := json.Unmarshal(data, &d); err != nil {
		return err
	}
	if d.Schema > schemaVersion {
		return fmt.Errorf("tally: document schema %d is newer than this build reads", d.Schema)
	}
	cells := make(map[string]cellEntry, len(d.Cells))
	for _, c := range d.Cells {
		a := cellAddr{metric: c.Metric, role: c.Role, model: c.Model, dims: copyDims(c.Dims)}
		k := a.key()
		e := cells[k]
		e.addr = a
		e.cell.N += c.N
		e.cell.Sum += c.Sum
		e.cell.SumSq += c.SumSq
		cells[k] = e
	}
	wins := make(map[winKey]int64, len(d.Wins))
	for _, w := range d.Wins {
		wins[winKey{role: w.Role, winner: w.Winner, loser: w.Loser}] += w.Wins
	}
	s.mu.Lock()
	s.cells = cells
	s.wins = wins
	s.mu.Unlock()
	return nil
}
