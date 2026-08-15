package tui2_test

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/tui2"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// TestDefaultMetricsAgreesWithTokenBreakpoints pins tui2.DefaultMetrics's
// dialog-fullscreen numbers to the tokens table (10.5.24), which is the
// single source of truth those numbers are stated in. The root tui2 package
// cannot import tokens itself without inverting the tokens → tui2 dependency
// the SEAM note in metrics.go documents, so DefaultMetrics restates the two
// numbers as literals; this external test is what makes future drift between
// the restatement and the table fail loudly instead of shipping quietly.
func TestDefaultMetricsAgreesWithTokenBreakpoints(t *testing.T) {
	m := tui2.DefaultMetrics()
	if got, want := m.DialogFullscreenBelowWidth, tokens.DialogFullscreenBelowWidth; got != want {
		t.Fatalf("DefaultMetrics().DialogFullscreenBelowWidth = %d, tokens.DialogFullscreenBelowWidth = %d — reconcile the literal in metrics.go", got, want)
	}
	if got, want := m.DialogFullscreenBelowHeight, tokens.DialogFullscreenBelowHeight; got != want {
		t.Fatalf("DefaultMetrics().DialogFullscreenBelowHeight = %d, tokens.DialogFullscreenBelowHeight = %d — reconcile the literal in metrics.go", got, want)
	}
}
