// Package celltest lets tests declare whether their door needs a real engine
// or only the availability of cells, independent of the build channel.
package celltest

import (
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/furrowbin"
	"testing"
)

// Available opts a wiring test into cells without running furrow. Tests of
// the off switch set it explicitly after declaring engine availability.
func Available(t *testing.T) {
	t.Helper()
	t.Setenv(cell.EnvVar, "1")
	t.Cleanup(cell.UseAvailabilityInTests(true))
}

// RequireEngine opts into cells and leaves acceptance visible when the
// configured or embedded binary is missing or lacks the exchange contract.
func RequireEngine(t *testing.T) {
	t.Helper()
	t.Setenv(cell.EnvVar, "1")
	if !furrowbin.CellsAvailable() {
		t.Skipf("no engine binary: %s", furrowbin.CellsUnavailable)
	}
}
