package tui3

import (
	"github.com/Agent-Field/codeaf/internal/config"
	"strings"
	"testing"
)

// C15 and C21 require slash help to explain enabling unavailable chat pairing,
// and C25 keeps a collection setting off a surface that cannot use cells.
func TestPairHelpAndBudgetWhenMovingChatsIsOff(t *testing.T) {
	t.Setenv("CODEAF_CELLS", "0")
	c := command{name: "pair", desc: "show a code"}
	if got := c.note(chordSpelling{}); !strings.Contains(got, "CODEAF_CELLS=1") {
		t.Fatalf("pair help: %s", got)
	}
	_, visible := config.NewSettings(config.SettingsOptions{ProfileDir: t.TempDir()}).Row(config.KeyCellBudget)
	if visible {
		t.Fatal("unavailable gc budget row is visible")
	}
}
