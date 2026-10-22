package store

import (
	"encoding/json"
	"log"
)

// PopulateMemoryTelemetry counts the admitted prompt lines in the same rune
// units as the budget. The source label remains intact; derived semantics
// decide the breakdown, with declared type as the compatibility fallback.
func PopulateMemoryTelemetry(block *MemoryBlock) {
	block.TotalRunes = 0
	block.PinnedRunes, block.BudgetedRunes = 0, 0
	block.RunesBySemanticType = make(map[string]int)
	block.BudgetedRunesBySemanticType = make(map[string]int)
	for _, memory := range block.Memories {
		kind := memory.SemanticType
		if kind == "" {
			kind = memory.Type
		}
		switch kind {
		case MemoryFact, MemoryPreference, MemoryCorrection, MemoryDecision, MemoryProjectState:
		default:
			kind = "unclassified"
		}
		runes := MemoryLineCost(memory.Text)
		block.TotalRunes += runes
		if memory.Rule || memory.Origin == OriginPerson {
			block.PinnedRunes += runes
		} else {
			block.BudgetedRunes += runes
			block.BudgetedRunesBySemanticType[kind] += runes
		}
		block.RunesBySemanticType[kind] += runes
	}
}

// LogMemoryAssembly records composition without memory text, IDs, context or
// owner names. The optional stage distinguishes a store assembly from the
// conversation's final set after dwell has been applied.
func LogMemoryAssembly(block MemoryBlock, stages ...string) {
	stage := "assembly"
	if len(stages) > 0 && stages[0] != "" {
		stage = stages[0]
	}
	shares := make(map[string]float64, len(block.RunesBySemanticType))
	for kind, runes := range block.RunesBySemanticType {
		if block.TotalRunes > 0 {
			shares[kind] = float64(runes) / float64(block.TotalRunes)
		}
	}
	data, err := json.Marshal(struct {
		Event                       string             `json:"event"`
		Stage                       string             `json:"stage"`
		TotalRunes                  int                `json:"total_runes"`
		PinnedRunes                 int                `json:"pinned_runes"`
		BudgetedRunes               int                `json:"budgeted_runes"`
		BudgetRunes                 int                `json:"budget_runes"`
		RunesBySemanticType         map[string]int     `json:"runes_by_semantic_type"`
		ShareBySemanticType         map[string]float64 `json:"share_by_semantic_type"`
		BudgetedRunesBySemanticType map[string]int     `json:"budgeted_runes_by_semantic_type"`
	}{"memory_admission", stage, block.TotalRunes, block.PinnedRunes, block.BudgetedRunes, MemoryBlockRunes, block.RunesBySemanticType, shares, block.BudgetedRunesBySemanticType})
	if err == nil {
		log.Print(string(data))
	}
}
