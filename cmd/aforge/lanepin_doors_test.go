package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	homepkg "github.com/Agent-Field/aforge-v2/internal/home"
	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
	"github.com/Agent-Field/aforge-v2/internal/provider"
)

const pinnedDoorLane = "brass"

// pinnedDoorHome gives one door a profile and router of its own. Both lane
// knobs and the shared belief are restored because all three outlive a single
// client in this process.
func pinnedDoorHome(t *testing.T) (*lanestub.Server, string) {
	t.Helper()
	beforePin := provider.CurrentLanePin()
	beforeGuard := provider.LaneGuardOn()
	t.Cleanup(func() {
		provider.SetLanePin(beforePin)
		provider.SetLaneGuard(beforeGuard)
	})
	lanes.Default().Reset()
	t.Cleanup(lanes.Default().Reset)

	state := t.TempDir()
	profile := filepath.Join(state, "profile")
	t.Setenv(homepkg.EnvVar, state)
	t.Setenv(config.ProfileDirEnv, profile)
	t.Setenv("OPENROUTER_API_KEY", "test-key")

	model := "test/pinned-headless-door"
	stub := lanestub.New(model, routedLanes()...)
	t.Cleanup(stub.Close)
	t.Setenv("AFORGE_BASE_URL", stub.URL())
	t.Setenv(config.ModelEnv, model)
	t.Setenv(config.PlanModelEnv, model)
	if err := config.SetLane(profile, config.LaneSlotTalk, pinnedDoorLane); err != nil {
		t.Fatal(err)
	}
	return stub, model
}

// firstAskCarriedPin asserts C2 at the wire, without treating the lane that
// happened to answer as evidence for what the request asked for.
func firstAskCarriedPin(t *testing.T, stub *lanestub.Server) {
	t.Helper()
	asks := stub.Asks()
	if len(asks) == 0 {
		t.Fatal("the headless door sent no completion request")
	}
	if got := asks[0].Only; len(got) != 1 || got[0] != pinnedDoorLane {
		t.Fatalf("the first request carried provider.only=%v, want [%s]", got, pinnedDoorLane)
	}
}

// TestAPinnedHomeSendsTheDemandOnTheFirstHeadlessRequest is C2 for `aforge do`.
// The outcome is deliberately irrelevant: the contract is the first request
// the real door put on the wire.
func TestAPinnedHomeSendsTheDemandOnTheFirstHeadlessRequest(t *testing.T) {
	stub, _ := pinnedDoorHome(t)
	var stdout, stderr strings.Builder
	_ = doErrand(doRequest{
		task: "write one sentence", timeout: 10 * time.Second, yesSpend: true,
		stdout: &stdout, stderr: &stderr,
	})
	firstAskCarriedPin(t, stub)
}

// TestEveryHeadlessDoorSendsThePinOnItsFirstRequest is C2 for the other first
// completion doors named by the contract. Each subtest ignores how its model's
// deliberately generic reply is parsed and judges only what the stub received.
func TestEveryHeadlessDoorSendsThePinOnItsFirstRequest(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, model string) error
	}{
		{
			name: "aforge exec",
			run: func(t *testing.T, model string) error {
				return runExec([]string{
					"answer with one sentence", "--dir", t.TempDir(), "--max-turns", "1",
					"--token-budget", "100", "--model", model, "--json",
				})
			},
		},
		{
			name: "aforge plan new",
			run: func(_ *testing.T, model string) error {
				return runPlanNew("plan new", []string{
					"make a one-step plan", "--passes", "off", "--model", model,
					"--plan-model", model, "--json",
				})
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stub, model := pinnedDoorHome(t)
			_ = test.run(t, model)
			firstAskCarriedPin(t, stub)
		})
	}
}

// TestNoDoorResolvesTheLaneRowForItself is C3's one-reading law. The TUI may
// still read LanePinned and LaneOpenRouter to DRAW a row in laneNow, armLanes,
// the picker and the settings panel; what no door may do is construct the
// transport's LanePin instead of asking internal/config for it.
func TestNoDoorResolvesTheLaneRowForItself(t *testing.T) {
	dirs := []string{".", filepath.Join("..", "..", "internal", "tui3")}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(dir, name)
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(file, func(node ast.Node) bool {
				selector, ok := node.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "LanePin" {
					return true
				}
				pkg, ok := selector.X.(*ast.Ident)
				if ok && pkg.Name == "provider" {
					t.Errorf("%s resolves a provider.LanePin for itself; doors must call config.LanePinAt or config.InstallLaneRows", path)
				}
				return true
			})
		}
	}

	chat, err := os.ReadFile("chatv3.go")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(chat), "config.InstallLaneRows(profileDir)"); got != 1 {
		t.Fatalf("chatv3.go installs the shared lane rows %d times, want exactly 1", got)
	}
	tui, err := os.ReadFile(filepath.Join("..", "..", "internal", "tui3", "lanes.go"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(tui), "provider.RepinLane(config.LanePinAt(a.profileDir, slot))"); got != 1 {
		t.Fatalf("the picker's person-act entrance uses the shared lane reading %d times, want exactly 1", got)
	}
}

// TestEveryDoorReachesTheLaneRowsThroughTheProfileLoad is C1's breadth, and it
// is the test that keeps this fix from rotting.
//
// THE INSTALL IS INVISIBLE FROM A DOOR ON PURPOSE. A door does not name the
// lane row at all any more; it gets one because loading the profile installs it
// ([config.InstallLaneRows], called from internal/config's load). That is the
// whole of why `aforge do` was sending its first request with nobody's answer
// on it: the pin was resolved at one door and every other door was written
// without knowing there was anything to resolve.
//
// So the law is not "call the installer" — it is "load the profile", which
// every door already had to do to find a key and a model. A door written
// tomorrow that builds a client from something other than a loaded profile is
// the next #485, and this fails on the day it lands rather than on the day
// somebody measures where their work went.
func TestEveryDoorReachesTheLaneRowsThroughTheProfileLoad(t *testing.T) {
	doors := map[string]string{
		"chat.go":            "the brain `aforge do` builds",
		"exec.go":            "aforge exec",
		"main.go":            "aforge plan new and aforge plan revise",
		"run.go":             "aforge run and aforge plan run",
		"subharness_run.go":  "aforge run subharness",
		"wake.go":            "aforge wake, the resident's own pass",
		"chatv3_standing.go": "the standing pass an aforge window and `aforge tick` both run",
		"chatv3_process.go":  "the conversation",
	}
	for name, door := range doors {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		source := string(raw)
		// LoadKeyless is the same function underneath and installs the same
		// rows; the standing pass uses it because most of its passes have
		// nothing to spend and need no key.
		if !strings.Contains(source, "config.Load()") && !strings.Contains(source, "config.LoadKeyless()") {
			t.Errorf("%s opens %s without loading the profile, so nothing installs the lane row the person set",
				name, door)
		}
	}
}
