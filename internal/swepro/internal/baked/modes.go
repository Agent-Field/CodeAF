// This file ports swe-pro/src/baked/modes.ts:1-84 at commit 3b25a1a. The
// review/architecture prompt rosters and tiers are cut at the coder-only seam,
// while their metadata remains available so tool visibility filtering keeps
// the TypeScript behavior.
package baked

import "strings"

// SpecialistMode is the coder-visible portion of a specialist harness
// descriptor. Agent Markdown and specialist tiers are intentionally omitted.
type SpecialistMode struct {
	Name           string   `json:"name"`
	AgentPrefix    string   `json:"agentPrefix"`
	EntryAgent     string   `json:"entryAgent"`
	AgentSourceDir string   `json:"agentSourceDir"`
	ExclusiveTools []string `json:"exclusiveTools"`
	ForbiddenTools []string `json:"forbiddenTools"`
}

var specialistModes = []SpecialistMode{
	{
		Name:           "review",
		AgentPrefix:    "review-",
		EntryAgent:     "review-anatomist",
		AgentSourceDir: "src/review/agents",
		ExclusiveTools: []string{
			"pr_diff",
			"repo_fingerprint",
			"review_blackboard",
			"review_surface",
			"review_plan",
			"review_candidate",
			"review_proof",
			"review_lens_rollup",
			"review_report",
			"review_wait",
			"search_code",
		},
		ForbiddenTools: []string{"edit", "write", "apply_patch", "bash", "shell", "plandb"},
	},
	{
		Name:           "arch",
		AgentPrefix:    "arch-",
		EntryAgent:     "arch-architect",
		AgentSourceDir: "src/arch/agents",
		ExclusiveTools: []string{},
		ForbiddenTools: []string{"task", "plandb"},
	},
}

// SpecialistModes returns mode metadata without the skipped specialist agent
// definitions.
func SpecialistModes() []SpecialistMode {
	out := make([]SpecialistMode, len(specialistModes))
	for i, mode := range specialistModes {
		out[i] = cloneMode(mode)
	}
	return out
}

func cloneMode(mode SpecialistMode) SpecialistMode {
	mode.ExclusiveTools = append(make([]string, 0, len(mode.ExclusiveTools)), mode.ExclusiveTools...)
	mode.ForbiddenTools = append(make([]string, 0, len(mode.ForbiddenTools)), mode.ForbiddenTools...)
	return mode
}

// ModeForAgent returns the specialist mode identified by an agent-name prefix.
func ModeForAgent(agentName string) (SpecialistMode, bool) {
	if agentName == "" {
		return SpecialistMode{}, false
	}
	for _, mode := range specialistModes {
		if strings.HasPrefix(agentName, mode.AgentPrefix) {
			return cloneMode(mode), true
		}
	}
	return SpecialistMode{}, false
}

// EntryAgentForMode returns a specialist harness entry agent.
func EntryAgentForMode(modeName string) (string, bool) {
	for _, mode := range specialistModes {
		if mode.Name == modeName {
			return mode.EntryAgent, true
		}
	}
	return "", false
}

// StringSet is an insertion-ordered set used for tool visibility checks.
type StringSet struct {
	order  []string
	values map[string]struct{}
}

// Has reports whether value belongs to the set.
func (s StringSet) Has(value string) bool {
	_, ok := s.values[value]
	return ok
}

// Values returns members in JavaScript Set insertion order.
func (s StringSet) Values() []string {
	return append([]string(nil), s.order...)
}

// AllExclusiveToolIDs returns every specialist-only tool ID. This remains
// populated even though specialist agent definitions are outside the coder
// port, preventing review tools from leaking into the default tool surface.
func AllExclusiveToolIDs() StringSet {
	out := StringSet{values: make(map[string]struct{})}
	for _, mode := range specialistModes {
		for _, toolID := range mode.ExclusiveTools {
			if _, exists := out.values[toolID]; exists {
				continue
			}
			out.values[toolID] = struct{}{}
			out.order = append(out.order, toolID)
		}
	}
	return out
}
