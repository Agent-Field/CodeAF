package config

import (
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/placegraph"
)

func TestPlacesSettingsStartOnTheDefaults(t *testing.T) {
	dir := t.TempDir()
	if DesktopPlacesPolicy(dir) != placegraph.DefaultRecommendPolicy() {
		t.Fatal("a fresh profile is not on the defaults")
	}
	for _, s := range DesktopPlacesSettings(dir) {
		if s.Chosen || s.Value != s.Default {
			t.Fatalf("%s: %v chosen=%v", s.Key, s.Value, s.Chosen)
		}
	}
}

func TestAPlacesChoiceIsKeptReadBackAndReset(t *testing.T) {
	dir := t.TempDir()
	if err := writeProfileValue(dir, "other.row", "kept"); err != nil {
		t.Fatal(err)
	}
	if err := WriteDesktopPlacesSetting(dir, "minClusterChats", json.RawMessage("8")); err != nil {
		t.Fatal(err)
	}
	if err := WriteDesktopPlacesSetting(dir, "autoFile", json.RawMessage("true")); err != nil {
		t.Fatal(err)
	}
	p := DesktopPlacesPolicy(dir)
	if p.MinClusterChats != 8 || !p.AutoFile {
		t.Fatalf("%+v", p)
	}
	if v, ok := DesktopPlacesSetting(dir, "minClusterChats"); !ok || !v.Chosen || v.Value != 8 {
		t.Fatalf("%+v", v)
	}
	if err := WriteDesktopPlacesSetting(dir, "minClusterChats", json.RawMessage("null")); err != nil {
		t.Fatal(err)
	}
	if DesktopPlacesPolicy(dir).MinClusterChats != 5 {
		t.Fatal("reset did not return to the default")
	}
	if got, ok := persistedString(dir, "other.row"); !ok || got != "kept" {
		t.Fatal("another row was lost")
	}
}

func TestAPlacesWriteRefusesWhatThePolicyCannotHold(t *testing.T) {
	dir := t.TempDir()
	for _, bad := range [][2]string{{"minClusterChats", "1"}, {"autoFile", `"yes"`}, {"noSuch", "1"}, {"maxAiDepth", "2.5"}} {
		if WriteDesktopPlacesSetting(dir, bad[0], json.RawMessage(bad[1])) == nil {
			t.Errorf("accepted %v", bad)
		}
	}
	if DesktopPlacesPolicy(dir) != placegraph.DefaultRecommendPolicy() {
		t.Fatal("a refused write changed the policy")
	}
}

// A file edited by hand to hold nonsense is held to the bounds, not obeyed.
func TestAHandEditedPlacesRowIsNotTrusted(t *testing.T) {
	dir := t.TempDir()
	_ = writeProfileValue(dir, desktopPlacesKey("filingCallsPerDay"), -10)
	_ = writeProfileValue(dir, desktopPlacesKey("autoFile"), "on")
	p := DesktopPlacesPolicy(dir)
	if p.FilingCallsPerDay != placegraph.DefaultRecommendPolicy().FilingCallsPerDay || p.AutoFile {
		t.Fatalf("%+v", p)
	}
}
