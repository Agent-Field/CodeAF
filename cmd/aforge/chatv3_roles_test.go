package main

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/roles"
)

// THE LADDER THE RUNNING SESSION'S AUXILIARY CALLS GO THROUGH.
//
// The settings panel builds a source of its own for what is on screen
// (internal/tui3's rolesSource); this is the one every actual call resolves
// against, and the two had drifted: the panel mapped all the tiers and the
// door mapped two, so a reflex — a call made TWICE EVERY TURN — resolved to the
// conversation's own model on every machine. That is not thrift misconfigured;
// it is the most expensive model in the build answering the cheapest question
// in it.
func TestTheDoorsRoleLadderCarriesEveryTier(t *testing.T) {
	dir := t.TempDir()
	source, err := v3RolesSource(t.TempDir(), dir)
	if err != nil {
		t.Fatalf("v3RolesSource: %v", err)
	}

	// An untouched profile: every class ships pointed at a model (the crew, in
	// internal/config's crew.go), so nothing here resolves to the conversation.
	reflex, err := roles.Resolve(roles.Source(source), roles.RoleReflex, "vendor/conversation")
	if err != nil {
		t.Fatalf("resolving the reflex role: %v", err)
	}
	if reflex != config.DefaultReflexModel {
		t.Fatalf("the reflex role resolves to %q, want the reflex tier's own %q", reflex, config.DefaultReflexModel)
	}

	// And a person who pinned the tier is obeyed.
	registry := config.NewSettings(config.SettingsOptions{
		ProfileDir: dir,
		ModelValue: func(slot string) string { return slot + "/model" },
		SetModel:   func(string, string) error { return nil },
		SplitPct:   func() int { return 0 },
	})
	row, found := registry.Row(config.KeyTierReflexModel)
	if !found {
		t.Fatalf("no settings row %q", config.KeyTierReflexModel)
	}
	if err := row.Apply("vendor/tiny"); err != nil {
		t.Fatalf("writing the reflex tier: %v", err)
	}
	source, err = v3RolesSource(t.TempDir(), dir)
	if err != nil {
		t.Fatalf("v3RolesSource: %v", err)
	}
	reflex, err = roles.Resolve(roles.Source(source), roles.RoleReflex, "vendor/conversation")
	if err != nil {
		t.Fatalf("resolving the reflex role: %v", err)
	}
	if reflex != "vendor/tiny" {
		t.Fatalf("the reflex role resolves to %q after the tier was pinned to vendor/tiny", reflex)
	}

	// EVERY CLASS IS WIRED, each to its own shipped model. The failure this holds
	// shut is a class the door forgot: a role on it would resolve to the
	// conversation's model, which is the mistake the reflex arm was added for.
	for _, c := range []struct {
		role roles.Role
		want string
	}{
		{roles.RoleTitle, config.DefaultLowModel},
		{roles.RoleWorker, config.DefaultWorkerModel},
		{roles.RoleCompaction, config.DefaultHighModel},
		// The mastermind's value carries a level, and Resolve hands back the id
		// alone — a colon in a model field is a request for a model nobody serves.
		{roles.RolePlanner, "z-ai/glm-5.3"},
		{roles.RoleDesigner, "z-ai/glm-5.3"},
	} {
		model, err := roles.Resolve(roles.Source(source), c.role, "vendor/conversation")
		if err != nil || model != c.want {
			t.Errorf("the %s role resolves to %q (%v), want its class's %q", c.role, model, err, c.want)
		}
	}
	// And the level reaches the caller as its own half.
	call, err := roles.ResolveCall(roles.Source(source), roles.RolePlanner, "vendor/conversation")
	if _, level := roles.SplitEffort(config.DefaultMastermindModel); err != nil || call.Effort != level {
		t.Fatalf("the planner resolved to %+v (%v), want the shipped level %q", call, err, level)
	}
}

// MEMORY OFF IS A DOOR THAT OPENS NO BRAIN. That is what makes "no block and no
// calls" a property of the wiring rather than a branch every caller has to
// remember (internal/session's memory.go states the law).
func TestTheDoorOpensNoBrainWhenMemoryIsOff(t *testing.T) {
	dir := t.TempDir()
	registry := config.NewSettings(config.SettingsOptions{
		ProfileDir: dir,
		ModelValue: func(slot string) string { return slot + "/model" },
		SetModel:   func(string, string) error { return nil },
		SplitPct:   func() int { return 0 },
	})
	row, found := registry.Row(config.KeyMemoryEnabled)
	if !found {
		t.Fatalf("no settings row %q", config.KeyMemoryEnabled)
	}
	if err := row.Apply(config.MemoryOff); err != nil {
		t.Fatalf("turning memory off: %v", err)
	}
	if brain := v3Memory(dir); brain != nil {
		_ = brain.Close()
		t.Fatal("the door opened a brain with memory turned off")
	}
}
