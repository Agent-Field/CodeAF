package store

import (
	"bytes"
	"encoding/json"
	"log"
	"strings"
	"testing"
)

func TestMemoryTelemetryUsesDerivedTypesAndSeparatesPins(t *testing.T) {
	pin := Memory{Text: "Mock pin uses café labels.", Type: MemoryPreference, Origin: OriginPerson}
	state := Memory{Text: "Mock release build passed.", Type: MemoryPreference, SemanticType: MemoryProjectState}
	decision := Memory{Text: "Mock output uses amber.", Type: MemoryDecision}
	block := MemoryBlock{Memories: []Memory{pin, state, decision}}
	PopulateMemoryTelemetry(&block)
	if block.RunesBySemanticType[MemoryProjectState] != MemoryLineCost(state.Text) || block.RunesBySemanticType[MemoryPreference] != MemoryLineCost(pin.Text) {
		t.Fatalf("wrong derived type composition: %+v", block.RunesBySemanticType)
	}
	if block.PinnedRunes != MemoryLineCost(pin.Text) || block.BudgetedRunes != MemoryLineCost(state.Text)+MemoryLineCost(decision.Text) || block.TotalRunes != block.PinnedRunes+block.BudgetedRunes {
		t.Fatalf("wrong pin accounting: %+v", block)
	}
}

func TestMemoryTelemetryLogContainsOnlyAggregateComposition(t *testing.T) {
	block := MemoryBlock{Memories: []Memory{{ID: "mock-private-id", Owner: "mock-private-owner", Text: "mock private body", Type: MemoryFact}, {Text: "mock second body", SemanticType: "mock private label", Type: MemoryPreference}}}
	PopulateMemoryTelemetry(&block)
	var out bytes.Buffer
	old, flags := log.Writer(), log.Flags()
	log.SetOutput(&out)
	log.SetFlags(0)
	defer log.SetOutput(old)
	defer log.SetFlags(flags)
	LogMemoryAssembly(block, "conversation")
	for _, private := range []string{"mock-private-id", "mock-private-owner", "mock private body", "mock private label", "mock second body"} {
		if strings.Contains(out.String(), private) {
			t.Fatalf("telemetry leaked content: %s", private)
		}
	}
	var got struct {
		Stage      string             `json:"stage"`
		TotalRunes int                `json:"total_runes"`
		Shares     map[string]float64 `json:"share_by_semantic_type"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	sum := 0.0
	for _, share := range got.Shares {
		sum += share
	}
	if got.Stage != "conversation" || got.TotalRunes != block.TotalRunes || sum < .9999 || sum > 1.0001 {
		t.Fatalf("invalid telemetry: %+v", got)
	}
}

func TestMemoryTelemetryExcludesPinnedProgressFromOrdinaryStatusShare(t *testing.T) {
	state := Memory{Type: MemoryProjectState, Text: "mock ordinary progress"}
	block := MemoryBlock{Memories: []Memory{state}}
	for i := 0; i < 16; i++ {
		block.Memories = append(block.Memories, Memory{Type: MemoryProjectState, Origin: OriginPerson, Text: strings.Repeat("mock pinned progress ", 20) + string(rune('a'+i))})
	}
	PopulateMemoryTelemetry(&block)
	if block.TotalRunes <= MemoryBlockRunes {
		t.Fatal("fixture must exceed ordinary budget through pins")
	}
	if block.BudgetedRunesBySemanticType[MemoryProjectState] != MemoryLineCost(state.Text) {
		t.Fatalf("pinned status polluted ordinary type share: %+v", block.BudgetedRunesBySemanticType)
	}
	if block.RunesBySemanticType[MemoryProjectState] <= block.BudgetedRunesBySemanticType[MemoryProjectState] {
		t.Fatal("overall composition lost pinned lines")
	}
}
