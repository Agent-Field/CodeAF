package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// THE FACTORY'S READ AND LOOK RIDE THE PAGE AI'S ROLES: a pin on
// insight_factory_read moves the read and not the look, a pin on the family
// moves both, and a profile with neither runs on the page AI's default
// (the small-work seat when that default's provider is not connected).
func TestTheFactoryReadAndLookFollowTheirInsightRoles(t *testing.T) {
	model := func(profile string, role roles.Role) string {
		t.Helper()
		m, err := factoryRoleModel(config.Config{ProfileDir: profile, Model: "profile/default"}, role)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	read := factoryPolicyProfile(t, map[string]any{"models.roles": "insight_factory_read:pinned/read"})
	if got := model(read, roles.RoleInsightFactoryRead); got != "pinned/read" {
		t.Errorf("read with its own pin = %q", got)
	}
	if got := model(read, roles.RoleInsightFactoryScout); got == "pinned/read" {
		t.Errorf("the look took the read's pin")
	}
	family := factoryPolicyProfile(t, map[string]any{"models.roles": "insight:pinned/insight"})
	for _, role := range []roles.Role{roles.RoleInsightFactoryRead, roles.RoleInsightFactoryScout} {
		if got := model(family, role); got != "pinned/insight" {
			t.Errorf("%s with the family pinned = %q", role, got)
		}
	}
	none := factoryPolicyProfile(t, map[string]any{"models.roles": ""})
	if got := model(none, roles.RoleInsightFactoryScout); got != config.InsightDefault.Model {
		t.Errorf("unpinned look = %q, want the page AI's default", got)
	}
}
