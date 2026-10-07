package config

// BudgetConfig ports config.py BudgetConfig — global and per-phase budget caps.
// Numeric defaults are the §D "Config numeric tables" verbatim. Note
// max_duration_seconds is always overridden per call to the resolved value
// (3600 by default) via ReviewConfig.FromInput.
type BudgetConfig struct {
	MaxCostUSD         float64            `json:"max_cost_usd"`
	MaxDurationSeconds int                `json:"max_duration_seconds"`
	PhaseBudgets       map[string]float64 `json:"phase_budgets"`

	MaxConcurrentReviewers int `json:"max_concurrent_reviewers"`

	MaxReferenceFollowsPerReviewer int `json:"max_reference_follows_per_reviewer"`
	MaxChildSpawnsPerReviewer      int `json:"max_child_spawns_per_reviewer"`

	MaxCrossRefDeepDives int `json:"max_cross_ref_deep_dives"`

	MaxCoverageIterations int `json:"max_coverage_iterations"`

	MaxReviewDepth int `json:"max_review_depth"`

	// EvidencePackReviewers pre-reads each dimension's target files and injects
	// them so reviewers reason over a primed pack. Default ON: env
	// PR_AF_EVIDENCE_PACK not in {"0","false","no"}.
	EvidencePackReviewers bool `json:"evidence_pack_reviewers"`
}

// DefaultPhaseBudgets is the per-phase USD allocation (config.py). Returned as a
// fresh map per call so callers cannot mutate a shared instance.
func DefaultPhaseBudgets() map[string]float64 {
	return map[string]float64{
		"intake":         0.05,
		"anatomy":        0.15,
		"meta_selectors": 0.30, // 3 parallel lenses
		"review":         0.90, // Most budget goes here
		"adversary":      0.40, // Parallel batches
		"cross_ref":      0.30,
		"coverage":       0.10,
		"synthesis":      0.00, // Code, no LLM cost
		"output":         0.00, // Code, no LLM cost
	}
}

// DefaultBudgetConfig builds a BudgetConfig with the config.py defaults, reading
// PR_AF_EVIDENCE_PACK at call time.
func DefaultBudgetConfig() BudgetConfig {
	return BudgetConfig{
		MaxCostUSD:                     2.0,
		MaxDurationSeconds:             3600,
		PhaseBudgets:                   DefaultPhaseBudgets(),
		MaxConcurrentReviewers:         8,
		MaxReferenceFollowsPerReviewer: 3,
		MaxChildSpawnsPerReviewer:      2,
		MaxCrossRefDeepDives:           5,
		MaxCoverageIterations:          2,
		MaxReviewDepth:                 2,
		EvidencePackReviewers:          evidencePackDefault(),
	}
}

// evidencePackDefault is on. pr-af read PR_AF_EVIDENCE_PACK to turn it off;
// inside codeaf the review reads no environment of its own, so the shipped
// default is the setting.
func evidencePackDefault() bool { return true }

// ResolveBudgetCaps ports app.py _resolve_budget_caps. An explicit argument
// always wins, and the defaults are 2.0 / 3600 (real reviews measure 60-70
// minutes — the historical 300s default killed every fresh install
// mid-pipeline). pr-af also read PR_AF_MAX_COST_USD and
// PR_AF_MAX_DURATION_SECONDS between the two; codeaf always passes the run's
// own caps (internal/praf's reviewInput), so nothing reads them.
func ResolveBudgetCaps(maxCostUSD *float64, maxDurationSeconds *int) (float64, int, error) {
	cost := 2.0
	if maxCostUSD != nil {
		cost = *maxCostUSD
	}
	dur := 3600
	if maxDurationSeconds != nil {
		dur = *maxDurationSeconds
	}
	return cost, dur, nil
}
