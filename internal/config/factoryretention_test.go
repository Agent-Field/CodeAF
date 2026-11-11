package config

import (
	"os"
	"strings"
	"testing"
)

// The retention choice defaults to one day, round-trips zero as a real answer,
// refuses a number it would not read back, and keeps the persisted key the app
// has always used.
func TestFactoryDoneDays(t *testing.T) {
	dir := t.TempDir()
	if got := AppFactoryDoneDaysAt(dir); got != 1 {
		t.Fatalf("default = %d", got)
	}
	for _, days := range []int{7, 0, 3650} {
		if err := WriteAppFactoryDoneDays(dir, days); err != nil {
			t.Fatal(err)
		}
		if got := AppFactoryDoneDaysAt(dir); got != days {
			t.Fatalf("wrote %d, read %d", days, got)
		}
	}
	for _, days := range []int{-1, 3651} {
		if err := WriteAppFactoryDoneDays(dir, days); err == nil {
			t.Fatalf("%d was accepted", days)
		}
	}
	if got := AppFactoryDoneDaysAt(dir); got != 3650 {
		t.Fatalf("a refused write moved the value to %d", got)
	}
	raw, _ := os.ReadFile(BudgetConfigPath(dir))
	if !strings.Contains(string(raw), `"app.factory.done_days"`) {
		t.Fatalf("persisted key changed: %s", raw)
	}
}
