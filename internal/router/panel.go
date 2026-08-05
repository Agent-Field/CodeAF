// Package router turns one model into a panel of them.
//
// It exists because of a measured result rather than an intuition. Two labs
// (bench/routerlab, bench/probelab) asked whether a harness can spend less and
// get more by not sending every call to the same model, and the answer they
// converged on is narrow and specific: predicting which model will fail does not
// pay, but *trying the cheap one and escalating on a verified failure* does. On
// a 36-task suite that policy reached 0.974 success at $0.000148 a task against
// the incumbent single model's 0.875 at $0.000076 — and it beat the best single
// model on the panel outright, at a sixth of that model's price.
//
// Everything here follows from that one sentence. The cascade needs a verifier,
// so verification is the centre of the design and an unverifiable call is
// treated as evidence about nothing. The order of the rungs needs an ability
// estimate, so there is a ledger — a one-parameter Rasch rating per model and
// call class, because the labs tested the two-parameter alternative and it did
// not pay for itself. And the ability estimate may not be seeded from price:
// measured against ability, log output price correlated at r = 0.46 with n = 7,
// and the second-cheapest model on the panel had the second-highest ability
// while the second-dearest sat sixth of seven.
//
// The whole package is inert unless AFORGE_MODELS names a panel. With it unset
// the harness builds the same single adapter it always did, and nothing below
// this line runs.
package router

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Spec is one model on the panel.
//
// Role is an operator's claim about where a model sits, not a measurement, and
// it is used for exactly one thing: the cold-start rating of a model nothing has
// been recorded about yet. It is worth about one observation and is updated away
// immediately. Price is an override for the catalog, for the case where an
// operator knows something the catalog does not — a negotiated rate, a private
// deployment — and not a place to encode an opinion about quality.
type Spec struct {
	Slug  string  `json:"slug"`
	Role  string  `json:"role,omitempty"`  // base, mid or top
	Price float64 `json:"price,omitempty"` // $/M output tokens, overriding the catalog
}

// Panel is the set of models one run may use, in the order the operator wrote
// them. That order is the tie-break of last resort: when nothing has been
// measured and no price is known, the panel is tried as written.
type Panel struct {
	Models []Spec `json:"models"`

	// MaxOutputPrice refuses a model dearer than this, in $/M output tokens. It
	// is a sanity cap rather than a budget: a typo in a slug that resolves to a
	// frontier model would otherwise be discovered on the invoice.
	MaxOutputPrice float64 `json:"max_output_price,omitempty"`
}

// LoadPanel reads AFORGE_MODELS. An empty value means no panel, which is not an
// error — it is the kill switch, and it is the default.
//
// Two forms are accepted. A comma-separated list of slugs is the one that gets
// typed, and it is deliberately the whole of what a first-time user needs to
// know. A path to a JSON file is for a panel worth keeping, where roles and a
// price cap are worth writing down. YAML was specified and is not implemented:
// this module has no YAML dependency and adding one to read a six-line file
// would be the most expensive line in the go.mod.
func LoadPanel(value string) (Panel, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return Panel{}, nil
	}
	if looksLikePath(value) {
		return loadPanelFile(value)
	}
	var panel Panel
	for _, field := range strings.Split(value, ",") {
		slug := strings.TrimSpace(field)
		if slug == "" {
			continue
		}
		panel.Models = append(panel.Models, Spec{Slug: slug})
	}
	if len(panel.Models) == 0 {
		return Panel{}, fmt.Errorf("AFORGE_MODELS: %q names no models", value)
	}
	return panel, nil
}

func loadPanelFile(path string) (Panel, error) {
	if extension := strings.ToLower(path); strings.HasSuffix(extension, ".yaml") || strings.HasSuffix(extension, ".yml") {
		return Panel{}, fmt.Errorf("AFORGE_MODELS: %s is YAML, which this build cannot read — "+
			"write the panel as JSON, or list the slugs directly: AFORGE_MODELS=a/b,c/d", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Panel{}, fmt.Errorf("AFORGE_MODELS: %w", err)
	}
	var panel Panel
	if err := json.Unmarshal(data, &panel); err != nil {
		return Panel{}, fmt.Errorf("AFORGE_MODELS: parse %s: %w", path, err)
	}
	kept := panel.Models[:0]
	for _, spec := range panel.Models {
		spec.Slug = strings.TrimSpace(spec.Slug)
		spec.Role = strings.ToLower(strings.TrimSpace(spec.Role))
		if spec.Slug == "" {
			continue
		}
		if spec.Role != "" && spec.Role != "base" && spec.Role != "mid" && spec.Role != "top" {
			return Panel{}, fmt.Errorf("AFORGE_MODELS: %s: unknown role %q for %s (base, mid, top)",
				path, spec.Role, spec.Slug)
		}
		kept = append(kept, spec)
	}
	panel.Models = kept
	if len(panel.Models) == 0 {
		return Panel{}, fmt.Errorf("AFORGE_MODELS: %s lists no models", path)
	}
	return panel, nil
}

// looksLikePath separates the two forms. A model slug is `vendor/model` and a
// path is anything with a directory in it, a leading dot, or a file extension —
// which never collide, because no slug carries an extension and no path is a
// bare `vendor/model`.
func looksLikePath(value string) bool {
	if strings.ContainsAny(value, ",") {
		return false
	}
	return strings.HasPrefix(value, "/") || strings.HasPrefix(value, ".") || strings.HasPrefix(value, "~") ||
		strings.HasSuffix(value, ".json") || strings.HasSuffix(value, ".yaml") || strings.HasSuffix(value, ".yml")
}

// coldStart is the rating a model nobody has measured starts from.
//
// Price is deliberately not consulted. The router lab measured ability against
// log output price at r = 0.46 over seven models, p ≈ 0.24 — not significant,
// and the shape of the miss is the real finding: the second-cheapest model on
// the panel had the second-highest ability, and the second-dearest sat sixth of
// seven, below a model costing thirteen times less. The operator's role hint is
// used instead, because it is a claim someone is making rather than a number
// being read as one, and one logit is about one observation's worth of pull.
func coldStart(role string) float64 {
	switch role {
	case "top":
		return 1
	case "base":
		return -1
	default:
		return 0
	}
}
