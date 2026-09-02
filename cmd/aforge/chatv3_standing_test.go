package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// THE STORE IS UNDER THE STATE ROOT AND NOWHERE ELSE, so AFORGE_HOME moves the
// ambient side with everything else it moves. A second spelling of this path
// would be two stores with half a person's reminders in each.
func TestStandingLivesUnderTheStateRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	t.Setenv("AFORGE_HOME", root)
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
	t.Setenv("AFORGE_HOME", root)
	seam := v3Standing(t.TempDir())
	if seam == nil || seam.Store == nil {
		t.Fatal("the door built no standing seam")
	}
	if seam.Store.Root() != v3StandingRoot() {
		t.Fatalf("the seam's store is at %q, want %q", seam.Store.Root(), v3StandingRoot())
	}
	// The daily rail is the person's own daily budget row and never a second
	// number invented for this.
	if seam.DailyRailUSD != v3StandingDailyRail(t.TempDir()) {
		t.Fatalf("the seam quotes %v as the daily rail", seam.DailyRailUSD)
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

func (d *driftedTimer) Drift() (standing.WatchDrift, error) { return d.drift, d.err }

func (d *driftedTimer) Install(context.Context) error {
	d.installs++
	return d.fail
}

// A TIMER POINTING AT A PROGRAM THAT MOVED RUNS NOTHING, and nothing on screen
// could say so — the honest reading of it is `off`, which is what /settings
// would show. So the launch puts it back, quietly, and says one line in the
// standing log.
func TestTheLaunchPutsABackgroundCheckBackWhenItsProgramMoved(t *testing.T) {
	timer := &driftedTimer{drift: standing.WatchDrift{
		Present: true, Stale: true, Gone: true, Executable: "/old/bin/aforge",
	}}
	line := repairBackgroundChecks(timer, true)
	if timer.installs != 1 {
		t.Fatalf("a drifted timer was installed %d times", timer.installs)
	}
	if !strings.Contains(line, "/old/bin/aforge") || !strings.Contains(line, "installed it again") {
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
	gone := filepath.Join(t.TempDir(), "aforge-that-moved")
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

	here := filepath.Join(t.TempDir(), "aforge")
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
