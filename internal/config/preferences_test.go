package config

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// TestC30EditingOneSettingPreservesOtherEditClocks proves C30.3 for older
// profiles without per-setting clocks: a budget edit cannot make an older
// model pick win over a newer choice on another computer.
func TestC30EditingOneSettingPreservesOtherEditClocks(t *testing.T) {
	dir := t.TempDir()
	raw, err := json.Marshal(map[string]any{KeyChatModel: "stub/older", KeyDailyBudget: 3})
	if err != nil {
		t.Fatal(err)
	}
	path := BudgetConfigPath(dir)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	at := time.Unix(1700000000, 0)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
	if err := WriteDailyBudgetUSD(dir, 7); err != nil {
		t.Fatal(err)
	}
	if got := PreferenceTime(dir, KeyChatModel); !got.Equal(at) {
		t.Fatalf("budget edit aged model: %v", got)
	}
	if got := PreferenceTime(dir, KeyDailyBudget); !got.After(at) {
		t.Fatalf("budget edit lost its own clock: %v", got)
	}
}
