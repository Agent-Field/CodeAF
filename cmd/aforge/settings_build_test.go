package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/command"
	"github.com/Agent-Field/aforge-v2/internal/config"
)

// notASettingsRow is the explicit, reasoned exemption list for the gate below.
// It is a map to a sentence rather than a set of names because an exemption
// with no reason is how a phantom row gets one.
var notASettingsRow = map[string]string{
	// boost_model is a transient escalation of the WORK binding and not a
	// concept of its own (8.2.16), which internal/tui2/modelui and
	// internal/tui2/chat both state in as many words. It had a row, and the row
	// was worse than no row: enter on it opened the models door for the slot,
	// the door resolved boost onto RoleWork, and picking a model there rebound
	// the work model while the reader believed they were setting boost. The
	// door that does set it is the models palette, where boost is a rider on
	// the work chip.
	"boost_model": "boost rides the work binding; the sheet must not offer a second door onto it",
}

// mediaSlotsMovedToTheProfile is the second exemption list, and it is separate
// from the first because it is a different kind of statement: these fields are
// not phantom rows, they are a store that stopped being the store.
//
// Under docs/MULTIMODAL.md Decision 5 the five capability slots are one knob
// each, written into the PROFILE under the registry's own key and read back
// there by every resolver — so the sheet fronts each of them, and none of them
// fronts this file any more. The fields survive as the v1/v2 commander's legacy
// first rung (internal/command's CurrentModel reads prefs, then the resolved
// settings), which is what keeps a choice made on the old surface from
// disappearing under someone.
var mediaSlotsMovedToTheProfile = map[string]bool{
	"voice_model": true, "image_model": true,
	"speech_model": true, "music_model": true, "video_model": true,
}

// The other half of the completeness gate: a preference that persists beside
// the graph has to be reachable from the settings sheet, or be exempted here in
// writing. Adding a field to chatPrefs without registering the row that fronts
// it fails here.
func TestEveryChatPreferenceIsFrontedByASettingsRow(t *testing.T) {
	registry := config.NewSettings(config.SettingsOptions{ProfileDir: t.TempDir()})
	fronted := make(map[string]string)
	for _, row := range registry.Rows() {
		if row.PrefsField != "" {
			fronted[row.PrefsField] = row.Key
		}
	}
	prefs := reflect.TypeOf(chatPrefs{})
	exempt := 0
	for index := 0; index < prefs.NumField(); index++ {
		field := prefs.Field(index)
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "" {
			t.Fatalf("chatPrefs.%s has no json name to register against", field.Name)
		}
		if reason := notASettingsRow[name]; reason != "" {
			if _, fronted := fronted[name]; fronted {
				t.Fatalf("chatPrefs.%s is both exempted (%s) and fronted by a row", field.Name, reason)
			}
			exempt++
			continue
		}
		if mediaSlotsMovedToTheProfile[name] {
			// The row exists and is reachable — it simply persists somewhere
			// else now, so it carries no PrefsField. The gate that matters is
			// still enforced: the slot has a settings row.
			if _, ok := registry.Row(config.ModelSettingKey(strings.TrimSuffix(name, "_model"))); !ok {
				t.Fatalf("chatPrefs.%s moved to the profile but has no settings row", field.Name)
			}
			exempt++
			continue
		}
		if _, ok := fronted[name]; !ok {
			t.Fatalf("chatPrefs.%s (%q) is not fronted by any settings row", field.Name, name)
		}
	}
	if len(fronted)+exempt != prefs.NumField() {
		t.Fatalf("settings front %d prefs fields and %d are exempt, chatPrefs has %d",
			len(fronted), exempt, prefs.NumField())
	}
}

// The registry the chat hands the surface has to be wired to this process: the
// live model slots, the divider, and the profile directory it persists into.
func TestChatCommanderBuildsALiveRegistry(t *testing.T) {
	dir := t.TempDir()
	commander := command.New(command.Options{
		PrefsDir: dir,
		Settings: config.Config{ProfileDir: dir},
		Prefs:    command.Prefs{ChatModel: "vendor/talk", SplitPct: 62},
	})

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
	if commander.DailyBudgetUSD() != 28 {
		t.Fatalf("the running rail = %v, want the applied 28", commander.DailyBudgetUSD())
	}
	if got, err := config.DailyBudgetUSDAt(dir); err != nil || got != 28 {
		t.Fatalf("persisted rail = %v err=%v", got, err)
	}
}
