package main

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/session"
)

// everyRolePinned is a profile that pins EVERY registered role, and every tier
// (the small-jobs auxiliary pin is the low tier row), to a model other than the
// one model, so any role a door lets through shows up as a stray model.
func everyRolePinned(t *testing.T, rows map[string]any) string {
	t.Helper()
	var pins []string
	for _, role := range roles.Registered() {
		pins = append(pins, string(role)+":pinned/"+string(role))
	}
	rows["models.roles"] = strings.Join(pins, "\n")
	rows[config.KeyTierLowModel] = "auxiliary/pin"
	return factoryPolicyProfile(t, rows)
}

// assertEveryTextRoleOn checks the routes a session built from cfg would take
// for every registered text role. The registry is enumerated, so a role
// registered tomorrow is held to the same promise.
func assertEveryTextRoleOn(t *testing.T, cfg session.Config, want string) {
	t.Helper()
	got := map[string]string{}
	for _, route := range session.ConfigRoutes(cfg) {
		got[route.Role] = route.Model
	}
	for _, role := range roles.Registered() {
		if roles.IsMedia(role) {
			continue
		}
		if model := got[string(role)]; model != want {
			t.Errorf("%s resolves to %q; want the one model %q", role, model, want)
		}
	}
}

// --one-model: every registered role rides the launch model, whatever the
// profile pins, and even when a door hands the session the profile's rows.
func TestOneModelFlagHoldsForEveryRegisteredRole(t *testing.T) {
	profile := everyRolePinned(t, map[string]any{})
	cfg, err := applyV3Governance(session.Config{Workspace: t.TempDir(), Model: savedOneModel}, profile, false, true)
	if err != nil {
		t.Fatal(err)
	}
	assertEveryTextRoleOn(t, cfg, savedOneModel)

	source, err := v3RolesSource(cfg.Workspace, profile)
	if err != nil {
		t.Fatal(err)
	}
	cfg.RolesSource = source
	assertEveryTextRoleOn(t, cfg, savedOneModel)
}

// The app's saved one-model setting (models.tasks.mode=one) holds the same way,
// over every role pin and the auxiliary pin.
func TestSavedOneModelSettingHoldsForEveryRegisteredRole(t *testing.T) {
	profile := everyRolePinned(t, map[string]any{
		config.KeyTaskModelsMode: "one",
		config.KeyTaskModelsOne:  savedOneModel,
	})
	cfg, err := applyV3Governance(session.Config{Workspace: t.TempDir(), Model: "conversation/model"}, profile, false, false)
	if err != nil {
		t.Fatal(err)
	}
	assertEveryTextRoleOn(t, cfg, savedOneModel)
}
