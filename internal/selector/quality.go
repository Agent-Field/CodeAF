package selector

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
)

// SharedSnapshot mirrors the shared component snapshot fields the selector reads.
// parameters carry quality/ROLE/MODEL keys: the posterior mean q (0..1);
// uncertainty carries quality/ROLE/MODEL: the posterior variance (sigma^2);
// counts is the observation count N; model_ids is the scored set. The
// parameters also carry quality_w/ROLE/residual_sigma, the residual-derived
// per-role sigma used as the global fallback.
type SharedSnapshot struct {
	SchemaVersion int                `json:"schema_version"`
	Parameters    map[string]float64 `json:"parameters"`
	Uncertainty   map[string]float64 `json:"uncertainty"`
	Counts        map[string]int     `json:"counts"`
	ModelIDs      []string           `json:"model_ids"`
	GeneratedBy   string             `json:"generated_by"`
}

// maxSnapshotBytes is a fuse, not a budget: a fitted snapshot is
// kilobytes. The path comes from profile config and is read on the routing
// hot path, so a refuse-to-load beats a multi-gigabyte pause per route.
const maxSnapshotBytes = 64 << 20

// LoadSnapshot reads a SharedSnapshot file.
func LoadSnapshot(path string) (*SharedSnapshot, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("read snapshot: %w", err)
	}
	if fi.Size() > maxSnapshotBytes {
		return nil, fmt.Errorf("snapshot is %d bytes, over the %d-byte fuse", fi.Size(), maxSnapshotBytes)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read snapshot: %w", err)
	}
	var s SharedSnapshot
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("parse snapshot: %w", err)
	}
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("parse snapshot: %w", err)
	}
	return &s, nil
}

// Validate refuses a snapshot whose numbers the gate cannot read: a NaN or
// infinite parameter, and a negative or non-finite uncertainty — which the
// gate square-roots, so a negative variance is a NaN sigma and every
// comparison against it is arbitrary. Counts below zero are refused the
// same way. Unknown keys are fine: the file is shared across lanes.
func (s *SharedSnapshot) Validate() error {
	for k, v := range s.Parameters {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("parameter %q is not finite", k)
		}
	}
	for k, v := range s.Uncertainty {
		if v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("uncertainty %q is %v, must be finite and >= 0", k, v)
		}
	}
	for k, n := range s.Counts {
		if n < 0 {
			return fmt.Errorf("count %q is %d, must be >= 0", k, n)
		}
	}
	return nil
}

// roleKey builds the snapshot key for one role/model.
func roleKey(role, model string) string { return "quality/" + role + "/" + model }

// prior is one model's quality estimate for one role: mean q, sigma, and how
// it was obtained (scored, or inherited line/vendor/global).
type prior struct {
	Q      float64
	Sigma  float64
	N      int
	Source string // scored | line | vendor | global
	From   string // the model id the prior came from ("" if scored)
}

// GlobalSigma returns the residual-derived per-role sigma from the snapshot's
// quality_w/ROLE/residual_sigma parameter. Falls back to the role's scored
// sigma when missing.
func (s *SharedSnapshot) GlobalSigma(role string) float64 {
	if v, ok := s.Parameters["quality_w/"+role+"/residual_sigma"]; ok && v > 0 {
		return v
	}
	var sum, n float64
	for _, m := range s.ModelIDs {
		if u, ok := s.Uncertainty[roleKey(role, m)]; ok && u > 0 {
			sum += math.Sqrt(u)
			n++
		}
	}
	if n == 0 {
		return 0.5
	}
	return sum / n
}

// PriorEngine builds priors from the snapshot, applying the inheritance
// ladder: scored row; else line predecessor (same canonical line, earlier
// created, carrying a score) then vendor mean then global. Alias rows
// resolve to their alias target before anything else. maxIdx holds the
// pool's maxima for [I, C, A] normalization, computed once at construction.
type PriorEngine struct {
	maxIdx [3]float64 // pool maxima for [I, C, A] normalization
	Snap   *SharedSnapshot
	Cat    *ModelCatalog
	// roleSigmas caches GlobalSigma per role.
	roleSigmas map[string]float64
}

func NewPriorEngine(s *SharedSnapshot, c *ModelCatalog) *PriorEngine {
	e := &PriorEngine{Snap: s, Cat: c, roleSigmas: map[string]float64{}}
	e.computeMaxIdx()
	return e
}

// computeMaxIdx fills maxIdx with the pool's maxima for [I, C, A] across
// all catalog rows that carry all three indexes and are not sentinels.
// Alias rows resolve to their target's indexes before being counted.
func (e *PriorEngine) computeMaxIdx() {
	for _, m := range e.Cat.Models {
		if m.IsSentinel() {
			continue
		}
		// Resolve aliases to their target's indexes.
		if m.AliasTarget != "" {
			if tgt, ok := e.Cat.byID[m.AliasTarget]; ok {
				m = *tgt
			}
		}
		if m.IntelligenceIndex > e.maxIdx[0] {
			e.maxIdx[0] = m.IntelligenceIndex
		}
		if m.CodingIndex > e.maxIdx[1] {
			e.maxIdx[1] = m.CodingIndex
		}
		if m.AgenticIndex > e.maxIdx[2] {
			e.maxIdx[2] = m.AgenticIndex
		}
	}
}

func (e *PriorEngine) sigma(role string) float64 {
	if v, ok := e.roleSigmas[role]; ok {
		return v
	}
	v := e.Snap.GlobalSigma(role)
	e.roleSigmas[role] = v
	return v
}

func (e *PriorEngine) scoredQ(role, model string) (float64, bool) {
	v, ok := e.Snap.Parameters[roleKey(role, model)]
	return v, ok
}

func (e *PriorEngine) scoredSigma(role, model string) (float64, bool) {
	u, ok := e.Snap.Uncertainty[roleKey(role, model)]
	if !ok {
		return 0, false
	}
	return math.Sqrt(u), true
}

// catalogWeights maps a ROLE (ClassShape.Role: worker|checker|thinker) to
// the crewpick seat weights [I, C, A]. These are the DefaultShapes values
// from the findings, keyed by the string catalogPrior is actually called
// with — keyed by class id instead, the checker seat's prior silently
// missed and inherited. The selector does not import crewpick, so they are
// duplicated here deliberately.
var catalogWeights = map[string][3]float64{
	"worker":  {0.2, 0.3, 0.5},
	"checker": {1.0 / 3.0, 1.0 / 3.0, 1.0 / 3.0},
	"thinker": {0.6, 0, 0.4},
}

// catalogPrior computes the crewpick-style weighted quality from the
// catalog's published benchmark indexes. The indexes are normalized by the
// pool's maxima (computed once when the PriorEngine is built). Sentinel
// values (catalog-unique exact rounds like 50.0) are treated as missing.
// Returns false when the model has no usable benchmarks.
func (e *PriorEngine) catalogPrior(role string, m *Model) (prior, bool) {
	if !m.HasBenchmarks() || m.IsSentinel() {
		return prior{}, false
	}
	w, ok := catalogWeights[role]
	if !ok {
		return prior{}, false
	}
	idx := [3]float64{m.IntelligenceIndex, m.CodingIndex, m.AgenticIndex}
	if idx[0] == 0 && idx[1] == 0 && idx[2] == 0 {
		return prior{}, false
	}
	q := 0.0
	for i := 0; i < 3; i++ {
		mx := e.maxIdx[i]
		if mx <= 0 {
			mx = 1.0
		}
		q += w[i] * (idx[i] / mx)
	}
	if q <= 0 {
		return prior{}, false
	}
	if q > 1.0 {
		q = 1.0
	}
	return prior{Q: q, Sigma: e.sigma(role), Source: "catalog"}, true
}

// PriorFor returns the prior for one canonical model id on one role.
// Chain: snapshot measured > catalog benchmarks > line > vendor > global.
func (e *PriorEngine) PriorFor(role, model string) prior {
	if q, ok := e.scoredQ(role, model); ok {
		sg, _ := e.scoredSigma(role, model)
		if sg == 0 {
			sg = e.sigma(role)
		}
		n := e.Snap.Counts[roleKey(role, model)]
		return prior{Q: q, Sigma: sg, N: n, Source: "scored"}
	}
	m, ok := e.Cat.byID[model]
	if !ok {
		return prior{Q: 0.5, Sigma: e.sigma(role), Source: "global"}
	}
	if p, ok := e.catalogPrior(role, m); ok {
		return p
	}
	if p, ok := e.linePrior(role, m); ok {
		return p
	}
	if p, ok := e.vendorPrior(role, m); ok {
		return p
	}
	return prior{Q: 0.5, Sigma: e.sigma(role), Source: "global"}
}

func (e *PriorEngine) linePrior(role string, m *Model) (prior, bool) {
	key := m.LineKey()
	best := prior{}
	found := false
	var bestFrom int64 = -1
	for i := range e.Cat.Models {
		o := &e.Cat.Models[i]
		if o.CanonicalSlug == m.CanonicalSlug {
			continue
		}
		if o.LineKey() != key {
			continue
		}
		ro, _ := e.Cat.Resolve(o)
		if ro.CanonicalSlug == m.CanonicalSlug {
			continue
		}
		if o.Created >= m.Created {
			continue // predecessor only
		}
		q, ok := e.scoredQ(role, ro.ID)
		if !ok {
			continue
		}
		if !found || o.Created > bestFrom {
			found = true
			bestFrom = o.Created
			sg, _ := e.scoredSigma(role, ro.ID)
			if sg == 0 {
				sg = e.sigma(role)
			}
			best = prior{Q: q, Sigma: sg, N: e.Snap.Counts[roleKey(role, ro.ID)], Source: "line", From: ro.ID}
		}
	}
	return best, found
}

func (e *PriorEngine) vendorPrior(role string, m *Model) (prior, bool) {
	vendor := m.Vendor()
	qs := []float64{}
	for i := range e.Cat.Models {
		o := &e.Cat.Models[i]
		if o.Vendor() != vendor {
			continue
		}
		ro, _ := e.Cat.Resolve(o)
		if q, ok := e.scoredQ(role, ro.ID); ok {
			qs = append(qs, q)
		}
	}
	if len(qs) == 0 {
		return prior{}, false
	}
	sum := 0.0
	for _, q := range qs {
		sum += q
	}
	return prior{Q: sum / float64(len(qs)), Sigma: e.sigma(role), Source: "vendor"}, true
}

// Vendor returns the vendor namespace from canonical_slug (schema field).
func (m *Model) Vendor() string {
	slug := m.CanonicalSlug
	if i := strings.Index(slug, "/"); i >= 0 {
		return slug[:i]
	}
	return slug
}

// sortedModels returns the catalog models in deterministic order (by id),
// with alias rows collapsed to their target.
func (e *PriorEngine) sortedModels() []*Model {
	ms := make([]*Model, 0, len(e.Cat.Models))
	for i := range e.Cat.Models {
		ms = append(ms, &e.Cat.Models[i])
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].ID < ms[j].ID })
	return ms
}

// ByID returns the catalog model with the given id, or nil.
func (c *ModelCatalog) ByID(id string) *Model { return c.byID[id] }

// ResolveTarget resolves a model id (or catalog pointer) to its canonical
// target model.
func (c *ModelCatalog) ResolveTarget(m *Model) *Model {
	ro, _ := c.Resolve(m)
	return ro
}

// PriorFor exposes the prior for a model id on a role.
func (e *Engine) PriorFor(role, model string) prior { return e.priors.PriorFor(role, model) }
