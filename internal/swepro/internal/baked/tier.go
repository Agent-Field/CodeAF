// This file ports swe-pro/src/baked/tier.ts:1-186 at commit 3b25a1a, excluding
// specialist review/architecture agent tiers at the coder-only seam.
package baked

// Tier is a model routing pool.
type Tier string

const (
	TierHigh     Tier = "high"
	TierLow      Tier = "low"
	TierFrontier Tier = "frontier"
)

// TierAssignment preserves JavaScript object insertion order for inspection
// and JSON parity.
type TierAssignment struct {
	Name string `json:"name"`
	Tier Tier   `json:"tier"`
}

var tierAssignments = []TierAssignment{
	{Name: "root-orchestrator", Tier: TierHigh},
	{Name: "deep-worker", Tier: TierHigh},
	{Name: "orchestrator", Tier: TierHigh},
	{Name: "planner", Tier: TierHigh},
	{Name: "planner-translate", Tier: TierHigh},
	{Name: "issue-writer", Tier: TierHigh},
	{Name: "superpowers-code-reviewer", Tier: TierHigh},
	{Name: "designer", Tier: TierHigh},
	{Name: "auditor", Tier: TierHigh},
	{Name: "auditor-light", Tier: TierHigh},
	{Name: "adjudicator", Tier: TierFrontier},
	{Name: "validity-judge", Tier: TierFrontier},
	{Name: "convention-scout", Tier: TierFrontier},
	{Name: "contract-reviewer", Tier: TierFrontier},
	{Name: "plan-arbiter", Tier: TierFrontier},
	{Name: "root-cause", Tier: TierFrontier},
	{Name: "plan-sketch", Tier: TierHigh},
	{Name: "pr-ready-planner", Tier: TierHigh},
	{Name: "pr-formatter", Tier: TierHigh},
	{Name: "issue-advisor", Tier: TierHigh},
	{Name: "replanner", Tier: TierHigh},
	{Name: "retry-advisor", Tier: TierHigh},
	{Name: "input-classifier", Tier: TierHigh},
	{Name: "product-manager", Tier: TierHigh},
	{Name: "architect", Tier: TierHigh},
	{Name: "tech-lead", Tier: TierHigh},
	{Name: "fix-generator", Tier: TierHigh},
	{Name: "gate-synthesizer", Tier: TierHigh},
	{Name: "merger", Tier: TierHigh},
	{Name: "observer", Tier: TierLow},
	{Name: "coder", Tier: TierHigh},
	{Name: "build", Tier: TierHigh},
	{Name: "plan", Tier: TierHigh},
	{Name: "subtask-executor", Tier: TierHigh},
	{Name: "fixer", Tier: TierHigh},
	{Name: "explorer", Tier: TierLow},
	{Name: "explore", Tier: TierLow},
	{Name: "general", Tier: TierLow},
	{Name: "scout", Tier: TierLow},
}

var categoryTierAssignments = []TierAssignment{
	{Name: "ultrabrain", Tier: TierHigh},
	{Name: "visual-engineering", Tier: TierHigh},
	{Name: "artistry", Tier: TierHigh},
	{Name: "unspecified-high", Tier: TierHigh},
	{Name: "deep", Tier: TierHigh},
	{Name: "writing", Tier: TierHigh},
	{Name: "quick", Tier: TierLow},
	{Name: "unspecified-low", Tier: TierLow},
}

var tierMap = assignmentMap(tierAssignments)
var categoryTierMap = assignmentMap(categoryTierAssignments)

func assignmentMap(assignments []TierAssignment) map[string]Tier {
	out := make(map[string]Tier, len(assignments))
	for _, assignment := range assignments {
		out[assignment.Name] = assignment.Tier
	}
	return out
}

// TierAssignments returns the coder-only TIER_MAP in JavaScript insertion
// order.
func TierAssignments() []TierAssignment {
	return append([]TierAssignment(nil), tierAssignments...)
}

// CategoryTierAssignments returns CATEGORY_TIER in JavaScript insertion order.
func CategoryTierAssignments() []TierAssignment {
	return append([]TierAssignment(nil), categoryTierAssignments...)
}

// TierFor returns the named agent/category tier. The optional fallback mirrors
// the TypeScript default parameter and defaults to high.
func TierFor(name string, fallback ...Tier) Tier {
	defaultTier := TierHigh
	if len(fallback) > 0 {
		defaultTier = fallback[0]
	}
	if name == "" {
		return defaultTier
	}
	if tier, ok := tierMap[name]; ok {
		return tier
	}
	if tier, ok := categoryTierMap[name]; ok {
		return tier
	}
	return defaultTier
}
