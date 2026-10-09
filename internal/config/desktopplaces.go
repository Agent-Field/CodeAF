package config

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/Agent-Field/codeaf/internal/placegraph"
)

// The desktop's Places organization settings: when codeaf offers to file a
// chat, offer a new place, or merge two, and how much it may spend doing so.
//
// THE TABLE OF SETTINGS IS placegraph's (PolicyFields), not this file's: the
// names, defaults and bounds live beside the code that obeys them, so the
// settings page cannot describe a limit the recommender does not enforce. This
// file only stores the person's choices, one row each, through the profile's
// one writer, and reads them back over the defaults.

func desktopPlacesKey(key string) string { return "desktop.places." + key }

// PlacesSettingView is one setting as the settings page reads it.
type PlacesSettingView struct {
	placegraph.PolicyField
	Value any `json:"value"`
	// Chosen is false while the setting is on its default.
	Chosen bool `json:"chosen"`
}

// DesktopPlacesPolicy is the policy the recommender runs on: every saved
// choice over the defaults, held to the bounds. A saved value the policy
// cannot hold (a hand-edited file) is ignored rather than trusted.
func DesktopPlacesPolicy(profileDir string) placegraph.RecommendPolicy {
	p := placegraph.DefaultRecommendPolicy()
	for _, f := range placegraph.PolicyFields() {
		if raw, ok := persistedValue(profileDir, desktopPlacesKey(f.Key)); ok {
			_ = p.Set(f.Key, raw)
		}
	}
	return p.Normalized()
}

// DesktopPlacesSettings lists every setting in page order with its value.
func DesktopPlacesSettings(profileDir string) []PlacesSettingView {
	p := DesktopPlacesPolicy(profileDir)
	var out []PlacesSettingView
	for _, f := range placegraph.PolicyFields() {
		v, _ := p.Value(f.Key)
		_, chosen := persistedValue(profileDir, desktopPlacesKey(f.Key))
		out = append(out, PlacesSettingView{PolicyField: f, Value: v, Chosen: chosen && v != f.Default})
	}
	return out
}

// DesktopPlacesSetting is one setting by key.
func DesktopPlacesSetting(profileDir, key string) (PlacesSettingView, bool) {
	for _, v := range DesktopPlacesSettings(profileDir) {
		if v.Key == key {
			return v, true
		}
	}
	return PlacesSettingView{}, false
}

// WriteDesktopPlacesSetting records one choice. An empty or null value puts
// the setting back on its default. A value the policy refuses is refused here
// too, and nothing is written.
func WriteDesktopPlacesSetting(profileDir, key string, raw json.RawMessage) error {
	p := placegraph.DefaultRecommendPolicy()
	if _, ok := p.Value(key); !ok {
		return fmt.Errorf("%w: %q", placegraph.ErrInvalid, key)
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return writeProfileValues(profileDir, map[string]any{desktopPlacesKey(key): removeProfileKey})
	}
	if err := p.Set(key, raw); err != nil {
		return err
	}
	v, _ := p.Value(key)
	return writeProfileValues(profileDir, map[string]any{desktopPlacesKey(key): v})
}
