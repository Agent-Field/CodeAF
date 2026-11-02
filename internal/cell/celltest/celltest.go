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

// Engine is the furrow seal engine codeaf itself would run: the configured
// binary or the embedded copy, and never whatever furrow sits on PATH. A test
// that seals through the product has to ask the same question the product
// asks, because a PATH engine is a different program with the same name, and
// a test that accepted one would run its fixture against an engine the code
// under test then refuses. The test skips, naming the missing engine and the
// way to provide it, only when there is no such engine at all. The make test
// targets stage the pinned engine first, so the gate never takes this skip.
func Engine(t testing.TB) string {
	t.Helper()
	binary, err := furrowbin.OwnedBinary()
	if err != nil {
		t.Skipf("no furrow seal engine: %v (run `make furrow` to embed the pinned engine in test builds, or set %s to a furrow binary)", err, furrowbin.BinaryEnvVar)
	}
	return binary
}
