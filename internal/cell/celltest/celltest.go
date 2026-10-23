// Package celltest lets tests declare whether their door needs a real engine
// or only the availability of cells, independent of the build channel.
package celltest

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/furrowbin"
)

// Available declares engine availability for wiring tests without running furrow.
func Available(t *testing.T) {
	t.Helper()
	t.Cleanup(cell.UseAvailabilityInTests(true))
}

// RequireEngine leaves acceptance visible when the configured or embedded
// binary is missing or lacks the exchange contract.
func RequireEngine(t *testing.T) {
	t.Helper()
	if !furrowbin.CellsAvailable() {
		t.Skipf("no engine binary: %s", furrowbin.CellsUnavailable)
	}
	t.Cleanup(cell.UseAvailabilityInTests(true))
}
