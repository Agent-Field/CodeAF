package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/crewroute"
)

// A FRESH PROFILE STARTS ON AUTO. With HOME and CODEAF_HOME both empty
// directories and no profile override, every crew seat is routed — worker,
// planner and checker — nothing is pinned, the allowed rule is `all`, a task
// is held to $5, and there is no daily crew cap until one is set. Reading all
// of it writes nothing.
func TestAFreshProfileStartsOnAutoWithTheDefaultLimits(t *testing.T) {
	crewProfile(t) // clears the seat and key variables, installs a catalog
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv(ProfileDirEnv, "")
	dir := ProfileDir()
	if dir != "" {
		t.Fatalf("profile override leaked: %q", dir)
	}
	if pins := CrewPinsAt(dir); len(pins) != 0 {
		t.Fatalf("a fresh profile has pins: %v", pins)
	}
	if rule := CrewAllowedAt(dir).String(); rule != "all" {
		t.Fatalf("a fresh profile allows %q, want all", rule)
	}
	if limit := CrewTaskCapAt(dir); limit != 5 {
		t.Fatalf("a fresh profile's per-task limit is $%.2f, want $5", limit)
	}
	if daily := CrewCapAt(dir); daily != 0 {
		t.Fatalf("a fresh profile has a daily crew cap of $%.2f, want none", daily)
	}
	for _, seat := range crewroute.Seats {
		row := TierSeatAt(dir, CrewSeatTier(seat))
		if row.Source != SeatRouted {
			t.Errorf("%s reads %+v on a fresh profile, want auto (routed)", seat, row)
		}
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("CODEAF_HOME"), "config.json")); !os.IsNotExist(err) {
		t.Fatalf("reading a fresh profile wrote config.json: %v", err)
	}
}

// NO SEAT FALLS BACK TO A MODEL THIS BUILD CHOSE. With nothing the router can
// seat, a crew seat reads empty — the role ladder's floor is the model the
// person is talking to — and a task's crew is an error that says so, never a
// hard-coded default.
func TestNoCrewSeatFallsBackToABuiltInModel(t *testing.T) {
	dir := crewProfile(t)
	CrewCatalog = func() []catalog.Model { return nil }
	builtIn := map[string]bool{DefaultModel: true, DefaultReflexModel: true, DefaultLowModel: true, FreeChatModel: true}
	for _, seat := range crewroute.Seats {
		row := TierSeatAt(dir, CrewSeatTier(seat))
		if row.Model != "" || builtIn[row.Model] {
			t.Errorf("%s with nothing routable reads %q, want empty", seat, row.Model)
		}
	}
	seats, err := ResolveSeats(dir, SeatFlags{}, CrewAsk{Task: crewroute.Task{Text: fixTask}})
	if err == nil {
		t.Fatalf("a crew with nothing routable resolved: %+v", seats)
	}
	for _, seat := range []Seat{seats.Work, seats.Plan, seats.Check} {
		if builtIn[seat.Model] {
			t.Errorf("%s fell back to the built-in %s", seat.Role, seat.Model)
		}
	}
}
