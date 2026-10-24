package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// THE CREW IS LIVE, and this is the claim: a change made while a session is open
// is honored by the NEXT auxiliary call, not by the next launch.
//
// It used to be resolved once at boot, on the argument that two calls in one
// conversation must not answer to different settings. /crew is what makes that
// the wrong way round — somebody who types `/crew pin planner …` because the
// planner is not thinking hard enough has said something about the run they
// are about to start.

func TestAMidSessionCrewChangeIsHonoredByTheNextCall(t *testing.T) {
	dir := t.TempDir()
	source, err := v3RolesSource(t.TempDir(), dir)
	if err != nil {
		t.Fatalf("v3RolesSource: %v", err)
	}

	// A pin written under a source that is already in use.
	if err := config.SetCrewPin(dir, crewroute.Planner, "vendor/first-planner"); err != nil {
		t.Fatalf("pinning the planner: %v", err)
	}
	after, err := roles.Resolve(roles.Source(source), roles.RolePlanner, "vendor/conversation")
	if err != nil {
		t.Fatal(err)
	}
	if after != "vendor/first-planner" {
		t.Fatalf("after /crew pin the planner resolves to %q, want vendor/first-planner", after)
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
		t.Fatalf("after a hand-set planner row the planner resolves to %+v", call)
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
	if err := config.SetCrewPin(dir, crewroute.Planner, "vendor/max-planner"); err != nil {
		t.Fatal(err)
	}
	if model, _ := roles.Resolve(roles.Source(source), roles.RolePlanner, "vendor/conversation"); model != "vendor/pinned" {
		t.Fatalf("a failed rebuild lost the pin and resolved %q", model)
	}

	// And once the row parses again, the new crew is picked up — a refusal is not
	// a latch.
	handEdit(t, dir, "models.roles", "")
	if err := config.SetCrewPin(dir, crewroute.Planner, "vendor/max-planner"); err != nil {
		t.Fatal(err)
	}
	if model, _ := roles.Resolve(roles.Source(source), roles.RolePlanner, "vendor/conversation"); model != "vendor/max-planner" {
		t.Fatalf("after the row parsed again the planner resolves to %q, want the pinned vendor/max-planner", model)
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
		for _, pin := range []string{"vendor/a", "vendor/b", "vendor/c"} {
			if err := config.SetCrewPin(dir, crewroute.Worker, pin); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	wait.Wait()
}

// The surface writes credit records while the engine seats the crew. External
// writes do not move this process's settings generation. The live source must
// notice the threshold record, but only explicit free-route opt-in may change
// its model; low means <= $0.50, not proof that the account is empty.
func TestACreditRecordWrittenByAnotherProcessReseatsTheLiveCrew(t *testing.T) {
	for _, freeAllowed := range []bool{false, true} {
		name := "free routes off keeps paid helpers"
		if freeAllowed {
			name = "explicit opt-in follows external balance"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			const key = "sk-or-v1-crewtest"
			t.Setenv(config.APIKeyEnv, key)
			if freeAllowed {
				if err := config.SetCrewFreeRoutes(dir, true); err != nil {
					t.Fatal(err)
				}
			}
			source, err := v3RolesSource(t.TempDir(), dir)
			if err != nil {
				t.Fatalf("v3RolesSource: %v", err)
			}
			reflex := roles.TierKey(roles.TierReflex)
			paid, _ := source(reflex)
			if paid != config.DefaultReflexModel {
				t.Fatalf("a profile with no record seated reflex on %q, want %q", paid, config.DefaultReflexModel)
			}
			generation := config.SettingsGeneration()
			writeExternalRecord := func(low bool, readAt string) {
				t.Helper()
				record, err := json.Marshal(map[string]any{"low": low, "known": true, "key": config.CreditsKeyPrint(key), "read_at": readAt})
				if err != nil {
					t.Fatal(err)
				}
				// Use the external writer's file path, never this process's settings API.
				if err := os.WriteFile(filepath.Join(dir, "credits.json"), record, 0o600); err != nil {
					t.Fatal(err)
				}
				if config.SettingsGeneration() != generation {
					t.Fatal("external credit record changed this process's settings generation")
				}
			}
			writeExternalRecord(true, "2026-09-24T21:44:22Z")
			if !config.CreditsLowAt(dir) {
				t.Fatal("external low threshold record was not visible")
			}
			current, _ := source(reflex)
			if freeAllowed {
				if current == paid || current != config.TierModelAt(dir, config.ModelTierReflex) || !strings.HasSuffix(current, ":free") {
					t.Fatalf("opted-in live crew ignored external low reading: %q", current)
				}
			} else {
				if current != paid {
					t.Fatalf("a low threshold record silently enabled free reflex: %q", current)
				}
				if low, _ := source(roles.TierKey(roles.TierLow)); strings.HasSuffix(low, ":free") {
					t.Fatalf("a low threshold record silently enabled free small-work: %q", low)
				}
			}
			writeExternalRecord(false, "2026-09-24T21:50:00Z")
			if back, _ := source(reflex); back != paid {
				t.Fatalf("after a top-up recorded elsewhere the live crew kept reflex on %q", back)
			}
			if config.CrewFreeRoutesAt(dir) != freeAllowed {
				t.Fatal("external balance read changed the person's free-route choice")
			}
		})
	}
}
