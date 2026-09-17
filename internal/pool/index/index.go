// Package index reads a schema-versioned JSON document of per-model
// measurements into a value every reader in a process can share.
//
// THE INDEX IS READ-ONLY AND IMMUTABLE, which is the whole design: nothing
// merges into it and nothing mutates it after Parse, so one document may be
// handed to every goroutine without a lock. Whoever read the file stays outside
// the package — Parse takes bytes, Read takes a reader, and Fallback chooses
// between two documents that both arrive as bytes.
//
// FORWARD COMPATIBILITY IS THE POINT. An unknown top-level field, an unknown
// metric kind, an undeclared dim on a cell and an unknown cell field are all
// ignored, because a reader that refused them would make every future field a
// flag day. The one thing refused outright is a schema this reader does not
// understand, which is what the schema number exists to say.
package index

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// ErrSchema is wrapped by the error a document whose schema is newer than
// this reader returns. A reader that is behind the document cannot guess what
// the newer fields mean, so it refuses rather than half-reads.
var ErrSchema = errors.New("document schema is newer than this reader")

// Cell is one measurement from the document. Dims carries the declared dims
// beyond role and model that the cell spelled, and is nil for a cell that
// carried none.
type Cell struct {
	Metric string
	Role   string
	Model  string
	Mean   float64
	SD     float64
	N      int
	Dims   map[string]string
}

// Want is one row of the document's wanted list, with the role normalised and
// the model resolved to its canonical id.
type Want struct {
	Role   string
	Model  string
	Weight float64
}

// Index is a parsed measurement document. Nothing on it mutates after it is
// built, and every accessor hands back copies, so many goroutines may read one
// Index at once.
type Index struct {
	schema      int
	generated   time.Time
	minInstalls int
	judges      []string
	rubrics     map[string]int
	// metrics holds the declared names as spelled; kinds and dims are keyed by
	// the folded name every lookup arrives under.
	metrics map[string]string
	kinds   map[string]string
	dims    map[string]map[string]bool
	// aliases maps a folded, ~-stripped id to the canonical id as the document
	// spells it. The canonical id is in the map under its own folded form.
	aliases map[string]string
	// cells is keyed by folded metric, then by an address that folds the role,
	// the canonical model and the normalised dims into one exact key.
	cells  map[string]map[string]Cell
	wanted []Want
}

// Parse reads a measurement document from bytes.
func Parse(data []byte) (*Index, error) {
	return parse(data)
}

// Read reads a measurement document from a reader. The reader is drained and
// closed by nobody but the caller; the document is all this package takes.
func Read(r io.Reader) (*Index, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read the index document: %w", err)
	}
	return parse(data)
}

// Fallback answers with the document that should be trusted: primary, unless
// it is missing, empty, unparsable, or older by its generated date, in which
// case the embedded one. Equal dates keep primary. When neither parses it
// returns the error from the embedded document.
func Fallback(primary, embedded []byte) (*Index, error) {
	p, perr := parse(primary)
	e, eerr := parse(embedded)
	if perr == nil {
		if eerr != nil || !p.generated.Before(e.generated) {
			return p, nil
		}
		return e, nil
	}
	if eerr != nil {
		return nil, eerr
	}
	return e, nil
}

// document is the wire shape. Fields this build does not know are dropped by
// the decoder, which is the forward-compatibility rule above.
type document struct {
	Schema      *int                  `json:"schema"`
	Generated   string                `json:"generated"`
	MinInstalls int                   `json:"min_installs"`
	Judges      []string              `json:"judges"`
	Rubrics     map[string]int        `json:"rubrics"`
	Aliases     map[string][]string   `json:"aliases"`
	Metrics     map[string]metricDecl `json:"metrics"`
	Cells       []map[string]any      `json:"cells"`
	Wanted      []wantEntry           `json:"wanted"`
}

type metricDecl struct {
	Kind string   `json:"kind"`
	Dims []string `json:"dims"`
}

type wantEntry struct {
	Role   string  `json:"role"`
	Model  string  `json:"model"`
	Weight float64 `json:"weight"`
}

// reservedCellFields are the fixed fields of a cell. A declared dim named one
// of these could never be read off a cell, so the name is reserved rather than
// resolved twice.
var reservedCellFields = map[string]bool{
	"metric": true, "role": true, "model": true, "mean": true, "sd": true, "n": true,
}

// The separators are unprintable so no dim key or value can contain one and
// forge a different address.
const (
	pairSep = "\x1f"
	keySep  = "\x1e"
)

func parse(data []byte) (*Index, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("index: empty document")
	}
	var d document
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("index: decode: %w", err)
	}
	if d.Schema != nil && *d.Schema > 1 {
		return nil, fmt.Errorf("index: schema %d is newer than this reader, which understands 1: %w", *d.Schema, ErrSchema)
	}

	x := &Index{
		schema:      0,
		minInstalls: d.MinInstalls,
		judges:      d.Judges,
		rubrics:     d.Rubrics,
		metrics:     map[string]string{},
		kinds:       map[string]string{},
		dims:        map[string]map[string]bool{},
		aliases:     map[string]string{},
		cells:       map[string]map[string]Cell{},
	}
	if d.Schema != nil {
		x.schema = *d.Schema
	}
	if stamp, err := time.Parse("2006-01-02", d.Generated); err == nil {
		x.generated = stamp.UTC()
	}

	// The declared metrics are keyed by their folded name for lookup, and
	// their kinds are folded once here rather than on every Kind call. The
	// names are walked in sorted order for the same reason the aliases below
	// are: two spellings that fold to one name are a first-claim-wins race,
	// and map order would settle it differently on every run.
	metricNames := make([]string, 0, len(d.Metrics))
	for name := range d.Metrics {
		metricNames = append(metricNames, name)
	}
	sort.Strings(metricNames)
	for _, name := range metricNames {
		decl := d.Metrics[name]
		folded := fold(name)
		if _, seen := x.metrics[folded]; !seen {
			x.metrics[folded] = name
		}
		if _, seen := x.kinds[folded]; !seen {
			x.kinds[folded] = fold(decl.Kind)
		}
		dimset := map[string]bool{}
		for _, dim := range decl.Dims {
			fd := fold(dim)
			if fd == "role" || fd == "model" {
				continue
			}
			dimset[fd] = true
		}
		if _, seen := x.dims[folded]; !seen {
			x.dims[folded] = dimset
		}
	}

	// Aliases are resolved in sorted canonical order so a name claimed by two
	// canonical ids always resolves the same way.
	ids := make([]string, 0, len(d.Aliases))
	for id := range d.Aliases {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		x.addAlias(id, id)
		for _, alt := range d.Aliases[id] {
			x.addAlias(alt, id)
		}
	}

	for _, raw := range d.Cells {
		x.addCell(raw)
	}

	for _, w := range d.Wanted {
		x.wanted = append(x.wanted, Want{Role: fold(w.Role), Model: x.resolve(w.Model), Weight: w.Weight})
	}
	sort.SliceStable(x.wanted, func(i, j int) bool {
		if x.wanted[i].Weight != x.wanted[j].Weight {
			return x.wanted[i].Weight > x.wanted[j].Weight
		}
		return x.wanted[i].Model < x.wanted[j].Model
	})
	return x, nil
}

// addAlias records that id is an alternate spelling of canonical, unless the
// id already resolves — the first claim in sorted order wins.
func (x *Index) addAlias(id, canonical string) {
	key := normModel(id)
	if _, seen := x.aliases[key]; !seen {
		x.aliases[key] = canonical
	}
}

// addCell reads one raw cell into the index, or drops it. A cell whose metric
// is not declared is skipped — a measurement with no metric behind it has no
// units, and guessing is worse than dropping it. A cell whose N is below the
// document's min_installs is skipped too: too few observations is not a
// measurement.
func (x *Index) addCell(raw map[string]any) {
	metricName, _ := raw["metric"].(string)
	mk := fold(metricName)
	if _, declared := x.metrics[mk]; !declared {
		return
	}
	role, _ := raw["role"].(string)
	model, _ := raw["model"].(string)
	n := int(number(raw["n"]))
	if n < x.minInstalls {
		return
	}
	dims := map[string]string{}
	for k, v := range raw {
		if reservedCellFields[fold(k)] {
			continue
		}
		if !x.dims[mk][fold(k)] {
			continue
		}
		if s, ok := v.(string); ok {
			dims[k] = s
		}
	}
	canon := x.resolve(model)
	c := Cell{
		Metric: metricName,
		Role:   role,
		Model:  canon,
		Mean:   number(raw["mean"]),
		SD:     number(raw["sd"]),
		N:      n,
	}
	if len(dims) > 0 {
		c.Dims = dims
	}
	bucket := x.cells[mk]
	if bucket == nil {
		bucket = map[string]Cell{}
		x.cells[mk] = bucket
	}
	bucket[address(role, canon, c.Dims)] = c
}

// address renders the exact place a measurement lives: folded role, folded
// canonical model, and the normalised dims as sorted key-value pairs. A nil
// dims map and an empty one render the same, which is the rule — they are the
// same address.
func address(role, canonicalModel string, dims map[string]string) string {
	parts := []string{fold(role), fold(canonicalModel)}
	if len(dims) > 0 {
		pairs := make([]string, 0, len(dims))
		for k, v := range dims {
			pairs = append(pairs, fold(k)+keySep+fold(v))
		}
		sort.Strings(pairs)
		parts = append(parts, strings.Join(pairs, pairSep))
	}
	return strings.Join(parts, pairSep)
}

// resolve answers with the canonical id a model id is an alternate of, or the
// normalised id itself when the document never names it.
func (x *Index) resolve(model string) string {
	key := normModel(model)
	if canon, ok := x.aliases[key]; ok {
		return canon
	}
	return key
}

// fold is how metric names, roles, dim keys and dim values are matched.
func fold(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// normModel folds a model id and then strips one leading "~".
func normModel(s string) string {
	return strings.TrimPrefix(fold(s), "~")
}

// number reads a JSON number, and zero for anything else on the cell — a
// missing field is not a malformed document.
func number(v any) float64 {
	f, _ := v.(float64)
	return f
}

// Schema is the document's schema version. A missing schema reads as 0 and is
// accepted.
func (x *Index) Schema() int { return x.schema }

// Generated is the document's "generated" day in UTC. A missing or unreadable
// date is the zero time, which sorts as the oldest thing there is.
func (x *Index) Generated() time.Time { return x.generated }

// MinInstalls is the document's floor on observations per measurement.
func (x *Index) MinInstalls() int { return x.minInstalls }

// Judges answers with a copy of the document's judges.
func (x *Index) Judges() []string {
	if x.judges == nil {
		return nil
	}
	return append([]string(nil), x.judges...)
}

// Rubrics answers with a copy of the document's rubrics.
func (x *Index) Rubrics() map[string]int {
	out := make(map[string]int, len(x.rubrics))
	for k, v := range x.rubrics {
		out[k] = v
	}
	return out
}

// Metrics lists the declared metric names, sorted.
func (x *Index) Metrics() []string {
	out := make([]string, 0, len(x.metrics))
	for _, name := range x.metrics {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Kind answers with the kind word the document spells for the metric,
// lowercased, whether or not this build knows it.
func (x *Index) Kind(metric string) (string, bool) {
	kind, ok := x.kinds[fold(metric)]
	return kind, ok
}

// Canonical answers with the canonical id the document spells for a model id,
// for the id itself and for any of its alternates, and with the normalised
// input for an id the document never names.
func (x *Index) Canonical(model string) string {
	return x.resolve(model)
}

// Cell addresses a measurement exactly: metric, role, model and dims all have
// to match, and a nil dims map and an empty one are the same address. A lookup
// with no dims finds the cell that carries none, and never a cell that carries
// some.
func (x *Index) Cell(metric, role, model string, dims map[string]string) (Cell, bool) {
	bucket := x.cells[fold(metric)]
	if bucket == nil {
		return Cell{}, false
	}
	c, ok := bucket[address(role, x.resolve(model), dims)]
	if !ok {
		return Cell{}, false
	}
	return copyCell(c), true
}

// Cells answers with every returnable cell of one metric, sorted by role, then
// model, then by its dims rendered as sorted "key=value" pairs.
func (x *Index) Cells(metric string) []Cell {
	bucket := x.cells[fold(metric)]
	out := make([]Cell, 0, len(bucket))
	for _, c := range bucket {
		out = append(out, copyCell(c))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Role != out[j].Role {
			return out[i].Role < out[j].Role
		}
		if out[i].Model != out[j].Model {
			return out[i].Model < out[j].Model
		}
		return dimsKey(out[i].Dims) < dimsKey(out[j].Dims)
	})
	return out
}

// Wanted answers with a copy of the wanted list, sorted by weight descending,
// then by model ascending.
func (x *Index) Wanted() []Want {
	return append([]Want(nil), x.wanted...)
}

// dimsKey renders a cell's dims for sorting: sorted "key=value" pairs.
func dimsKey(dims map[string]string) string {
	pairs := make([]string, 0, len(dims))
	for k, v := range dims {
		pairs = append(pairs, k+"="+v)
	}
	sort.Strings(pairs)
	return strings.Join(pairs, ",")
}

// copyCell hands back a cell whose Dims map is its own, so a caller cannot
// reach into the shared document.
func copyCell(c Cell) Cell {
	if c.Dims == nil {
		return c
	}
	dims := make(map[string]string, len(c.Dims))
	for k, v := range c.Dims {
		dims[k] = v
	}
	c.Dims = dims
	return c
}
