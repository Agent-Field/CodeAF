package session

// THE FUEL TABLE HAS TO KNOW EVERY MODEL THIS BUILD SHIPS. The table is the
// fallback an orchestrated run meters against when no catalog reader is
// installed, so a shipped id with no row is a seat that quietly bills at the
// unpriced rate. This pins the two shipped small-work defaults to a real row,
// so a new default fails the build until the table is repaid.

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/orchestrate"
)

// TestEveryShippedModelHasAFuelRow: every default resolves to a known fuel
// row rather than falling through to unpriced.
func TestEveryShippedModelHasAFuelRow(t *testing.T) {
	shipped := []string{config.DefaultReflexModel, config.DefaultLowModel}
	for _, id := range shipped {
		if _, known := orchestrate.PriceOf(id); !known && !datedRowFor(id) {
			t.Errorf("no fuel row for shipped model %q", id)
		}
	}
}

// datedRowFor reports whether the table holds a dated build of a lineage id.
// A default may name a model by its lineage (`deepseek/deepseek-v4-flash`)
// and the table by the build the catalog serves (`…-0731`); the undated id is a
// retired row the table must NOT hold (orchestrate's TestRetiredIdsAreStrangers),
// so the lineage is matched here rather than priced there.
func datedRowFor(lineage string) bool {
	for _, dated := range []string{"deepseek/deepseek-v4-flash-0731", "qwen/qwen3.8-max-0902"} {
		if _, known := orchestrate.PriceOf(dated); known && crewroute.Lineage(dated) == lineage {
			return true
		}
	}
	return false
}
