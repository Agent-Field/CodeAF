package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
)

// notASettingsRow is the explicit, reasoned exemption list for the gate below.
// It is a map to a sentence rather than a set of names because an exemption
// with no reason is how a phantom row gets one.
var notASettingsRow = map[string]string{
	// boost_model is a transient escalation of the WORK binding and not a
	// concept of its own (8.2.16). It had a row, and the row was worse than no
	// row: enter on it opened the models door for the slot, the door resolved
	// boost onto the work role, and picking a model there rebound the work model
	// while the reader believed they were setting boost. Boost rides the work
	// binding rather than owning a second settings row.
	"boost_model": "boost rides the work binding; the sheet must not offer a second door onto it",
}

// mediaSlotsMovedToTheProfile is the second exemption list, and it is separate
// from the first because it is a different kind of statement: these fields are
// not phantom rows, they are a store that stopped being the store.
//
// Under docs/MULTIMODAL.md Decision 5 the five capability slots are one knob
// each, written into the PROFILE under the registry's own key and read back
// there by every resolver — so the sheet fronts each of them, and none of them
// fronts this file any more. The fields survive so a choice made on the old
// surface is not silently dropped from a settings file somebody still has.
var mediaSlotsMovedToTheProfile = map[string]bool{
	"voice_model": true, "image_model": true,
	"speech_model": true, "music_model": true, "video_model": true,
}

// The other half of the completeness gate: a preference that persists beside
// the graph has to be reachable from the settings sheet, or be exempted here in
// writing. Adding a field to Prefs without registering the row that fronts it
// fails here.
func TestEveryChatPreferenceIsFrontedByASettingsRow(t *testing.T) {
	// The seams are supplied because the divider row is built only for a caller
	// that can save one, so a bare registry would be asking whether a preference
	// is reachable from a sheet nobody here opens.
	registry := config.NewSettings(config.SettingsOptions{
		ProfileDir:   t.TempDir(),
		SplitPct:     func() int { return 0 },
		SaveSplitPct: func(int) {},
	})
	fronted := make(map[string]string)
	for _, row := range registry.Rows() {
		if row.PrefsField != "" {
			fronted[row.PrefsField] = row.Key
		}
	}
	prefs := reflect.TypeOf(Prefs{})
	exempt := 0
	for index := 0; index < prefs.NumField(); index++ {
		field := prefs.Field(index)
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "" {
			t.Fatalf("Prefs.%s has no json name to register against", field.Name)
		}
		if reason := notASettingsRow[name]; reason != "" {
			if _, fronted := fronted[name]; fronted {
				t.Fatalf("Prefs.%s is both exempted (%s) and fronted by a row", field.Name, reason)
			}
			exempt++
			continue
		}
		if mediaSlotsMovedToTheProfile[name] {
			// The row exists and is reachable — it simply persists somewhere
			// else now, so it carries no PrefsField. The gate that matters is
			// still enforced: the slot has a settings row.
			if _, ok := registry.Row(config.ModelSettingKey(strings.TrimSuffix(name, "_model"))); !ok {
				t.Fatalf("Prefs.%s moved to the profile but has no settings row", field.Name)
			}
			exempt++
			continue
		}
		if _, ok := fronted[name]; !ok {
			t.Fatalf("Prefs.%s (%q) is not fronted by any settings row", field.Name, name)
		}
	}
	if len(fronted)+exempt != prefs.NumField() {
		t.Fatalf("settings front %d prefs fields and %d are exempt, Prefs has %d",
			len(fronted), exempt, prefs.NumField())
	}
}
