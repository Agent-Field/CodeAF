package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
)

// The other half of the completeness gate: a preference that persists beside
// the graph has to be reachable from the settings sheet. Adding a field to
// chatPrefs without registering the row that fronts it fails here.
func TestEveryChatPreferenceIsFrontedByASettingsRow(t *testing.T) {
	registry := config.NewSettings(config.SettingsOptions{ProfileDir: t.TempDir()})
	fronted := make(map[string]string)
	for _, row := range registry.Rows() {
		if row.PrefsField != "" {
			fronted[row.PrefsField] = row.Key
		}
	}
	prefs := reflect.TypeOf(chatPrefs{})
	for index := 0; index < prefs.NumField(); index++ {
		field := prefs.Field(index)
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "" {
			t.Fatalf("chatPrefs.%s has no json name to register against", field.Name)
		}
		if _, ok := fronted[name]; !ok {
			t.Fatalf("chatPrefs.%s (%q) is not fronted by any settings row", field.Name, name)
		}
	}
	if len(fronted) != prefs.NumField() {
		t.Fatalf("settings front %d prefs fields, chatPrefs has %d", len(fronted), prefs.NumField())
	}
}

// The registry the chat hands the surface has to be wired to this process: the
// live model slots, the divider, and the profile directory it persists into.
func TestChatCommanderBuildsALiveRegistry(t *testing.T) {
	dir := t.TempDir()
	commander := &chatCommander{prefsDir: dir}
	commander.settings.ProfileDir = dir
	commander.prefs.ChatModel = "vendor/talk"
	commander.prefs.SplitPct = 62

	registry := commander.Settings()
	if registry == nil {
		t.Fatal("the chat commander built no registry")
	}
	talk, ok := registry.Row(config.ModelSettingKey("talk"))
	if !ok || talk.Value() != "vendor/talk" {
		t.Fatalf("the talk row reads %q", talk.Value())
	}
	width, ok := registry.Row(config.KeySplitPct)
	if !ok || width.Value() != "62%" {
		t.Fatalf("the chat width row reads %q", width.Value())
	}
	if err := width.Apply("70"); err != nil || commander.SplitPct() != 70 {
		t.Fatalf("apply chat width: saved=%d err=%v", commander.SplitPct(), err)
	}

	// The dollar rail is the one this process honors without a relaunch.
	t.Setenv("AFORGE_DAILY_BUDGET", "")
	budget, _ := registry.Row(config.KeyDailyBudget)
	if err := budget.Apply("28"); err != nil {
		t.Fatal(err)
	}
	if commander.settings.DailyBudgetUSD != 28 {
		t.Fatalf("the running rail = %v, want the applied 28", commander.settings.DailyBudgetUSD)
	}
	if got, err := config.DailyBudgetUSDAt(dir); err != nil || got != 28 {
		t.Fatalf("persisted rail = %v err=%v", got, err)
	}
}
