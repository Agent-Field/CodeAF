//go:build !windows

package baked

import "strings"

// Tier is a model routing pool. The router keeps one pool per tier and
// resolves any tier whose pool is empty to the high pool, so a run given
// nothing but `--high` routes every tier on that one pool.
type Tier string

const (
	TierHigh     Tier = "high"
	TierLow      Tier = "low"
	TierFrontier Tier = "frontier"
)

// tierMap is the agent-to-tier mapping. This table and the optional `tier:`
// frontmatter key that overrides it are the only things that decide which
// pool a call routes on.
//
//   - coder: the implementation turns, on the high pool.
//   - compaction: the transcript summary call, on the low pool. It is an
//     auxiliary call that recurs through a long run, so it is the one place
//     a cheaper pool is worth configuring.
var tierMap = map[string]Tier{
	"coder":      TierHigh,
	"compaction": TierLow,
}

// TierFor returns the named agent's routing tier. A baked agent may override
// the table with a `tier:` frontmatter key; an absent or unrecognised value,
// and any name the table does not list, routes on the high pool.
func TierFor(name string) Tier {
	metadata, _ := GetBakedAgentMetadata(name)
	return tierFrom(metadata, name)
}

// tierFrom answers for an agent whose frontmatter metadata is already in
// hand. A nil map means the name has no baked document, which is how the
// compaction summary reaches the table.
func tierFrom(metadata map[string]any, name string) Tier {
	if tier, ok := parseTier(metadata["tier"]); ok {
		return tier
	}
	if tier, ok := tierMap[name]; ok {
		return tier
	}
	return TierHigh
}

// parseTier reads a frontmatter `tier:` value. It reports false for anything
// that is not one of the three tier names, leaving the table's answer in
// place.
func parseTier(value any) (Tier, bool) {
	text, ok := value.(string)
	if !ok {
		return "", false
	}
	switch Tier(strings.ToLower(strings.TrimSpace(text))) {
	case TierHigh:
		return TierHigh, true
	case TierLow:
		return TierLow, true
	case TierFrontier:
		return TierFrontier, true
	default:
		return "", false
	}
}
