package session

// THE FUEL TABLE HAS TO KNOW EVERY MODEL THIS BUILD SHIPS. The table is the
// fallback an orchestrated run meters against when no catalog reader is
// installed, so a shipped id with no row is a seat that quietly bills at the
// unpriced rate — the most expensive default seat in particular. This pins the
// five shipped defaults and every id in both crew tables to a real row, so a
// new default or a moved crew seat fails the build until the table is repaid.

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/orchestrate"
)

// TestEveryShippedModelHasAFuelRow: every default and every crew-table id
// resolves to a known fuel row rather than falling through to unpriced.
func TestEveryShippedModelHasAFuelRow(t *testing.T) {
	shipped := []string{
		config.DefaultReflexModel,
		config.DefaultLowModel,
		config.DefaultWorkerModel,
		config.DefaultHighModel,
		config.DefaultMastermindModel,
	}
	for _, source := range config.CrewSources {
		for _, preset := range config.CrewPresets {
			models, ok := config.CrewModelsForSource(source, preset)
			if !ok {
				t.Fatalf("the %s crew has no %s preset", source, preset)
			}
			for _, model := range models {
				shipped = append(shipped, model)
			}
		}
	}
	for _, id := range shipped {
		if _, known := orchestrate.PriceOf(id); !known {
			t.Errorf("no fuel row for shipped model %q", id)
		}
	}
}
