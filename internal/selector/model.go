// Package selector implements the pareto selector (lane C): a deterministic
// crew picker built on lane B's SharedSnapshot (posterior quality means and
// variances), reading the live model catalog through its own input struct so
// it uses the schema fields (canonical_slug, created, alias_target) only —
// never name-parsing heuristics. It emits contract.DecisionEvents for every
// seat of every task class.
//
// It is not a router. Its picks reach a real crew through one seam only —
// internal/config's selectorwire.go, which runs it over a profile's fitted
// snapshot and hands the seat picks to internal/crewroute as
// Request.Chosen; with no snapshot configured nothing builds the engine and
// no crew reads it.
package selector

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Model is the catalog row the selector reads, restricted to the schema
// fields the design uses. Loading goes through ModelCatalog so the selector
// never depends on internal/catalog's shape.
type Model struct {
	ID                string   `json:"id"`
	CanonicalSlug     string   `json:"canonical_slug"`
	Created           int64    `json:"created"`
	AliasTarget       string   `json:"alias_target"`
	ContextLength     int      `json:"context_length"`
	PromptPrice       float64  `json:"prompt_price"`
	Completion        float64  `json:"completion_price"`
	CacheRead         float64  `json:"cache_read_price"`
	ModalitiesIn      []string `json:"input_modalities"`
	ModalitiesOut     []string `json:"output_modalities"`
	IntelligenceIndex float64  `json:"intelligence_index"`
	CodingIndex       float64  `json:"coding_index"`
	AgenticIndex      float64  `json:"agentic_index"`
	Reasoning         struct {
		Known     bool `json:"known"`
		Mandatory bool `json:"mandatory"`
	} `json:"reasoning"`
}

// HasToolUse reports whether the model can take tool calls: it must accept
// text input and not be restricted to a pure-vision surface. Determined from
// the modalities schema field only.
func (m Model) HasToolUse() bool {
	return hasString(m.ModalitiesIn, "text") || hasString(m.ModalitiesIn, "text/plain")
}

// HasImages reports whether the model accepts or produces image media, from
// the modalities schema fields only.
func (m Model) HasImages() bool {
	return hasAny(m.ModalitiesIn, "image", "image/*") || hasAny(m.ModalitiesOut, "image", "image/*")
}

func hasString(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// HasBenchmarks reports whether the catalog row carries at least one
// published benchmark index. A catalog-unique exact round value (like 50.0)
// is treated as missing — the sentinel defense from the findings.
func (m Model) HasBenchmarks() bool {
	return m.IntelligenceIndex > 0 || m.CodingIndex > 0 || m.AgenticIndex > 0
}

// IsSentinel reports whether any index is a catalog-unique exact round value
// (e.g. exactly 50.0) — the kimi-k3 agentic=50.0 case from the findings.
// Such values are treated as missing, not trusted.
func (m Model) IsSentinel() bool {
	for _, v := range [3]float64{m.IntelligenceIndex, m.CodingIndex, m.AgenticIndex} {
		if v == 50.0 {
			return true
		}
	}
	return false
}

func hasAny(ss []string, wants ...string) bool {
	for _, s := range ss {
		for _, w := range wants {
			if s == w {
				return true
			}
		}
	}
	return false
}

// ModelCatalog is a loaded catalog.
type ModelCatalog struct {
	Models []Model `json:"models"`
	// byID and bySlug index rows for alias resolution.
	byID   map[string]*Model
	bySlug map[string]*Model
}

// LoadCatalog reads the live model-catalog.json.
func LoadCatalog(path string) (*ModelCatalog, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read catalog: %w", err)
	}
	var c ModelCatalog
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parse catalog: %w", err)
	}
	c.index()
	return &c, nil
}

func (c *ModelCatalog) index() {
	c.byID = make(map[string]*Model, len(c.Models))
	c.bySlug = make(map[string]*Model, len(c.Models))
	for i := range c.Models {
		m := &c.Models[i]
		if m.ID != "" {
			c.byID[m.ID] = m
		}
		if m.CanonicalSlug != "" {
			c.bySlug[m.CanonicalSlug] = m
		}
	}
}

// Resolve follows alias_target chains to a canonical row, using the alias
// schema field only. A row with no alias_target resolves to itself. A cycle
// or a dangling target resolves to the row itself with an ok=false.
func (c *ModelCatalog) Resolve(m *Model) (*Model, bool) {
	seen := map[string]bool{}
	cur := m
	for cur.AliasTarget != "" {
		if seen[cur.ID] {
			return m, false
		}
		seen[cur.ID] = true
		next, ok := c.byID[cur.AliasTarget]
		if !ok {
			next, ok = c.bySlug[cur.AliasTarget]
		}
		if !ok {
			return m, false
		}
		cur = next
	}
	return cur, true
}

// LineKey groups a row's release line by the schema fields alone: the vendor
// (first path segment of canonical_slug) plus the slug with its trailing
// -YYYYMMDD stamp removed. No token parsing — the stamp is a fixed 8-digit
// suffix the slug format defines.
func (m *Model) LineKey() string {
	slug := m.CanonicalSlug
	// find last slash (vendor boundary)
	slash := strings.LastIndex(slug, "/")
	var vendor, name string
	if slash < 0 {
		vendor, name = "", slug
	} else {
		vendor, name = slug[:slash], slug[slash+1:]
	}
	if len(name) > 9 && name[len(name)-9] == '-' && isDigits(name[len(name)-8:]) {
		name = name[:len(name)-9]
	}
	return vendor + "/" + name
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
