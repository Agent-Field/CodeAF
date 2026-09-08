package config

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// preserveLaneRows puts both process-wide rows back after a test. A leaked pin
// or guard would silently change every provider request made later in this
// package's process.
func preserveLaneRows(t *testing.T) {
	t.Helper()
	pin := provider.CurrentLanePin()
	guard := provider.LaneGuardOn()
	t.Cleanup(func() {
		provider.SetLanePin(pin)
		provider.SetLaneGuard(guard)
	})
}

// loadLaneProfile drives the same profile-loading path every command uses.
func loadLaneProfile(t *testing.T, profileDir string) Config {
	t.Helper()
	t.Setenv(ProfileDirEnv, profileDir)
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	loaded, err := Load()
	if err != nil {
		t.Fatalf("load the profile: %v", err)
	}
	return loaded
}

// TestLoadingAProfilePinsTheLaneItsRowNames is C1: loading the profile is the
// common door, so the machine named there must already be in force afterwards.
func TestLoadingAProfilePinsTheLaneItsRowNames(t *testing.T) {
	preserveLaneRows(t)
	dir := t.TempDir()
	if err := SetLane(dir, LaneSlotTalk, "quicksilver"); err != nil {
		t.Fatal(err)
	}

	loaded := loadLaneProfile(t, dir)
	if loaded.ProfileDir != dir {
		t.Fatalf("loaded profile %q, want %q", loaded.ProfileDir, dir)
	}
	if got := provider.CurrentLanePin().Lane; got != "quicksilver" {
		t.Fatalf("loaded lane %q, want the machine the profile names", got)
	}
}

// TestTheThreeStatesOfTheLaneRowAreTheThreeStatesOfThePin is C3. Each case
// enters through Load rather than calling the resolver directly, because that
// is the path whose answer every door receives.
func TestTheThreeStatesOfTheLaneRowAreTheThreeStatesOfThePin(t *testing.T) {
	preserveLaneRows(t)
	tests := []struct {
		name   string
		lane   string
		write  bool
		borrow bool
		want   provider.LanePin
	}{
		{name: "an unwritten row is auto"},
		{name: "auto leaves the choice to the belief", lane: LaneAuto, write: true},
		{name: "openrouter demands no lane", lane: LaneOpenRouter, write: true, want: provider.LanePin{OpenRouter: true}},
		{name: "a machine name is a strict pin", lane: "brass", write: true, want: provider.LanePin{Lane: "brass"}},
		{name: "the borrow row travels with a machine name", lane: "molasses", write: true, borrow: true, want: provider.LanePin{Lane: "molasses", Borrow: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			if test.write {
				if err := SetLane(dir, LaneSlotTalk, test.lane); err != nil {
					t.Fatal(err)
				}
			}
			if test.borrow {
				if err := SetLaneBorrow(dir, LaneSlotTalk, true); err != nil {
					t.Fatal(err)
				}
			}
			loadLaneProfile(t, dir)
			if got := provider.CurrentLanePin(); got != test.want {
				t.Fatalf("loaded pin = %+v, want %+v", got, test.want)
			}
		})
	}
}

// TestTheGuardRowReachesEveryDoorAndNotOnlyTheChat is C4: the guard is part of
// the profile posture installed by the common load path.
func TestTheGuardRowReachesEveryDoorAndNotOnlyTheChat(t *testing.T) {
	preserveLaneRows(t)
	dir := t.TempDir()
	if err := writeProfileValue(dir, KeyLaneGuard, false); err != nil {
		t.Fatal(err)
	}

	loadLaneProfile(t, dir)
	if provider.LaneGuardOn() {
		t.Fatal("loading lane.guard=false left the process-wide guard on")
	}
}

// TestTheLaneInstallerUsesTheResolversEntranceAndNotThePersons is C5, and it
// is named for what it can actually see. The retirement a refusal writes is
// private to internal/provider, so the BEHAVIOUR — loading the same row twice
// forgets nothing the wire said — is held there, by that package's own tests.
// What this package owns is the entrance it goes in by, and that is a fact
// about this source: [InstallLaneRows] is a resolver reading a row and handing
// the answer down, so it calls [provider.SetLanePin], which forgets only a row
// that CHANGED. [provider.RepinLane] forgets unconditionally and belongs to a
// person choosing a machine in the picker; an installer that reached for it
// would clear a retirement every five minutes when the standing ticker rebuilt
// the posture, which is the exact defect PR #533 closed.
func TestTheLaneInstallerUsesTheResolversEntranceAndNotThePersons(t *testing.T) {
	raw, err := os.ReadFile("settings.go")
	if err != nil {
		t.Fatal(err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), "settings.go", raw, 0)
	if err != nil {
		t.Fatal(err)
	}
	var installer *ast.FuncDecl
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == "InstallLaneRows" {
			installer = function
			break
		}
	}
	if installer == nil {
		t.Fatal("settings.go has no InstallLaneRows resolver")
	}
	set, repin := 0, 0
	ast.Inspect(installer.Body, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := selector.X.(*ast.Ident)
		if !ok || pkg.Name != "provider" {
			return true
		}
		switch selector.Sel.Name {
		case "SetLanePin":
			set++
		case "RepinLane":
			repin++
		}
		return true
	})
	if set != 1 || repin != 0 {
		t.Fatalf("InstallLaneRows calls SetLanePin %d times and RepinLane %d times; loading the same row must use the resolver's entrance exactly once", set, repin)
	}
}
