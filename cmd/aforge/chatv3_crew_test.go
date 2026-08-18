package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/roles"
)

// THE CREW IS LIVE, and this is the claim: a change made while a session is open
// is honored by the NEXT auxiliary call, not by the next launch.
//
// It used to be resolved once at boot, on the argument that two calls in one
// conversation must not answer to different settings. /crew is what makes that
// the wrong way round — somebody who types `/crew max` because the planner is not
// thinking hard enough has said something about the run they are about to start.

func TestAMidSessionCrewChangeIsHonoredByTheNextCall(t *testing.T) {
	dir := t.TempDir()
	source, err := v3RolesSource(t.TempDir(), dir)
	if err != nil {
		t.Fatalf("v3RolesSource: %v", err)
	}

	// The shipped crew, resolved once. Everything after this is a change made
	// under a source that is already in use.
	before, err := roles.Resolve(roles.Source(source), roles.RolePlanner, "vendor/conversation")
	if err != nil {
		t.Fatal(err)
	}
	if before != "moonshotai/kimi-k3" {
		t.Fatalf("the planner started on %q", before)
	}

	if err := config.ApplyCrew(dir, config.CrewFrugal); err != nil {
		t.Fatalf("setting the crew to frugal: %v", err)
	}
	after, err := roles.Resolve(roles.Source(source), roles.RolePlanner, "vendor/conversation")
	if err != nil {
		t.Fatal(err)
	}
	if after != config.DefaultHighModel {
		t.Fatalf("after /crew frugal the planner resolves to %q, want %q", after, config.DefaultHighModel)
	}

	// A single class answered by hand is seen the same way, and so is a pin —
	// every persisted write goes through the one writer the counter sits on.
	registry := config.NewSettings(config.SettingsOptions{
		ProfileDir: dir,
		ModelValue: func(slot string) string { return slot + "/model" },
		SetModel:   func(string, string) error { return nil },
		SplitPct:   func() int { return 0 },
	})
	row, found := registry.Row(config.KeyTierMastermindModel)
	if !found {
		t.Fatalf("no settings row %q", config.KeyTierMastermindModel)
	}
	if err := row.Apply("vendor/thinker:high"); err != nil {
		t.Fatal(err)
	}
	call, err := roles.ResolveCall(roles.Source(source), roles.RolePlanner, "vendor/conversation")
	if err != nil {
		t.Fatal(err)
	}
	if call.Model != "vendor/thinker" || call.Effort != "high" {
		t.Fatalf("after a hand-set mastermind the planner resolves to %+v", call)
	}

	pins, found := registry.Row(config.KeyModelRoles)
	if !found {
		t.Fatalf("no settings row %q", config.KeyModelRoles)
	}
	if err := pins.Apply("planner:vendor/pinned"); err != nil {
		t.Fatal(err)
	}
	pinned, err := roles.Resolve(roles.Source(source), roles.RolePlanner, "vendor/conversation")
	if err != nil {
		t.Fatal(err)
	}
	if pinned != "vendor/pinned" {
		t.Fatalf("after a pin the planner resolves to %q", pinned)
	}
}

// THE SNAPSHOT SURVIVES A ROW SOMEBODY HAS JUST BROKEN. A pins row edited into
// something unparseable must not silently un-pin every role — that would move
// work onto another model without saying so — so the last reading that parsed
// keeps answering, and the failure is what the next settings read shows them.
func TestABrokenRowLeavesTheLastGoodCrewInPlace(t *testing.T) {
	dir := v3Profile(t, map[string]any{"models.roles": "planner:vendor/pinned"})
	source, err := v3RolesSource(t.TempDir(), dir)
	if err != nil {
		t.Fatalf("v3RolesSource: %v", err)
	}
	if model, _ := roles.Resolve(roles.Source(source), roles.RolePlanner, "vendor/conversation"); model != "vendor/pinned" {
		t.Fatalf("the planner started on %q", model)
	}

	// The registry refuses a malformed pins row, so the only way one reaches the
	// file is a hand edit — which is exactly the case this guards. The generation
	// is then moved by an unrelated write, so the source does rebuild and does
	// meet the broken row.
	handEdit(t, dir, "models.roles", "planner")
	if err := config.ApplyCrew(dir, config.CrewMax); err != nil {
		t.Fatal(err)
	}
	if model, _ := roles.Resolve(roles.Source(source), roles.RolePlanner, "vendor/conversation"); model != "vendor/pinned" {
		t.Fatalf("a failed rebuild lost the pin and resolved %q", model)
	}

	// And once the row parses again, the new crew is picked up — a refusal is not
	// a latch.
	handEdit(t, dir, "models.roles", "")
	if err := config.ApplyCrew(dir, config.CrewMax); err != nil {
		t.Fatal(err)
	}
	want, _ := config.CrewModels(config.CrewMax)
	if model, _ := roles.Resolve(roles.Source(source), roles.RolePlanner, "vendor/conversation"); model != "moonshotai/kimi-k3" {
		t.Fatalf("after the row parsed again the planner resolves to %q, want the max crew's %q",
			model, want[config.ModelTierMastermind])
	}
}

// handEdit writes one key into a profile's config.json the way a person editing
// the file does — around the registry, which would have refused it.
func handEdit(t *testing.T, dir, key, value string) {
	t.Helper()
	path := filepath.Join(dir, "config.json")
	values := map[string]any{}
	raw, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(raw, &values); err != nil {
			t.Fatal(err)
		}
	}
	values[key] = value
	encoded, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
}

// CONCURRENT TURNS READ IT AT THE SAME TIME. A task fans out several nodes and
// each resolves its own worker model; the reflex pair resolves twice a turn. Run
// under -race this is the assertion that the snapshot is not torn.
func TestTheCrewSourceIsSafeUnderConcurrentTurns(t *testing.T) {
	dir := t.TempDir()
	source, err := v3RolesSource(t.TempDir(), dir)
	if err != nil {
		t.Fatalf("v3RolesSource: %v", err)
	}
	var wait sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			for round := 0; round < 20; round++ {
				if _, err := roles.Resolve(roles.Source(source), roles.RoleWorker, "vendor/conversation"); err != nil {
					t.Error(err)
					return
				}
			}
		}(worker)
	}
	// And one of them is a person pressing enter on the crew row while the rest
	// are resolving.
	wait.Add(1)
	go func() {
		defer wait.Done()
		for _, preset := range []string{config.CrewFrugal, config.CrewMax, config.CrewBalanced} {
			if err := config.ApplyCrew(dir, preset); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	wait.Wait()
}
