package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// A text role the engine registers but no desktop role owns would run on the
// conversation's model with no way for the person to see or change it, so the
// settings page would be quietly incomplete.
func TestEveryRegisteredTextRoleBelongsToADesktopRole(t *testing.T) {
	owned := map[roles.Role]bool{}
	for _, role := range config.DesktopRoles() {
		for _, engine := range role.Engine {
			owned[engine] = true
		}
	}
	// Vision reads images and needs a model that can see, so it stays out of
	// the page until the page can offer only capable models.
	skipped := map[roles.Role]bool{roles.RoleVision: true}
	for _, role := range roles.Registered() {
		if !owned[role] && !skipped[role] {
			t.Errorf("engine role %q is owned by no desktop role", role)
		}
	}
}
