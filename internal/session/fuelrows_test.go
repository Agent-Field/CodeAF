package session

// THE FUEL TABLE HAS TO KNOW EVERY MODEL THIS BUILD SHIPS. The table is the
// fallback an orchestrated run meters against when no catalog reader is
// installed, so a shipped id with no row is a seat that quietly bills at the
// unpriced rate. This pins the two shipped small-work defaults and every model
// the crew router's evidence table measured to a real row, so a new default or
// a newly measured model fails the build until the table is repaid.

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/orchestrate"
)

// TestEveryShippedModelHasAFuelRow: every default and every measured crew
// model resolves to a known fuel row rather than falling through to unpriced.
func TestEveryShippedModelHasAFuelRow(t *testing.T) {
	shipped := []string{config.DefaultReflexModel, config.DefaultLowModel}
	shipped = append(shipped, crewroute.Measured()...)
	for _, id := range shipped {
		if _, known := orchestrate.PriceOf(id); !known {
			t.Errorf("no fuel row for shipped model %q", id)
		}
	}
}
