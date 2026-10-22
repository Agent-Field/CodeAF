// Package celltest lets tests declare whether their door needs a real engine
// or only the availability of cells, independent of build staging.
package celltest

import (
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/furrowbin"
	"testing"
)

// Available declares that a wiring test needs cells but never runs furrow.
// Rollout settings keep their normal effect, including an explicit off switch.
func Available(t *testing.T) {
	t.Helper()
	t.Cleanup(cell.UseAvailabilityInTests(true))
}

// RequireEngine leaves real-engine acceptance explicit and visible when the
// configured or embedded binary is missing or lacks the exchange contract.
func RequireEngine(t *testing.T) {
	t.Helper()
	if !furrowbin.CellsAvailable() {
		t.Skipf("no engine binary: %s", furrowbin.CellsUnavailable)
	}
}
