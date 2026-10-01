package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/standing"
)

// THE STORE IS UNDER THE STATE ROOT AND NOWHERE ELSE, so CODEAF_HOME moves the
// ambient side with everything else it moves. A second spelling of this path
// would be two stores with half a person's reminders in each.
func TestStandingLivesUnderTheStateRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	t.Setenv("CODEAF_HOME", root)
	if got, want := v3StandingRoot(), home.Join("v3", "standing"); got != want {
		t.Fatalf("standing root = %q, want %q", got, want)
	}
	if !strings.HasPrefix(v3StandingRoot(), root) {
		t.Fatalf("standing root %q is outside the state root %q", v3StandingRoot(), root)
	}
}

// THE SEAM IS BUILT FOR A CONVERSATION AND THE STORE IS THE ONE AT THAT PATH.
// A door that could not open it hands over nil, which every caller reads as the
// ambient side being off — the absence law, not a broken tool.
func TestStandingSeamOpensTheStoreAtThatPath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	t.Setenv("CODEAF_HOME", root)
	profile := t.TempDir()
	seam := v3Standing(profile)
	if seam == nil || seam.Store == nil {
		t.Fatal("the door built no standing seam")
	}
	if seam.Store.Root() != v3StandingRoot() {
		t.Fatalf("the seam's store is at %q, want %q", seam.Store.Root(), v3StandingRoot())
	}
	// The daily rail is the person's own daily budget row and never a second
	// number invented for this.
	if seam.DailyRail == nil {
		t.Fatal("the seam must read the current daily rail")
	}
	for _, budget := range []string{"5", "7"} {
		if err := os.WriteFile(filepath.Join(profile, "config.json"), []byte(`{"daily_budget_usd":`+budget+`}`), 0600); err != nil {
			t.Fatal(err)
		}
		if got, want := seam.DailyRail(), v3StandingDailyRail(profile); got != want || got == 0 {
			t.Fatalf("daily rail = %v, want %v", got, want)
		}
	}
}

// THE OFFER IS NEVER MADE WHILE THERE IS NO TIMER TO INSTALL. standingWatch is
// the core lane's seam and answers nil until it lands; a card that offered to
// keep checking with nothing behind the yes would be a promise this build
// cannot keep.

// ── the launch's repair ─────────────────────────────────────────────────────

// driftedTimer is a definition on disk, stood in for. Nothing here goes near
// this machine's launchd.
type driftedTimer struct {
	drift    standing.WatchDrift
	err      error
	installs int
	fail     error
}

func (d *driftedTimer) Repair(context.Context) (standing.WatchDrift, error) {
	if d.err != nil {
		return standing.WatchDrift{}, d.err
	}
	if !d.drift.Present || !d.drift.Stale {
		return standing.WatchDrift{}, nil
	}
	d.installs++
	return d.drift, d.fail
}

// A TIMER POINTING AT A PROGRAM THAT MOVED RUNS NOTHING, and nothing on screen
// could say so — the honest reading of it is `off`, which is what /settings
// would show. So the launch puts it back, quietly, and says one line in the
// standing log.
func TestTheLaunchPutsABackgroundCheckBackWhenItsProgramMoved(t *testing.T) {
	timer := &driftedTimer{drift: standing.WatchDrift{
		Present: true, Stale: true, Gone: true, Executable: "/old/bin/codeaf",
	}}
	line := repairBackgroundChecks(timer, true)
	if timer.installs != 1 {
		t.Fatalf("a drifted timer was installed %d times", timer.installs)
	}
	if !strings.Contains(line, "/old/bin/codeaf") || !strings.Contains(line, "installed it again") {
		t.Fatalf("the log line does not say what happened: %q", line)
	}
}

// AND IT ONLY EVER REPAIRS. A machine with no definition has never had one or
// had it turned off, and a row the person turned off is a machine left exactly
// as they left it — writing one for either would make the row a suggestion.
func TestTheLaunchInstallsNothingItWasNotAlreadyAskedFor(t *testing.T) {
	cases := []struct {
		name   string
		timer  *driftedTimer
		wanted bool
	}{
		{"nothing installed", &driftedTimer{drift: standing.WatchDrift{}}, true},
		{"already right", &driftedTimer{drift: standing.WatchDrift{Present: true}}, true},
		{"the row is off", &driftedTimer{drift: standing.WatchDrift{Present: true, Stale: true}}, false},
		{"cannot be read", &driftedTimer{err: errors.New("no")}, true},
	}
	for _, one := range cases {
		if line := repairBackgroundChecks(one.timer, one.wanted); line != "" {
			t.Fatalf("%s: said %q", one.name, line)
		}
		if one.timer.installs != 0 {
			t.Fatalf("%s: installed a timer anyway", one.name)
		}
	}
	if line := repairBackgroundChecks(nil, true); line != "" {
		t.Fatalf("a machine with no timer said %q", line)
	}
}

// AND A REPAIR THAT DID NOT TAKE SAYS SO IN THE LOG rather than claiming the
// checks are back.
func TestARepairThatFailedSaysSoInTheLog(t *testing.T) {
	timer := &driftedTimer{
		drift: standing.WatchDrift{Present: true, Stale: true},
		fail:  errors.New("launchctl bootstrap failed"),
	}
	line := repairBackgroundChecks(timer, true)
	if !strings.Contains(line, "could not put the background check back") {
		t.Fatalf("a failed repair said %q", line)
	}
}

// AND THE WHOLE THING OVER A REAL DEFINITION, with this machine's scheduler
// stood in for: a plist naming a program that is not there, repaired into one
// naming the program that is.
func TestTheRepairRewritesADefinitionThatNamesADeadPath(t *testing.T) {
	homeDir := t.TempDir()
	gone := filepath.Join(t.TempDir(), "codeaf-that-moved")
	if err := os.WriteFile(gone, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	stale, err := standing.NewWatch(standing.WatchOptions{
		Platform: "darwin", HomeDir: homeDir, Executable: gone, UID: 501, Runner: quietHost{},
	})
	if err != nil {
		t.Fatalf("NewWatch: %v", err)
	}
	if err := stale.Install(context.Background()); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if err := os.Remove(gone); err != nil {
		t.Fatalf("remove: %v", err)
	}

	here := filepath.Join(t.TempDir(), "codeaf")
	if err := os.WriteFile(here, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	live, err := standing.NewWatch(standing.WatchOptions{
		Platform: "darwin", HomeDir: homeDir, Executable: here, UID: 501, Runner: quietHost{},
	})
	if err != nil {
		t.Fatalf("NewWatch: %v", err)
	}
	if status, err := live.Status(); err != nil || status.Installed {
		t.Fatalf("a definition naming a dead program read as installed: %+v (%v)", status, err)
	}
	if line := repairBackgroundChecks(live, true); line == "" {
		t.Fatal("the launch left a timer pointing at nothing")
	}
	if status, err := live.Status(); err != nil || !status.Installed {
		t.Fatalf("the repair did not take: %+v (%v)", status, err)
	}
	// And a second launch over a definition that is now right does nothing.
	if line := repairBackgroundChecks(live, true); line != "" {
		t.Fatalf("a healthy timer was repaired anyway: %q", line)
	}
}

// quietHost is launchctl, stood in for.
type quietHost struct{}

func (quietHost) Run(context.Context, string, ...string) error { return nil }

// ── the accounts a firing inherits ──────────────────────────────────────────

// standingAccountsFixture points the state root and the profile at fresh temp
// directories and silences every key variable, so nothing below touches this
// machine's own accounts and no probe can buy a model call.
func standingAccountsFixture(t *testing.T) string {
	t.Helper()
	t.Setenv(home.EnvVar, t.TempDir())
	profile := t.TempDir()
	t.Setenv(config.ProfileDirEnv, profile)
	t.Setenv(config.APIKeyEnv, "")
	t.Setenv("OPENAI_API_KEY", "")
	return profile
}

// standingServicesItem is the smallest item whose probe reaches a belt tool:
// asking which accounts are connected. It is exactly the shape the existing
// 15-minute sync fires with, minus the cadence.
func standingServicesItem(t *testing.T) standing.Item {
	t.Helper()
	return standing.Item{
		Schema:    1,
		ID:        "test-services",
		Words:     "which accounts do I have",
		Workspace: t.TempDir(),
		When: standing.When{
			Kind:  standing.WhenProbe,
			Probe: standing.Probe{Tool: "services"},
		},
		Does:  standing.Action{Kind: standing.ActionSay, Say: "the accounts"},
		Rails: standing.Rails{PerRunUSD: 0.05, MaxPerDay: 3},
	}
}

// A FIRING'S BELT REACHES THE VERY MANAGER ITS CALLER HOLDS. The live process
// resolved one manager at the door and every conversation's belt reaches it; a
// firing ticked by that same window must too, or the two halves would keep
// separate caches and a token either refreshed would be a token the other still
// believed had not moved. The posture must hand back the caller's object, never
// a second one built over the same store.
func TestStandingPostureUsesTheCallersAccountsManager(t *testing.T) {
	profile := standingAccountsFixture(t)
	conns := v3Connect(profile)
	if conns == nil {
		t.Fatal("a fresh profile must still resolve an accounts manager")
	}
	settings, err := config.LoadKeyless()
	if err != nil {
		t.Fatal(err)
	}
	posture, models, err := v3StandingPosture(settings, conns)
	if err != nil {
		t.Fatal(err)
	}
	defer models.Close()
	if posture.Connect != conns {
		t.Fatal("the firing posture built or borrowed a second accounts manager instead of the caller's")
	}
}

// AND THROUGH THE FIRING'S OWN PROBE PATH THE ACCOUNTS TOOLS ARE ACTUALLY
// THERE. The ticker's Runner is what a pass calls after its judgment says yes,
// so a `services` probe that answers the accounts list is the whole behavior
// this fix restores: before it, v3StandingPosture left Connect empty, the hub
// was nil, and the same probe came back "Unknown tool: services".
func TestStandingFiringReachesTheConnectedAccountsTools(t *testing.T) {
	profile := standingAccountsFixture(t)
	store, err := standing.Open(home.Join("v3", "standing"))
	if err != nil {
		t.Fatal(err)
	}
	ticker, release, err := v3StandingTicker(store, v3Connect(profile))
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	out, err := ticker.Runner.Probe(context.Background(), standingServicesItem(t))
	if err != nil {
		t.Fatalf("the firing's probe failed: %v", err)
	}
	if strings.Contains(out, "Unknown tool") {
		t.Fatalf("a firing with a connected manager carried no accounts tool: %q", out)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("the services probe answered nothing")
	}
}

// AND A FIRING WITH NO MANAGER GETS NO MANAGER INVENTED FOR IT. Nil is the same
// absence the belt reads everywhere: no services tool, no use_service, and a
// probe that names one answered "Unknown tool" rather than a fabricated account
// list. This is the disconnected-profile half of the regression.
func TestStandingFiringInventsNoAccountsManagerWhenThereIsNone(t *testing.T) {
	standingAccountsFixture(t)
	settings, err := config.LoadKeyless()
	if err != nil {
		t.Fatal(err)
	}
	posture, models, err := v3StandingPosture(settings, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer models.Close()
	if posture.Connect != nil {
		t.Fatal("a firing with no accounts manager was handed one anyway")
	}
	store, err := standing.Open(home.Join("v3", "standing"))
	if err != nil {
		t.Fatal(err)
	}
	ticker, release, err := v3StandingTicker(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	out, err := ticker.Runner.Probe(context.Background(), standingServicesItem(t))
	if err != nil {
		t.Fatalf("the firing's probe failed: %v", err)
	}
	if !strings.Contains(out, "Unknown tool: services") {
		t.Fatalf("a firing with no accounts manager still reached accounts tools: %q", out)
	}
}
