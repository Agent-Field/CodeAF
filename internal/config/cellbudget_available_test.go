package config

import (
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"testing"
)

// C25 leaves the budget as explicit gc's default, absent when cells are off.
func TestCellBudgetRowRequiresAvailableCells(t *testing.T) {
	celltest.Available(t)
	for _, enabled := range []string{"0", "1"} {
		t.Setenv("CODEAF_CELLS", enabled)
		_, exists := NewSettings(SettingsOptions{ProfileDir: t.TempDir()}).Row(KeyCellBudget)
		if exists != (enabled == "1") {
			t.Fatalf("cells=%s, budget row=%t", enabled, exists)
		}
	}
}
