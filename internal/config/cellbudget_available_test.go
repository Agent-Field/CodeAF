package config

import (
	"strings"
	"testing"
)

// C25: the budget is explicit gc's default and nothing reads it by itself, so
// the row is declared on every build — the settings laws, the profile ledger
// and the settings tool all read one registry — and its hint says plainly that
// nothing is removed automatically.
func TestCellBudgetRowIsDeclaredOnEveryBuild(t *testing.T) {
	for _, enabled := range []string{"", "0", "1"} {
		t.Setenv("CODEAF_CELLS", enabled)
		row, exists := NewSettings(SettingsOptions{ProfileDir: t.TempDir()}).Row(KeyCellBudget)
		if !exists {
			t.Fatalf("cells=%q: the budget row is not declared", enabled)
		}
		if !strings.Contains(row.Hint, "Nothing is removed automatically") {
			t.Fatalf("cells=%q: the budget hint does not say nothing is removed automatically: %s", enabled, row.Hint)
		}
	}
}
